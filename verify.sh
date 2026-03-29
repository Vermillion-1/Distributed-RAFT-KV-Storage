#!/bin/bash
# ═══════════════════════════════════════════════════════════════════
# verify.sh — Exhaustive Chaos Dashboard Test Suite
# ═══════════════════════════════════════════════════════════════════
# Usage: bash verify.sh [port]     e.g.  bash verify.sh 8080
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

header()  { echo -e "\n${CYAN}${BOLD}═══ $1 ═══${RESET}"; }
pass()    { echo -e "  ${GREEN}✅ PASS${RESET} — $1"; ((PASS++)); }
fail()    { echo -e "  ${RED}❌ FAIL${RESET} — $1"; ((FAIL++)); }
# A4 (GCP_TODO.md): skip() helper — used for expected CP-behavior cases (e.g. L2 quorum loss)
skip()    { echo -e "  ${YELLOW}⏭  SKIP${RESET} — $1"; ((SKIP++)); }
info()    { echo -e "  ${YELLOW}ℹ️  ${RESET}$1"; }

cluster() { curl -sf "${API}/cluster" 2>/dev/null; }
leader()  { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n.get('alive') and n.get('state')=='Leader']; print(ns[0]['config']['id'] if ns else '')" 2>/dev/null; }
alive_count() { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); print(sum(1 for n in d['nodes'] if n.get('alive')))" 2>/dev/null; }
applied_index() { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); print(max((n.get('applied_index',0) for n in d['nodes'] if n.get('alive')), default=0))" 2>/dev/null; }
node_applied() { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('applied_index',0) if ns else 0)" 2>/dev/null; }
node_state()   { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('state','') if ns else 'Dead')" 2>/dev/null; }
kill_node()    { curl -sf -X POST "${API}/kill/$1" > /dev/null; }
restart_node() { curl -sf -X POST "${API}/restart/$1" > /dev/null; }
drop_packets() { curl -sf -X POST "${API}/chaos/drop/$1?rate=1.0" > /dev/null; }
add_latency()  { curl -sf -X POST "${API}/chaos/delay/$1?ms=$2" > /dev/null; }
stop_chaos()   { curl -sf -X POST "${API}/chaos/stop/$1" > /dev/null; }

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
  echo "  ./kv-dashboard -nodes=3 -port=${PORT}"
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
  pass "New leader elected: ${NEW_L} in ${MTTR}s  (Liveness guarantee ✓)"
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
  skip "L2: Quorum lost with 2/3 dead — no leader is correct CP behavior (expected)"
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


# ═══════════════════════════════════════════════════════════════════
# PHASE 2: NETWORK PARTITIONS
# Uses kv-chaos drop proxy (100% packet drop = hard partition)
# kv-client writes create real applied-index drift we can observe.
# ═══════════════════════════════════════════════════════════════════
header "PHASE 2: NETWORK PARTITIONS"

BIN_DIR="$(dirname $(realpath $0))"
KV_CLIENT="${BIN_DIR}/kv-client"

# Helper: write N KV pairs to the current leader's gRPC port
pump_writes() {
  local n="${1:-10}" port="${2:-50051}"
  for i in $(seq 1 $n); do
    "${KV_CLIENT}" -addr "127.0.0.1:${port}" -cmd set -key "chaos_p2_${i}" -val "val_${i}" 2>/dev/null || true
  done
}

# Get gRPC port for a node id (node0→50051, node1→50052, ...)
grpc_port() {
  local id=$1
  cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n for n in d['nodes'] if n['config']['id']=='${id}']
print(ns[0]['config']['grpc_addr'].split(':')[1] if ns else '50051')
" 2>/dev/null
}

# ── P1: Minority Partition (Isolate 1 follower) ───────────────────────
echo -e "\n${BOLD}P1: Minority Partition — isolate 1 follower, verify index drift${RESET}"

