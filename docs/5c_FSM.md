# 5c — `server/fsm.go` (Finite State Machine)

**File:** `store/server/fsm.go`  
**Role:** Implements `raft.FSM` — the interface HashiCorp Raft calls to apply committed log entries to application state. Contains the actual key-value map, idempotency tracking, and snapshot logic.

---

## KVStore Struct

```go
type KVStore struct {
    mu          sync.RWMutex
    m           map[string]string  // The actual KV data
    Peers       map[string]string  // raftAddr → grpcAddr (for redirect)
    lastApplied map[string]uint64  // clientID → last seq num (idempotency)
}
```

---

## FSM Interface Methods

### `Apply(log *raft.Log) interface{}`
Called by Raft **on every node** after a log entry is committed by quorum. This is how writes flow into the KV map.

```
Raft commits entry
    └── FSM.Apply(log) called on ALL nodes
            └── Unmarshal command JSON { op, key, val, clientID, seqNum }
            └── Check idempotency: if seqNum <= lastApplied[clientID] → skip (duplicate)
            └── Execute: Set / Delete / RegisterNode
            └── Update lastApplied[clientID] = seqNum
```

Idempotency prevents a retried write (e.g. after a timeout + redirect) from being applied twice.

### `Snapshot() (raft.FSMSnapshot, error)`
Called periodically by Raft to compact the log. Serializes the entire KV map + peers map to JSON.

```go
// Produces a point-in-time snapshot of all state
return &kvSnapshot{data: json.Marshal(s.m, s.Peers)}, nil
```

### `Restore(snapshot io.ReadCloser) error`
Called on startup if a snapshot is newer than the log. Deserializes the JSON snapshot back into the KV map.

```go
// Replaces in-memory state entirely from snapshot
json.Unmarshal(snapshot) → s.m = newMap
```

---

## Command Format (JSON in Raft log)

Every write that goes through Raft is encoded as:

```json
{
  "op":       "set",
  "key":      "hello",
  "value":    "world",
  "clientID": "client-uuid",
  "seqNum":   42
}
```

Operations: `"set"`, `"delete"`, `"register_node"` (for cluster membership).

---

## Why Not Read from Followers?

`Get` is not implemented in the FSM — it's handled directly in `node.go` by reading `fsm.m` only when the node is confirmed leader. Followers could have stale state between heartbeats, so reads always route to the leader.
