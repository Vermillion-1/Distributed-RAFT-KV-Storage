# Raft KV Store — Chaos Testing Dashboard

A fault-tolerant, distributed key-value store built on [HashiCorp Raft](https://github.com/hashicorp/raft), with a web-based Chaos Testing Dashboard for live failure injection and real-time cluster observation.

---

## Documentation

| # | Document | Contents |
|---|---|---|
| 1 | [Quickstart](docs/1_QUICKSTART.md) | Clone, build, and run the cluster |
| 2 | [Architecture](docs/2_ARCHITECTURE.md) | System design, write/read paths, two-port layout |
| 3 | [Guarantees](docs/3_GUARANTEES.md) | Consistency, MTTR, quorum safety, partition safety, durability |
| 4 | [Tests & Results](docs/4_TESTS.md) | All 4 test phases with results tables |
| 5 | [Code Overview](docs/5_CODE.md) | Per-file deep dives (node, FSM, dashboard, chaos proxy, frontend) |

---

## Quick Start

```bash
# Build
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-dashboard ./cmd/dashboard/

# Run 3-node cluster
./kv-dashboard -nodes=3 -port=8080

# Open http://localhost:8080
```

---

## What It Does

- **Distributed KV Store** — `Get`, `Set`, `Delete` with strong consistency via Raft consensus
- **Fault Tolerance** — tolerates `⌊N/2⌋` simultaneous node failures
- **Chaos Dashboard** — kill nodes, inject network partitions, add latency, drop packets
- **Live Topology** — canvas visualization of cluster state (Leader/Follower/Dead)
- **Automated Tests** — `verify.sh` + `verify_phase2.sh` scripts with pass/fail assertions

---

## Proven Guarantees

| Guarantee | Result |
|---|---|
| MTTR after leader crash | **~1 second** (3-node and 11-node) |
| Quorum boundary | **Exact `⌊N/2⌋+1`** (verified at every cluster size) |
| No split-brain | ✅ Proven via SIGSTOP partition tests |
| Write durability | ✅ BoltDB persistence, survives restart |
| Log catch-up | ✅ Full replication sync after partition heal |

---

## Failure Model Coverage

| Failure | Mechanism | Status |
|---|---|---|
| Crash / Fail-stop | `SIGKILL` → `/api/kill` | ✅ Implemented |
| Network Partition | `SIGSTOP` → `/api/pause` | ✅ Implemented |
| Receive Omission | chaos proxy drop rate | ✅ Implemented |
| Send Omission / Latency | chaos proxy delay | ✅ Implemented |
| Slow node (resource) | Phase 3 tests | 🔄 Pending |
| Durability (total wipe) | Phase 4 tests | 🔄 Pending |

---

## Project Structure

```
store/
├── main.go              # kv-store node entrypoint
├── server/
│   ├── node.go          # Raft setup + gRPC handlers
│   └── fsm.go           # Key-value FSM + snapshots
├── proto/               # Protobuf service definitions
├── cmd/
│   ├── client/          # kv-client CLI tool
│   ├── chaos/           # kv-chaos TCP fault proxy
│   └── dashboard/       # Dashboard HTTP server + frontend
├── docs/                # All documentation (see table above)
├── verify.sh            # Phase 1 tests
├── verify_phase2.sh     # Phase 2 tests (SIGSTOP partitions)
└── run_chaos_test.sh    # Integration test suite
```