CUR_L=$(leader)
# Pick a follower to isolate
ISOLATED=$(cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${CUR_L}']
print(ns[0] if ns else '')
" 2>/dev/null)

info "Leader: ${CUR_L} | Isolating: ${ISOLATED}"
IDX_BEFORE=$(node_applied "$ISOLATED")
info "Isolated node applied index BEFORE: ${IDX_BEFORE}"

# Start chaos proxy — drops all packets arriving at the isolated node
drop_packets "$ISOLATED"
info "🌩️  Chaos proxy active — 100% drop on ${ISOLATED}"
sleep 2

# Write 15 KV pairs through the leader so applied index advances
L_PORT=$(grpc_port "$CUR_L")
info "Pumping 15 writes through leader (port ${L_PORT})..."
pump_writes 15 "$L_PORT"
sleep 4

IDX_LEADER=$(node_applied "$CUR_L")
IDX_ISO_AFTER=$(node_applied "$ISOLATED")
info "Leader applied index: ${IDX_LEADER}"
info "Isolated node applied index AFTER: ${IDX_ISO_AFTER}"

# Leader should have made progress; isolated node should be stuck
if [ "$IDX_LEADER" -gt "$IDX_BEFORE" ] 2>/dev/null; then
  pass "P1a: Leader committed ${IDX_LEADER} entries while follower was partitioned (Liveness ✓)"
else
  fail "P1a: Leader did not advance applied index — writes may have failed"
fi

if [ "$IDX_ISO_AFTER" -le "$IDX_BEFORE" ] 2>/dev/null; then
  pass "P1b: Isolated follower's index stalled at ${IDX_ISO_AFTER} (no commits without majority)"
else
  DRIFT=$((IDX_ISO_AFTER - IDX_BEFORE))
  fail "P1b: Isolated follower index advanced by ${DRIFT} — unexpected commit without quorum"
fi

# Verify isolated node did NOT become leader
ISO_STATE=$(node_state "$ISOLATED")
if [[ "$ISO_STATE" != "Leader" ]]; then
  pass "P1c: Isolated node stayed ${ISO_STATE} (no split-brain)"
else
  fail "P1c: Isolated node became Leader while partitioned — SPLIT-BRAIN"
fi

stop_chaos "$ISOLATED"
sleep 3


# ── P2: Majority Partition (Isolate the leader) ───────────────────────
echo -e "\n${BOLD}P2: Majority Partition — isolate leader from followers, verify step-down${RESET}"

CUR_L=$(leader)
info "Current leader: ${CUR_L}"
info "Dropping packets TO leader (followers can't reach it, leader can't hear quorum)"

BEFORE_LEADER=$CUR_L
drop_packets "$CUR_L"
info "🌩️  Chaos proxy active — 100% drop on leader ${CUR_L}"

# Wait for election timeout (followers notice leader gone → elect new one)
sleep 8
NEW_L=$(leader)
info "Leader after partition: '${NEW_L}'"

if [ -n "$NEW_L" ] && [ "$NEW_L" != "$BEFORE_LEADER" ]; then
  pass "P2a: Followers elected new leader ${NEW_L} after losing contact with old leader"
else
  fail "P2a: No new leader elected after leader was partitioned"
fi

# Old leader should be unreachable / step down
OLD_STATE=$(node_state "$BEFORE_LEADER")
info "Old leader ${BEFORE_LEADER} state: ${OLD_STATE}"
if [[ "$OLD_STATE" == "Follower" || "$OLD_STATE" == "Dead" || "$OLD_STATE" == "Candidate" ]]; then
  pass "P2b: Old leader stepped down (now ${OLD_STATE}) — no split-brain"
else
  fail "P2b: Old leader is still reporting as '${OLD_STATE}' — possible split-brain"
fi

stop_chaos "$BEFORE_LEADER"
sleep 3


# ── P3: Heal & Catch-up ───────────────────────────────────────────────
echo -e "\n${BOLD}P3: Heal & Catch-up — rejoin partitioned node, verify log catch-up${RESET}"

# Use the node that was isolated in P1
# Find the node with the lowest applied index (lagging)
LAGGING=$(cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n for n in d['nodes'] if n.get('alive')]
n=min(ns, key=lambda x: x.get('applied_index',0))
print(n['config']['id'])
" 2>/dev/null)

IDX_LAG_BEFORE=$(node_applied "$LAGGING")
CUR_L=$(leader)
IDX_LEAD=$(node_applied "$CUR_L")
DRIFT=$((IDX_LEAD - IDX_LAG_BEFORE))
info "Lagging node: ${LAGGING} (index=${IDX_LAG_BEFORE}, leader=${IDX_LEAD}, drift=${DRIFT})"

# Pump more writes to widen the gap
L_PORT=$(grpc_port "$CUR_L")
info "Pumping 20 more writes..."
pump_writes 20 "$L_PORT"
sleep 3

IDX_LEAD_NEW=$(node_applied "$CUR_L")
info "Leader index after 20 writes: ${IDX_LEAD_NEW}"

# Now wait for catch-up (log replication)
info "Waiting 8s for Raft log replication catch-up..."
sleep 8

IDX_LAG_AFTER=$(node_applied "$LAGGING")
CAUGHT=$((IDX_LAG_AFTER - IDX_LAG_BEFORE))
info "${LAGGING} index after heal: ${IDX_LAG_AFTER} (advanced by ${CAUGHT})"

if [ "$IDX_LAG_AFTER" -ge "$IDX_LEAD_NEW" ] 2>/dev/null; then
  pass "P3: Lagging node fully caught up (${IDX_LAG_AFTER} = leader ${IDX_LEAD_NEW}) — log replication ✓"
elif [ "$CAUGHT" -gt 0 ] 2>/dev/null; then
  pass "P3: Lagging node partially caught up (+${CAUGHT} entries, now at ${IDX_LAG_AFTER}/${IDX_LEAD_NEW})"
else
  fail "P3: Lagging node did not advance after partition healed"
fi


# ══════════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}${BOLD}═══ PHASE 2 COMPLETE ═══${RESET}"
echo -e "  ${GREEN}✅ PASS: ${PASS}${RESET}  |  ${RED}❌ FAIL: ${FAIL}${RESET}  |  ${YELLOW}⏭  SKIP: ${SKIP}${RESET}"
echo ""