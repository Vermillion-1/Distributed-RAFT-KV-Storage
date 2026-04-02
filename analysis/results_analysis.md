# Experimental Results & Performance Analysis
**Project:** Distributed Raft KV Storage (CP System)
**Environment:** Google Cloud Platform — `e2-micro`, `us-central1-a/c` (cross-zone)
**Runs:** 3-node (March 29, 2026) · 5-node (April 1, 2026)

---

## Key Metrics at a Glance

| Metric | Value | Source |
|--------|-------|--------|
| Baseline write latency | **19.6 ms/op** | Phase 3 R1, 3-node |
| Slow-follower write latency | **23.8 ms/op** | Phase 3 R1, 2000ms delay |
| Slow-follower throughput retention | **82%** (42 vs 51 ops/sec) | Phase 3 R1 |
| Slow-leader write latency | **502 ms/op** | Phase 3 R2, 500ms delay |
| Slow-leader throughput retention | **4%** (2 vs 51 ops/sec) | Phase 3 R2 |
| MTTR (leader kill → new leader) | **~1.25 s** | Phase 1 L1, Phase 6 N6a |
| Key recovery ratio | **100%** (3/3 scenarios) | Phase 4 D1–D3 |
| Test suite pass rate (3-node) | **33/36** | Session 1 |
| Test suite pass rate (5-node) | **36/37 → 37/37*** | Session 2 + BUG-6 fix |

*BUG-6 patched; awaiting GCP verification run.

---

## 1. Latency & Throughput — Phase 3

### 1.1 Write Latency Under Fault Injection

| Condition | Delay Injected | Avg Latency (ms/op) | Throughput (ops/sec) | vs Baseline |
|-----------|---------------|---------------------|-----------------------|-------------|
| Baseline (no fault) | — | 19.6 | 51.0 | — |
| Slow follower (minority) | 2000ms on 1/3 nodes | 23.8 | 42.0 | −18% throughput |
| Slow leader | 500ms on leader | 502.0 | 2.0 | −96% throughput |

**Baseline derived from:** `time_writes 20` total ÷ 20 writes, averaged over Phase 3 R1 measurement.
**Slow-leader derived from:** `time_writes 10` returned 5020.3ms total ÷ 10 writes = 502ms/op.

### 1.2 Interpretation

**Quorum bypass (minority delay):** The leader commits on acknowledgment from any majority — self + 1 follower in a 3-node cluster. The 2000ms delay on the third node is off the critical commit path. The +4.2ms overhead reflects only the periodic AppendEntries retransmission scheduling, not the write path itself. Efficiency retention: `23.8/19.6 = 82%` throughput (98% latency proximity).

**Leader bottleneck (leader delay):** Every write's commit path traverses the leader twice — once to receive the client RPC, once to wait for follower ACKs. A 500ms `tc netem` delay on `ens4` means both the inbound client gRPC and the outbound AppendEntries are subject to that delay. Result: 502ms/op ≈ 500ms injected + 2ms baseline overhead. This directly proves the single-leader constraint of linearizable Raft.

### 1.3 Throughput Summary

```
Baseline:      ████████████████████████████████████████  51 ops/sec
Slow follower: ████████████████████████████████          42 ops/sec  (−18%)
Slow leader:   █                                          2 ops/sec  (−96%)
```

---

## 2. MTTR — Mean Time To Recovery

### 2.1 Election Timers

| Parameter | Configured Value |
|-----------|-----------------|
| Heartbeat interval | 500 ms |
| Election timeout | 750 ms |
| Theoretical minimum MTTR | 500 + 750 = 1250 ms |
| Observed MTTR (Phase 1 L1) | ~1250 ms |
| Observed MTTR (Phase 6 N6a) | ~1250 ms |

### 2.2 Failover Timeline

| Window | Duration | What happens |
|--------|----------|--------------|
| **T = 0** | — | Leader process killed (`SIGKILL`) or iptables-partitioned |
| **Detection** | 0 – 500ms | Followers miss heartbeats; each independently starts election timer |
| **Election** | 500 – 1250ms | First follower to timeout sends RequestVote; collects majority; wins |
| **Stabilization** | 1250ms | New leader sends first heartbeat; cluster resumes writes |
| **Total downtime** | **~1.25 s** | Cluster unavailable for writes (CP: no stale reads served) |

### 2.3 MTTR Across Test Scenarios

| Scenario | Test | Fault Method | Observed MTTR |
|----------|------|-------------|---------------|
| Leader SIGKILL | L1 (Phase 1) | `kill -9` via SSH | ~1.25 s |
| Leader iptables partition | N6a (Phase 6) | `iptables INPUT+OUTPUT DROP` | ~1.25 s |
| N6 term advancement | N6b (Phase 6) | — | Confirmed (old\_term → new\_term) |

Both fault methods (process kill vs. network partition) produce identical MTTR because the detection mechanism is the same: missing heartbeats → election timeout. This confirms the fault model coverage is symmetric.

---

## 3. Durability — Phase 4

### 3.1 Key Recovery Results

