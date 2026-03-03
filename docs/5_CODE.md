# 📁 Code Overview

This document is broken into sub-files for readability. Each covers one key file in depth.

## File Map

```
store/
├── main.go                       → [5a] Node entrypoint & cluster join logic
├── server/
│   ├── node.go                   → [5b] Raft setup, gRPC server, all RPC handlers
│   └── fsm.go                    → [5c] Finite State Machine: KV map + snapshots + idempotency
├── proto/
│   └── kv.proto                  → [5d] Protobuf service definition (Get/Set/Delete/Join/Health)
├── cmd/
│   ├── client/main.go            → [5e] kv-client CLI tool
│   ├── chaos/main.go             → [5f] kv-chaos TCP proxy (drop/delay)
│   └── dashboard/
│       ├── main.go               → [5g] Dashboard HTTP server + Manager + all API handlers
│       └── index.html            → [5h] Frontend: canvas, polling, chaos controls
├── verify.sh                     → [5i] Phase 1 test script walkthrough
└── verify_phase2.sh              → [5j] Phase 2 test script walkthrough
```

## Sub-Documents

| File | Topic |
|---|---|
| [5a_NODE_ENTRYPOINT.md](5a_NODE_ENTRYPOINT.md) | `main.go` — flags, bootstrap vs join, graceful shutdown |
| [5b_RAFT_NODE.md](5b_RAFT_NODE.md) | `server/node.go` — Raft config, transport, storage, gRPC handlers |
| [5c_FSM.md](5c_FSM.md) | `server/fsm.go` — FSM Apply, Snapshot, Restore, idempotency |
| [5d_PROTO.md](5d_PROTO.md) | `proto/kv.proto` — service definition and message types |
| [5e_CLIENT.md](5e_CLIENT.md) | `cmd/client/main.go` — CLI flags, RPC calls, redirect handling |
| [5f_CHAOS_PROXY.md](5f_CHAOS_PROXY.md) | `cmd/chaos/main.go` — TCP proxy, drop rate, delay, limitations |
| [5g_DASHBOARD_BACKEND.md](5g_DASHBOARD_BACKEND.md) | `cmd/dashboard/main.go` — Manager, all HTTP handlers, SIGSTOP/SIGCONT |
| [5h_DASHBOARD_FRONTEND.md](5h_DASHBOARD_FRONTEND.md) | `cmd/dashboard/index.html` — canvas renderer, polling loop, chaos buttons |
