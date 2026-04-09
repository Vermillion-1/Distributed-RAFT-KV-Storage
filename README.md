# Fault-Tolerant Distributed Key-Value Store (Raft)

**Course:** CMPT 756 — Fault-Tolerant Distributed Systems
**Team:** Group 15 — Aarish · Ankith · Dhwani · Ankush
**Status:** v1.3 · 37/37 tests passing on GCP (April 4, 2026)
**Language:** Go · gRPC · HashiCorp Raft v1.7.3 · BoltDB

---

A **CP key-value store** built on the Raft consensus algorithm, deployed and verified on Google Cloud Platform. The system achieves linearizable reads, exactly-once writes, and sub-1.5s leader failover across 3-node and 5-node GCP clusters.

**CAP position:** Consistency + Partition Tolerance. Under a network partition, the minority partition sacrifices availability rather than serve stale data. This is an intentional design choice — not a limitation.

---

## Architecture

```
┌─────────────── GCP VM (×3 or ×5) ───────────────┐
│  kv-store   (Raft Replica)                        │
│  ├── Raft TCP  :12000–12004  ← consensus          │
│  └── gRPC      :50051–50055  ← client API         │
│                                                    │
│  node-agent (Sidecar)                             │
│  └── HTTP      :9000         ← fault injection    │
└───────────────────────────────────────────────────┘

kv-client    ── smart CLI client with leader auto-redirect
kv-dashboard ── web UI for chaos testing and cluster health
```

Each VM runs exactly two processes. The sidecar agent operates at the OS level (signals, iptables, tc netem) — independently of application logic — so fault injection cannot be accidentally bypassed by the system under test.

---

## Key Features

| Feature | Description | Evidence |
|---------|-------------|----------|
| **Follower Reads (v1.3)** | Read-Index protocol: follower asks leader for `commit_index` N, waits until `appliedIndex ≥ N`, serves read locally. Linearizable reads without routing every GET to leader. | `GCP_verify_phase7.sh` 6/6 |
| **Exactly-Once Writes** | Per-client `(client_id, seq_num)` dedup table in FSM. Retried write after leader change is silently dropped — value never applied twice. | P5 I2/I3 PASS |
| **Linearizable Reads (Leader)** | `VerifyLeader()` heartbeat before every GET. Deposed leader cannot serve stale data. Minority partition → reads blocked (CP enforced). | P2c, L3b, N6c PASS |
| **Deployment Engine** | Provisions N GCP VMs, cross-compiles `linux/amd64` on macOS, SSH retry loop, bootstraps full N-node quorum in < 2 minutes. | 37/37 GCP confirmed |
| **Sidecar Fault Injection** | Bidirectional `iptables` (INPUT+OUTPUT DROP) per Raft port. NIC auto-detect via `ip route get 8.8.8.8`. Kill, pause, partition, netem, restart. | N6a–N6c PASS |
| **Aggressive Snapshotting** | `SnapshotThreshold=10` entries → `InstallSnapshot` RPC teleports full FSM state to any lagging replica on restart. | D3: 30 missed → 100% recovery |

---

## Test Results

**37/37** on GCP 6-phase suite · **+6/6** Phase 7 (follower reads) · **43 tests total**

| Phase | Focus | Tests |
|-------|-------|-------|
| P1 — Liveness | Leader failover, MTTR (~1.25s) | L1, L3b, L1c |
| P2 — Partitions | CP safety, minority unavailability | P2a, P2c |
| P3 — Latency | Write throughput under follower/leader delay | R1, R2 |
| P4 — Durability | Cluster wipe, dirty crash, snapshot catch-up | D1, D2, D3 |
| P5 — Idempotency | Exactly-once semantics across failover | I2, I3 |
| P6 — Kernel Chaos | iptables bidirectional partition, 6 assertions | N6a–N6f |
| P7 — Follower Reads | Read-Index linearizability off-leader (v1.3) | T1–T6 |

Key performance numbers:

| Metric | Value |
|--------|-------|
| Baseline throughput | 62.9 ops/sec (15.9 ms/op) |
| Slow-follower throughput | 61.3 ops/sec (−2.5%, quorum bypass confirmed) |
| Leader-fault throughput | 0.6 ops/sec (−99%, election fires — expected) |
| MTTR (SIGKILL or iptables) | ~1.25s |
| Key recovery ratio | 100% |

---

## Quick Start

### Local 3-node cluster

```bash
# Requires Go 1.21+
go build ./...
bash local_deploy.sh
# In a separate terminal:
./kv-client -addrs=localhost:50051,localhost:50052,localhost:50053 -cmd=set -key=foo -val=bar
./kv-client -addrs=localhost:50051,localhost:50052,localhost:50053 -cmd=get -key=foo
```

### GCP deployment (full)

```bash
# Prerequisites: gcloud CLI authenticated, project set
./dynamic_deploy.sh          # provisions VMs, deploys, bootstraps cluster (~2 min)

# Run the full 6-phase test suite (from node0 on GCP)
bash GCP_verify_phase1.sh
bash GCP_verify_phase2.sh
bash GCP_verify_phase3.sh
bash GCP_verify_phase4.sh
bash GCP_verify_phase5.sh
bash GCP_verify_phase6.sh

# Teardown (important — stops GCP billing)
./teardown.sh
```

---

## Project Structure

```
.
├── main.go                    # Entry point
├── server/
│   ├── node.go                # Raft node: leader election, log replication, FSM apply
│   └── fsm.go                 # Finite State Machine: KV store, idempotency table, snapshots
├── cmd/
│   ├── client/                # kv-client CLI
│   ├── agent/                 # node-agent sidecar (fault injection HTTP API)
│   └── dashboard/             # kv-dashboard web UI
├── proto/kv.proto             # gRPC service definitions
├── GCP_verify_phase[1-7].sh   # Automated test phases (43 tests total)
├── dynamic_deploy.sh          # GCP cluster provisioning
├── local_deploy.sh            # Local 3-node cluster launcher
├── submission_docs/
│   ├── REPORT.md              # Full technical report (955 lines)
│   └── 756 PPT v2-proto.pptx  # Presentation slides
└── docs/
    ├── ARCHITECTURE.md        # Architecture defense
    └── HOW_TO_RUN.md          # Detailed setup guide
```

---

## Documentation

- **[Full Technical Report](submission_docs/REPORT.md)** — Design, implementation, bugs, performance analysis, 37/37 results
- **[Detailed Walkthrough](detailed_walkthrough.md)** — Step-by-step guide through every system component and design decision
- **[Architecture Overview](docs/ARCHITECTURE.md)** — Core principles and component design
- **[How to Run](docs/HOW_TO_RUN.md)** — Detailed setup and deployment guide