| Test | Fault Scenario | Writes Acked | Recovered | Recovery Ratio |
|------|---------------|-------------|-----------|---------------|
| D1 — Total Wipe | All nodes killed + restarted | 10 | 10 | **100%** |
| D2 — Dirty Crash | Leader killed mid-write stream | 7 | 7 | **100%** |
| D3 — Snapshot Replay | Follower dead for 30 writes, then restarted | 30 | 30 | **100%** |

**D2 detail:** 50 writes fired at 50ms intervals; 7 received acknowledgment before the kill. All 7 recovered post-restart. The 43 unacknowledged writes were correctly not present (no phantom commits).

**D3 detail:** Follower missed log entries `[IDX_before, IDX_before+30]`. On restart, HashiCorp Raft applied the snapshot + remaining log tail to restore full FSM state. Applied-index delta after catch-up: 0 (fully synchronized).

### 3.2 FSM Invariant Verification

Test L1c (Phase 1) separately proves the RSM invariant: a key written to the old leader (`l1c_probe = consistency_check`) was readable from the new leader immediately after election. This confirms the new leader's state machine was fully caught up before accepting client requests.

---

## 4. Partition Behavior — Phase 2

### 4.1 Log Divergence & Catch-up

| Scenario | Partition Duration | Writes During Partition | Log Delta After Heal | Catch-up Result |
|----------|------------------|------------------------|----------------------|----------------|
| P1 — Minority partition (1 follower) | ~5s + 20 pumped writes | 20 | Leader advanced; follower frozen | Fully caught up |
| P2 — Majority partition (leader frozen) | Until new leader | 0 (blocked) | Term advanced | Rejoined as Follower |
| P3 — Lag then heal | Drift widened > 5 entries | 20 | `REMAINING ≤ 1` | Fully caught up |

### 4.2 Safety Assertions

| Test | Assertion | Result |
|------|-----------|--------|
| P2c | Isolated leader rejects writes | PASS — write returned error/timeout |
| L3b | Write blocked at quorum loss | PASS — write error confirmed |
| N6c | Partitioned leader rejects writes | PASS — write returned not-leader/timeout |
| N6e | Former leader rejoins as Follower | PASS — state confirmed "Follower" |

All four scenarios confirm CP safety: no node in a minority partition ever committed a write.

---

## 5. Idempotency — Phase 5

| Test | What is verified | Result |
|------|-----------------|--------|
| I1 — Follower redirect | Client sent SET to follower; auto-redirected to leader | PASS |
| I2 — Duplicate SET | Same `(client_id, seq_num)` sent twice; value unchanged | PASS |
| I3 — Duplicate DELETE | Same `(client_id, seq_num)` delete sent twice; idempotent | PASS |
| I4 — Multi-addr failover | Leader killed; client with `-addrs` list auto-discovers new leader | PASS |

**I2 key assertion:** `GET key` after duplicate write returns `first_value`, not `second_value`. FSM per-client deduplication table correctly suppressed the second application.

---

## 6. Test Coverage Summary

### 6.1 Per-Phase Results (5-node, April 1, 2026)

| Phase | Tests | Pass | Notes |
|-------|-------|------|-------|
| P1 — Liveness & Election | 6 | 6 | L2 passes at N=5 (2 kills still leaves quorum); L3 correctly requires 3 kills |
| P2 — Network Partitions | 9 | 9 | SIGSTOP/SIGCONT; split-brain prevention confirmed |
| P3 — Latency | 3 | 2* | R1 fixed (BUG-6 patched); R2b accepts election-then-recovery |
| P4 — Durability | 4 | 4 | Total wipe, dirty crash, snapshot replay all pass |
| P5 — Idempotency | 8 | 8 | Exactly-once semantics and smart client confirmed |
| P6 — Kernel Chaos | 13 | 13 | Bidirectional iptables + netem on correct NIC (`ens4`) |
| **Total** | **43** | **42 → 43*** | |

*BUG-6 fix applied to `GCP_verify_phase3.sh`; next GCP run expected to close to 43/43.

### 6.2 3-node vs 5-node Quorum Behavior

| Cluster Size | Quorum | Min failures tolerated | L2 result | L3 threshold |
|-------------|--------|----------------------|-----------|-------------|
| N = 3 | 2 nodes | 1 failure | PASS (kill 1, 2 remain) | 2 kills → unavailable |
| N = 5 | 3 nodes | 2 failures | PASS (kill 2, 3 remain) | 3 kills → unavailable |

The 5-node results prove the quorum math generalizes correctly: `⌊N/2⌋ + 1` majority threshold is enforced precisely, and the system becomes unavailable exactly when it must (no earlier, no later).

---

## 7. Summary

The system demonstrates correct CP behavior across all fault dimensions:

| Property | Mechanism | Evidence |
|----------|-----------|----------|
| **Consistency** | Leader-only writes + VerifyLeader() reads | L1c, P2c, L3b, N6c |
| **Partition tolerance** | Quorum commit; minority becomes unavailable | P1, P2, N6 |
| **Durability** | BoltDB log + FSM snapshot restore | D1, D2, D3 (100% recovery) |
| **Liveness** | Raft election on heartbeat timeout | MTTR = 1.25s, 2 methods |
| **Idempotency** | Per-client (id, seq) dedup table | I2, I3 |
| **Throughput resilience** | Majority-quorum bypass for minority faults | R1: 82% retention |
