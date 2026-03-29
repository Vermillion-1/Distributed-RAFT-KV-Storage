# 5d — `proto/kv.proto` (Service Definition)

**File:** `store/proto/kv.proto`  
**Role:** Defines the gRPC service contract between all components — `kv-client`, `kv-store` nodes, and the dashboard's health polling. Compiled to `proto/kv.pb.go` and `proto/kv_grpc.pb.go` via `protoc`.

---

## Service Definition

```protobuf
service KVStore {
  rpc Get(GetRequest)       returns (GetResponse);
  rpc Set(SetRequest)       returns (SetResponse);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
  rpc Join(JoinRequest)     returns (JoinResponse);
  rpc Health(HealthRequest) returns (HealthResponse);
}
```

---

## Messages

### `Get` — Read a key
```protobuf
message GetRequest  { string key = 1; }
message GetResponse {
  string value       = 1;
  bool   found       = 2;
  string leader_addr = 3;  // Non-empty if this node is not the leader → client should redirect
}
```

### `Set` — Write a key (idempotent)
```protobuf
message SetRequest {
  string key          = 1;
  string value        = 2;
  string client_id    = 3;  // UUID per kv-client instance
  uint64 sequence_num = 4;  // Monotonically increasing per client — prevents duplicate writes
}
message SetResponse {
  bool   success     = 1;
  string leader_addr = 2;  // Redirect hint if not leader
}
```

### `Delete` — Remove a key (idempotent)
```protobuf
message DeleteRequest {
  string key          = 1;
  string client_id    = 2;
  uint64 sequence_num = 3;
}
message DeleteResponse {
  bool   success     = 1;
  string leader_addr = 2;
}
```

### `Join` — Add a node to the cluster
```protobuf
message JoinRequest {
  string node_id   = 1;
  string raft_addr = 2;  // Raft consensus port (:12000+N)
  string grpc_addr = 3;  // Client API port (:50051+N)
}
message JoinResponse {
  bool   success     = 1;
  string leader_addr = 2;  // Redirect to leader if this node can't process Join
}
```

### `Health` — Node status (polled by dashboard every 1s)
```protobuf
message HealthRequest {}
message HealthResponse {
  string  node_id       = 1;
  string  state         = 2;  // "Leader", "Follower", or "Candidate"
  string  leader_addr   = 3;  // Current leader's gRPC address
  uint64  applied_index = 4;  // Last Raft log entry applied to FSM
  uint32  num_peers     = 5;  // Number of nodes in the cluster config
}
```

---

## Idempotency Design

`Set` and `Delete` include `client_id` + `sequence_num`. The FSM tracks the last applied `sequence_num` per `client_id` in `lastApplied map[string]uint64`. If a retried request arrives with the same or older sequence number, the FSM skips it silently.

This prevents **duplicate mutations** when a client retries after a timeout — the write may have already been committed even if the response was lost.

---

## Known Limitation

`HealthResponse` does **not expose the Raft term number**. The dashboard currently displays `num_peers` labelled as "Raft Term", which is inaccurate. To fix: add `uint64 term = 6;` and populate it from `node.raft.Stats()["last_log_term"]`.
