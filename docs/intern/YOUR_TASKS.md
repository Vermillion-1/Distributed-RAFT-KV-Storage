# ✅ Your Tasks — What To Build Next

This file tells you exactly what to implement, why it matters, and how to approach it.  
Each task includes the goal, what to check, and code hints.

---

## Before You Start Any Task

Always start with a clean cluster:
```bash
pkill -f kv-dashboard; pkill -f kv-store; pkill -f kv-chaos; sleep 2
./kv-dashboard -nodes=3 -port=8080 &
sleep 10
# verify the cluster is healthy before running tests
curl -s http://localhost:8080/api/cluster | python3 -c \
  "import sys,json; d=json.load(sys.stdin); print('Nodes alive:', sum(1 for n in d['nodes'] if n.get('alive')))"
```

---

## Phase 3 — Resource & Latency Tests

**Goal:** Create `store/verify_phase3.sh`  
**Copy the structure from:** `store/verify_phase2.sh` — same helper functions, same pattern.

---

### R1: Slow Follower — Does it Hurt Write Speed?

**Theory:** The leader commits an entry after *any majority* of nodes acknowledge.  
In a 3-node cluster, that means 2 nodes. If follower B is slow, the leader + follower C can still commit — follower B's slowness doesn't matter.

**What to test:**
1. Measure how long 20 writes take (baseline)
2. Add 2000ms delay to a follower via the chaos proxy
3. Measure how long 20 writes take again (to the leader's real port)
4. Assert: times are similar (follower latency doesn't affect leader commit speed)

**Code pattern:**
```bash
# Start chaos proxy on a follower
curl -X POST "http://localhost:8080/api/chaos/delay/${FOLLOWER_ID}?ms=2000"

# Time 20 writes to the LEADER (not the follower)
start_ms=$(date +%s%3N)
for i in $(seq 1 20); do
    ./kv-client -addr 127.0.0.1:${LEADER_GRPC_PORT} -cmd set -key "r1_$i" -val "v$i"
done
end_ms=$(date +%s%3N)
elapsed=$((end_ms - start_ms))

# Assert: elapsed < 5000ms (added delay shouldn't affect leader writes)
```

**Stop chaos after:**
```bash
curl -X POST "http://localhost:8080/api/chaos/stop/${FOLLOWER_ID}"
```

---

### R2: Slow Leader — Does Client Latency Rise?

**Theory:** Writes go through the leader. If the leader is slow to respond to clients, every write is slower.  
Note: the chaos proxy slows the *client gRPC port*. Raft heartbeats use the *Raft port* — so the leader won't be voted out, just slow to respond to clients.

**What to test:**
1. Start chaos proxy on the **leader** with 500ms delay
2. Route `kv-client` through the proxy port (`:22000` — the proxy listens here)
3. Time 10 writes through the proxy
4. Assert: average write latency ≥ 400ms
5. Assert: no new election fired (leader is still leader after the test)

**Key insight for routing through proxy:**
```bash
# The chaos proxy port is always :22000 if it's the first proxy started
./kv-client -addr 127.0.0.1:22000 -cmd set -key "r2_$i" -val "v$i"
```

---

### R3: Packet Loss — What Happens at 50% Drop?

**Theory:** The chaos proxy drops ~50% of new client TCP connections.  
Clients that get dropped will retry. Clients that go through will succeed.

**What to test:**
1. Start chaos proxy on a follower with 50% drop rate
2. Send 20 requests to the follower through the proxy port
3. Count: how many succeed vs fail
4. Assert: roughly 8–12 succeed (50% with some variance)
5. Assert: follower eventually shows as `Dead` in health polling (its health check also gets dropped)

**Code pattern:**
```bash
curl -X POST "http://localhost:8080/api/chaos/drop/${FOLLOWER_ID}?rate=0.5"
sleep 2

ok=0; fail=0
for i in $(seq 1 20); do
    ./kv-client -addr 127.0.0.1:22000 -cmd set -key "r3_$i" -val "v" 2>/dev/null && ((ok++)) || ((fail++))
done
echo "Success: $ok/20, Failed: $fail/20"
```

---

## Phase 4 — Durability Tests

**Goal:** Create `store/verify_phase4.sh`  
**These tests use `kill` and `restart`, no new API endpoints needed.**

---

### D1: Total Wipe — Does Data Survive a Full Cluster Restart?

**Theory:** All committed writes are stored in BoltDB on disk (at `/tmp/raft-kv/nodeN/`).  
Restarting all nodes should restore the full state by replaying the log.

**What to test:**
```bash
# 1. Write 10 known keys
for i in $(seq 1 10); do
    ./kv-client -addr 127.0.0.1:50051 -cmd set -key "d1_key_$i" -val "d1_val_$i"
done

# 2. Kill all nodes (via dashboard API)
curl -X POST http://localhost:8080/api/kill/node0
curl -X POST http://localhost:8080/api/kill/node1
curl -X POST http://localhost:8080/api/kill/node2
sleep 2

# 3. Restart all nodes
curl -X POST http://localhost:8080/api/restart/node0
curl -X POST http://localhost:8080/api/restart/node1
curl -X POST http://localhost:8080/api/restart/node2
sleep 10  # wait for election

# 4. Read back every key and verify the value
for i in $(seq 1 10); do
    result=$(./kv-client -addr 127.0.0.1:50051 -cmd get -key "d1_key_$i" 2>/dev/null)
    # assert result contains "d1_val_$i"
done
```

---

### D2: Dirty Restart — Kill During an Active Write

**Theory:** A write is only acknowledged *after* it's committed to quorum. If the leader dies mid-write, the write either committed (and other nodes have it) or didn't commit (and nothing has it). Either way, no corruption.

**What to test:**
1. Start writing in a background loop
2. Simultaneously kill the leader
3. Let the new leader take over
4. Verify: all acknowledged writes are present; no partial writes exist

---

### D3: Snapshot Recovery

**Theory:** Raft periodically compacts its log into a snapshot (a full state dump). On restart, a node can load the snapshot instead of replaying thousands of log entries.

**What to test:**
1. Write enough entries to trigger a snapshot (Raft does this automatically after ~8192 entries by default)
2. Kill the leader
3. Restart it
4. Verify: the applied_index matches the cluster's committed index (node caught up)

**Observation to note:** The snapshot threshold may be too high to trigger in a short test. Document this as a limitation and note how to lower it (change `SnapshotInterval` in `server/node.go`).

---

## Helper Functions to Reuse

Copy these from `verify_phase2.sh` into your new scripts — they're tested and working:

```bash
cluster()      { curl -sf "http://localhost:${PORT}/api/cluster" 2>/dev/null; }
leader()       { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n.get('alive') and n.get('state')=='Leader']; print(ns[0]['config']['id'] if ns else '')" 2>/dev/null; }
node_applied() { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${1}']; print(ns[0].get('applied_index',0) if ns else 0)" 2>/dev/null; }
pause_node()   { curl -sf -X POST "${API}/pause/$1" > /dev/null; }
resume_node()  { curl -sf -X POST "${API}/resume/$1" > /dev/null; }
grpc_port()    { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${1}']; print(ns[0]['config']['grpc_addr'].split(':')[1] if ns else '50051')" 2>/dev/null; }
wait_leader()  { local t=0; while [ $t -lt $1 ]; do local l=$(leader); [ -n "$l" ] && echo "$l" && return; sleep 1; ((t++)); done; echo ""; }
```

---

## How to Report a Result

Each test should output one of:
```
✅ PASS — D1: All 10 keys recovered after full cluster restart ✓
❌ FAIL — D1: Key d1_key_3 not found after restart (expected d1_val_3)
```

Use the same color codes as the existing scripts for consistency.

---

## Questions? Stuck?

1. Read the relevant doc in `docs/5*.md` for the file you're working with
2. Check `docs/intern/RAFT_EXPLAINED.md` if something seems wrong conceptually
3. The dashboard event log (bottom of the page) shows exactly what the backend is doing in real time — use it for debugging
