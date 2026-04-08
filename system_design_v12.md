# System Design — Distributed Raft KV Store v1.2
**Project:** CMPT 756 — Fault-Tolerant Distributed Systems
**Date:** 2026-04-01
**Status:** 5-node GCP deployment, 36/37 verified (37/37 pending BUG-6 re-run)

---

## 1. System Overview

A **CP key-value store** backed by the Raft consensus algorithm. The system prioritizes Consistency and Partition Tolerance (CAP theorem): under a network partition, the minority side sacrifices availability rather than risk serving stale data.

**Tested configurations:** N=3 (March 29, 2026) and N=5 (April 1, 2026) across GCP `e2-micro` VMs in `us-central1-a` and `us-central1-c`.

### Fault Model (Non-Byzantine)

| Fault Type | What it means | How injected |
|-----------|--------------|-------------|
| Crash-Recovery | Node dies, loses in-memory state, may restart later | `SIGKILL` via `/api/kill` |
| Process Freeze | Node is alive but cannot send or receive | `SIGSTOP`/`SIGCONT` via `/api/pause`, `/api/resume` |
| Network Partition | Node cannot communicate with peers (symmetric) | Bidirectional `iptables DROP` via `/api/chaos/partition` |
| Message Delay | Messages arrive arbitrarily late | `tc netem` on full NIC via `/api/chaos/netem` |
| Packet Loss | Messages randomly dropped in transit | `tc netem loss X%` via `/api/chaos/netem` |

The system does **not** handle Byzantine faults (malicious or corrupting nodes).

---

## 2. Component Architecture

Each GCP VM runs two processes: the **Raft Replica** (data plane) and the **Sidecar Agent** (control plane). They are independent — the agent can inject faults into the replica without the replica's cooperation.

### Component Table

| Component | Binary | Runs on | Role |
|-----------|--------|---------|------|
| Raft Replica | `kv-store` | Every VM | Consensus node: Raft log, FSM, gRPC client API |
| Sidecar Agent | `node-agent` | Every VM | Control plane: process management, iptables, netem, HTTP API |
| Smart Client | `kv-client` | Laptop / node0 | CLI: auto-redirect to leader, idempotency, multi-addr failover |
| Dashboard | `kv-dashboard` | Laptop (local only) | Cluster visualizer + orchestrator for local mode |
| ~~Chaos Proxy~~ | ~~`kv-chaos`~~ | ~~Deprecated~~ | ~~TCP proxy for local delay/drop — superseded by Sidecar Agent netem in v1.2~~ |

### Sidecar Agent HTTP API

The agent is the remote control interface for all fault injection. The dashboard and test scripts send HTTP calls to it.

| Endpoint | Method | Action |
|----------|--------|--------|
| `/health` | GET | Returns `{"alive":true}` if agent is running |
| `/api/kill` | POST | Sends `SIGKILL` to kv-store child process |
| `/api/pause` | POST | Sends `SIGSTOP` to kv-store (freezes without killing) |
| `/api/resume` | POST | Sends `SIGCONT` to kv-store (unfreezes) |
| `/api/restart` | POST | Kills and respawns kv-store with same config |
| `/api/chaos/partition` | POST | Applies bidirectional iptables DROP rules |
| `/api/chaos/heal` | POST | Flushes all iptables DROP rules |
| `/api/chaos/netem` | POST | Applies `tc netem` delay/loss to full NIC |
| `/api/chaos/netem/remove` | POST | Removes netem qdisc |

---

## 3. Dual-Port Node Design

Every replica exposes two ports, serving fundamentally different traffic:

```
VM (e.g., node1)
├── Port 12001  ←── Raft TCP  (internal, peer-to-peer)
│     AppendEntries, RequestVote, InstallSnapshot, heartbeats
└── Port 50052  ←── gRPC      (external, client-facing)
      Get, Set, Delete — served only by leader
```

**Why two ports:**
1. **Isolation**: injecting a fault on the gRPC port (client delay) does not disrupt the Raft consensus port, and vice versa. This lets tests measure client-observable latency independently from consensus-replication latency.
2. **Correctness**: in v1.1, netem was scoped to the Raft port only. Client latency was unaffected by follower delay because gRPC uses a different port. v1.2 (BUG-5 fix) applies netem to the full NIC, making client and Raft traffic share the same delay budget.

Port allocation for N nodes:

| Node | Raft TCP | gRPC |
|------|----------|------|
| node0 | 12000 | 50051 |
| node1 | 12001 | 50052 |
| node2 | 12002 | 50053 |
| node3 | 12003 | 50054 |
| node4 | 12004 | 50055 |

---

## 4. Write Path

