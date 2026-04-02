# Changelog — v1.2

---

## v1.2.3 — Deploy Verification (2026-04-01, Session 4)

| ID | Change | File |
|----|--------|------|
| FEAT-1 | Added Step 10: polls `/api/cluster` on node0's dashboard every 3s (30s timeout) until a node reports `state == "Leader"` — deploy now fails fast instead of silently handing off a leaderless cluster | `dynamic_deploy.sh` |
| FEAT-3 | Added Step 11: SSHes to node0 and runs `kv-client set deploy_probe=ok` + `kv-client get` over internal gRPC addresses — end-to-end proof that the full write path (client → gRPC → Raft → FSM → response) is operational | `dynamic_deploy.sh` |

---

## v1.2.2 — Deploy Hardening & Terminology Cleanup (2026-04-01, Session 3)

| ID | Change | File |
|----|--------|------|
| E-1 | Prepended `COPYFILE_DISABLE=1` to `tar -czf` — suppresses macOS extended attribute warnings on GCP VMs | `dynamic_deploy.sh` |
| E-2 | Added `--ssh-flag="-T"` to all 7 non-interactive `gcloud compute ssh` calls — eliminates "Pseudo-terminal will not be allocated" noise | `dynamic_deploy.sh` |
| E-3 | Removed dead code: `mkdir -p ~/cmd/dashboard` + `scp index.html` — `index.html` is embedded via `go:embed` since v1.2 and the file copy was a no-op | `dynamic_deploy.sh` |
| E-4 | Updated failure model table and project structure comment: `kv-chaos TCP fault proxy` → deprecated; Sidecar Agent is the actual fault injection mechanism | `README.md` |
| E-5 | Replaced "Chaos Proxy" with "Sidecar Agent" in dual-port networking rationale | `docs/ARCHITECTURE.md` |

---

## v1.2.0 — Bug Fix Session (2026-03-31)
**Branch/State:** Post-GCP-verification, pre-presentation bug fixes

## Summary of Changes

| # | Bug | Severity | File(s) | Status |
|---|-----|----------|---------|--------|
| 5 | Dashboard HTML blank page when run from wrong directory | CRITICAL | `cmd/dashboard/main.go` | ✅ Fixed |
| 2 | Chaos proxy (Drop/Delay) silently broken in GCP mode | MEDIUM | `cmd/dashboard/main.go` | ✅ Fixed |
| 1 | KV API returns success=false when targeting a follower | MEDIUM | `cmd/dashboard/main.go` | ✅ Fixed |
| 3 | Dashboard restart wiped BoltDB data (durability demo killer) | LOW | `cmd/dashboard/main.go` | ✅ Fixed |
| 4 | RestartNode selected dead process as join target | LOW | `cmd/dashboard/main.go` | ✅ Fixed |
| 6 | `-seq-num=0` silently ignored with no user warning | INFO | `cmd/client/main.go` | ✅ Fixed |

---

## Fix 1: Embed `index.html` into Dashboard Binary (Bug 5 — CRITICAL)

### Problem
`cmd/dashboard/main.go` served the dashboard UI with:
```go
http.ServeFile(w, r, binDir+"/cmd/dashboard/index.html")
```
where `binDir = os.Getwd()`. If the binary was run from any directory other than the repo root (e.g., `/tmp/kv-dashboard`), the browser received a 404 or 500, resulting in a completely blank dashboard page. This was the highest-risk demo failure mode.

### Fix
Used Go's `embed` package to bundle `index.html` directly into the binary at compile time:

```go
// Added to imports:
"embed"

// Added package-level directive:
//go:embed index.html
var indexHTML embed.FS

// Handler changed from:
http.ServeFile(w, r, binDir+"/cmd/dashboard/index.html")

// To:
data, err := indexHTML.ReadFile("index.html")
w.Header().Set("Content-Type", "text/html; charset=utf-8")
w.Write(data)
```

### Files Changed
- `cmd/dashboard/main.go`: added `embed` import, `//go:embed` directive, rewrote `/` handler

### Test
```bash
cd /tmp && /tmp/kv-dashboard -nodes=3 -port=8080 &
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/
# Result: 200 | Body: 41055 bytes ✅
```

---

## Fix 2: GCP Mode Guard for Chaos Proxy (Bug 2 — MEDIUM)

### Problem
`StartChaosProxy` checked `m.nodes[nodeID]` which is always empty in GCP mode (nodes run on remote VMs, not as local child processes). Clicking "Drop Packets" or "Add Latency" in the dashboard while in GCP mode always returned `"node X not found"` with no explanation.

### Fix
Added an explicit GCP mode check at the top of `StartChaosProxy`, matching the pattern used by `KillNode`, `PauseNode`, etc.:

