# 🗺️ Codebase Tour

Read this while the cluster is running at http://localhost:8080. Open files alongside this guide.

---

## Start Here: The Full Picture

```
Write request → kv-client → kv-store (gRPC :50051) → Raft log → FSM (KV map)
                                  ↑
                           kv-dashboard starts and manages all kv-store processes
                                  ↑
                           kv-chaos proxy can sit in front of :50051 to inject faults
```

---

## File 1: `main.go` — The Node Entrypoint

Open this first. It's short (~120 lines) and shows you the startup flow.

**What to notice:**
```go
nodeID := flag.String("id", "node0", "Node ID")   // command line flags
n, err := server.NewNode(...)   // creates the Raft node
n.StartGRPCServer()             // starts listening for client requests
n.Bootstrap()                   // OR joinCluster() — two startup modes
```

**The join flow:** if `-join` is set, this node sends a `Join` gRPC call to the existing cluster. If that node isn't the leader, it returns `leader_addr`. Then this repeats at the leader. This is the redirect pattern.

---

## File 2: `server/node.go` — The Heart

This is the most important file. ~350 lines.

**What to notice:**
```go
config.HeartbeatTimeout = 200 * time.Millisecond   // miss 5 of these = start election
config.ElectionTimeout  = 200 * time.Millisecond   // why MTTR ≈ 1s
```

Every gRPC handler checks if it's the leader:
```go
if m.raft.State() != raft.Leader {
    // tell the client to go to the leader instead
    return ..., "not leader, try grpc_of_leader"
}
```

For writes, it goes through Raft:
```go
f := m.raft.Apply(commandJSON, timeout)  // blocks until quorum commits
```

---

## File 3: `server/fsm.go` — The KV Map

This is what Raft actually updates. It implements 3 methods:

- `Apply(log)` — called after every committed Raft entry. Reads the command and updates the map.
- `Snapshot()` — periodically saves the whole map to disk (for fast recovery)
- `Restore(snapshot)` — called on startup if a snapshot exists

**Key insight about idempotency:**
```go
// Every Set/Delete includes clientID + sequenceNum
// The FSM stores the last seqNum per client
if seqNum <= lastApplied[clientID] {
    return  // already applied — skip duplicate
}
```

This prevents a retry from writing the same value twice.

---

## File 4: `proto/kv.proto` — The Contract

All communication between `kv-client`, `kv-store`, and `kv-dashboard` uses gRPC. The `.proto` file defines what messages exist.

You won't need to change this, but you should know: **every field you see in the dashboard (state, applied_index, etc.) comes from the `HealthResponse` message**.

---

## File 5: `cmd/chaos/main.go` — The Fault Proxy

Only 77 lines. Read it in full.

**Critical limitation to understand early:**  
The proxy intercepts connections at `:22000` and forwards them to `:50051` (client gRPC).  
It does **NOT** touch the Raft port (`:12000`).  
→ Chaos proxy = client-side omission only. Not a real network partition.  
→ Real partitions use `SIGSTOP` (`/api/pause`).

---

## File 6: `cmd/dashboard/main.go` — The Controller

This is where all the magic happens. ~480 lines.

**`Manager` struct** — owns all node processes:
```go
type Manager struct {
    nodes   map[string]*nodeProcess    // nodeID → OS process
    proxies map[string]*ChaosProxy     // nodeID → chaos proxy process
    events  []Event                    // timestamped log shown in dashboard
}
```

**`/api/cluster` handler** — polls every node's gRPC Health endpoint every time the frontend asks:
```go
resp, _ := client.Health(ctx, &pb.HealthRequest{})
// packages state + applied_index into JSON
```

**New this session — `/api/pause` and `/api/resume`:**
```go
np.cmd.Process.Signal(syscall.SIGSTOP)  // freeze it
np.cmd.Process.Signal(syscall.SIGCONT)  // unfreeze it
```

---

## File 7: `cmd/dashboard/index.html` — The Frontend

Single HTML file. The JavaScript at the bottom polls `/api/cluster` every second and:
1. Redraws the canvas (circle of nodes, gold=Leader, blue=Follower, red=Dead)
2. Updates the metrics panel (Leader name, Applied Index, Alive count, MTTR)
3. Appends new events to the event log
4. Refreshes the node selector dropdown

All 10 buttons call `apiPost(url, message)` which sends a POST and logs the result.

---

## File 8: `verify.sh` + `verify_phase2.sh` — The Test Scripts

These are shell scripts that simulate a QA engineer:
1. Call the dashboard API to inject failures
2. Poll `/api/cluster` to check the outcome
3. Print `✅ PASS` or `❌ FAIL` with a reason

Read the top of `verify.sh` to understand the helper functions (`leader()`, `node_applied()`, `pause_node()`, etc.) — you'll reuse all of them in Phase 3.

---

## Things to Try Right Now

With the cluster running at http://localhost:8080:

```bash
# 1. Kill the leader — watch it recover
curl -X POST http://localhost:8080/api/kill/node0

# 2. Check who the new leader is
curl -s http://localhost:8080/api/cluster | python3 -c \
  "import sys,json; d=json.load(sys.stdin); print([n for n in d['nodes'] if n.get('state')=='Leader'])"

# 3. Restart node0 and watch it rejoin
curl -X POST http://localhost:8080/api/restart/node0

# 4. Write a key, kill a node, read the key — data should still be there
./kv-client -cmd set -key test -val hello -addr 127.0.0.1:50051
curl -X POST http://localhost:8080/api/kill/node1
./kv-client -cmd get -key test -addr 127.0.0.1:50051   # should still return "hello"
```
