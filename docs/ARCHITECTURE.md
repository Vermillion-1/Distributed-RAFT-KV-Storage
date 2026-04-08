# System Architecture: Design Defense

This document covers the core architectural decisions in the Distributed Raft KV Store (v1.3, 37/37 GCP tests).

---

## 1. Core Principles: Decentralization and Quorum

Every GCP VM runs the same `kv-store` binary. No node permanently "owns" the data; instead, nodes form a quorum and elect a temporary leader.

- **Leader election:** If the current leader stops sending heartbeats (500ms timeout), followers start an election. The new leader is elected within ~750ms — total MTTR ~1.25s.
- **Quorum commit:** A write is acknowledged only after `⌊N/2⌋ + 1` nodes confirm it. At N=3, that is 2 nodes. At N=5 (also tested and verified), that is 3 nodes.
- **Linearizability:** Every read calls `VerifyLeader()` before returning data. A deposed leader that cannot reach the majority refuses reads rather than return stale data.

**v1.3 addition:** Follower reads (Read-Index protocol) allow followers to serve linearizable reads locally, without routing every GET to the leader. Opt-in via `-follower-read` on the kv-client.

---

## 2. Networking Design: Dual-Port Pattern

Each replica exposes two distinct port ranges:

| Port range | Protocol | Traffic |
|------------|----------|---------|
| 12000–12004 | Raft TCP | `AppendEntries`, `RequestVote`, `InstallSnapshot`, heartbeats |
| 50051–50055 | gRPC | `Get`, `Set`, `Delete`, `Health`, `Join` — client-facing |

**Why two port ranges:** Fault injection can target one traffic type independently of the other. For example, a network partition scoped to the Raft ports simulates a pure consensus disruption, while netem delay applied to the full NIC affects client-observable latency end-to-end.

Note: netem is applied to the full NIC (`tc qdisc add dev ens4 root netem delay Xms`), not scoped to individual ports. Port-scoped netem was an early bug (BUG-5) — it only delayed Raft traffic while leaving client gRPC traffic unaffected, causing latency tests to measure the wrong thing.

---

## 3. Persistence: Write-Ahead Log and Snapshots

**BoltDB** provides durable on-disk storage for both the Raft log and the KV state machine:

- **Durability:** Log entries are `fsync`'d to at least `⌊N/2⌋ + 1` disks before the write is acknowledged. The system survives total cluster restarts with 100% key recovery (verified by D1).
- **Snapshots:** A binary snapshot of the full FSM state (KV map + idempotency table) is taken every 10 committed log entries (`SnapshotThreshold=10`). This bounds log replay on restart and enables fast follower catch-up via `InstallSnapshot` RPC.

---

## 4. Fault Model and Resilience

The system handles crash-stop and network faults. Byzantine faults (malicious nodes) are out of scope.

| Fault Type | Mitigation | Verified by |
|------------|------------|-------------|
| Crash failure (SIGKILL) | BoltDB durability + Raft election | Phase 1 (L1), Phase 4 (D1, D2) |
| Network partition | Quorum commit; minority partition becomes unavailable | Phase 2 (P2a, P2c), Phase 6 (N6a–N6f) |
| Message latency | Configurable heartbeat/election timeouts; quorum bypass for slow followers | Phase 3 (R1, R2) |
| Split-brain | Raft term numbers prevent two simultaneous leaders | Phase 6 (N6c) |
| Process freeze (SIGSTOP) | Heartbeat timeout fires; election promotes a new leader | Phase 1 (L3b) |
| Duplicate writes (retry after failover) | Per-client `(client_id, seq_num)` dedup table in FSM | Phase 5 (I2, I3) |

---

## 5. GCP Deployment

The cluster is deployed across two availability zones (`us-central1-a` and `us-central1-c`) on e2-micro VMs. Cross-zone latency is approximately 15ms RTT, which informed the heartbeat/election timeout configuration (500ms heartbeat, 750ms election).

Both N=3 (majority=2) and N=5 (majority=3) configurations have been fully validated with the 6-phase test suite (37/37, April 4, 2026).

The sidecar agent (`node-agent`) runs alongside the replica on each VM and handles fault injection at the OS level — independently of the application — so the system under test cannot accidentally bypass or detect the fault injection.