```go
// Added as first check in StartChaosProxy:
if len(m.agentAddrs) > 0 {
    return fmt.Errorf("chaos proxy (drop/delay) is not available in GCP mode — use 'Netem' or 'Partition' buttons instead")
}
```

### Files Changed
- `cmd/dashboard/main.go:StartChaosProxy`

### Test
In GCP mode (`-agent-addrs` flag set), `/api/chaos/drop/node0` now returns a clear, actionable error message instead of a confusing "node not found".

---

## Fix 3: KV API Auto-Redirects to Leader (Bug 1 — MEDIUM)

### Problem
`DirectKVSet`, `DirectKVGet`, and `DirectKVDelete` returned an error when the target node was a follower rather than automatically following the leader redirect embedded in the response. While the UI JavaScript auto-discovers the leader before calling, a leadership change between the `/api/cluster` poll and the KV call caused a confusing error in the UI.

### Fix
Refactored the three Direct* functions around a shared `kvGRPCConn` helper, and added a single leader-redirect retry inside each operation:

```go
func kvGRPCConn(addr string, fn func(pb.KVStoreClient, context.Context) error) error { ... }

func (m *Manager) DirectKVSet(addr, key, val string) (bool, error) {
    // ... on follower response with LeaderAddr != "":
    return kvGRPCConn(resp.LeaderAddr, func(c2 ...) error {
        resp2, _ := c2.Set(ctx2, &pb.SetRequest{Key: key, Value: val})
        result = resp2.Success
        return nil
    })
}
```

Same pattern applied to `DirectKVGet` and `DirectKVDelete`.

### Files Changed
- `cmd/dashboard/main.go`: replaced `DirectKVSet/Get/Delete` with redirect-aware versions; added `kvGRPCConn` helper

### Test
```bash
# node1 (127.0.0.1:50052) is a follower, leader is node0 (50051)
curl -X POST "http://localhost:8080/api/kv/set?key=autored&val=works&addr=127.0.0.1:50052"
# Before fix: {"success":false,"error":"not leader, redirect to 127.0.0.1:50051"}
# After fix:  {"success":true} ✅

curl "http://localhost:8080/api/kv/get?key=autored&addr=127.0.0.1:50052"
# After fix:  {"found":true,"value":"works"} ✅

curl -X POST "http://localhost:8080/api/kv/delete?key=autored&addr=127.0.0.1:50052"
# After fix:  {"success":true} ✅
```

---

## Fix 4: Data Wipe Guard on Dashboard Restart (Bug 3 — LOW)

### Problem
`StartAll()` used an in-memory `freshStart bool` flag to decide whether to wipe `/tmp/raft-kv/`. Since `freshStart` starts as `false`, the very first `StartAll()` call always wiped the data directory — even if the dashboard was restarted mid-demo with existing BoltDB data. This made the Phase 4 durability test fragile: restarting the dashboard would destroy the data it was supposed to prove survived.

### Fix
Replaced the in-memory flag with a filesystem check — look for actual BoltDB files:

```go
// New helper:
func hasBoltData(configs []NodeConfig) bool {
    for _, cfg := range configs {
        if _, err := os.Stat(cfg.DataDir + "/raft-log.bolt"); err == nil {
            return true
        }
    }
    return false
}

// In StartAll():
if hasBoltData(m.configs) {
    // Preserve existing data
    m.log("info", "Restart: existing BoltDB data found, preserving data directories")
} else {
    // Safe to wipe
    os.RemoveAll("/tmp/raft-kv/")
    m.log("info", "Fresh start: no existing data found, created clean directories")
}
```

### Files Changed
- `cmd/dashboard/main.go`: added `hasBoltData()`, rewrote `StartAll()` wipe logic

### Test
```bash
# With BoltDB present:
# Dashboard log shows: "Restart: existing BoltDB data found, preserving data directories" ✅
# Without BoltDB: "Fresh start: no existing data found, created clean directories" ✅
```

---

## Fix 5: RestartNode Liveness Check via signal(0) (Bug 4 — LOW)

### Problem
`RestartNode` searched for a live peer to use as a join target by checking `n.cmd.Process != nil`. However, a process that has been killed still has a non-nil `Process` struct in Go. This meant a previously killed node could be selected as the join target, causing the restarting node to fail its join attempt and wait out the full 10-retry exponential backoff (up to ~2 minutes in a demo).

### Fix
Added `signal(0)` check — a zero signal tests process liveness without sending an actual signal:

```go
// Before:
if id != nodeID && n.cmd.Process != nil {
    joinAddr = n.config.GRPCAddr

// After:
if id != nodeID && n.cmd.Process != nil {
    if err := n.cmd.Process.Signal(syscall.Signal(0)); err == nil {
        joinAddr = n.config.GRPCAddr
```

### Files Changed
- `cmd/dashboard/main.go:RestartNode`

