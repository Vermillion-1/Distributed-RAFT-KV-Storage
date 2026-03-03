# 5f — `cmd/chaos/main.go` (kv-chaos TCP Proxy)

**File:** `store/cmd/chaos/main.go`  
**Role:** A lightweight TCP proxy that sits between a client and a target port. Injects faults by randomly dropping connections (omission) or adding latency (delay). Used by the dashboard to simulate client-facing failures.

---

## How It Works

```
kv-client ──TCP──▶ [kv-chaos :22000] ──TCP──▶ [kv-store :50051]
```

For each incoming TCP connection, it decides:
1. **Drop** — if `rand.Float64() < dropRate`, close the connection immediately (simulates packet loss / receive omission)
2. **Delay** — if `delayMs > 0`, sleep for a random duration up to `delayMs` milliseconds before forwarding
3. **Forward** — otherwise, open a connection to the target and `io.Copy` bidirectionally

---

## CLI Flags

| Flag | Default | Purpose |
|---|---|---|
| `-listen` | `127.0.0.1:12001` | Port the proxy listens on |
| `-target` | `127.0.0.1:12000` | Port it forwards to |
| `-drop` | `0.0` | Probability (0.0–1.0) of dropping each connection |
| `-delay` | `0` | Max random delay in ms added to each forwarded connection |

---

## Critical Limitation

> **The proxy only intercepts the client-facing gRPC port, NOT the Raft inter-node port.**

The dashboard calls `StartChaosProxy(nodeID, dropRate, delayMs)` which starts the proxy targeting `np.config.GRPCAddr` (`:50051`). The Raft transport uses `np.config.RaftAddr` (`:12000`) directly — the proxy has no visibility into Raft traffic.

**What this means:**
- ✅ Models **client-side receive/send omission** (requests to `kv-client` are dropped/delayed)
- ❌ Cannot model **network partitions** at the Raft level (use SIGSTOP for that)

---

## Connection Handling

```go
func handleConnection(clientConn, targetAddr, dropRate, delayMs) {
    if rand.Float64() < dropRate {
        clientConn.Close()  // DROP — connection refused
        return
    }
    if delayMs > 0 {
        time.Sleep(rand.Intn(delayMs) * time.Millisecond)  // random delay up to max
    }
    targetConn := net.Dial("tcp", targetAddr)
    // Bidirectional copy — transparent proxy
    go io.Copy(targetConn, clientConn)
    go io.Copy(clientConn, targetConn)
}
```
