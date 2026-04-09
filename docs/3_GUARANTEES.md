#  Consistency Guarantees

This system is a **CP** system in the CAP theorem that chooses **Consistency** over Availability when a network partition reduces available nodes below quorum.

---

## Proven Guarantees (Empirically Verified)

### G1 — Linearizability (Strong Consistency)
Every read reflects the most recent write that was acknowledged. Reads always go through the leader's committed log, never stale follower state.

> **How:** `kv-store` followers redirect all `Get` requests to the leader. Writes are only acknowledged after quorum commits.

---

### G2 — Leader Election Liveness (MTTR ≈ 1s)
After a leader failure, the cluster elects a new leader in approximately **1 second**, regardless of cluster size.

| Cluster Size | Fault Tolerance | Measured MTTR |
|---|---|---|
| 3 nodes | f = 1 | **~1s** |
| 11 nodes | f = 5 | **~1s** |

> **Why ~1s?** The Raft heartbeat timeout is set to `200ms`. A follower starts an election after `~5 missed heartbeats × 200ms = ~1s`. The election itself resolves in one round-trip.

---

### G3 — Safety Under Quorum Loss (No Split-Brain)
When fewer than `⌊N/2⌋ + 1` nodes are alive, **no node can become leader**. The cluster stops accepting writes rather than risk inconsistency.

```
3-node cluster: kill 2 nodes → 0 leaders. 
11-node cluster: kill 6 nodes → 0 leaders. 
```

This was verified exactly at the quorum boundary in cascading failure tests — the cluster continued operating through kills 1 through (f), then entered safety mode at kill (f+1).

---

### G4 — Partition Safety (No Dual-Leader)
When the leader is network-partitioned (via `SIGSTOP`):
- The frozen leader cannot send heartbeats
- Followers time out → elect a new leader
- The old leader (frozen) cannot hear the new election; when unfrozen it rejoins as a **Follower**, never as a competing leader

> Raft's **term numbers** enforce this: the new election bumps the term. When the old leader is unfrozen, it sees the higher term and immediately steps down.

---

### G5 — Durability (Log Persistence)
All committed entries are written to **BoltDB** on disk before being acknowledged. On restart, nodes replay from their persisted log.

```
/tmp/raft-kv/nodeN/
├── raft-log.bolt   ← all committed Raft entries
├── stable.bolt     ← current term + vote
└── snapshots/      ← periodic compacted snapshots
```

---

## Known Limitations

| Limitation | Impact |
|---|---|
| `kv-chaos` proxy only covers client gRPC port, not Raft port | Chaos proxy cannot simulate Raft-level network partitions (use SIGSTOP instead) |
| No TLS | All gRPC is plaintext — not for production use |
| Single-region | No cross-datacenter replication |
| No read scaling | All reads go through leader; followers are write-only replicas |
| Raft term not exposed in Health RPC proto | Dashboard displays peer count instead of actual term number |
