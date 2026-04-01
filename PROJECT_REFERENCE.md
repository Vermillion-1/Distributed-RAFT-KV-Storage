# Project Reference: Distributed Raft KV Storage — v1.1 Beta
> Internal knowledge dump for Claude and team. Last updated: 2026-03-31.

---

## Table of Contents
1. [What This Project Is](#1-what-this-project-is)
2. [Repository Structure](#2-repository-structure)
3. [System Architecture](#3-system-architecture)
4. [Component Deep Dives](#4-component-deep-dives)
5. [Configuration & Tunables](#5-configuration--tunables)
6. [How to Run (Local)](#6-how-to-run-local)
7. [Known Bugs & Fragile Points](#7-known-bugs--fragile-points)
8. [Testing Plan Summary](#8-testing-plan-summary)
9. [GCP Deployment](#9-gcp-deployment)
10. [Professor & Course Context](#10-professor--course-context)
11. [Where We Stand vs. Expectations](#11-where-we-stand-vs-expectations)
12. [Directions for Improvement](#12-directions-for-improvement)

---

## 1. What This Project Is

**Team Pluto** — CMPT 756 (Distributed Systems and the Cloud), SFU.

A from-scratch implementation of a **fault-tolerant, strongly consistent, distributed key-value store** using the **Raft consensus algorithm**, deployed on Google Cloud Platform (GCP). This is essentially a student-scale version of **etcd** (the backing store of Kubernetes) or **rqlite**, built for learning purposes.

**Tech stack:** Go · gRPC/Protobuf · HashiCorp Raft v1.7.3 · BoltDB · HTML/CSS/JS dashboard · GCP Compute Engine VMs

**Project Title (Option 3):** *Implement a fault-tolerant distributed system*

**Problem Statement summary:** Build a fault-tolerant key-value store with Raft consensus covering crash failures, network partitions, message omission/latency, and storage failures. Validate with systematic chaos testing.

**CAP Theorem classification:** This is a **CP system** — it prioritizes Consistency and Partition Tolerance over Availability. When a partition isolates the minority side, it becomes unavailable rather than serving stale data.

---

## 2. Repository Structure

```
Distributed-RAFT-KV-Storage-prototype/
├── main.go                     # kv-store entry point (per-node binary)
├── go.mod / go.sum             # Go modules
├── proto/
│   ├── kv.proto                # gRPC service + message definitions
│   ├── kv.pb.go                # generated protobuf code
│   └── kv_grpc.pb.go           # generated gRPC code
├── server/
│   ├── node.go                 # Raft node: setup, gRPC handlers, bootstrap
│   ├── fsm.go                  # Finite State Machine: KV store + idempotency
│   └── fsm_test.go             # 16 unit tests for FSM (all pass)
├── cmd/
│   ├── client/main.go          # kv-client: CLI for SET/GET/DELETE/HEALTH
│   ├── dashboard/
│   │   ├── main.go             # kv-dashboard: HTTP server + cluster manager
│   │   └── index.html          # Single-page dashboard UI (~1000 lines)
│   ├── chaos/main.go           # kv-chaos: TCP drop/delay proxy
│   └── agent/main.go           # node-agent: sidecar for GCP remote control
├── bin/                        # pre-built Linux/AMD64 binaries for GCP
├── analysis/
│   ├── generate_graphs.py      # produces academic metric charts
│   └── results_analysis.md     # quantitative results interpretation
├── docs/                       # HOW_TO_RUN, ARCHITECTURE, DELIVERABLES, FILE_MANIFEST
├── start_cluster.sh            # RETIRED local cluster launcher
├── run_chaos_test.sh           # local chaos test suite (5 tests)
├── deploy.sh / dynamic_deploy.sh  # GCP deployment scripts
├── GCP_verify_phase*.sh        # Phase 1-6 GCP verification scripts
└── problem_statement.md        # Full course context + prof feedback
```

---

## 3. System Architecture

### The 4 Components (important: prof noted only 3 were described in progress report)

```
┌──────────────────────────────────────────────────────┐
│  Component 1: kv-client (CLI)                        │
│  - Connects to any node via gRPC                     │
│  - Smart failover: tries multiple addrs              │
│  - Auto-redirects to leader on follower response     │
│  - Idempotent: UUID client-id + atomic seq-num       │
└────────────────────┬─────────────────────────────────┘
                     │ gRPC (port 50051-50053)
┌──────────────┐     │     ┌──────────────┐     ┌──────────────┐
│  Component 2 │◄────┼────►│  Component 2 │◄───►│  Component 2 │
│  kv-store    │     │     │  kv-store    │     │  kv-store    │
│  node0       │◄────┼────►│  node1       │◄───►│  node2       │
│  Leader ★    │     │     │  Follower    │     │  Follower    │
│  :12000 Raft │◄────┼────►│  :12001 Raft │◄───►│  :12002 Raft │
│  :50051 gRPC │     │     │  :50052 gRPC │     │  :50053 gRPC │
└──────┬───────┘     │     └──────────────┘     └──────────────┘
       │             │           Raft TCP (AppendEntries, Heartbeats,
       │             │           InstallSnapshot — port 12000-12002)
┌──────▼───────┐     │
│  Component 3 │     │
│  kv-chaos    │     │     ┌──────────────────────────────────────┐
│  TCP proxy   │     │     │  Component 4: kv-dashboard           │
│  drop/delay  │     │     │  HTTP server :8080                   │
│  client-side │     └────►│  - Spawns/kills kv-store processes   │
└──────────────┘           │  - /api/cluster → live state         │
                           │  - /api/kv/set|get|delete            │
                           │  - /api/kill|restart|pause|resume    │
                           │  - /api/chaos/drop|delay|netem       │
                           │  - /api/partition|unpartition        │
                           │  Canvas UI with topology visualizer  │
                           └──────────────────────────────────────┘
```

### Data Flow: Write (SET)
```
Client → gRPC SET(key, val, clientId, seqNum)
  → Leader node receives
  → FSM checks isDuplicate(clientId, seqNum) — if duplicate: return success (idempotent)
  → Leader encodes command as JSON → Raft.Apply()
  → Raft sends AppendEntries RPC to 2 followers
  → Quorum (≥2 nodes) acks → entry committed
  → FSM.Apply() called on all 3 nodes → KV map updated
  → Leader responds Success=true to client
```

### Data Flow: Read (GET)
```
Client → gRPC GET(key)
  → Node checks if it's Leader
  → If follower: returns {Found:false, LeaderAddr: <leader grpc addr>}
  → Client follows redirect
  → Leader calls raft.VerifyLeader() — confirms still leader (linearizable)
  → Returns {Found: true/false, Value: val}
```

### Why 3 nodes?
- Minimum odd number for Raft quorum (majority = 2/3)
- Tolerates 1 node failure: cluster stays available
- 5 nodes would tolerate 2 failures but adds latency (3 acks needed vs 2)
- 3 nodes = optimal for student-scale demo and matches GCP free tier

---

## 4. Component Deep Dives

### 4.1 FSM (`server/fsm.go`)

The Finite State Machine is what makes this a *replicated* state machine, not just a single server.

```go
type KVStore struct {
    mu          sync.RWMutex
    m           map[string]string       // the actual KV data
    Peers       map[string]string       // raftAddr → grpcAddr (for redirects)
    lastApplied map[string]*clientEntry // clientID → {seqNum, lastSeen} (idempotency)
}
```

**Three operations in the FSM:**
- `set` — writes key=val, checks idempotency first
- `delete` — removes key, checks idempotency first
- `register` — maps a node's Raft addr to its gRPC addr (used for leader redirect)

**Idempotency logic:**
```
isDuplicate(clientID, seqNum):
  if clientID == "": return false  // backward compatible
  if exists and seqNum <= lastApplied[clientID]: return true  // skip
  else: apply and record seqNum
```

**Snapshot/Restore:** Serializes `m`, `Peers`, and `lastApplied` to JSON. On restore, `lastApplied` times are reset to `time.Now()` (not persisted, TTL resets). Idempotency is preserved because seqNums are restored.

**Stale client eviction:** Background goroutine runs every 1 minute, evicts clients not seen for 10 minutes. Prevents unbounded `lastApplied` map growth.

**Unit tests (all 16 pass):**
- Basic CRUD, overwrite, delete non-existent
- Idempotency: same seq, older seq, higher seq, different clients, no clientID
- Peer registration and overwrite
- Unknown command (no panic)
- Snapshot + restore (including idempotency state)
- 100-key concurrency stress

### 4.2 Node (`server/node.go`)

**Raft configuration (tuned for GCP ~1ms RTT):**
```go
HeartbeatTimeout  = 500ms
ElectionTimeout   = 750ms
CommitTimeout     = 100ms
LeaderLeaseTimeout = 400ms
SnapshotInterval  = 10s        // aggressive: forces snapshots in testing
SnapshotThreshold = 10 entries // after 10 log entries, take snapshot
TrailingLogs      = 10         // keep 10 logs after snapshot
```

**Storage:**
- `raft-log.bolt` — BoltDB for Raft log (durable)
- `raft-stable.bolt` — BoltDB for stable store (term, vote)
- File snapshot store with 2 retained snapshots

**Bootstrap logic:**
- Node 0: `Bootstrap()` → becomes single-node cluster → wins election → registers itself via FSM `register` command
- Nodes 1, 2: `joinCluster()` → gRPC Join RPC to node 0 → leader adds them as voters → registers their addrs

**Graceful shutdown:** If leader, calls `LeadershipTransfer()` before `Raft.Shutdown()` — minimizes cluster disruption.

**gRPC handlers:**
- `Get` — follower returns redirect; leader calls `VerifyLeader()` for linearizable read
- `Set` — follower returns redirect; leader encodes + applies to Raft
- `Delete` — same pattern as Set
- `Join` — idempotent: if node already exists with same ID+addr, returns success
- `Health` — returns state, leader addr, applied index, num peers, term

### 4.3 Client (`cmd/client/main.go`)

- Auto-generated UUID `clientID` per process instance
- Atomic `sequenceNo` counter (thread-safe for concurrent use)
- `smartRequestLoop`: tries each addr in round-robin, follows leader redirects, 3 retry attempts with 1s sleep between rounds
- `explicitClientID` / `explicitSeqNum` flags for idempotency testing
- **Edge case:** `-seq-num=0` is silently ignored (treated as auto-increment). Can't test seqNum=0 explicitly.

### 4.4 Dashboard (`cmd/dashboard/main.go` + `index.html`)

**Manager struct** — orchestrates everything:
- `nodes map[string]*nodeProcess` — tracks spawned child processes (local mode only)
- `proxies map[string]*ChaosProxy` — tracks kv-chaos child processes
- `agentAddrs map[string]string` — nodeID → agent IP:port (GCP mode)
- `events []Event` — ring buffer of last 500 chaos log events
- `freshStart bool` — controls whether StartAll() wipes data dir

**Dual mode:**
- **Local mode**: dashboard spawns kv-store child processes, can SIGSTOP/SIGKILL them
- **GCP mode** (`-agent-addrs` flag): routes lifecycle commands to remote node-agents via HTTP POST

**HTTP API surface:**
```
GET  /                          → serve index.html
GET  /api/cluster               → live JSON cluster state
POST /api/kill/{nodeID}
POST /api/restart/{nodeID}
POST /api/pause/{nodeID}        → SIGSTOP
POST /api/resume/{nodeID}       → SIGCONT
POST /api/chaos/drop/{nodeID}?rate=1.0
POST /api/chaos/delay/{nodeID}?ms=3000
POST /api/chaos/stop/{nodeID}
POST /api/chaos/netem/{nodeID}?delay=500&loss=30&jitter=10  (GCP only)
POST /api/chaos/unnetem/{nodeID}  (GCP only)
POST /api/partition/{nodeID}    → iptables DROP (GCP only)
POST /api/unpartition/{nodeID}  (GCP only)
POST /api/kv/set?key=&val=&addr=
GET  /api/kv/get?key=&addr=
POST /api/kv/delete?key=&addr=
GET  /api/logs/{nodeID}         → last 8KB of node log (local only)
POST /api/run-tests             → runs run_chaos_test.sh
```

**UI features:**
- Canvas topology: animated ring of nodes with color-coded state (Leader=gold, Follower=blue, Dead=red, Candidate=purple)
- Leader pulsing animation
- Metrics bar: Current Leader, Applied Index, Term, Nodes Alive/Total, MTTR
- Node selector dropdown for chaos ops
- Chaos event log (scrollable, color-coded)
- KV Operations panel: SET/GET/DELETE directly from browser (auto-discovers leader)
- "Reset ALL Chaos" button: stops proxy + removes netem + unpartitions + resumes
- Node log viewer

**Critical: HTML path bug** — Dashboard must be run from repo root:
```bash
# WRONG (will show blank page):
cd /tmp && ./kv-dashboard

# CORRECT:
cd /path/to/Distributed-RAFT-KV-Storage-prototype && /tmp/kv-dashboard
```
Because `index.html` is served from `os.Getwd() + "/cmd/dashboard/index.html"`.

### 4.5 node-agent (`cmd/agent/main.go`)

Sidecar process running on each GCP VM. Allows the dashboard to remotely control nodes via HTTP.

**Endpoints:**
- `POST /kill` — kills kv-store process
- `POST /pause` — SIGSTOP
- `POST /resume` — SIGCONT
- `POST /restart` — kills + restarts with same args
- `POST /partition` — iptables DROP on Raft port (kernel-level partition)
- `POST /unpartition` — removes iptables rules
- `POST /netem?delay=500&loss=30&jitter=10` — tc/netem latency injection
- `POST /unnetem` — removes tc/netem rules

**Key design decision:** `iptables` partition keeps the process running but drops Raft TCP — more realistic than SIGSTOP which freezes everything. `tc/netem` affects ALL traffic on the interface (including Raft heartbeats), unlike kv-chaos which only affects client-side gRPC traffic.

### 4.6 kv-chaos (`cmd/chaos/main.go`)

TCP proxy that sits between client and a kv-store node. Injects faults at the gRPC layer:
- `-drop=1.0` — drops 100% of connections (Receive Omission)
- `-delay=3000` — adds 3000ms delay to all connections (Send Omission)
- Combination: drop + delay

**Limitation:** Only affects client→node traffic. Raft heartbeats between nodes are unaffected.

---

## 5. Configuration & Tunables

| Parameter | Value | Location | Notes |
|-----------|-------|----------|-------|
| HeartbeatTimeout | 500ms | server/node.go:54 | Time before follower suspects leader dead |
| ElectionTimeout | 750ms | server/node.go:55 | Time before follower starts election |
| CommitTimeout | 100ms | server/node.go:56 | Max time to commit a log entry |
| LeaderLeaseTimeout | 400ms | server/node.go:57 | Must be < HeartbeatTimeout |
| SnapshotInterval | 10s | server/node.go:60 | How often Raft checks if snapshot needed |
| SnapshotThreshold | 10 | server/node.go:61 | Log entries before snapshot triggered |
| TrailingLogs | 10 | server/node.go:62 | Logs kept after snapshot |
| Client RPC timeout | 2s | cmd/client/main.go:157 | Circuit breaker for stale connections |
| Dashboard health poll | 500ms | cmd/dashboard/main.go:536 | Per-node health check timeout |
| Bootstrap wait | 5s | server/node.go:133 | Timeout for node0 to win election |
| Join retries | 10 | main.go:58 | With exponential backoff (max 30s each) |
| Client retries | 3 rounds | cmd/client/main.go:90 | With 1s between rounds |
| Client eviction TTL | 10min | server/fsm.go:38 | Stale idempotency entry eviction |
| Event ring buffer | 500 | cmd/dashboard/main.go:108 | Dashboard log history |

---

## 6. How to Run (Local)

### Prerequisites
```bash
go version  # need go 1.21+
```

### Build
```bash
cd Distributed-RAFT-KV-Storage-prototype/
go build -o /tmp/kv-store .
go build -o /tmp/kv-client ./cmd/client/
go build -o /tmp/kv-chaos ./cmd/chaos/
go build -o /tmp/kv-dashboard ./cmd/dashboard/
```

### Start cluster manually
```bash
# Clean up any stale state
killall kv-store kv-chaos 2>/dev/null
lsof -ti:50051,50052,50053,12000,12001,12002 | xargs kill -9 2>/dev/null
rm -rf /tmp/raft-kv/

# Node 0 (bootstrap leader)
/tmp/kv-store -id=node0 -raft=127.0.0.1:12000 -grpc=127.0.0.1:50051 -data=/tmp/raft-kv/node0 > /tmp/raft-kv/node0.log 2>&1 &
sleep 2

# Nodes 1 and 2 (join)
/tmp/kv-store -id=node1 -raft=127.0.0.1:12001 -grpc=127.0.0.1:50052 -data=/tmp/raft-kv/node1 -join=127.0.0.1:50051 > /tmp/raft-kv/node1.log 2>&1 &
/tmp/kv-store -id=node2 -raft=127.0.0.1:12002 -grpc=127.0.0.1:50053 -data=/tmp/raft-kv/node2 -join=127.0.0.1:50051 > /tmp/raft-kv/node2.log 2>&1 &
sleep 3
```

### Start via dashboard (recommended for demo)
```bash
# MUST run from repo root
cd /path/to/Distributed-RAFT-KV-Storage-prototype
/tmp/kv-dashboard -nodes=3 -port=8080
# Open http://localhost:8080
```

### Basic client usage
```bash
/tmp/kv-client -cmd=health -addr=127.0.0.1:50051
/tmp/kv-client -cmd=set -key=hello -val=world -addr=127.0.0.1:50051
/tmp/kv-client -cmd=get -key=hello -addr=127.0.0.1:50051
/tmp/kv-client -cmd=delete -key=hello -addr=127.0.0.1:50051

# Smart multi-addr (tries all, follows leader redirect)
/tmp/kv-client -cmd=set -key=test -val=v1 -addrs=127.0.0.1:50051,127.0.0.1:50052,127.0.0.1:50053

# Idempotency testing
/tmp/kv-client -cmd=set -key=idem -val=first -client-id=myc -seq-num=1 -addr=127.0.0.1:50051
/tmp/kv-client -cmd=set -key=idem -val=SKIP  -client-id=myc -seq-num=1 -addr=127.0.0.1:50051
/tmp/kv-client -cmd=get -key=idem -addr=127.0.0.1:50051  # → first (duplicate skipped)
```

### Run unit tests
```bash
go test ./server/... -v  # 16 tests, all should pass
```

---

## 7. Known Bugs & Fragile Points

### Bug 1 — MEDIUM: KV API returns HTTP 200 on follower-redirect errors
**Location:** `cmd/dashboard/main.go:779`
**Issue:** `DirectKVSet/Get/Delete` return `{"success":false,"error":"not leader"}` with HTTP 200 instead of a redirect or retry. The UI JS auto-discovers the leader before calling so the demo path is fine, but a leadership change between the `/api/cluster` poll and the KV call will show a confusing error.
**Demo risk:** Low — only happens during elections.

### Bug 2 — MEDIUM: Chaos proxy (Drop/Delay) breaks in GCP mode
**Location:** `cmd/dashboard/main.go:433`
**Issue:** `StartChaosProxy` checks `m.nodes[nodeID]` which is always empty in GCP mode. Clicking "Drop Packets" or "Add Latency" in GCP mode returns "node X not found" error.
**Demo risk:** Medium if demonstrating on GCP. Use Partition/Netem buttons instead.

### Bug 3 — LOW: Dashboard wipes data on restart
**Location:** `cmd/dashboard/main.go:127`
**Issue:** `freshStart` is an in-memory flag. If kv-dashboard is killed and restarted, `freshStart=false`, and `StartAll()` will `os.RemoveAll("/tmp/raft-kv/")` wiping all Bolt data. Durability demo fails if this happens.
**Mitigation:** Don't restart the dashboard during the durability phase.

### Bug 4 — LOW: RestartNode uses Process!=nil as liveness check
**Location:** `cmd/dashboard/main.go:351`
**Issue:** A killed process still has `cmd.Process != nil`. Could select a dead node's address as the join target, triggering a 10-retry backoff hang (up to ~2 minutes).
**Demo risk:** Low — the retry eventually finds a live node, but it's slow.

### Bug 5 — CRITICAL for demo: Dashboard HTML served from cwd
**Location:** `cmd/dashboard/main.go:632`
**Issue:** `http.ServeFile(w, r, binDir+"/cmd/dashboard/index.html")` where `binDir = os.Getwd()`. If the binary is not run from the repo root, the browser gets a 404 or 500 on the main page.
**Fix:** Always `cd` to the repo root before running kv-dashboard.

### Bug 6 — INFO: seq-num=0 is silently ignored
**Location:** `cmd/client/main.go:43`
**Issue:** `if *explicitSeqNum != 0` means passing `-seq-num=0` has no effect. Cannot test idempotency at sequence number 0 explicitly.
**Impact:** Only affects targeted idempotency tests, not normal operation.

### Bug 7 — INFO: Empty LeaderAddr before peers register
**Location:** `server/node.go:210`, `server/fsm.go:72`
**Issue:** For ~2s after startup, the peers map may not have all entries. A follower redirect will return `LeaderAddr:""`, and the client will fail to redirect. The bootstrap wait and 200ms stagger between node starts mitigates this.
**Demo risk:** Only if you hammer the cluster immediately after start.

---

## 8. Testing Plan Summary

See the full testing plan in the previous conversation. Quick reference:

| Phase | What's tested | Key assertion |
|-------|---------------|---------------|
| 0 | Build + unit tests | 16/16 pass, all binaries compile |
| 1 | Cluster bootstrap | 1 Leader, 2 Followers, same applied_index |
| 2 | Core KV (SET/GET/DELETE) | CRUD works, missing key handled |
| 3 | Follower redirect | Writing to follower still succeeds |
| 4 | Idempotency | Duplicate seq skipped; higher seq applied |
| 5 | Leader failover (MTTR) | New leader in <3s, no data loss |
| 6 | Recovery & Durability | Node catches up via log replay + BoltDB |
| 7 | Chaos injection | Proxy drops/delays handled gracefully |
| 8 | Dashboard API (curl) | All endpoints return correct JSON |
| 9 | Edge cases | Unicode keys, empty keys, concurrent writes |

---

## 9. GCP Deployment

**VM layout (3 VMs, same region for low latency):**
- vm-0: node0 (12000/50051) + dashboard (:8080) + node-agent (:9000)
- vm-1: node1 (12001/50052) + node-agent (:9000)
- vm-2: node2 (12002/50053) + node-agent (:9000)

**Firewall rules needed:**
- TCP 12000-12002 between VMs (Raft)
- TCP 50051-50053 between VMs and from dashboard (gRPC)
- TCP 9000 between dashboard VM and all VMs (agent API)
- TCP 8080 from your IP (dashboard UI)

**deploy.sh flow:**
1. Provisions 3 VMs
2. Uploads binaries from `bin/`
3. Starts node-agents on each VM
4. Starts node0 (bootstrap), waits for election
5. Starts node1, node2 with `-join=vm-0-ip:50051`
6. Starts dashboard with `-agent-addrs=node0=vm0-ip:9000,...`

**GCP-specific fixes applied:**
- Bind gRPC to explicit internal IPs (not 0.0.0.0) for redirect accuracy
- `shopt -s nullglob` in chmod commands to prevent script crash on empty globs
- Raft timeouts tuned for actual inter-VM latency

**GCP verification phases:**
- Phase 1: Liveness — election works, leader responds to health checks
- Phase 2: Partition — SIGSTOP/SIGCONT partition, verify no split-brain
- Phase 3: Latency — chaos proxy delay, measure latency impact
- Phase 4: Durability — kill all nodes, restart, verify BoltDB recovery
- Phase 5: Idempotency — duplicate detection end-to-end
- Phase 6: Kernel chaos — iptables partition, netem delay/loss on real Raft traffic
- **All 6 phases passed on GCP.**

---

## 10. Professor & Course Context

**Prof: Ouldooz Baghban Karimi** (SFU School of Computing Science)
- PhD from SFU, thesis: "Efficient Resource Utilization in Advanced Wireless Networks"
- Industry background: Software Engineer at Cyan (→ Ciena/BluePlanet), network orchestration/SDN
- Teaches: Distributed Systems and the Cloud, Network Security, Web Systems Architecture
- Research: Cloud & distributed systems, network function virtualization, DEI in CS education
- Values: Problem-based learning, correct use of distributed systems concepts, working systems

**What she explicitly said about our project:**
1. *"I like that you chose this project. I believe it includes a lot of learning."* — she's rooting for us
2. *"It seems four components expected, but three are listed"* — we described KV-Store, Client, Chaos Proxy but didn't clearly call out the Dashboard as a 4th component. Fix this in slides.
3. *"If you have a 'core server' which sounds very centralized, where are your replicated state machines?"* — she wants us to clearly emphasize the DISTRIBUTED nature. Each node IS a state machine; the system is a cluster of replicated state machines, not a single server.
4. *"What is the expected number of nodes? What system considerations led you to this number?"* — answer: 3 nodes = minimum Raft quorum (majority=2), tolerates 1 failure. 5 nodes would tolerate 2 but doubles latency requirement (3 acks). 3 is optimal for demo-scale.
5. Her simulated grade: 8/10, 8/10, 8/10 — we're good but not exceptional. The gap is in articulation and depth of analysis.

**Grading rubric for final submission:**
| Category | Points | Gap area |
|----------|--------|----------|
| System design | 10 | Emphasize distributed nature, all 4 components, reasoning |
| Implementation | 10 | Show correct DS concepts: quorum, linearizability, FSM |
| Testing tools & results | 5 | Show methodology, not just pass/fail |
| Depth of experiment & analysis | 10 | Explain WHY results look the way they do |
| Writing & presentation | 5 | Brevity, structure, clarity |

**Presentation constraints:** 5 minutes, 4 slides only. April 2 or April 9, 2026.
- Slide 1: Title + team (30s)
- Slide 2: System functionality + design (90-120s)
- Slide 3: Implementation details (60-90s)
- Slide 4: Results + analysis (60-90s)

**Key: she penalizes for screenshots.** Use diagrams, graphs, and tables instead.

---

## 11. Where We Stand vs. Expectations

### Strengths (what we have that's excellent)
- **Working distributed system**: All 6 GCP verification phases passed. This alone is impressive for a class project.
- **Idempotency**: Proper client-id + sequence number deduplication. Most student projects skip this.
- **Snapshot/Restore**: Working InstallSnapshot for log compaction. Again, most skip this.
- **Chaos dashboard**: Live topology visualizer, kernel-level chaos (iptables + tc/netem), not just process-level
- **4 components** that are clearly separated by concern
- **Academic-grade metrics**: MTTR measurements, latency charts, durability validation
- **BoltDB durability**: Data persists across full cluster restarts

### Gaps (what could be sharper for max marks)

**For the prof (academic rigor):**
- Need to explicitly articulate **CAP theorem placement** (CP system) with measurement evidence
- Need to show **linearizability** is maintained (VerifyLeader() call is the mechanism — explain it)
- The analysis in `results_analysis.md` needs to be the *centerpiece* of the report, not an afterthought
- **Quorum loss behavior** — show what happens when you kill 2/3 nodes: writes must block (not silently fail or split-brain). This proves the safety guarantee.
- **Comparison data**: even a before/after latency graph (normal vs. leader election happening) tells a story

**For peer reviewers and HRs (professional polish):**
- The dashboard frontend needs a few UX fixes (see Section 12)
- A `README.md` with a 1-command demo is currently missing at repo root
- Code comments explaining the *distributed systems concept* being implemented (e.g., "// Circuit Breaker pattern") are already there — lean into this in the presentation

---

## 12. Directions for Improvement

Listed in priority order: **highest impact for demo + academic + resume** first.

### Priority 1 — Demo-critical (do before April 2)

**12.1 Fix dashboard HTML path (Bug 5)**
The one change that could silently break the entire demo. Either embed the HTML in the binary or make the path absolute/configurable.

**12.2 Dashboard UX: auto-resolve leader in KV panel**
Currently the KV panel already auto-discovers the leader from `/api/cluster` — this is correct. But the UX should show "Talking to: node2 (Leader)" so the demo audience sees it's finding the leader dynamically. Small label addition.

**12.3 Add "Demo Mode" script**
A single `./demo.sh` that: cleans up, builds, starts cluster, opens browser, and is ready in 10 seconds. Reduces demo failure surface area to nearly zero.

### Priority 2 — Academic depth (highest GPA impact)

**12.4 Measure and graph quorum loss**
Run: write continuously → kill 2 nodes → verify writes block → restart 1 node → writes resume. Capture timestamps. This demonstrates the **safety guarantee** (no writes without quorum) that is the core of Raft. Prof will love this.

**12.5 Quantify linearizability**
Show that stale reads are impossible by: (1) write to leader, (2) immediately read from follower that redirects, (3) show the read never returns the old value. The `VerifyLeader()` call is the mechanism — explain it in the report.

**12.6 Latency breakdown table**
| Scenario | p50 latency | p99 latency |
|----------|-------------|-------------|
| Normal (3 nodes healthy) | ~Xms | ~Xms |
| Leader on netem 500ms delay | ~Xms | ~Xms |
| Follower on netem 500ms delay | ~Xms | ~Xms |
| During election | — | blocked for ~Ys |

This is the kind of analysis that goes from 8/10 to 10/10.

**12.7 Explicitly state the fault model in slides**
Prof's option 3 requires identifying "the fault model." Ours is:
- Crash failures (fail-stop): nodes can die and restart
- Network partitions: communication loss between subsets
- Message omission (send/receive): delays and drops
- We explicitly do NOT handle Byzantine failures (nodes lying)

### Priority 3 — Professional polish (resume/HR value)

**12.8 Add a proper README.md**
One-command quickstart, architecture diagram (ASCII), what it demonstrates, tech stack. This is what HRs and GitHub visitors see first.

**12.9 Write a `DEMO.md` with a scripted demo narrative**
"Step 1: Open the dashboard. Step 2: Observe all 3 nodes. Step 3: Click Kill Leader. Step 4: Watch the election happen in real-time (the MTTR counter will count up, then a new leader appears in gold). Step 5: ..."
This makes the demo reproducible and impressive regardless of nerves.

**12.10 Add throughput measurement**
A simple loop:
```bash
for i in $(seq 1 100); do
    ./kv-client -cmd=set -key="k$i" -val="v$i" -addr=<leader> &
done
time wait
```
Calculate ops/sec. Compare: normal vs. one follower down vs. leader election mid-run. This is a concrete performance metric, not just correctness.

**12.11 Dashboard: show replication lag**
The `applied_index` is already shown per-node in `/api/cluster`. The gap between the leader's and a follower's `applied_index` IS the replication lag. Show this in the UI during chaos. Visually compelling.

**12.12 (Stretch) Write/read separation**
Currently all reads go through the leader (`VerifyLeader()`). A well-known alternative is **stale reads from followers** (relaxed consistency) for read scalability. Even just documenting this trade-off in the report shows depth of understanding.

---

## Key Talking Points for Presentation

1. **"This is the same algorithm that runs etcd, the database behind every Kubernetes cluster."** — immediately establishes relevance.

2. **"We implemented 3 things that most student implementations skip: idempotent writes with sequence numbers, snapshot-based log compaction, and kernel-level chaos with iptables — not just process kills."**

3. **"The system is CP in CAP terms. We proved this: when we partition 2 of 3 nodes, the minority side refuses writes. That's by design — consistency over availability."**

4. **"Our chaos dashboard lets you crash the leader live during a demo and watch the new election happen in real time, with MTTR measured to the millisecond."**

5. **On the 4th component question:** "Our 4 components are: the kv-store node (runs on each of the 3 VMs as a replicated state machine), the kv-client CLI, the kv-chaos proxy for fault injection, and the kv-dashboard for orchestration and visualization."

6. **On replicated state machines:** "Each kv-store node IS a state machine. The Raft log is the replication mechanism. When a command is committed — meaning a majority of nodes have durably written it to BoltDB — all nodes apply the same FSM transition, guaranteed to produce the same result."
