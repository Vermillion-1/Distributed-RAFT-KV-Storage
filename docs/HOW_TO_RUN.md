# How to Run: Deployment and Usage Guide

This document covers how to provision, deploy, and interact with the Raft KV cluster — both locally and on GCP.

---

## Prerequisites

Before starting, ensure you have:

1. **Go 1.21+** — for building and cross-compiling binaries
2. **gcloud CLI** — authenticated and pointing at your project:
   ```bash
   gcloud auth login
   gcloud config set project [YOUR_PROJECT_ID]
   ```
3. **SSH key** — ensure your `ssh-agent` is running for automated VM access

---

## Option A: Local 3-Node Cluster

The fastest way to run the system for development or testing:

```bash
go build ./...
./start_cluster.sh
```

This starts 3 `kv-store` processes on `localhost` ports 50051–50053 (Raft on 12001–12003).

Interact with the cluster:

```bash
# Set a key (client auto-redirects to leader if needed)
./kv-client -addrs=localhost:50051,localhost:50052,localhost:50053 -cmd=set -key=hello -val=world

# Get a key
./kv-client -addrs=localhost:50051,localhost:50052,localhost:50053 -cmd=get -key=hello

# Get via follower read-index (v1.3, linearizable off-leader)
./kv-client -addrs=localhost:50051,localhost:50052,localhost:50053 -cmd=get -key=hello -follower-read

# Check node health
./kv-client -addrs=localhost:50051,localhost:50052,localhost:50053 -cmd=health
```

---

## Option B: GCP Deployment (Full)

### Step 1: Deploy

```bash
export GCP_PROJECT=[YOUR_PROJECT_ID]
./dynamic_deploy.sh 3        # 3-node cluster (default)
# or
./dynamic_deploy.sh 5        # 5-node cluster
```

The script:
1. Provisions N GCE VMs across `us-central1-a` and `us-central1-c`
2. Cross-compiles `kv-store` and `node-agent` for `linux/amd64`
3. Uploads binaries and bootstraps the Raft quorum
4. Polls `/api/cluster` until a leader is elected
5. Prints the external IPs and Chaos Dashboard URL

Total time: ~2 minutes for a 3-node cluster.

### Step 2: Interact with the Cluster

Replace `NODE0_IP`, `NODE1_IP`, `NODE2_IP` with the external IPs from the deploy output:

```bash
# Set a key
./kv-client -addrs=NODE0_IP:50051,NODE1_IP:50052,NODE2_IP:50053 -cmd=set -key=hello -val=world

# Get a key
./kv-client -addrs=NODE0_IP:50051,NODE1_IP:50052,NODE2_IP:50053 -cmd=get -key=hello

# Follower read (v1.3) — linearizable, served locally by a follower
./kv-client -addrs=NODE0_IP:50051,NODE1_IP:50052,NODE2_IP:50053 -cmd=get -key=hello -follower-read
```

The client automatically redirects to the leader if it contacts a follower first. All addresses are tried in order on connection failure.

### Step 3: Chaos Dashboard

Open `http://[NODE0_EXTERNAL_IP]:8080` in your browser. The dashboard shows:

- Current leader, Raft term, and applied log index for each node
- Controls to kill, restart, partition, or apply latency to individual nodes
- KV store read/write interface

### Step 4: Run the Verification Suite

SSH into `node0` and run the phase scripts:

```bash
gcloud compute ssh node0 --zone=us-central1-a

# Inside node0:
bash ~/GCP_verify_phase1.sh    # Liveness (leader failover, MTTR)
bash ~/GCP_verify_phase2.sh    # Network partitions (CP safety)
bash ~/GCP_verify_phase3.sh    # Write latency under delay
bash ~/GCP_verify_phase4.sh    # Durability (cluster wipe, snapshots)
bash ~/GCP_verify_phase5.sh    # Idempotency (exactly-once writes)
bash ~/GCP_verify_phase6.sh    # Kernel chaos (iptables bidirectional partition)
```

Each script prints PASS/FAIL per test with diagnostic output. Expected result: **37/37**.

### Step 5: Teardown (Important — stops GCP billing)

```bash
./teardown.sh
```

This deletes all provisioned VMs for the configured cluster size. If you provisioned additional nodes manually, delete them via the GCP Console.

---

## kv-client Flag Reference

| Flag | Default | Description |
|------|---------|-------------|
| `-addrs` | — | Comma-separated `host:port` list of cluster nodes |
| `-cmd` | `get` | Command: `get`, `set`, `delete`, `health` |
| `-key` | — | Key to operate on |
| `-val` | — | Value to set (for `-cmd=set`) |
| `-follower-read` | false | Serve reads via follower read-index (v1.3, linearizable) |
| `-client-id` | auto UUID | Explicit client ID for idempotency testing |
| `-seq-num` | auto | Explicit sequence number for idempotency testing (minimum: 1) |
| `-addr` | — | Single address (deprecated — use `-addrs`) |
