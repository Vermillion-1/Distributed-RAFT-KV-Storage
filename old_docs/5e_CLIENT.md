# 5e — `cmd/client/main.go` (kv-client CLI)

**File:** `store/cmd/client/main.go`  
**Role:** Command-line tool for interacting with the kv-store cluster. Supports `get`, `set`, `delete`, and `health` operations. Handles leader redirects and retries automatically.

---

## CLI Flags

```bash
./kv-client -cmd set -key hello -val world -addr 127.0.0.1:50051
```

| Flag | Default | Purpose |
|---|---|---|
| `-cmd` | `get` | Operation: `get`, `set`, `delete`, `health` |
| `-key` | `""` | Key to operate on (required for get/set/delete) |
| `-val` | `""` | Value to set (required for set) |
| `-addr` | `127.0.0.1:50051` | gRPC address of any cluster node |

---

## Idempotency

Each `kv-client` instance generates a UUID on startup and maintains an atomic sequence counter:

```go
var (
    clientID   = uuid.New().String()  // unique per process run
    sequenceNo uint64
)
// Each write increments: seqNum := atomic.AddUint64(&sequenceNo, 1)
```

This `clientID` + `sequenceNum` pair is included in every `Set` and `Delete` request. If a write is retried (e.g. after a timeout), the server-side FSM detects the duplicate and skips re-applying it.

---

## Retry + Leader Redirect Loop

```go
for i := 0; i < 5; i++ {
    success, leaderAddr := sendRequest(cmd, key, val, addr)
    if success { return }
    if leaderAddr != "" {
        addr = leaderAddr  // follow the redirect
        continue
    }
    time.Sleep(1 * time.Second)  // transient error, wait and retry
}
```

This means you can point `kv-client` at **any node** — it will automatically be redirected to the current leader within 1–2 hops.

---

## Timeout as Circuit Breaker

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
```

Each RPC has a hard 2s timeout. If a node is frozen (SIGSTOP) or dead, the client gives up after 2s and retries elsewhere. This prevents a single slow node from blocking the client indefinitely.

---

## Usage Examples

```bash
# Write a key
./kv-client -cmd set -key foo -val bar -addr 127.0.0.1:50051

# Read a key
./kv-client -cmd get -key foo -addr 127.0.0.1:50052  # any node works

# Delete a key
./kv-client -cmd delete -key foo -addr 127.0.0.1:50051

# Check node health
./kv-client -cmd health -addr 127.0.0.1:50051
# Output: Node: node0 | State: Leader | Leader: 127.0.0.1:50051 | Applied: 42 | Peers: 3

# Route through chaos proxy (for Phase 3 latency/drop tests)
./kv-client -cmd set -key test -val val -addr 127.0.0.1:22000
```