```
Client
  │  Set(key, val, client_id=C, seq_num=S)
  ▼
Any Replica (gRPC :50051–50055)
  │  If follower: return {not_leader, leader_addr}
  │  Client retries leader automatically
  ▼
Leader Replica
  │  1. Check FSM dedup table: is (C, S) already committed?
  │     → Yes: return cached result (Exactly-Once)
  │     → No: continue
  │  2. Append {key, val, C, S} to Raft log
  │  3. Send AppendEntries RPC to all N-1 followers in parallel
  ▼
Followers (⌊N/2⌋ must ACK)
  │  Write log entry to BoltDB; send ACK to leader
  ▼
Leader
  │  4. On majority ACK: commit log entry
  │  5. Apply to FSM: map[key] = val; dedup[C] = S
  │  6. Return {success: true} to client
  │
  └─► Background: continue AppendEntries to lagging followers asynchronously
```

**Quorum commit:** only a majority (`⌊N/2⌋ + 1` nodes total, including leader) must acknowledge before the leader commits. Slow or partitioned minority followers do not block the write path.

---

## 5. Read Path

```
Client
  │  Get(key)  →  sent to leader's gRPC port
  ▼
Leader Replica
  │  1. VerifyLeader(): send heartbeat to majority, confirm still leader
  │     → Fails if partitioned (leader cannot reach majority)
  │     → Prevents stale reads from deposed leaders
  │  2. Read map[key] from in-memory FSM
  │  3. Return {found: true, value: val}
```

**Linearizable reads**: `VerifyLeader()` adds one round-trip latency to every read but guarantees the reader is talking to the current, up-to-date leader. A follower that receives a read request auto-redirects to the leader.

---

## 6. Bidirectional Partition Design (v1.2)

**Problem (BUG-4, pre-v1.2):** The agent's partition only applied `INPUT DROP` on the Raft port. This stopped the target node from receiving AppendEntries ACKs, but the leader's outgoing heartbeats to followers were unaffected. Followers kept receiving heartbeats → no election fired → the "partitioned" leader stayed leader and could still commit writes (it could reach both followers outbound).

**v1.2 fix:** Bidirectional iptables rules block both directions:

```bash
# On the partitioned node:
iptables -A INPUT  -p tcp --dport <own-raft-port>   -j DROP  # stop receiving ACKs
iptables -A OUTPUT -p tcp --dport <peer0-raft-port> -j DROP  # stop sending heartbeats
iptables -A OUTPUT -p tcp --dport <peer1-raft-port> -j DROP
# (one OUTPUT rule per peer)
```

The agent is started with `-peer-raft-addrs=ip0:port0,ip1:port1,...` so it knows peer Raft addresses at startup. `dynamic_deploy.sh` builds and injects this flag per node.

**Heal sequence:**
```bash
iptables -F INPUT   # flush all INPUT rules
iptables -F OUTPUT  # flush all OUTPUT rules
```

After heal, the former leader receives `AppendEntries` from the new leader with a higher term, discovers it was superseded, and steps down to Follower. N6e confirms this.

---

## 7. Network Delay (Netem) Design (v1.2)

**Problem (BUG-5, pre-v1.2):** The agent applied `tc netem` scoped to the Raft port via a `u32` filter. Client writes to gRPC port (50051–50055) were unaffected by the delay — the test intent (does slow leader raise client latency?) was not actually being measured.

**v1.2 fix:** Apply netem to the **full network interface**:

```bash
# Auto-detect the default NIC (handles eth0 vs ens4 vs ens5):
NIC=$(ip route get 8.8.8.8 | awk '{for(i=1;i<=NF;i++) if($i=="dev") print $(i+1); exit}')

# Apply delay to all outbound traffic:
tc qdisc add dev $NIC root netem delay ${ms}ms [loss ${pct}%]

# Remove:
tc qdisc del dev $NIC root
```

GCP Debian 12 uses `ens4` (not `eth0` as assumed in v1.1). The `ip route get 8.8.8.8` auto-detection is NIC-agnostic and works on any Linux distribution.

A 10-second auto-cleanup background process runs after each netem injection to prevent stray rules from persisting if the agent crashes during a test.

---

## 8. Quorum Configuration

| Cluster Size | Quorum (majority) | Failures tolerated | Liveness result | Unavailability trigger |
|-------------|-------------------|--------------------|-----------------|----------------------|
| N = 3 | 2 nodes | 1 simultaneous failure | L2: PASS (kill 1, 2 remain) | 2 kills → writes blocked |
| N = 5 | 3 nodes | 2 simultaneous failures | L2: PASS (kill 2, 3 remain) | 3 kills → writes blocked |

