# CHANGELOG v1.1 — March 29–31, 2026
**Branch:** `stable-gcp`
**Sprint Goal:** Finalize GCP verification, add kernel-level chaos testing, dashboard KV ops, and election tracking.

---

## Part A — Core v1.1 Achievements (March 29)

### 1. Snapshot Recovery Bug Fixed
Lowered `SnapshotInterval` and `SnapshotThreshold` in `server/node.go` to force aggressive log compaction. This validated **Phase 4 (Durability)** by triggering `InstallSnapshot` RPCs during recovery.

### 2. Client Redirect Loop Fixed
Updated `dynamic_deploy.sh` and `deploy.sh` to bind gRPC services to explicit **GCP Internal IPs** instead of `0.0.0.0`. This allowed follower nodes to accurately resolve the Leader's address for client-side redirection.

### 3. 100% Verification Pass (4 Phases)
Successfully executed and passed all 4 phases of the GCP Verification Suite: Liveness, Partitions, Latency, and Durability.

### 4. Metric Visualization
Created `analysis/generate_graphs.py` to produce academic-grade charts for Latency Quorum, Durability, and MTTR. Authored `analysis/results_analysis.md` interpreting the metric data from a distributed systems perspective.

### 5. Documentation Overhaul
Created a new `docs/` suite: `HOW_TO_RUN.md`, `ARCHITECTURE.md`, `DELIVERABLES.md`, `FILE_MANIFEST.md`.

### 6. Technical Fixes (March 29)
- **`dynamic_deploy.sh`**: Added `shopt -s nullglob` to `chmod` commands to prevent script crashes when `.sh` files are missing on a target node.
- **`GCP_verify_phase3.sh`**: Silenced stdout/stderr in the `time_writes` loop to prevent output pollution in bash arithmetic variables.
- **`node-agent`**: Verified the sidecar's ability to reliably signal child processes under heavy load.

### 7. Version Control
Initialized Git and pushed the submission-grade codebase to the `stable-gcp` branch on GitHub.

---

## Part B — Feature Sessions (March 30–31)

### Session 1: Phase 5 & 6 Test Scripts

**`GCP_verify_phase5.sh`** (NEW — Idempotency)
- I1: Follower Redirect — verify client receives leader redirect from follower
- I2: Idempotent Set — same `clientId+seq` twice; verify FSM deduplication
- I3: Idempotent Delete — same DELETE twice; verify idempotent behavior
- I4: Client Failover — kill leader; verify client writes to new leader

**`GCP_verify_phase6.sh`** (NEW — Kernel Chaos)
- N1: iptables Partition — drop Raft traffic via kernel-level iptables
- N2: netem on Follower — 500ms delay; verify writes still fast (quorum bypass)
- N3: netem on Leader — 500ms delay; verify client latency increases
- N4: netem + Packet Loss — 30% loss + 200ms delay
- N5: Dashboard access during chaos — UI available via other nodes

Auto-cleanup: all netem/iptables rules removed after 10s timeout.

**Testing results:** Phase 5: 7/7 ✅ | Phase 6: 7/7 ✅

---

### Session 2: Client Idempotency Testing Support

**`cmd/client/main.go`**

Added `-client-id` and `-seq-num` flags for explicit idempotency testing:
```go
explicitClientID := flag.String("client-id", "", "Explicit client ID (default: auto-generated UUID)")
explicitSeqNum   := flag.Uint64("seq-num", 0, "Explicit sequence number (default: auto-increment)")
```

Usage:
```bash
# Send same write twice — FSM should deduplicate; value stays "v1"
./kv-client -addr node0:50051 -cmd set -key test -val v1 -client-id foo -seq-num 1
./kv-client -addr node0:50051 -cmd set -key test -val v2 -client-id foo -seq-num 1
```

---

### Session 3: Agent Netem Endpoints

**`cmd/agent/main.go`**

Added `netIface` global (auto-detected via `-iface` flag, defaults to `eth0`).

New endpoints:
- **`POST /netem?delay=<ms>&jitter=<ms>&loss=<%>`** — applies `tc qdisc add dev <iface> root netem`
- **`POST /unnetem`** — removes `tc qdisc` rules (non-fatal if none exist)

Rationale: `kv-chaos` proxy only affects client-side connections. `tc/netem` affects ALL traffic on the interface including Raft heartbeats — enabling true cluster-level latency testing.

