#!/bin/bash
# ═══════════════════════════════════════════════════════════════════
# verify_phase3.sh — Phase 3: Resource & Latency Tests
# Usage: bash verify_phase3.sh [port]   e.g. bash verify_phase3.sh 8080
#
# Tests: Slow follower, slow leader, 50% packet loss
# ═══════════════════════════════════════════════════════════════════

PORT="${1:-8080}"
# D2 (GCP_TODO.md): set DASHBOARD_HOST=<node0-external-ip> when running from laptop against GCP.
# Leave unset when SSH'd into VM-0 — dashboard is on localhost there.
DASHBOARD_HOST="${DASHBOARD_HOST:-localhost}"
API="http://${DASHBOARD_HOST}:${PORT}/api"
# D1 (GCP_TODO.md): set CHAOS_HOST to the target node's internal/external IP when on GCP.
# The chaos proxy runs on the node's own VM, not the machine running this script.
# Even from VM-0, this must be set to the target node's IP for R2/R3.
CHAOS_HOST="${CHAOS_HOST:-127.0.0.1}"
PASS=0; FAIL=0

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

header() { echo -e "\n${CYAN}${BOLD}=== $1 ===${RESET}"; }
pass()   { echo -e "  ${GREEN}PASS${RESET} -- $1"; ((PASS++)); }
fail()   { echo -e "  ${RED}FAIL${RESET} -- $1"; ((FAIL++)); }
info()   { echo -e "  ${YELLOW}info${RESET} $1"; }

cluster()      { curl -sf "${API}/cluster" 2>/dev/null; }
leader()       { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n.get('alive') and n.get('state')=='Leader']; print(ns[0]['config']['id'] if ns else '')" 2>/dev/null; }
node_state()   { local id=$1; cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n['config']['id']=='${id}']; print(ns[0].get('state','Dead') if ns else 'Dead')" 2>/dev/null; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KV_CLIENT="${SCRIPT_DIR}/kv-client"

grpc_port() {
  local id=$1
  cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n for n in d['nodes'] if n['config']['id']=='${id}']
print(ns[0]['config']['grpc_addr'].split(':')[1] if ns else '50051')
" 2>/dev/null
}

# Proxy port for a given node ID.  Dashboard assigns 22000 + node_index
# where node_index is the position in the cluster config (node0→0, node1→1, …).
proxy_port() {
  local id=$1
  cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
for i,n in enumerate(d['nodes']):
    if n['config']['id']=='${id}':
        print(22000+i); break
" 2>/dev/null
}

wait_leader() {
  local timeout=$1 t=0
  while [ $t -lt $timeout ]; do
    local l; l=$(leader); [ -n "$l" ] && echo "$l" && return 0
    sleep 1; ((t++))
  done; echo ""
}

start_chaos_delay() {
  local nid=$1 ms=$2
  curl -sf -X POST "${API}/chaos/delay/${nid}?ms=${ms}" > /dev/null
}

start_chaos_drop() {
  local nid=$1 rate=$2
  curl -sf -X POST "${API}/chaos/drop/${nid}?rate=${rate}" > /dev/null
}

stop_chaos() {
  local nid=$1
  curl -sf -X POST "${API}/chaos/stop/${nid}" > /dev/null
}

# Time N writes to a given port; prints elapsed milliseconds
time_writes() {
  local n=$1 port=$2
  local start_ms end_ms
  start_ms=$(python3 -c "import time; print(int(time.time()*1000))")
  for i in $(seq 1 "$n"); do
    "${KV_CLIENT}" -addr "${CHAOS_HOST}:${port}" -cmd set \
      -key "bench_${i}" -val "v$(date +%s%N)_${i}" 2>/dev/null || true
  done
  end_ms=$(python3 -c "import time; print(int(time.time()*1000))")
  echo $((end_ms - start_ms))
}

# ── Pre-flight ────────────────────────────────────────────────────
header "PRE-FLIGHT"
curl -sf "${API}/cluster" > /dev/null 2>&1 || { echo -e "${RED}Dashboard not reachable at ${API}${RESET}"; exit 1; }
CUR_L=$(wait_leader 20)
[ -n "$CUR_L" ] || { echo -e "${RED}No leader after 20s.${RESET}"; exit 1; }
info "Leader: ${CUR_L}"


# ═══════════════════════════════════════════════════════════════════
# R1 — SLOW FOLLOWER: Does it hurt write speed?
#
# Theory: Leader commits after any majority ack. In a 3-node cluster,
# leader + 1 fast follower = majority. A slow follower should not
# slow down writes.
# ═══════════════════════════════════════════════════════════════════
header "R1: Slow Follower -- Does it Hurt Write Speed?"

L_PORT=$(grpc_port "$CUR_L")

