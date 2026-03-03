# 🚀 Quickstart — Clone & Run

> **Prerequisites:** Go 1.21+, `git`, `curl`, `python3`, `jq` (optional but helpful for manual inspection)

---

## 1. Clone the Repository

```bash
git clone <your-repo-url>
cd store
```

---

## 2. Build All Binaries

> **macOS note:** The Go build cache lives in a sandboxed location that can cause permission errors on some systems. Use the workaround below.

```bash
# Build everything (macOS-safe)
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-store .
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-client  ./cmd/client/
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-chaos   ./cmd/chaos/
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-dashboard ./cmd/dashboard/
```

On Linux (no sandbox issue):
```bash
go build -o kv-store .
go build -o kv-client  ./cmd/client/
go build -o kv-chaos   ./cmd/chaos/
go build -o kv-dashboard ./cmd/dashboard/
```

---

## 3. Option A — Run with the Chaos Dashboard (Recommended)

The dashboard is the easiest way to start the cluster. It spawns and manages all node processes for you.

```bash
# Start a 3-node cluster + dashboard on port 8080
./kv-dashboard -nodes=3 -port=8080
```

Open **http://localhost:8080** in your browser.

To run a larger cluster:
```bash
./kv-dashboard -nodes=5 -port=8080   # 5-node (f=2 fault tolerance)
./kv-dashboard -nodes=11 -port=8080  # 11-node (f=5 fault tolerance)
```

> The cluster takes ~5–10 seconds to elect a leader on first start. Watch the Event Log in the dashboard.

---

## 4. Option B — Run Manually (Without Dashboard)

If you prefer to manage nodes yourself:

```bash
# Terminal 1: Bootstrap node0 (the first node forms the cluster)
./kv-store -id=node0 -raft=127.0.0.1:12000 -grpc=127.0.0.1:50051 -data=/tmp/raft-kv/node0

# Terminal 2: node1 joins node0
./kv-store -id=node1 -raft=127.0.0.1:12001 -grpc=127.0.0.1:50052 -data=/tmp/raft-kv/node1 \
  -join=127.0.0.1:50051

# Terminal 3: node2 joins node0
./kv-store -id=node2 -raft=127.0.0.1:12002 -grpc=127.0.0.1:50053 -data=/tmp/raft-kv/node2 \
  -join=127.0.0.1:50051
```

Or use the convenience script:
```bash
bash start_cluster.sh
```

---

## 5. Verify the Cluster is Running

```bash
# Via dashboard API
curl -s http://localhost:8080/api/cluster | python3 -m json.tool

# Expected output (3-node):
# {
#   "nodes": [
#     { "config": {"id": "node0", ...}, "state": "Leader", "alive": true, "applied_index": 7 },
#     { "config": {"id": "node1", ...}, "state": "Follower", "alive": true, "applied_index": 7 },
#     { "config": {"id": "node2", ...}, "state": "Follower", "alive": true, "applied_index": 7 }
#   ]
# }
```

---

## 6. Write and Read Data

```bash
# Write a key (goes to leader, auto-redirects if sent to follower)
./kv-client -addr 127.0.0.1:50051 -cmd set -key hello -val world

# Read a key (consistent read from leader)
./kv-client -addr 127.0.0.1:50051 -cmd get -key hello

# Check node health
./kv-client -addr 127.0.0.1:50051 -cmd health
```

---

## 7. Stop the Cluster

```bash
# Stop everything started by kv-dashboard
pkill -f kv-dashboard; pkill -f kv-store; pkill -f kv-chaos

# Clean up Raft data (fresh start next time)
rm -rf /tmp/raft-kv
```

---

## Port Reference

| Binary | Port | Purpose |
|---|---|---|
| `kv-store` node N | `:12000 + N` | Raft inter-node consensus (TCP) |
| `kv-store` node N | `:50051 + N` | gRPC client API |
| `kv-chaos` proxy | `:22000+` | Chaos proxy (client gRPC only) |
| `kv-dashboard` | `:8080` (configurable) | HTTP dashboard + API |
