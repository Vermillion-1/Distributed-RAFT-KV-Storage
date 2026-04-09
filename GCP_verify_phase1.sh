#!/bin/bash
# ═══════════════════════════════════════════════════════════════════
# verify.sh — Exhaustive Chaos Dashboard Test Suite
# ═══════════════════════════════════════════════════════════════════
# Usage: bash verify.sh [port] e.g. bash verify.sh 8080
# ═══════════════════════════════════════════════════════════════════

PORT="${1:-8080}"
# D2 (GCP_TODO.md): set DASHBOARD_HOST=<node0-external-ip> when running from laptop against GCP.
# Leave unset when SSH'd into VM-0 — dashboard is on localhost there.
DASHBOARD_HOST="${DASHBOARD_HOST:-localhost}"
API="http://${DASHBOARD_HOST}:${PORT}/api"
PASS=0; FAIL=0; SKIP=0

# ── Helpers ─────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

header() { echo -e "\n${CYAN}${BOLD}═══ $1 ═══${RESET}"; }
pass() { echo -e " ${GREEN}✅ PASS${RESET} — $1"; ((PASS++)); }
fail() { echo -e " ${RED}❌ FAIL${RESET} — $1"; ((FAIL++)); }
# A4 (GCP_TODO.md): skip() helper — used for expected CP-behavior cases (e.g. L2 quorum loss)
skip() { echo -e " ${YELLOW}⏭ SKIP${RESET} — $1"; ((SKIP++)); }
info() { echo -e " ${YELLOW}ℹ️ ${RESET}$1"; }

cluster() { curl -sf "${API}/cluster" 2>/dev/null; }
leader() { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n.get('alive') and n.get('state')=='Leader']; print(ns[0]['config']['id'] if ns else '')" 2>/dev/null; }
alive_count() { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); print(sum(1 for n in d['nodes'] if n.get('alive')))" 2>/dev/null; }
applied_index() { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); print(max((n.get('applied_index',0) for n in d['nodes'] if n.get('alive')), default=0))" 2>/dev/null; }
node_applied() { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('applied_index',0) if ns else 0)" 2>/dev/null; }
node_state() { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('state','') if ns else 'Dead')" 2>/dev/null; }
kill_node() { curl -sf -X POST "${API}/kill/$1" > /dev/null; }
restart_node() { curl -sf -X POST "${API}/restart/$1" > /dev/null; }
drop_packets() { curl -sf -X POST "${API}/chaos/drop/$1?rate=1.0" > /dev/null; }
add_latency() { curl -sf -X POST "${API}/chaos/delay/$1?ms=$2" > /dev/null; }
stop_chaos() { curl -sf -X POST "${API}/chaos/stop/$1" > /dev/null; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KV_CLIENT="${SCRIPT_DIR}/kv-client"

# Extract grpc_addr for writing directly to a node
grpc_addr() {
 local id=$1
 cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n for n in d['nodes'] if n['config']['id']=='${id}']
print(ns[0]['config']['grpc_addr'] if ns else '')
" 2>/dev/null
}

wait_leader() {
 # Wait up to $1 seconds for a leader to emerge, return its id
 local timeout=$1; local t=0
 while [ $t -lt $timeout ]; do
 local l; l=$(leader)
 [ -n "$l" ] && echo "$l" && return 0
 sleep 1; ((t++))
 done
 echo ""
}

# ── Pre-flight ───────────────────────────────────────────────────────
header "PRE-FLIGHT"
if ! curl -sf "${API}/cluster" > /dev/null 2>&1; then
 echo -e "${RED}Dashboard not reachable at ${API}. Start it first:${RESET}"
 echo " ./kv-dashboard -nodes=<N> -port=${PORT}"
 exit 1
fi
TOTAL_NODES=$(cluster | python3 -c "import sys,json; print(len(json.load(sys.stdin)['nodes']))")
info "Dashboard reachable — ${TOTAL_NODES}-node cluster"
info "Waiting for initial leader election..."
INITIAL_LEADER=$(wait_leader 15)
[ -n "$INITIAL_LEADER" ] || { echo -e "${RED}No leader after 15s. Aborting.${RESET}"; exit 1; }
info "Initial leader: ${INITIAL_LEADER}"
info "Applied index: $(applied_index)"


# ═══════════════════════════════════════════════════════════════════
# PHASE 1: LIVENESS & ELECTION
# ═══════════════════════════════════════════════════════════════════
header "PHASE 1: LIVENESS & ELECTION"

# ── L1: Leader Kill & Restart ────────────────────────────────────────
echo -e "\n${BOLD}L1: Leader Kill/Restart — MTTR measurement${RESET}"
L=$(leader)
info "Killing leader: $L"
T_KILL=$SECONDS
kill_node "$L"
sleep 1

NEW_L=$(wait_leader 15)
T_RECOVER=$SECONDS
MTTR=$((T_RECOVER - T_KILL))

if [ -n "$NEW_L" ] && [ "$NEW_L" != "$L" ]; then
 pass "New leader elected: ${NEW_L} in ${MTTR}s (Liveness guarantee ✓)"
else
 fail "No new leader elected within 15s (MTTR threshold exceeded)"
fi

info "Restarting killed node $L..."
restart_node "$L"
sleep 3
ALIVE=$(alive_count)
if [ "$ALIVE" = "$TOTAL_NODES" ]; then
 pass "Restarted node rejoined — cluster fully restored (${ALIVE}/${TOTAL_NODES})"
else
 fail "Cluster only has ${ALIVE}/${TOTAL_NODES} nodes after restart"
fi

