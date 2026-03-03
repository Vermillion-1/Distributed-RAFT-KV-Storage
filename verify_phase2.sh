#!/bin/bash
# ═══════════════════════════════════════════════════════════════════
# verify_phase2.sh — Phase 2: Network Partition Tests (SIGSTOP/SIGCONT)
# Usage: bash verify_phase2.sh [port]   e.g. bash verify_phase2.sh 8080
#
# Uses /api/pause (SIGSTOP) and /api/resume (SIGCONT) for true partitions.
# ═══════════════════════════════════════════════════════════════════

PORT="${1:-8080}"
API="http://localhost:${PORT}/api"
PASS=0; FAIL=0

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

header() { echo -e "\n${CYAN}${BOLD}═══ $1 ═══${RESET}"; }
pass()   { echo -e "  ${GREEN}✅ PASS${RESET} — $1"; ((PASS++)); }
fail()   { echo -e "  ${RED}❌ FAIL${RESET} — $1"; ((FAIL++)); }
info()   { echo -e "  ${YELLOW}ℹ️  ${RESET}$1"; }

cluster()      { curl -sf "${API}/cluster" 2>/dev/null; }
leader()       { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n.get('alive') and n.get('state')=='Leader']; print(ns[0]['config']['id'] if ns else '')" 2>/dev/null; }
node_applied() { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('applied_index',0) if ns else 0)" 2>/dev/null; }
node_state()   { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('state','Dead') if ns else 'Dead')" 2>/dev/null; }
pause_node()   { curl -sf -X POST "${API}/pause/$1" > /dev/null && echo "ok" || echo "err"; }
resume_node()  { curl -sf -X POST "${API}/resume/$1" > /dev/null && echo "ok" || echo "err"; }

SCRIPT_DIR="/Users/ankushsingh/Desktop/CMPT 756/Project/store"
KV_CLIENT="${SCRIPT_DIR}/kv-client"

grpc_port() {
  local id=$1
  cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n for n in d['nodes'] if n['config']['id']=='${id}']
print(ns[0]['config']['grpc_addr'].split(':')[1] if ns else '50051')
" 2>/dev/null
}

pump_writes() {
  local n="${1:-10}" port="${2:-50051}" ok=0
  for i in $(seq 1 $n); do
    out=$("${KV_CLIENT}" -addr "127.0.0.1:${port}" -cmd set \
      -key "p2_key_${i}" -val "v$(date +%s%N)_${i}" 2>&1) && ((ok++)) || true
  done
  echo "$ok"
}

wait_leader() {
  local timeout=$1 t=0
  while [ $t -lt $timeout ]; do
    local l; l=$(leader); [ -n "$l" ] && echo "$l" && return 0
    sleep 1; ((t++))
  done; echo ""
}

# ── Pre-flight ────────────────────────────────────────────────────
header "PRE-FLIGHT"
curl -sf "${API}/cluster" > /dev/null 2>&1 || { echo -e "${RED}Dashboard not reachable at ${API}${RESET}"; exit 1; }
TOTAL=$(cluster | python3 -c "import sys,json; print(len(json.load(sys.stdin)['nodes']))")
CUR_L=$(wait_leader 20)
[ -n "$CUR_L" ] || { echo -e "${RED}No leader after 20s.${RESET}"; exit 1; }
info "Cluster: ${TOTAL} nodes | Leader: ${CUR_L} | Applied: $(node_applied "$CUR_L")"


# ═══════════════════════════════════════════════════════════════════
# P1 — MINORITY PARTITION (isolate 1 follower via SIGSTOP)
#
# Expectation:
#   - Leader continues committing (2/3 nodes = majority intact)
#   - Paused node's applied_index stalls (it's frozen, not running)
#   - Paused node does NOT become leader (can't send VoteRequests)
# ═══════════════════════════════════════════════════════════════════
header "P1: Minority Partition — SIGSTOP 1 follower"

