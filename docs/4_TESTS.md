# 🧪 Tests & Results

## Test Structure

Tests are split into phases matching the failure model categories from distributed systems theory:

```
verify.sh           → Phase 1: Liveness & Election
verify_phase2.sh    → Phase 2: Network Partitions (SIGSTOP/SIGCONT)
verify_phase3.sh    → Phase 3: Resource & Latency       [pending]
verify_phase4.sh    → Phase 4: Durability & State       [pending]
```

All scripts take an optional port argument: `bash verify.sh 8080`

---

## Phase 1: Liveness & Election

**Script:** `verify.sh`  
**Run:** `bash verify.sh 8080`

| ID | Test | What's Measured | 3-Node | 11-Node |
|---|---|---|---|---|
| L1 | Leader Kill & Restart — MTTR | Time from kill to new election | ✅ **1s** | ✅ **1s** |
| L1b | Killed node rejoins | Alive count after restart | ✅ 3/3 | ✅ 11/11 |
| L2 | Kill 2 nodes simultaneously | New leader? (only if quorum intact) | ❌ (correct — quorum lost) | ✅ PASS (9 remain) |
| L3 | Cascading kills until quorum loss | Safety mode entry at exact boundary | ✅ at 2/3 | ✅ at 6/11 |

**Key findings:**
- MTTR = 1 second on both 3-node and 11-node clusters — election time does not scale with N
- L2 "FAIL" on 3-node is **correct Raft CP behavior**, not a bug: no leader without quorum
- L3 confirms the `⌊N/2⌋ + 1` quorum formula enforced exactly at the boundary

---

## Phase 2: Network Partitions (SIGSTOP/SIGCONT)

**Script:** `verify_phase2.sh`  
**Run:** `bash verify_phase2.sh 8080`

Uses `SIGSTOP` (freeze process) and `SIGCONT` (resume) for true partition simulation.

| ID | Test | Expected | Result |
|---|---|---|---|
| P1a | Leader commits during minority partition | 20/20 writes succeed | ✅ 20/20 |
| P1b | Frozen follower is unreachable | Health RPC times out → idx=0 | ✅ Unreachable |
| P1c | No split-brain (follower can't become leader) | State = Dead, not Leader | ✅ No split-brain |
| P2a | New leader after leader SIGSTOP | New leader elected | ✅ ~3s MTTR |
| P2b | Old leader steps down | Old leader = Dead/Follower | ✅ Stepped down |
| P3a | Log catch-up after SIGCONT | Formerly-frozen node syncs | ✅ Full sync |
| P3b | Rejoins as Follower | No competing leader | ✅ Follower |

**Score: 7/7 PASS**

**Key finding:** Why SIGSTOP and not the chaos proxy?

The `kv-chaos` proxy **only intercepts the client gRPC port** (`:50051`), not the Raft inter-node port (`:12000`). Raft heartbeats flow directly between nodes on the Raft port — the chaos proxy is invisible to them. `SIGSTOP` freezes the entire process, making it truly unreachable on all ports.

---

## Phase 3: Resource & Latency [pending]

**Script:** `verify_phase3.sh` (not yet written)  
**Run:** `bash verify_phase3.sh 8080`

| ID | Test | Expected | Status |
|---|---|---|---|
| R1 | Slow Follower (2000ms latency) | Write latency unaffected (leader handles writes) | ⬜ Pending |
| R2 | Slow Leader (500ms latency via proxy) | Write latency +500ms; no re-election via Raft port | ⬜ Pending |
| R3 | 50% packet loss on follower | ~50% client requests fail; cluster continues | ⬜ Pending |

**Implementation note for next session:**
- After calling `/api/chaos/delay/:id?ms=N`, the proxy listens on **`:22000`** (first proxy started)
- Route `kv-client` to `:22000` with `-addr 127.0.0.1:22000` to measure chaos effects
- Use `date +%s%3N` for millisecond-resolution timing

---

## Phase 4: Durability & State [pending]

**Script:** `verify_phase4.sh` (not yet written)

| ID | Test | Expected | Status |
|---|---|---|---|
| D1 | Total wipe (kill all, restart all) | Applied index restored from BoltDB | ⬜ Pending |
| D2 | Dirty restart (kill during active write) | No data corruption; log integrity | ⬜ Pending |
| D3 | Snapshot recovery | Recovery from snapshot after log deletion | ⬜ Pending |

---

## Running All Tests

```bash
# Start cluster first
pkill -f kv-dashboard; pkill -f kv-store
./kv-dashboard -nodes=3 -port=8080 &
sleep 10

# Phase 1
bash verify.sh 8080

# Phase 2 (clean restart recommended between phases)
pkill -f kv-dashboard; pkill -f kv-store; sleep 2
./kv-dashboard -nodes=3 -port=8080 &
sleep 10
bash verify_phase2.sh 8080
```

---

## Dashboard-Based Chaos Testing

The dashboard at **http://localhost:8080** exposes all chaos controls interactively:

| Button | API Call | Effect |
|---|---|---|
| 💀 Kill Leader | `POST /api/kill/<leader-id>` | SIGKILL — process dies |
| 🎯 Kill Node | `POST /api/kill/<id>` | SIGKILL — process dies |
| ♻️ Restart Node | `POST /api/restart/<id>` | Respawn with log replay |
| ⏸ Pause Node | `POST /api/pause/<id>` | SIGSTOP — simulate partition |
| ▶ Resume Node | `POST /api/resume/<id>` | SIGCONT — heal partition |
| 📵 Drop Packets | `POST /api/chaos/drop/<id>?rate=1.0` | 100% client gRPC drop |
| 🐢 Add Latency | `POST /api/chaos/delay/<id>?ms=3000` | 3s client gRPC delay |
| ✅ Stop Chaos | `POST /api/chaos/stop/<id>` | Remove chaos proxy |
| 🧪 Run All Tests | `POST /api/run-tests` | Execute `run_chaos_test.sh` |