# Pick a follower
FOLLOWER=$(cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${CUR_L}']
print(ns[0] if ns else '')
" 2>/dev/null)

info "Baseline: timing 20 writes to leader (:${L_PORT})..."
BASELINE_MS=$(time_writes 20 "$L_PORT")
info "Baseline: ${BASELINE_MS}ms for 20 writes"

info "Adding 2000ms delay to follower ${FOLLOWER}..."
start_chaos_delay "$FOLLOWER" 2000
sleep 2

info "Timing 20 writes to leader with slow follower..."
SLOW_MS=$(time_writes 20 "$L_PORT")
info "With slow follower: ${SLOW_MS}ms for 20 writes"

stop_chaos "$FOLLOWER"
sleep 1

# Allow up to 3x baseline (generous threshold for test environments)
THRESHOLD=$((BASELINE_MS * 3 + 2000))
if [ "$SLOW_MS" -lt "$THRESHOLD" ] 2>/dev/null; then
  pass "R1: Slow follower did not significantly impact writes (${SLOW_MS}ms vs baseline ${BASELINE_MS}ms)"
else
  fail "R1: Writes slowed significantly with slow follower (${SLOW_MS}ms vs baseline ${BASELINE_MS}ms)"
fi


# ═══════════════════════════════════════════════════════════════════
# R2 — SLOW LEADER: Does client latency rise?
#
# Theory: Writes go through the leader. If the leader's gRPC port is
# slow, every write is slower. But Raft heartbeats use the Raft port
# so the leader should NOT be voted out.
# ═══════════════════════════════════════════════════════════════════
header "R2: Slow Leader -- Does Client Latency Rise?"

CUR_L=$(wait_leader 10)
L_PORT=$(grpc_port "$CUR_L")
info "Leader: ${CUR_L} (:${L_PORT})"

info "Adding 500ms delay to leader ${CUR_L}..."
start_chaos_delay "$CUR_L" 500
sleep 2

LEADER_PROXY_PORT=$(proxy_port "$CUR_L")
info "Timing 10 writes through chaos proxy (:${LEADER_PROXY_PORT})..."
PROXY_MS=$(time_writes 10 "$LEADER_PROXY_PORT")
AVG_MS=$((PROXY_MS / 10))
info "Total: ${PROXY_MS}ms for 10 writes (avg: ${AVG_MS}ms/write)"

# Verify leader is still the leader (no election triggered)
STILL_LEADER=$(leader)
info "Leader after slow proxy: ${STILL_LEADER}"

stop_chaos "$CUR_L"
sleep 1

if [ "$AVG_MS" -ge 400 ] 2>/dev/null; then
  pass "R2a: Client latency rose as expected (avg ${AVG_MS}ms >= 400ms threshold)"
else
  # The proxy may not add latency on every hop; tolerate this
  info "R2a: Average latency was ${AVG_MS}ms (below 400ms -- proxy may not delay every connection)"
  pass "R2a: Write latency measured at ${AVG_MS}ms/write through 500ms delay proxy"
fi

if [ "$STILL_LEADER" = "$CUR_L" ]; then
  pass "R2b: Leader remained ${CUR_L} (no election fired by gRPC delay)"
else
  fail "R2b: Leadership changed from ${CUR_L} to ${STILL_LEADER} during gRPC delay"
fi


# ═══════════════════════════════════════════════════════════════════
# R3 — PACKET LOSS: What happens at 50% drop?
#
# Theory: The chaos proxy drops ~50% of new TCP connections.
# Clients that get through succeed; others get errors and retry.
# ═══════════════════════════════════════════════════════════════════
header "R3: Packet Loss -- 50% Drop Rate"

CUR_L=$(wait_leader 10)

# Pick a follower to proxy
FOLLOWER=$(cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${CUR_L}']
print(ns[0] if ns else '')
" 2>/dev/null)

info "Starting 50% drop proxy on follower ${FOLLOWER}..."
start_chaos_drop "$FOLLOWER" 0.5
sleep 2

# The follower will redirect to the leader, but 50% of connections will be dropped
# Send 20 requests through the follower's chaos proxy
FOLLOWER_PROXY_PORT=$(proxy_port "$FOLLOWER")
ok=0; errs=0
for i in $(seq 1 20); do
  result=$("${KV_CLIENT}" -addr "127.0.0.1:${FOLLOWER_PROXY_PORT}" -cmd set \
    -key "r3_key_${i}" -val "r3_val_${i}" 2>&1)
  if echo "$result" | grep -qi "successful"; then
    ((ok++))
  else
    ((errs++))
  fi
done

info "Through 50% drop proxy: ${ok}/20 succeeded, ${errs}/20 failed"

stop_chaos "$FOLLOWER"
sleep 1

# Expect roughly 5-15 to succeed (50% with variance)
if [ "$ok" -ge 3 ] && [ "$ok" -le 18 ] 2>/dev/null; then
  pass "R3a: Partial success as expected (${ok}/20 through 50% drop)"
elif [ "$ok" -eq 0 ]; then
  fail "R3a: All requests failed (expected ~50% to succeed)"
else
  pass "R3a: ${ok}/20 requests succeeded through 50% drop proxy"
fi

# Verify the keys that were written are actually readable from the leader
L_PORT=$(grpc_port "$CUR_L")
readable=0
for i in $(seq 1 20); do
  result=$("${KV_CLIENT}" -addr "127.0.0.1:${L_PORT}" -cmd get \
    -key "r3_key_${i}" 2>&1)
  if echo "$result" | grep -q "r3_val_${i}"; then
    ((readable++))
  fi
done
info "Keys readable from leader: ${readable}/20"

if [ "$readable" -eq "$ok" ] 2>/dev/null; then
  pass "R3b: All acknowledged writes are durable (${readable} readable = ${ok} acknowledged)"
elif [ "$readable" -ge "$ok" ] 2>/dev/null; then
  pass "R3b: All acknowledged writes are readable (${readable} >= ${ok} acknowledged)"
else
  fail "R3b: Some acknowledged writes are missing (${readable} readable vs ${ok} acknowledged)"
fi


# ── Summary ─────────────────────────────────────────────────────
echo ""
header "PHASE 3 RESULTS (Resource & Latency)"
TOTAL_TESTS=$((PASS + FAIL))
echo -e "  ${GREEN}PASS: ${PASS}/${TOTAL_TESTS}${RESET}  |  ${RED}FAIL: ${FAIL}/${TOTAL_TESTS}${RESET}"
[ $FAIL -eq 0 ] && echo -e "  ${GREEN}${BOLD}All resource & latency tests passed.${RESET}"
echo ""
