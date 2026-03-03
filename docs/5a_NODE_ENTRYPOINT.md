# 5a — `main.go` (Node Entrypoint)

**File:** `store/main.go`  
**Role:** Entry point for a single `kv-store` node process. Parses flags, sets up the node, and handles two startup modes: **bootstrap** (first node) or **join** (subsequent nodes).

---

## CLI Flags

| Flag | Default | Purpose |
|---|---|---|
| `-id` | `node0` | Unique node identifier used by Raft as `LocalID` |
| `-raft` | `127.0.0.1:12000` | TCP address for Raft inter-node communication |
| `-grpc` | `127.0.0.1:50051` | TCP address for the gRPC client API |
| `-data` | `/tmp/raft-kv/node0` | Directory for BoltDB log, stable store, and snapshots |
| `-join` | `""` | gRPC address of an existing node to join. Empty = bootstrap |

---

## Startup Flow

```
Parse flags
    │
    ├── server.NewNode(id, raftAddr, grpcAddr, dataDir, peers)
    │       └── Sets up Raft, BoltDB, TCP transport (see 5b)
    │
    ├── n.StartGRPCServer()
    │       └── Binds grpcAddr, registers KVStoreServer
    │
    ├── if join == ""
    │       └── n.Bootstrap()  ← First node: create single-node cluster
    │
    └── else
            └── joinCluster(join, id, raftAddr, grpcAddr)  ← Ask leader to add us
```

---

## Bootstrap vs Join

**Bootstrap** (`-join` is empty):
- Calls `raft.BootstrapCluster` with a single-server configuration
- This node starts as both candidate and leader
- All subsequent nodes must `-join` this node

**Join** (`-join=<leader-grpc-addr>`):
- Calls `joinCluster()` which sends a `Join` gRPC RPC to the target node
- Retries up to 10 times with 1s delay
- If the target is not the leader, it redirects (returns `leader_addr`) and we retry at the leader

---

## joinCluster() — Redirect Logic

```go
resp, err := c.Join(ctx, &pb.JoinRequest{
    NodeId:   nodeID,
    RaftAddr: raftAddr,
    GrpcAddr: grpcAddr,
})
if !resp.Success && resp.LeaderAddr != "" {
    return joinCluster(resp.LeaderAddr, ...)  // recursive redirect to leader
}
```

This allows any node's address to be used as the `-join` target — it will always find its way to the leader.

---

## Graceful Shutdown

```go
terminate := make(chan os.Signal, 1)
signal.Notify(terminate, os.Interrupt, syscall.SIGTERM)
<-terminate
n.Stop()
```

`n.Stop()` shuts down the Raft instance and gRPC server cleanly, allowing the node to be restarted and rejoin the cluster.