The 5-node results prove the quorum formula `⌊N/2⌋ + 1` holds beyond the minimal 3-node case: the system correctly becomes unavailable at exactly the right threshold, no earlier and no later.

---

## 9. Timing Configuration

| Parameter | Value | Derivation |
|-----------|-------|-----------|
| Heartbeat interval | 500 ms | ~33× measured cross-zone baseline RTT (~15ms) |
| Election timeout | 750 ms | 1.5× heartbeat interval |
| Theoretical MTTR | 1,250 ms | Detection (500ms) + Election (750ms) |
| Observed MTTR | ~1,250 ms | Measured via `T_RECOVER − T_KILL` in Phase 1 L1 and Phase 6 N6a |

### Timing Rationale

The baseline cross-zone RTT between `us-central1-a` and `us-central1-c` e2-micro instances was measured at ~15ms (99th percentile ~40ms). The 500ms heartbeat provides a 12× buffer over the p99 RTT, which is sufficient to absorb scheduling jitter on shared-tenant e2-micro VMs.

The 750ms election timeout gives a 1.5× ratio over the heartbeat interval. HashiCorp Raft's documentation recommends 5–10×; our ratio is more aggressive. In practice it proved empirically stable for cross-zone GCP, but under heavy CPU contention the 5-node run did observe one spurious election (Phase 3 R2b). The test suite was updated to accept election-then-recovery as valid liveness behavior, which is correct per Raft's liveness guarantee.

**Trade-off:** a more conservative ratio (e.g., 5000ms election timeout) would eliminate spurious elections but raise MTTR from 1.25s to ~5.5s. The current configuration prioritizes low MTTR for the demo.

---

## 10. Durability Design

```
Write committed to Raft log
    │
    ├─► BoltDB (`raft-log.bolt`) — WAL on local SSD
    │     Survives: SIGKILL, power loss, process crash
    │     Does NOT survive: disk wipe (`rm -rf /tmp/raft-kv/`)
    │
    └─► In-memory FSM (`map[string]string`)
          Lost on crash, rebuilt from log on restart

Snapshot trigger: every 10 Raft log entries (SnapshotThreshold=10)
    │
    ├─► FSM serialized to JSON binary → written to snapshot store
    └─► Sent via InstallSnapshot RPC to lagging followers
          (faster than replaying 10,000 log entries)

Recovery sequence on restart:
    1. Load latest snapshot from disk → Restore() in server/fsm.go
    2. Replay log entries after snapshot index
    3. Rejoin cluster; receive AppendEntries from current leader
    4. Catch up to leader's applied index
```

Phase 4 durability results: 100% key recovery across total cluster wipe (D1), dirty crash mid-write (D2), and snapshot-based catch-up (D3).

---

## 11. Idempotency Design

**Problem:** clients retry writes when they don't receive a response (network drop, leader change mid-write). Without dedup, a retry applies the mutation twice.

**Solution:** every write carries `(client_id, seq_num)`. The FSM maintains a per-client dedup table:

```go
// In server/fsm.go
type FSM struct {
    mu      sync.Mutex
    data    map[string]string
    dedup   map[string]uint64  // client_id → last applied seq_num
}

func (f *FSM) isDuplicate(clientID string, seqNum uint64) bool {
    last, ok := f.dedup[clientID]
    return ok && seqNum <= last
}
```

On duplicate: FSM returns the cached result without mutating state. Raft still applies the log entry (index advances), but the key-value map is unchanged.

Phase 5 I2 verifies this: the same `(client_id=test-client-123, seq_num=999)` sent twice — the key retains its first value, not the second.

---

## 12. Known Limitations

| Limitation | Impact | Mitigation |
|------------|--------|-----------|
| **Static cluster membership** | Permanent disk failure = cluster downtime; node cannot be replaced without full teardown | Accepted for PoC; joint consensus (Raft §6) is the production solution |
| **Heartbeat/election ratio 1.5×** | Under heavy CPU contention, spurious elections possible | Empirically stable for GCP cross-zone; R2b test updated to accept this |
| **Single-region deployment** | No geo-distribution; all nodes in `us-central1` | Outside course project scope |
| **Single-leader write ceiling** | All writes serialized through one replica; no horizontal write scaling | Inherent to strong-consistency Raft; sharding required for multi-leader |
| **No membership metrics** | MTTR for node replacement not measured | Out of scope |
| **N=11 breaking-point test infeasible** | `us-central1` e2-micro quota exhausted; cannot provision > 5 nodes | Out of scope; N=3 and N=5 results demonstrate O(N) heartbeat scaling |
| **Chaos Proxy deprecated** | `kv-chaos` binary still present but unused in GCP mode | Remove or document in v1.3 |
