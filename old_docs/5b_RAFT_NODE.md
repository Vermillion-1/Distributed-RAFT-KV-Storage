# 5b — `server/node.go` (Raft Node + gRPC Handlers)

**File:** `store/server/node.go`  
**Role:** The heart of the system. Sets up HashiCorp Raft, starts the gRPC server, and implements all client-facing RPCs (`Get`, `Set`, `Delete`, `Join`, `Health`).

---

## Node Struct

```go
type Node struct {
    pb.UnimplementedKVStoreServer
    raft          *raft.Raft        // HashiCorp Raft instance
    fsm           *KVStore          // The state machine (in-memory KV map)
    nodeID        string
    raftAddr      string            // :12000+N — Raft TCP port
    grpcAddr      string            // :50051+N — client gRPC port
    server        *grpc.Server
    logStore      raft.LogStore     // BoltDB — Raft log entries
    stableStore   raft.StableStore  // BoltDB — term + vote
    snapshotStore raft.SnapshotStore // File system — periodic snapshots
}
```

---

## Raft Configuration (Tuned for Fast Simulation)

```go
config.HeartbeatTimeout  = 200ms   // How long follower waits before starting election
config.ElectionTimeout   = 200ms   // How long candidate waits for votes
config.CommitTimeout     = 50ms    // How quickly leader commits after quorum
config.LeaderLeaseTimeout = 150ms  // How long leader assumes it's still leader
```

> These values are intentionally fast (vs Raft default of 1s) to make MTTR ≈ 1s in tests.

---

## Storage Setup (per node)

```
/tmp/raft-kv/nodeN/
├── raft-log.bolt    ← BoltDB: all Raft log entries (commands)
├── stable.bolt      ← BoltDB: current term, last voted-for server
└── snapshots/       ← FileSnapshotStore: compacted state snapshots
```

All three are required for crash recovery. On restart, the Raft library replays `raft-log.bolt` unless a more recent snapshot exists.

---

## gRPC RPC Handlers

### `Set` (write)
```
Client → Set(key, val) → node
    └── If not leader → return error with LeaderAddr (client redirects)
    └── If leader:
            marshal command as JSON → raft.Apply(cmd, timeout)
            wait for FSM.Apply() to commit → return success
```
Writes block until **quorum of nodes** have written the entry to their logs.

### `Get` (read)
```
Client → Get(key) → node
    └── If not leader → return LeaderAddr for redirect
    └── If leader → fsm.Get(key) → return value
```
Reads always go to the leader for linearizability. No stale reads from followers.

### `Delete`
Same flow as `Set` — goes through Raft log for consistency.

### `Join`
```
New node → Join(nodeID, raftAddr, grpcAddr) → existing node
    └── If not leader → return LeaderAddr
    └── If leader:
            raft.AddVoter(nodeID, raftAddr)   ← adds to Raft cluster config
            fsm.RegisterNode(raftAddr, grpcAddr) ← stores gRPC addr for redirects
```

### `Health`
```
Any caller → Health() → node
    Returns: state (Leader/Follower/Candidate), leaderAddr, appliedIndex, numPeers
```
Called every second by the dashboard to populate the cluster state.

---

## Leader Redirect Pattern

Every write RPC (Set, Delete, Join) that arrives at a non-leader node:
```go
if m.raft.State() != raft.Leader {
    leaderAddr := string(m.raft.Leader())        // Raft addr of leader
    grpcAddr := m.fsm.GetGrpcAddr(leaderAddr)   // Map Raft→gRPC addr
    return nil, status.Errorf(codes.FailedPrecondition,
        "not leader, try %s", grpcAddr)
}
```
`kv-client` and `main.go`'s `joinCluster()` both handle this redirect automatically.
