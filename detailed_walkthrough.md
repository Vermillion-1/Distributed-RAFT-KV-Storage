# Detailed Technical Walkthrough

**Project:** Fault-Tolerant Distributed Key-Value Store (Raft)
**Version:** v1.3 · 37/37 GCP tests confirmed (April 4, 2026)

This document walks through every major design decision, data path, and system component in depth. It is intended for readers who want to understand the code, not just the results.

---

## Table of Contents

1. [Design Philosophy](#1-design-philosophy)
2. [System Components](#2-system-components)
3. [Raft Consensus Layer](#3-raft-consensus-layer)
4. [Write Path](#4-write-path)
5. [Read Path — Leader](#5-read-path--leader)
6. [Read Path — Follower Read-Index (v1.3)](#6-read-path--follower-read-index-v13)
7. [Idempotency Design](#7-idempotency-design)
8. [Durability and Snapshots](#8-durability-and-snapshots)
9. [Fault Injection Infrastructure](#9-fault-injection-infrastructure)
10. [6-Phase Test Suite](#10-6-phase-test-suite)
11. [Performance Analysis](#11-performance-analysis)
12. [Key Bugs Discovered and Fixed](#12-key-bugs-discovered-and-fixed)

---

## 1. Design Philosophy

### Why CP over AP?

The CAP theorem forces a trade-off: under a network partition, a distributed system must choose between **Consistency** (every read sees the most recent write) and **Availability** (every request receives a response). This system chooses **CP**.

The motivation is correctness-first: a key-value store that returns stale or divergent data is unsafe for any application that uses it for coordination, configuration, or locking. A store that temporarily refuses reads and writes during a partition is recoverable — the cluster heals when the partition clears, and all nodes converge to the same state. A store that serves stale data silently corrupts application state.

### Why Raft?

Raft was chosen over Paxos for implementability. Raft's single-leader model and explicit log structure make it easier to reason about correctness. The leader is always the authoritative source for the current state; followers never serve reads without confirming the leader's identity.

The implementation uses **HashiCorp Raft v1.7.3** — the same library used by Consul, Vault, and Nomad in production.

### Why the Sidecar Pattern?

Each VM runs two independent processes: the Raft replica (`kv-store`) and a fault injection agent (`node-agent`). The agent operates at the OS level — `kill -9`, `iptables`, `tc netem` — and has no runtime dependency on the replica. This means:

- The agent can inject faults the application cannot detect or work around
- Tests exercise real fault scenarios (process death, network partition, NIC-level delay), not mocks
- The replica's correctness under fault is genuinely tested, not simulated

---

## 2. System Components

| Binary | Source | Role |
|--------|--------|------|
| `kv-store` | `main.go` + `server/` | Raft replica: consensus, log, FSM, gRPC API |
| `node-agent` | `cmd/agent/` | Sidecar: HTTP API for fault injection |
| `kv-client` | `cmd/client/` | CLI: multi-address failover, idempotency, leader redirect |
| `kv-dashboard` | `cmd/dashboard/` | Web UI: cluster health, chaos controls, KV operations |

### Key source files

- **`server/node.go`** — Raft node initialization, leader election callbacks, `Join` handler (`AddVoter`), `Get`/`Set`/`Delete` gRPC handlers, `VerifyLeader()` call on reads
- **`server/fsm.go`** — Finite State Machine: `Apply()` for writes, `Snapshot()`/`Restore()` for durability, idempotency table (`map[clientID]lastSeq`)
- **`proto/kv.proto`** — gRPC service definition: `KVService` with `Get`, `Set`, `Delete`, `Health`, `Join` RPCs

### Network ports

| Port range | Protocol | Traffic |
|------------|----------|---------|
| 12000–12004 | Raft TCP | `AppendEntries`, `RequestVote`, `InstallSnapshot`, heartbeats |
| 50051–50055 | gRPC | `Get`, `Set`, `Delete`, `Health`, `Join` — client-facing |
| 9000 | HTTP | node-agent fault injection API |

The dual-port design allows fault injection to target Raft traffic and client traffic independently — or both simultaneously (as in the full iptables partition tests).

---

## 3. Raft Consensus Layer

### Library and Configuration

```
Library:              HashiCorp Raft v1.7.3
Storage (log):        BoltDB (raft.db)
Storage (stable):     BoltDB (stable.db)
Snapshot store:       FileSnapshotStore (on-disk)
HeartbeatTimeout:     500ms
ElectionTimeout:      750ms
SnapshotThreshold:    10 log entries
```

The 500ms heartbeat and 750ms election timeout are tuned for GCP cross-zone latency (~15ms RTT). The ratio (1.5×) is below HashiCorp's recommended 5–10× but was empirically validated: the full 37-test suite passes with zero false elections, and one observed spurious election in P3 R2 was self-healing within one term.

### Leader Election

1. A follower starts an election after `ElectionTimeout` elapses without a heartbeat
2. It increments its term, votes for itself, and sends `RequestVote` RPCs to all peers
3. If it receives a majority of votes (⌊N/2⌋ + 1), it becomes leader
4. The new leader immediately sends heartbeats to suppress further elections

**Observed MTTR:** ~1.25 seconds across both SIGKILL (process death) and iptables partition fault scenarios. The timing: election fires at ~500ms after last heartbeat, new leader elected within one additional election timeout.

### Quorum and N=5 Generalization

With N=5 nodes, quorum = ⌊5/2⌋ + 1 = **3**. The full 6-phase suite was run on a 5-node GCP cluster (April 1, 2026) and confirmed 37/37. The 3-kill threshold test (killing 3 nodes makes the cluster unavailable exactly at the quorum boundary) passed correctly.

---

## 4. Write Path

A complete write (`Set foo=bar`) traverses the following path:

```
kv-client
  │  (1) sends Set RPC to any known address
  ▼
kv-store (follower)
  │  (2) if not leader: returns redirect error with leader address
  ▼
kv-client
  │  (3) retries Set RPC at leader address
  ▼
kv-store (leader)
  │  (4) creates Raft log entry: {op: SET, key: foo, val: bar, clientID, seqNum}
  │  (5) sends AppendEntries to followers
  ▼
kv-store (followers ×2)
  │  (6) append entry to local log, send ACK
  ▼
kv-store (leader)
  │  (7) quorum ACK received (leader + 1 follower = 2/3 = majority)
  │  (8) commits entry, advances commit index
  │  (9) calls FSM.Apply()
  ▼
server/fsm.go Apply()
  │  (10) checks idempotency table: if clientID+seqNum already seen → skip
  │  (11) writes key→value to BoltDB KV bucket
  │  (12) updates idempotency table: lastSeq[clientID] = seqNum
  ▼
kv-store (leader)
  │  (13) returns success response to kv-client
```

**Key property:** The write is durable (BoltDB) and linearizable (only committed after majority ACK) before the response is returned. If the leader crashes after step 7 but before step 13, the entry is still committed — the new leader will apply it and return the result on retry.

---

## 5. Read Path — Leader

All reads go through the leader with a `VerifyLeader()` call before returning data:

```
kv-client
  │  sends Get RPC to leader
  ▼
kv-store (leader)
  │  (1) calls VerifyLeader() — sends heartbeat to majority of peers
  │      if majority unreachable → returns error (CP safety enforced)
  │  (2) reads key from BoltDB KV bucket
  │  (3) returns value
```

`VerifyLeader()` adds one round-trip (~30ms cross-zone on GCP) to every read. This prevents a deposed leader (one that lost connectivity to the majority) from serving stale data to a client that still believes it is the leader.

**Tested by:** P2c (isolated leader cannot serve reads), L3b (CP safety confirmed), N6c (iptables-partitioned leader rejects writes and reads).

---

## 6. Read Path — Follower Read-Index (v1.3)

The follower read-index protocol (FEAT-RI) allows followers to serve linearizable reads without routing every GET to the leader:

```
kv-client
  │  sends Get RPC with --follower-read flag to any follower
  ▼
kv-store (follower)
  │  (1) calls GetReadIndex() on local Raft instance
  │      → Raft library sends ReadIndex RPC to leader
  ▼
kv-store (leader)
  │  (2) confirms it is still leader via heartbeat
  │  (3) returns current commit_index N to follower
  ▼
kv-store (follower)
  │  (4) waits until appliedIndex ≥ N
  │  (5) reads key from local BoltDB KV bucket
  │  (6) returns value to client
```

**Why this is linearizable:** The follower's `appliedIndex ≥ N` guarantee means it has applied every log entry up to the leader's current commit point. Any write that completed before the read (from the client's perspective) has a Raft log index ≤ N, so the follower has necessarily applied it.

**Tested by:** `verify_follower_read.sh` — 6 scenarios covering fresh writes, writes under partition, and cross-follower consistency. All 6/6 pass.

---

## 7. Idempotency Design

### The Problem

In a distributed system with retries, a client may send the same write twice: once before a leader failure, and once after connecting to the new leader. Without idempotency, the write would be applied twice, corrupting state.

### The Solution

Each client assigns a monotonically increasing `seqNum` to every write. The FSM maintains a per-client deduplication table:

```go
// server/fsm.go
type KVStore struct {
    data      map[string]string   // key → value
    lastSeq   map[string]uint64   // clientID → last applied seqNum
}

func (k *KVStore) Apply(log *raft.Log) interface{} {
    // ...decode command...
    if cmd.SeqNum > 0 && cmd.SeqNum <= k.lastSeq[cmd.ClientID] {
        return nil  // duplicate — silently drop
    }
    k.data[cmd.Key] = cmd.Value
    k.lastSeq[cmd.ClientID] = cmd.SeqNum
}
```

**Key property:** The dedup table is part of the FSM state. It is included in snapshots and replayed during log catch-up, so idempotency is preserved across leader changes and node restarts.

**Tested by:** P5 I2 (same `(client_id, seq)` sent twice after failover — value applied exactly once), P5 I3 (duplicate dropped on new leader).

---

## 8. Durability and Snapshots

### BoltDB Persistence

Every log entry is written to BoltDB (an embedded B-tree store) before being acknowledged. BoltDB uses `fsync` on every write by default, so committed entries survive process crashes and power failures.

### Snapshot Mechanism

```
SnapshotThreshold: 10 log entries
```

After every 10 committed entries, the Raft library calls `FSM.Snapshot()`, which serializes the full KV store state (including the idempotency table) to disk. This bounds log replay time on restart — a node never replays more than ~10 entries before its state is current.

**InstallSnapshot RPC:** When a follower is so far behind that the leader has already compacted the relevant log entries, the leader sends the full FSM snapshot via `InstallSnapshot`. The follower applies it atomically, bringing its state up to date instantly.

**Tested by:**
- **D1** — All 3 nodes SIGKILL'd then restarted; 10/10 keys recovered (100%)
- **D2** — Leader killed mid-write during 50-write burst; 7/7 acknowledged keys intact, 43 unacknowledged = absent (correct — never committed)
- **D3** — Follower offlined during 30 writes; on restart, `InstallSnapshot` delivers full state; 30/30 keys recovered, `appliedIndex delta = 0`

---

## 9. Fault Injection Infrastructure

The `node-agent` sidecar exposes an HTTP API on `:9000`:

| Endpoint | Mechanism | Effect |
|----------|-----------|--------|
| `POST /api/kill` | `kill -9 <pid>` | Immediate process death (no cleanup) |
| `POST /api/pause` | `kill -STOP / -CONT` | Freeze/unfreeze process (simulates GC pause) |
| `POST /api/partition` | `iptables -A INPUT -p tcp --dport 12001 -j DROP` + OUTPUT DROP | Bidirectional network partition on Raft port |
| `POST /api/unpartition` | `iptables -D` | Restore network connectivity |
| `POST /api/netem` | `tc qdisc add dev ens4 root netem delay Xms` | Full-NIC artificial latency |
| `POST /api/clear-netem` | `tc qdisc del dev ens4 root` | Remove latency |
| `POST /api/restart` | Start `kv-store` process | Restart a killed/stopped node |

**Critical implementation details:**

- **Bidirectional iptables (BUG-4 fix):** The initial implementation dropped only INPUT traffic. A partitioned node could still send heartbeats out, so the leader never detected the partition. The fix drops both INPUT and OUTPUT on the Raft port, creating a true split-brain scenario.

- **Full-NIC netem (BUG-5 fix):** The initial implementation applied `tc filter u32 match ip dport 12001`, scoping delay to the Raft port only. The kv-client communicates on gRPC ports 50051–50055, which were unaffected — the test was not measuring client-observable latency. The fix applies `tc qdisc add dev ens4 root netem delay Xms` to the full NIC.

- **NIC auto-detection:** GCP Debian 12 uses `ens4`, not `eth0`. The agent auto-detects the primary NIC via `ip route get 8.8.8.8` and uses the result (`dev ens4`) for all `tc` commands.

---

## 10. 6-Phase Test Suite

All tests run from `node0` on GCP. Each phase has a dedicated script (`GCP_verify_phase[1-6].sh`).

### Phase 1 — Liveness (Leader Failover)

| Test | What it does |
|------|-------------|
| L1 | Kill leader via SIGKILL; measure time until new leader elected and cluster accepts writes (MTTR ~1.25s) |
| L1c | After failover, verify RSM consistency: all 3 nodes agree on the same key-value state |
| L3b | Partition leader (iptables); verify cluster elects new leader and CP safety holds |

### Phase 2 — Network Partitions

| Test | What it does |
|------|-------------|
| P1a | Partition 1 node (minority); verify cluster continues (majority available) |
| P2a | Partition 2 nodes (majority); verify cluster becomes unavailable (no quorum) |
| P2c | Partition the leader; verify new leader elected, old leader cannot serve stale reads |

### Phase 3 — Write Latency Under Delay

| Test | What it does |
|------|-------------|
| R1 (slow follower) | Apply 2000ms netem to a non-leader follower; measure write throughput. Expected: quorum bypass — only −2.5% impact |
| R2 (slow leader) | Apply 500ms netem to leader; measure write throughput. Expected: election fires, −99% during transition |

### Phase 4 — Durability

| Test | What it does |
|------|-------------|
| D1 | SIGKILL all 3 nodes; restart all; verify 10/10 pre-crash keys recovered |
| D2 | Kill leader mid-write during 50-write burst; verify acked writes are intact, unacked are absent |
| D3 | Offline a follower for 30 writes; bring it back; verify InstallSnapshot delivers all 30 keys |

### Phase 5 — Idempotency

| Test | What it does |
|------|-------------|
| I2 | Send `(client_id=X, seq=1, key=foo, val=bar)` twice after a leader failover; verify value applied exactly once |
| I3 | Send `(client_id=X, seq=1)` to two different nodes in quick succession; verify no phantom commit |

### Phase 6 — Kernel Chaos (iptables)

6 assertions in a single orchestrated scenario:

| Test | What it checks |
|------|---------------|
| N6a | Partition follower A; verify cluster continues (majority still available) |
| N6b | Partition follower B; verify cluster becomes unavailable (quorum lost) |
| N6c | Restore follower A; partition leader instead; verify new leader elected, old leader cannot commit |
| N6d | Restore leader; verify full cluster re-convergence |
| N6e | Verify CP safety: no write went through while quorum was lost |
| N6f | Verify state consistency: all 3 nodes agree on final KV state |

---

## 11. Performance Analysis

### Baseline

Measured on a 3-node GCP cluster (`us-central1-a` + `us-central1-c`, e2-micro VMs):

| Metric | Value |
|--------|-------|
| Baseline write throughput | **62.9 ops/sec** (15.9 ms/op) |
| Baseline read throughput | Similar (VerifyLeader adds ~30ms cross-zone) |

### Quorum Bypass (Slow Follower)

With 2000ms netem on one of two followers:
- Throughput: **61.3 ops/sec** (−2.5%)
- Explanation: the leader requires ACK from only 1 follower (⌊3/2⌋ = 1). The slow follower is genuinely off the critical path. This is stronger than expected — the slow node adds zero observable latency to the commit path.

### Leader Bottleneck (Slow Leader)

With 500ms netem on the leader:
- Throughput: **0.6 ops/sec** (−99%)
- Explanation: the election fires (leader's heartbeats to followers are delayed, followers time out). During election (~1.25s), all writes block. After the new leader is elected, throughput recovers — but the test window captures the transition, so the average is very low. This is correct behavior.

### MTTR Breakdown

```
t=0ms    Last heartbeat from old leader
t=500ms  Follower election timeout fires → election starts
t=750ms  Election timeout window → new leader elected
t=1250ms New leader sends first AppendEntries → followers confirm
         → Cluster accepts writes again
```

MTTR ~1.25s is consistent across both SIGKILL and iptables partition fault types — the election timing dominates, not the fault mechanism.

---

## 12. Key Bugs Discovered and Fixed

### BUG-4: Unidirectional iptables Partition

**Symptom:** Phase 2 and Phase 6 N6 partition tests consistently passed even when the partition was not working.

**Root cause:** `iptables -A INPUT -j DROP` (inbound only). The "partitioned" node could still send heartbeats and AppendEntries out — the leader never detected a problem.

**Fix:** Add `iptables -A OUTPUT -j DROP` on the Raft port as well, creating a true bidirectional partition.

**Impact:** Without this fix, every partition test was a false positive — the system was not actually being tested under partition conditions.

---

### BUG-5: Port-Scoped netem (Wrong Measurement)

**Symptom:** Phase 3 R1 slow-follower test showed no latency increase on the client side with 2000ms netem applied.

**Root cause:** `tc filter u32 match ip dport 12001` — netem scoped to Raft port only. kv-client communicates on gRPC ports 50051–50055, which were unaffected.

**Fix:** Apply netem to the full NIC: `tc qdisc add dev ens4 root netem delay Xms`. Added NIC auto-detection via `ip route get 8.8.8.8` (GCP Debian 12 uses `ens4` not `eth0`).

**Impact:** The test intent — "do clients observe the delay?" — was not being verified. After the fix, the quorum bypass result (−2.5%) is a real, client-observable measurement.

---

### BUG-6: Follower Selector Picked the Test Node

**Symptom:** Phase 3 R1 consistently showed much higher latency than the 2000ms netem could explain.

**Root cause:** `random.choice([n for n in nodes if n != leader])` could select `node0`. The test script runs on `node0` — delaying its NIC with 2000ms netem affected the kv-client process on the same VM. The test was measuring "slow test client," not "slow follower."

**Fix:** `followers = [n for n in all_nodes if n not in [leader, "node0"]]`

**Impact:** This was the final blocking bug before 37/37. After this fix, the suite score went from 36/37 to 37/37 (April 4, 2026).