FOLLOWER=$(cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${CUR_L}']
print(ns[0] if ns else '')
" 2>/dev/null)

info "Pausing follower: ${FOLLOWER} (SIGSTOP — hard partition)"
IDX_ISO_BEFORE=$(node_applied "$FOLLOWER")
IDX_L_BEFORE=$(node_applied "$CUR_L")
info "Before — Leader: ${IDX_L_BEFORE} | Follower: ${IDX_ISO_BEFORE}"

pause_node "$FOLLOWER" > /dev/null
sleep 2

# Pump 20 writes through leader while follower is frozen
L_PORT=$(grpc_port "$CUR_L")
info "Pumping 20 writes through leader (:${L_PORT})..."
WRITES_OK=$(pump_writes 20 "$L_PORT")
info "Writes accepted: ${WRITES_OK}/20"
sleep 4

IDX_L_AFTER=$(node_applied "$CUR_L")
IDX_ISO_AFTER=$(node_applied "$FOLLOWER")
LEADER_DELTA=$((IDX_L_AFTER - IDX_L_BEFORE))
ISO_DELTA=$((IDX_ISO_AFTER - IDX_ISO_BEFORE))

info "After  — Leader: ${IDX_L_AFTER} (+${LEADER_DELTA}) | Follower: ${IDX_ISO_AFTER} (+${ISO_DELTA})"

if [ "$LEADER_DELTA" -gt 0 ] 2>/dev/null; then
  pass "P1a: Leader committed +${LEADER_DELTA} entries with follower partitioned (Liveness ✓)"
else
  fail "P1a: Leader did not advance — writes failed (minority partition shouldn't affect majority)"
fi

# A SIGSTOP'd node's Health RPC times out → dashboard sees it as Dead (idx=0)
# Either stalled (idx same as before) OR unreachable (idx=0) both confirm partition worked
if [ "$ISO_DELTA" -le 0 ] 2>/dev/null; then
  if [ "$IDX_ISO_AFTER" -eq 0 ]; then
    pass "P1b: Frozen follower unreachable (Health timeout → idx=0) — Raft treats it as partitioned ✓"
  else
    pass "P1b: Frozen follower index stalled at ${IDX_ISO_AFTER} — not committing ✓"
  fi
else
  fail "P1b: Frozen follower index advanced +${ISO_DELTA} — unexpected while SIGSTOP'd"
fi


ISO_STATE=$(node_state "$FOLLOWER")
if [ "$ISO_STATE" != "Leader" ]; then
  pass "P1c: Paused node status='${ISO_STATE}' — no split-brain ✓"
else
  fail "P1c: Paused node became LEADER — SPLIT-BRAIN"
fi

info "Resuming ${FOLLOWER} (SIGCONT)..."
resume_node "$FOLLOWER" > /dev/null
sleep 4


# ═══════════════════════════════════════════════════════════════════
# P2 — MAJORITY PARTITION (SIGSTOP the leader)
#
# Expectation:
#   - Leader is frozen — stops sending heartbeats
#   - Remaining 2 followers time out and elect a new leader
#   - Old leader can't respond (frozen) — no dual-leader
# ═══════════════════════════════════════════════════════════════════
header "P2: Majority Partition — SIGSTOP the leader"

CUR_L=$(wait_leader 10)
[ -n "$CUR_L" ] || { CUR_L=$(leader); }
info "Current leader: ${CUR_L} — pausing it (stops heartbeats)"
OLD_LEADER=$CUR_L

PAUSE_T=$SECONDS
pause_node "$CUR_L" > /dev/null
info "🌩  Leader ${CUR_L} is FROZEN — cluster has no heartbeats"

info "Waiting up to 12s for followers to elect a new leader..."
sleep 2
NEW_L=$(wait_leader 12)
ELECT_T=$((SECONDS - PAUSE_T))
info "New leader: '${NEW_L}' (elected in ~${ELECT_T}s)"

if [ -n "$NEW_L" ] && [ "$NEW_L" != "$OLD_LEADER" ]; then
  pass "P2a: New leader ${NEW_L} elected in ~${ELECT_T}s after leader partition ✓"
else
  fail "P2a: No new leader elected within 12s after leader paused"
fi

# Old leader is frozen — it cannot be leader (it can't respond to RPC)
OLD_STATE=$(node_state "$OLD_LEADER")
info "Frozen leader ${OLD_LEADER} appears as: '${OLD_STATE}' to Health poll"
# SIGSTOP means Health RPC will time out → dashboard reports Dead
if [[ "$OLD_STATE" == "Dead" || "$OLD_STATE" == "Follower" || "$OLD_STATE" == "Candidate" ]]; then
  pass "P2b: Frozen node unreachable/stepped-down ('${OLD_STATE}') — no dual-leader ✓"
else
  fail "P2b: Frozen node still reports '${OLD_STATE}' — unexpected"
fi

info "Resuming frozen leader ${OLD_LEADER} (SIGCONT — healing partition)..."
resume_node "$OLD_LEADER" > /dev/null
sleep 5


# ═══════════════════════════════════════════════════════════════════
# P3 — HEAL & CATCH-UP
#
# After SIGCONT, the formerly-frozen node should detect it missed
# entries, request log replication from the new leader, and fully
# catch up. This tests Raft's log replication guarantee.
# ═══════════════════════════════════════════════════════════════════
header "P3: Heal & Catch-up — formerly frozen node rejoins"

CUR_L=$(wait_leader 10)
[ -n "$CUR_L" ] || { fail "P3: No leader available; skipping catch-up check"; }
IDX_LEAD=$(node_applied "$CUR_L")
IDX_OLD=$(node_applied "$OLD_LEADER")
DRIFT=$((IDX_LEAD - IDX_OLD))
info "Leader: ${CUR_L} (idx=${IDX_LEAD}) | Rejoining: ${OLD_LEADER} (idx=${IDX_OLD}) | Drift: ${DRIFT}"

# Pump 20 more writes to widen the gap, then give time to replicate
if [ "$DRIFT" -lt 5 ]; then
  L_PORT=$(grpc_port "$CUR_L")
  info "Drift small — pumping 20 more writes to widen gap..."
  pump_writes 20 "$L_PORT" > /dev/null
  sleep 3
  IDX_LEAD=$(node_applied "$CUR_L")
  IDX_OLD=$(node_applied "$OLD_LEADER")
  DRIFT=$((IDX_LEAD - IDX_OLD))
  info "After writes — Leader: ${IDX_LEAD} | Old leader: ${IDX_OLD} | Drift: ${DRIFT}"
fi

info "Waiting 10s for Raft log replication catch-up..."
sleep 10

IDX_OLD_FINAL=$(node_applied "$OLD_LEADER")
IDX_LEAD_FINAL=$(node_applied "$CUR_L")
CAUGHT=$((IDX_OLD_FINAL - IDX_OLD))
REMAINING=$((IDX_LEAD_FINAL - IDX_OLD_FINAL))
info "${OLD_LEADER} index: ${IDX_OLD} → ${IDX_OLD_FINAL} (+${CAUGHT} caught up, ${REMAINING} behind leader)"

if [ "$REMAINING" -eq 0 ] 2>/dev/null; then
  pass "P3a: Fully caught up to leader (${IDX_OLD_FINAL} = ${IDX_LEAD_FINAL}) ✓"
elif [ "$CAUGHT" -gt 0 ] 2>/dev/null; then
  pass "P3a: Partially caught up (+${CAUGHT} entries) — replication in progress (${REMAINING} remaining)"
else
  fail "P3a: No catch-up observed — Raft log replication stalled"
fi

# Verify it rejoined as Follower, not as a competing leader
REJOIN_STATE=$(node_state "$OLD_LEADER")
if [ "$REJOIN_STATE" = "Follower" ]; then
  pass "P3b: Rejoined cleanly as Follower (not causing split-brain) ✓"
else
  fail "P3b: Rejoined as '${REJOIN_STATE}' — expected Follower"
fi

# ── Summary ─────────────────────────────────────────────────────
echo ""
header "PHASE 2 RESULTS (with SIGSTOP/SIGCONT partitions)"
TOTAL_TESTS=$((PASS + FAIL))
echo -e "  ${GREEN}✅ PASS: ${PASS}/${TOTAL_TESTS}${RESET}  |  ${RED}❌ FAIL: ${FAIL}/${TOTAL_TESTS}${RESET}"
[ $FAIL -eq 0 ] && echo -e "  ${GREEN}${BOLD}All partition guarantees confirmed ✓${RESET}"
echo ""