---

### Session 4: Dashboard Chaos Endpoints

**`cmd/dashboard/main.go`**

New functions and HTTP endpoints:

| Function | Endpoint | Action |
|----------|----------|--------|
| `PartitionNode()` | `POST /api/partition/{nodeID}` | Routes to agent `/partition` (iptables DROP) |
| `UnpartitionNode()` | `POST /api/unpartition/{nodeID}` | Routes to agent `/unpartition` |
| `ApplyNetem()` | `POST /api/chaos/netem/{nodeID}?delay=&loss=&jitter=` | Routes to agent `/netem` |
| `RemoveNetem()` | `POST /api/chaos/unnetem/{nodeID}` | Routes to agent `/unnetem` |

All GCP-mode only (require `-agent-addrs` flag).

**Why iptables over SIGSTOP for partition tests:**
- SIGSTOP freezes the entire process — too severe
- iptables keeps the process alive but drops Raft TCP — correct partition semantics
- Tests whether Raft timeout-based failure detection works, not just whether a dead process gets detected

---

### Session 5: KV Operations via Dashboard

**`cmd/dashboard/main.go`**

New direct KV functions + endpoints:

| Function | Endpoint | Action |
|----------|----------|--------|
| `DirectKVSet()` | `POST /api/kv/set?key=&val=&addr=` | gRPC SET to target address |
| `DirectKVGet()` | `POST /api/kv/get?key=&addr=` | gRPC GET to target address |
| `DirectKVDelete()` | `POST /api/kv/delete?key=&addr=` | gRPC DELETE to target address |

Follows one leader redirect if the target is a follower.

---

### Session 6: Election Term Tracking

**`proto/kv.proto`** — Added `uint64 term = 6` to `HealthResponse`.

**`server/node.go`** — Extracts term from Raft stats and includes in `HealthResponse`:
```go
var term uint64
fmt.Sscanf(stats["term"], "%d", &term)
return &pb.HealthResponse{ ..., Term: term }
```

**`cmd/dashboard/main.go`** — Added `Term uint64 \`json:"term"\`` to `NodeState`; populated from health poll.

Rationale: Term number increments on each election — visual proof that leader elections are occurring.

---

### Session 7: Dashboard UI Enhancements

**`cmd/dashboard/index.html`**

1. **"Reset ALL Chaos" button** — single click:
   - Stops kv-chaos proxy (`/api/chaos/stop/{id}`)
   - Removes netem rules (`/api/chaos/unnetem/{id}`)
   - Heals iptables partition (`/api/unpartition/{id}`)
   - Resumes paused nodes (`/api/resume/{id}`)

2. **KV Operations Panel** — SET/GET/DELETE inputs with result display; calls `/api/kv/*` endpoints.

3. **Term Display** — changed from "N peers" to "Term: X" in cluster metrics bar.

---

### Session 8: Compilation & Deployment

Cross-compiled all binaries for `linux/amd64`:
```bash
GOOS=linux GOARCH=amd64 go build -o kv-store .
GOOS=linux GOARCH=amd64 go build -o kv-client ./cmd/client/
GOOS=linux GOARCH=amd64 go build -o kv-dashboard ./cmd/dashboard/
GOOS=linux GOARCH=amd64 go build -o kv-chaos ./cmd/chaos/
GOOS=linux GOARCH=amd64 go build -o node-agent ./cmd/agent/
```
Regenerated protobuf after adding `term` field to `HealthResponse`.

---

## Files Modified in v1.1

| File | Type | Session |
|------|------|---------|
| `GCP_verify_phase5.sh` | NEW | 1 |
| `GCP_verify_phase6.sh` | NEW | 1 |
| `cmd/client/main.go` | MODIFIED | 2 |
| `cmd/agent/main.go` | MODIFIED | 3 |
| `cmd/dashboard/main.go` | MODIFIED | 4, 5, 6 |
| `proto/kv.proto` | MODIFIED | 6 |
| `server/node.go` | MODIFIED | 6 |
| `cmd/dashboard/index.html` | MODIFIED | 7 |
| `analysis/generate_graphs.py` | NEW | A |
| `analysis/results_analysis.md` | NEW | A |
| `docs/` suite | NEW | A |
| `dynamic_deploy.sh`, `deploy.sh` | MODIFIED | A |
