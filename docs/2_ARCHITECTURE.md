# 🏗️ Architecture

## Overview

This project is a **fault-tolerant, distributed key-value store** built on the [HashiCorp Raft](https://github.com/hashicorp/raft) consensus library. It is accompanied by a **Chaos Testing Dashboard** that allows live injection of failures (crashes, network partitions, latency, packet loss) and real-time observation of how the cluster responds.

---

## System Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Chaos Dashboard                          │
│              http://localhost:8080   (kv-dashboard)          │
│   ┌──────────────┐   ┌─────────────┐   ┌────────────────┐   │
│   │  Canvas UI   │   │ Event Log   │   │  Chaos Controls│   │
│   │  (topology)  │   │ (timeline)  │   │  Kill/Pause/   │   │
│   └──────┬───────┘   └──────┬──────┘   │  Drop/Delay    │   │
│          │                  │          └────────┬───────┘   │
│          └──────────────── API ─────────────────┘           │
└─────────────────────────────┬───────────────────────────────┘
                              │ HTTP /api/*
                              │
              ┌───────────────┼───────────────┐
              │               │               │
    ┌─────────▼──┐  ┌─────────▼──┐  ┌────────▼───┐
    │  kv-store  │  │  kv-store  │  │  kv-store  │
    │   node0    │  │   node1    │  │   node2    │
    │            │  │            │  │            │
    │ gRPC :50051│  │ gRPC :50052│  │ gRPC :50053│
    │ Raft :12000│  │ Raft :12001│  │ Raft :12002│
    └─────┬──────┘  └─────┬──────┘  └─────┬──────┘
          │               │               │
          └───────────────┴───────────────┘
                   Raft Consensus (TCP)
                   AppendEntries / VoteRequest / Heartbeat
```

---

## The Two-Port Design

Each `kv-store` node runs **two TCP servers**:

| Port | Protocol | Purpose |
|---|---|---|
| `:12000+N` | Raw TCP | **Raft inter-node** — heartbeats, log replication, elections |
| `:50051+N` | gRPC | **Client API** — `Get`, `Set`, `Delete`, `Join`, `Health` |

This separation is critical for chaos testing: the `kv-chaos` proxy only sits in front of the **gRPC client port**. Raft consensus traffic bypasses it entirely (see [Architecture Decision](#architecture-decision-sigstop-vs-chaos-proxy)).

---

## Write Path (Raft Log Replication)

```
kv-client -cmd set -key foo -val bar -addr node0:50051
       │
       ▼
  node0 (Leader)
  ├── 1. Receives Put RPC
  ├── 2. Appends entry to local Raft log
  ├── 3. Sends AppendEntries to all followers (Raft port)
  │       ├── node1 acknowledges
  │       └── node2 acknowledges
  ├── 4. Quorum reached (2/3 acks) → entry COMMITTED
  ├── 5. FSM.Apply() called → key inserted into map[string]string
  └── 6. Returns success to kv-client
```

A write is **only acknowledged** after a quorum of nodes have written it to their Raft logs. This is the durability guarantee.

---

## Read Path

```
kv-client -cmd get -key foo -addr nodeN:50051
       │
       ▼
  nodeN (any node)
  ├── If Leader: reads directly from FSM (in-memory map)
  └── If Follower: redirects client to Leader's gRPC address
```

Reads are **consistent** — they always go through the leader's committed state.

---

## Storage Layer

Each node persists state using:

| File | Library | Contains |
|---|---|---|
| `raft-log.bolt` | BoltDB | All Raft log entries (commands) |
| `stable.bolt` | BoltDB | Current term, last voted-for |
| `snapshots/` | File system | Periodic FSM snapshots |

On restart, a node replays its BoltDB log to rebuild the in-memory KV map.

---

## Chaos Proxy Architecture

```
kv-client ──TCP──▶ [kv-chaos :22000] ──TCP──▶ [kv-store gRPC :50051]

Per connection: randomly DROP (close socket) or DELAY (sleep N ms)
```

The proxy intercepts **new TCP connections** only. Existing connections and Raft traffic are unaffected.

---

## Architecture Decision: SIGSTOP vs Chaos Proxy

The chaos proxy models **omission failures** at the client interface. For true **network partitions** (isolating Raft replication), use the dashboard's **⏸ Pause Node** button, which sends `SIGSTOP` to the node process.

| Failure Type | Mechanism | What's Affected |
|---|---|---|
| Crash / Fail-Stop | `SIGKILL` via `/api/kill` | Full process death |
| Network Partition | `SIGSTOP` via `/api/pause` | All network I/O (Raft + gRPC) |
| Receive Omission | chaos proxy drop=1.0 | New client gRPC connections only |
| Latency / Slowness | chaos proxy delay=Nms | New client gRPC connections only |

Use `SIGCONT` via `/api/resume` to heal any `SIGSTOP` partition.

---

## Cluster Topology Calculation

For a cluster of **N** nodes:

| Property | Formula | Example (N=3) | Example (N=11) |
|---|---|---|---|
| Fault tolerance | `f = ⌊N/2⌋` | f = 1 | f = 5 |
| Quorum required | `Q = ⌊N/2⌋ + 1` | Q = 2 | Q = 6 |
| Max simultaneous failures | f | 1 | 5 |