---

## Fix 6: Document `-seq-num=0` Limitation in Client (Bug 6 — INFO)

### Problem
Passing `-seq-num=0` was silently ignored (treated identically to not passing the flag at all) because `0` is the `uint64` zero value. No warning was emitted, making it difficult to diagnose why idempotency tests at sequence 0 weren't working.

### Fix
Added a comment documenting the limitation and a `log.Printf` when an explicit non-zero seq-num is used, so testers can confirm the value was accepted:

```go
// LIMITATION: -seq-num=0 cannot be detected as "explicitly set" because 0 is the
// uint64 zero value. Use -seq-num=1 as the minimum testable sequence number.
if *explicitSeqNum != 0 {
    sequenceNo = *explicitSeqNum - 1
    log.Printf("Using explicit seq-num: %d (for idempotency testing)", *explicitSeqNum)
}
```

### Files Changed
- `cmd/client/main.go`

---

## Test Results After All Fixes

### Unit Tests
```
go test ./server/... -v
```
**Result: 16/16 PASS** (unchanged — all pre-existing tests still pass)

### Build Verification
All 4 binaries build cleanly:
```
go build -o /tmp/kv-store .              ✅
go build -o /tmp/kv-client ./cmd/client/ ✅
go build -o /tmp/kv-chaos ./cmd/chaos/   ✅
go build -o /tmp/kv-dashboard ./cmd/dashboard/ ✅
```

### Integration Smoke Tests
| Test | Expected | Result |
|------|----------|--------|
| Dashboard HTML from wrong dir | HTTP 200, 41KB body | ✅ |
| KV SET to follower addr | `{"success":true}` | ✅ |
| KV GET from follower addr | `{"found":true,"value":"works"}` | ✅ |
| KV DELETE from follower addr | `{"success":true}` | ✅ |
| BoltDB data preserved on restart | Log: "preserving data directories" | ✅ |
| Leader election, 3-node cluster | 1 Leader, 2 Followers | ✅ |

---

## Backward Compatibility
All changes are fully backward compatible:
- `go:embed` is transparent — binary is larger but behavior is identical
- GCP mode error message is additive (previously was a confusing error)
- KV redirect retry is additive (previously returned error, now succeeds)
- BoltDB check replaces in-memory flag but produces same result on clean start
- signal(0) check is purely defensive, doesn't change control flow in normal case
- Client log line is informational only

---

---

## v1.2.1 — Phase Script Hardening (2026-03-31)
**Date:** 2026-03-31
**Session goal:** Patch broken tests, add missing critical tests grounded in lecture material (03-756-FT.pdf).

### Summary of Script Changes

| Script | Change | Type |
|--------|--------|------|
| `GCP_verify_phase1.sh` | Add `L1c`: write+read after election (RSM invariant) | New test |
| `GCP_verify_phase1.sh` | Add `L3b`: write blocked under quorum loss (CP Safety) | New test |
| `GCP_verify_phase2.sh` | Add `P1d`: data content check after partition heal | New test |
| `GCP_verify_phase2.sh` | Add `P2c`: write attempt to isolated leader (CP Safety) | New test |
| `GCP_verify_phase3.sh` | GCP mode detection + netem fallback for R1/R2 | **Bug fix** |
| `GCP_verify_phase3.sh` | Add R2b: 500ms delay < ElectionTimeout (no spurious election) | New assertion |
| `GCP_verify_phase5.sh` | Fix I2: value check now unconditional (not gated on DELTA==0) | **Bug fix** |
| `GCP_verify_phase5.sh` | Fix I4: use `-addrs` for failover write (tests actual smart-client) | **Bug fix** |
| `GCP_verify_phase6.sh` | Add `N6` (a–f): iptables partition of leader, 6 assertions | New test |

---

### Fix 7: Phase 3 GCP Mode Detection (Bug — MEDIUM)

#### Problem
After Bug 2 fix (v1.2), `/api/chaos/delay` returns HTTP 4xx in GCP mode. Phase 3 R1 and R2 called this endpoint unconditionally. Additionally R2 used `proxy_port()` to route writes through a local TCP proxy process that does not exist on remote GCP VMs. Both tests silently failed in GCP mode — they either errored out or measured nothing meaningful.

#### Fix
Added a GCP mode probe at the top of Phase 3:
```bash
PROBE_RESPONSE=$(curl -sf -X POST "${API}/chaos/delay/${CUR_L}?ms=1" 2>&1; echo "exit:$?")
if echo "$PROBE_RESPONSE" | grep -q "exit:0"; then
  CHAOS_MODE="proxy"
else
  CHAOS_MODE="netem"   # GCP mode — use kernel-level tc/netem instead
fi
```
Added `apply_delay()` / `remove_delay()` wrappers that dispatch to `/api/chaos/delay` (local) or `/api/chaos/netem` (GCP). R2 no longer uses `proxy_port` in GCP mode — netem applies at the NIC level, which is actually a more realistic test of Raft transport delay.

