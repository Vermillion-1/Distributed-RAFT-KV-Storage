# 5g — `cmd/dashboard/main.go` (Dashboard HTTP Backend)

**File:** `store/cmd/dashboard/main.go`  
**Role:** The command-and-control server. Spawns all `kv-store` node processes, tracks their state, serves the frontend, and exposes a REST API for chaos control.

---

## Key Types

```go
// Static config per node (from startup)
type NodeConfig struct {
    ID       string `json:"id"`
    RaftAddr string `json:"raft_addr"`   // :12000+N
    GRPCAddr string `json:"grpc_addr"`   // :50051+N
    DataDir  string `json:"data_dir"`
}

// Live state per node (polled every second via gRPC Health RPC)
type NodeState struct {
    State       string `json:"state"`         // Leader / Follower / Candidate
    LeaderAddr  string `json:"leader_addr"`
    AppliedIndex uint64 `json:"applied_index"`
    NumPeers    int    `json:"num_peers"`
    Alive       bool   `json:"alive"`
}

// Manager owns all nodes + their processes
type Manager struct {
    mu      sync.Mutex
    nodes   map[string]*nodeProcess   // nodeID → process + config
    proxies map[string]*ChaosProxy    // nodeID → chaos proxy process
    events  []Event                   // timestamped event log
    binDir  string                    // where kv-store, kv-chaos binaries live
}
```

---

## Startup Sequence

```
main()
  ├── Parse -nodes, -port flags
  ├── mgr := NewManager(binDir)
  ├── for i := 0; i < nodes; i++:
  │       mgr.startNode(nodeConfig{id: nodeN, raftAddr: :12000+N, grpcAddr: :50051+N})
  │           └── exec.Command("kv-store", flags...) → cmd.Start()
  │           └── if N > 0: call Join RPC to node0 (with retry)
  ├── Register all HTTP handlers on mux
  └── http.ListenAndServe(:8080, mux)
```

---

## All API Endpoints

| Method | Path | What it does |
|---|---|---|
| `GET` | `/` | Serve `index.html` |
| `GET` | `/api/cluster` | Poll all nodes via Health gRPC, return JSON state |
| `POST` | `/api/kill/:id` | `SIGKILL` the node process |
| `POST` | `/api/restart/:id` | Kill + re-exec the node process |
| `POST` | `/api/pause/:id` | **`SIGSTOP`** — freeze process (partition simulation) |
| `POST` | `/api/resume/:id` | **`SIGCONT`** — unfreeze process (heal partition) |
| `POST` | `/api/chaos/drop/:id?rate=` | Start kv-chaos proxy with drop rate |
| `POST` | `/api/chaos/delay/:id?ms=` | Start kv-chaos proxy with delay |
| `POST` | `/api/chaos/stop/:id` | Kill the chaos proxy for a node |
| `GET` | `/api/logs/:id` | Read last 8KB of node's stderr log |
| `POST` | `/api/run-tests` | Execute `run_chaos_test.sh`, return output |

---

## SIGSTOP / SIGCONT (new this session)

```go
func (m *Manager) PauseNode(nodeID string) error {
    np.cmd.Process.Signal(syscall.SIGSTOP)  // freeze all I/O on the process
}

func (m *Manager) ResumeNode(nodeID string) error {
    np.cmd.Process.Signal(syscall.SIGCONT)  // unfreeze, all state intact
}
```

`SIGSTOP` is the correct way to simulate a network partition because it freezes the entire process — both the gRPC port and the Raft port go silent simultaneously. Other nodes timeout waiting for heartbeats and elect a new leader. On `SIGCONT`, the node resumes with all in-memory state intact and catches up via Raft log replication.

---

## Chaos Proxy Port Assignment

```go
// First proxy started after clean startup → port 22000
// Second proxy → port 22001 (if first still running)
listenPort := 22000 + len(m.proxies)
```

To route `kv-client` through the proxy, use `-addr 127.0.0.1:22000`.
