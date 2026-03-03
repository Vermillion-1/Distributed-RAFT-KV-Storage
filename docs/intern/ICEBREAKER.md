# 🧊 Project Icebreaker — New Intern Guide

Welcome! This guide will take you from zero to contributing to the Raft KV Chaos Dashboard project.  
No prior Go experience required. Read this in order.

---

## Reading Order

| Step | File | What You'll Learn |
|---|---|---|
| 1 | This file | What the project is, what you need to do |
| 2 | [GO_BASICS.md](GO_BASICS.md) | Just enough Go to read and write code here |
| 3 | [RAFT_EXPLAINED.md](RAFT_EXPLAINED.md) | Raft consensus in plain English with analogies |
| 4 | [CODEBASE_TOUR.md](CODEBASE_TOUR.md) | Walk through every file, what it does, how it fits |
| 5 | [YOUR_TASKS.md](YOUR_TASKS.md) | Exactly what to build next, with guided hints |

---

## What Is This Project?

Imagine you have a key-value store like a dictionary: `"hello" → "world"`.  
Now imagine that dictionary is running on **3 separate computers** at the same time.

The challenge: if one computer crashes, the dictionary shouldn't lose data or start returning wrong answers.  
The solution: **Raft** — a consensus algorithm that makes all 3 computers agree on every write before confirming it.

This project implements that distributed key-value store **from scratch in Go**, and adds a **Chaos Dashboard** — a webpage where you can deliberately crash nodes, cut off their network, or slow them down, and watch the cluster recover in real time.

```
You type in browser:  "Kill Leader"
         ↓
Dashboard pauses node0 (SIGSTOP)
         ↓
node1 & node2 notice: "we haven't heard a heartbeat in 1 second"
         ↓
They vote, elect a new leader
         ↓
Dashboard shows: new leader in ~1s  ← the cluster recovered
```

---

## The Big Picture (3 Sentences)

1. **`kv-store`** is a Go program. Each instance is one node in the cluster. They talk to each other via Raft.
2. **`kv-dashboard`** is a separate Go program that starts N copies of `kv-store` and gives you a web UI to crash/pause/delay them.
3. **`verify*.sh`** are shell scripts that test the cluster's guarantees automatically (like unit tests, but for distributed system properties).

---

## What Has Already Been Done

| Phase | What | Status |
|---|---|---|
| Phase 1 | Liveness: leader election, MTTR, cascading failures | ✅ Tests written + run |
| Phase 2 | Network partitions: SIGSTOP/SIGCONT | ✅ Tests written + run |
| Phase 3 | Resource/Latency: slow follower, slow leader, packet loss | 🔄 Tests designed, script not written |
| Phase 4 | Durability: total wipe, dirty restart, snapshot recovery | 🔄 Not started |

**Your job: implement Phase 3 and Phase 4.**

---

## What You Need Installed

```bash
# Check Go version (need 1.21+)
go version

# Check other tools
curl --version
python3 --version
jq --version   # optional but nice
```

---

## How to Run the Project (30 seconds)

```bash
cd ~/Desktop/CMPT\ 756/Project/store

# Build everything (macOS needs these env vars to avoid permission errors)
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-dashboard ./cmd/dashboard/

# Start a 3-node cluster
pkill -f kv-dashboard; pkill -f kv-store   # kill any old ones first
./kv-dashboard -nodes=3 -port=8080 &

# Wait 10 seconds, then open  http://localhost:8080
sleep 10 && open http://localhost:8080
```

You should see 3 nodes in a circle — one gold (leader), two blue (followers).

---

## How to Know Something Is Working

- **Dashboard:** http://localhost:8080 shows 3 nodes, all alive, one is Leader
- **API check:** `curl -s http://localhost:8080/api/cluster | python3 -m json.tool`
- **Write test:** `./kv-client -cmd set -key test -val hello -addr 127.0.0.1:50051`
- **Read test:** `./kv-client -cmd get -key test -addr 127.0.0.1:50051`

---

## Project Directory at a Glance

```
store/
├── main.go                 ← kv-store node (each cluster node runs this)
├── server/node.go          ← Raft setup + gRPC API handlers
├── server/fsm.go           ← The actual key-value map + persistence logic
├── proto/kv.proto          ← Interface definition (what RPCs exist)
├── cmd/client/main.go      ← kv-client CLI tool
├── cmd/chaos/main.go       ← kv-chaos fault injection proxy
├── cmd/dashboard/main.go   ← Dashboard HTTP server
├── cmd/dashboard/index.html← Dashboard web frontend
├── verify.sh               ← Phase 1 automated tests
├── verify_phase2.sh        ← Phase 2 automated tests
└── docs/                   ← All documentation lives here
```

---

## Immediate Next Steps

1. Read [GO_BASICS.md](GO_BASICS.md) — takes ~20 minutes
2. Run the cluster (steps above) and click every button in the dashboard
3. Read [CODEBASE_TOUR.md](CODEBASE_TOUR.md) while the cluster is running
4. Open [YOUR_TASKS.md](YOUR_TASKS.md) and start Phase 3