#### Lecture grounding
Delayed Messages (slide 6): messages that arrive after expected time. Both chaos proxy and netem simulate this failure model. Netem operates at the communication layer (slide 3: Communication Failure Models) which is the more correct abstraction.

---

### Fix 8: Phase 5 I2 — Wrong Idempotency Invariant (Bug — MEDIUM)

#### Problem
The test checked `if [ "$DELTA" -eq 0 ]` to decide whether to verify the stored value. But Raft **always** advances `applied_index` when it commits a log entry — even duplicates. The FSM's `isDuplicate()` skips the KV mutation but Raft still calls `Apply()`. So `DELTA` is always 1, the value check (`grep "first_value"`) never executed, and the test always hit the trivial `else` pass. The idempotency guarantee was never actually verified.

#### Fix
Removed the `DELTA` gate entirely. The test now always checks:
```bash
if echo "$GET_DUP" | grep -q "first_value"; then
  pass "I2b: Value preserved — FSM correctly ignored duplicate payload ✓"
elif echo "$GET_DUP" | grep -q "second_value"; then
  fail "I2b: Value mutated — FSM deduplication FAILED"
fi
```
`DELTA` is now informational only (expected value: ≤1).

#### Lecture grounding
Duplicate Messages (slide 6): the system must handle them by skipping state mutation. `isDuplicate()` in `server/fsm.go` is our implementation. The test now actually exercises this guarantee end-to-end.

---

### Fix 9: Phase 5 I4 — Wrong Client Failover Path (Bug — LOW)

#### Problem
I4 ("Smart client survives leader death") manually discovered the new leader via `wait_leader()` then wrote directly to `NEW_L_ADDR`. This bypassed the entire smart-client failover mechanism — it wasn't testing anything about the `-addrs` multi-endpoint logic.

#### Fix
The post-kill write now uses `-addrs ${ALL_ADDRS}`, letting the client's `smartRequestLoop` discover the new leader via leader redirects:
```bash
WRITE_OUT=$("${KV_CLIENT}" -addrs "${ALL_ADDRS}" \
  -cmd set -key "i4_after" -val "post_kill" 2>&1)
```

#### Lecture grounding
Retry and Health Endpoint Monitoring resiliency patterns (slide 9). The client uses a 2s gRPC timeout as a Circuit Breaker (slide 9) and retries across all nodes to find the new leader.

---

### New Test: L1c — RSM Consistency After Election

After L1's leader kill + new election, write a key and immediately read it back from the new leader. If the read returns the correct value, the new leader's state machine is consistent.

**Lecture grounding:** Replicated State Machine (slide 17) — all server copies must execute commands in identical order. A stale read after leadership change would indicate RSM divergence.

---

### New Test: L3b — CP Safety Under Quorum Loss

After L3 confirms no leader (quorum lost), attempt a write to the last known leader address. The write **must** fail or timeout.

**Lecture grounding:** Consensus Safety property (slide 16) — "Never return an incorrect result." A successful write without quorum would mean a node committed without majority agreement, violating the core Raft safety invariant.

---

### New Test: P1d — Data Integrity After Partition Heal

After P1's minority partition heals, read back partition-era keys from both the healed follower and the leader. Verifies that log replay populated the correct values (not just the correct index).

**Lecture grounding:** Replicated State Machine (slide 17) — log replay must produce identical state on all replicas.

---

### New Test: P2c — Isolated Leader Write Blocking

While the leader is SIGSTOP'd (P2 scenario), attempt a write to its gRPC address. Must fail.

**Lecture grounding:** Send Omission failure model (slide 3) — node sends but cannot reach majority. CP property (slide 16): consistency is preserved by refusing to commit.

---

### New Test: N6 — iptables Partition of the Leader (6 assertions)

The critical missing test from the original suite. N1 only tested follower partition (minority). N6 tests leader partition (majority):

| Assertion | What it verifies |
|-----------|-----------------|
| N6a | New leader elected after leader iptables DROP; MTTR measured |
| N6b | Term advanced — proves genuine election, stale responses rejected |
| N6c | Isolated old-leader rejects writes (CP Safety) |
| N6d | New leader accepts writes normally (Liveness restored) |
| N6e | Old leader rejoins as Follower after heal (no split-brain) |
| N6f | Data written during partition period is durable |

**Lecture grounding:** Communication Crash Failure (link stop, slide 3). Leader Election resiliency pattern (slide 9). CAP theorem CP behavior (consistency + partition tolerance, availability sacrificed for minority side).

---

*End of Changelog v1.2 / v1.2.1*