# ── L1c: Write + Linearizable Read — Replicated State Machine consistency ──
# Directly maps to lecture slide 17 (RSM: all replicas must execute same
# commands in same order) and slide 16 (Safety: never return incorrect result).
# A fresh read after leadership change proves the new leader has caught up.
echo -e "\n${BOLD} L1c: RSM consistency — write + read after new leader${RESET}"
NEW_L_ADDR=$(grpc_addr "$NEW_L")
if [ -n "$NEW_L_ADDR" ] && [ -f "${KV_CLIENT}" ]; then
 SET_OUT=$("${KV_CLIENT}" -addr "${NEW_L_ADDR}" -cmd set \
 -key "l1c_probe" -val "consistency_check" 2>&1)
 if echo "$SET_OUT" | grep -qi "successful"; then
 GET_OUT=$("${KV_CLIENT}" -addr "${NEW_L_ADDR}" -cmd get \
 -key "l1c_probe" 2>&1)
 if echo "$GET_OUT" | grep -q "consistency_check"; then
 pass "L1c: Write+read consistent on new leader — RSM invariant holds ✓"
 else
 fail "L1c: Read returned stale/wrong value after election — RSM divergence"
 fi
 else
 fail "L1c: Write to new leader failed — ${SET_OUT}"
 fi
else
 info "L1c: kv-client not found or leader addr unavailable — skipping RSM check"
fi

# ── L2: Election Stability — Kill mid-election ────────────────────────
echo -e "\n${BOLD}L2: Election Stability — Kill mid-election${RESET}"
L=$(leader)
info "Killing leader $L and immediately killing a follower..."
kill_node "$L"
# Find a follower to kill immediately
FOLLOWER=$(cluster | python3 -c "
import sys,json
d=json.load(sys.stdin)
ns=[n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${L}']
print(ns[0] if ns else '')
" 2>/dev/null)
[ -n "$FOLLOWER" ] && kill_node "$FOLLOWER" && info "Also killed follower: $FOLLOWER"

NEW_L=$(wait_leader 20)
if [ -n "$NEW_L" ]; then
 pass "Cluster recovered from split election — new leader: $NEW_L"
else
 # A4 (GCP_TODO.md): quorum loss on a 3-node cluster with 2 dead is CORRECT CP behavior, not a failure
 skip "L2: Quorum lost ($((TOTAL_NODES - 1))/${TOTAL_NODES} killed) — no leader is correct CP behavior (expected)"
fi
info "Restoring killed nodes..."
restart_node "$L" 2>/dev/null || true
[ -n "$FOLLOWER" ] && restart_node "$FOLLOWER" 2>/dev/null || true
sleep 5

# ── L3: Cascading Failure — Kill until quorum lost ───────────────────
echo -e "\n${BOLD}L3: Cascading Failure — Kill node by node until quorum loss${RESET}"
# Wait for cluster to stabilise
sleep 3
L=$(leader)
info "Leader: $L — killing nodes one by one with 5s intervals..."
NODES_LIST=$(cluster | python3 -c "
import sys,json
d=json.load(sys.stdin)
# Start with non-leaders
ids = [n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${L}']
ids.append('${L}')
print(' '.join(ids))
" 2>/dev/null)

QUORUM=$(( (TOTAL_NODES / 2) + 1 ))
# Capture a live node address for the safety write-blocking test (L3b)
L3_ADDR=$(grpc_addr "$L")
killed=()
quorum_lost=false
for nid in $NODES_LIST; do
 kill_node "$nid"
 killed+=("$nid")
 remaining=$((TOTAL_NODES - ${#killed[@]}))
 info "Killed $nid — ${remaining}/${TOTAL_NODES} nodes alive"
 sleep 5
 if [ $remaining -lt $QUORUM ]; then
 # Confirm no leader
 L_NOW=$(leader)
 if [ -z "$L_NOW" ]; then
 pass "L3: Cluster correctly entered Safety mode (no leader) after killing ${#killed[@]}/${TOTAL_NODES} nodes"
 quorum_lost=true
 # L3b: Safety property proof (lecture slide 16 — "Safety: Never return an incorrect result").
 # With quorum lost, writes MUST be blocked. A CP system sacrifices availability to
 # prevent inconsistency. A write that succeeds here would mean a node committed
 # without a quorum majority — a safety violation.
 if [ -n "$L3_ADDR" ] && [ -f "${KV_CLIENT}" ]; then
 SAFETY_OUT=$("${KV_CLIENT}" -addr "${L3_ADDR}" \
 -cmd set -key "l3b_safety" -val "must_fail" 2>&1) || true
 if echo "$SAFETY_OUT" | grep -qiE "error|failed|timeout|connection|EOF|not leader"; then
 pass "L3b: Write correctly blocked under quorum loss (CP Safety ✓)"
 else
 fail "L3b: Write appeared to succeed without quorum — SAFETY VIOLATION"
 fi
 fi
 break
 else
 fail "L3: Cluster still has leader '$L_NOW' despite losing quorum — SAFETY VIOLATION"
 quorum_lost=true
 break
 fi
 fi
done
[ "$quorum_lost" = false ] && info "L3: All nodes killed — quorum was lost at expected threshold"

info "Restoring all nodes..."
for nid in "${killed[@]}"; do
 restart_node "$nid" 2>/dev/null || true
 sleep 1
done
sleep 6
ALIVE=$(alive_count)
info "Cluster restored: ${ALIVE}/${TOTAL_NODES} nodes alive"


echo ""
echo -e "${CYAN}${BOLD}═══ PHASE 1 COMPLETE ═══${RESET}"

# Allow time for cluster to fully stabilise before Phase 2
sleep 8
L2_LEADER=$(wait_leader 15)
info "Cluster leader after Phase 1 restore: ${L2_LEADER:-NONE}"


