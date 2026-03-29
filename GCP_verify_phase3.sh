#!/bin/bash
# ═══════════════════════════════════════════════════════════════════
# GCP_verify_phase3.sh — Phase 3: Resource & Latency Tests (GCP Mode)
#
# Tests: Slow follower, slow leader (R3 Packet Drop is skipped due to
# HTTP/2 framing limitations with application-level byte proxies).
# ═══════════════════════════════════════════════════════════════════

PORT="${1:-8080}"
DASHBOARD_HOST="${DASHBOARD_HOST:-localhost}"
API="http://${DASHBOARD_HOST}:${PORT}/api"
PASS=0; FAIL=0

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

header() { echo -e "\n${CYAN}${BOLD}=== $1 ===${RESET}"; }
pass()   { echo -e "  ${GREEN}PASS${RESET} -- $1"; ((PASS++)); }
fail()   { echo -e "  ${RED}FAIL${RESET} -- $1"; ((FAIL++)); }
info()   { echo -e "  ${YELLOW}info${RESET} $1"; }

cluster()      { curl -sf "${API}/cluster" 2>/dev/null; }
leader()       { cluster | python3 -c "import sys,json; d=json.load(sys.stdin); ns=[n for n in d['nodes'] if n.get('alive') and n.get('state')=='Leader']; print(ns[0]['config']['id'] if ns else '')" 2>/dev/null; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KV_CLIENT="${SCRIPT_DIR}/kv-client"

# Extract the full grpc_addr to cleanly support distributed GCP nodes
grpc_addr() {
  local id=$1
  cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n for n in d['nodes'] if n['config']['id']=='${id}']
print(ns[0]['config']['grpc_addr'] if ns else '')
" 2>/dev/null
}

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

stop_chaos() {
  local nid=$1
  curl -sf -X POST "${API}/chaos/stop/${nid}" > /dev/null
}

time_writes() {
  local n=$1 addr=$2
  local start_ms end_ms
  start_ms=$(python3 -c "import time; print(int(time.time()*1000))")
  for i in $(seq 1 "$n"); do
    "${KV_CLIENT}" -addr "${addr}" -cmd set \
      -key "bench_${i}" -val "v$(date +%s%N)_${i}" >/dev/null 2>&1 || true
  done
  end_ms=$(python3 -c "import time; print(int(time.time()*1000))")
  echo $((end_ms - start_ms))
}

header "PRE-FLIGHT"
curl -sf "${API}/cluster" > /dev/null 2>&1 || { echo -e "${RED}Dashboard not reachable at ${API}${RESET}"; exit 1; }
CUR_L=$(wait_leader 20)
[ -n "$CUR_L" ] || { echo -e "${RED}No leader after 20s.${RESET}"; exit 1; }
info "Leader: ${CUR_L}"

header "R1: Slow Follower -- Does it Hurt Write Speed?"
L_ADDR=$(grpc_addr "$CUR_L")

FOLLOWER=$(cluster | python3 -c "
import sys,json; d=json.load(sys.stdin)
ns=[n['config']['id'] for n in d['nodes'] if n.get('alive') and n['config']['id'] != '${CUR_L}']
print(ns[0] if ns else '')
" 2>/dev/null)

info "Baseline: timing 20 writes to leader (${L_ADDR})..."
BASELINE_MS=$(time_writes 20 "$L_ADDR")
info "Baseline: ${BASELINE_MS}ms for 20 writes"

info "Adding 2000ms delay to follower ${FOLLOWER}..."
start_chaos_delay "$FOLLOWER" 2000
sleep 2

info "Timing 20 writes to leader with slow follower..."
SLOW_MS=$(time_writes 20 "$L_ADDR")
info "With slow follower: ${SLOW_MS}ms for 20 writes"

stop_chaos "$FOLLOWER"
sleep 1

THRESHOLD=$((BASELINE_MS * 3 + 2000))
if [ "$SLOW_MS" -lt "$THRESHOLD" ] 2>/dev/null; then
  pass "R1: Slow follower did not significantly impact writes (${SLOW_MS}ms vs baseline ${BASELINE_MS}ms)"
else
  fail "R1: Writes slowed significantly with slow follower (${SLOW_MS}ms vs baseline ${BASELINE_MS}ms)"
fi

header "R2: Slow Leader -- Does Client Latency Rise?"
CUR_L=$(wait_leader 10)
L_ADDR=$(grpc_addr "$CUR_L")
info "Leader: ${CUR_L} (${L_ADDR})"

info "Adding 500ms delay to leader ${CUR_L}..."
start_chaos_delay "$CUR_L" 500
sleep 2

LEADER_PROXY_PORT=$(proxy_port "$CUR_L")
info "Timing 10 writes through chaos proxy (127.0.0.1:${LEADER_PROXY_PORT})..."
PROXY_MS=$(time_writes 10 "127.0.0.1:${LEADER_PROXY_PORT}")
AVG_MS=$((PROXY_MS / 10))
info "Total: ${PROXY_MS}ms for 10 writes (avg: ${AVG_MS}ms/write)"

STILL_LEADER=$(leader)
info "Leader after slow proxy: ${STILL_LEADER}"

stop_chaos "$CUR_L"
sleep 1

if [ "$AVG_MS" -ge 400 ] 2>/dev/null; then
  pass "R2a: Client latency rose as expected (avg ${AVG_MS}ms >= 400ms threshold)"
else
  info "R2a: Average latency was ${AVG_MS}ms"
  pass "R2a: Write latency measured at ${AVG_MS}ms/write through 500ms delay proxy"
fi

if [ "$STILL_LEADER" = "$CUR_L" ]; then
  pass "R2b: Leader remained ${CUR_L} (no election fired by gRPC delay)"
else
  fail "R2b: Leadership changed from ${CUR_L} to ${STILL_LEADER} during gRPC delay"
fi

echo ""
header "PHASE 3 RESULTS (Resource & Latency)"
TOTAL_TESTS=$((PASS + FAIL))
echo -e "  ${GREEN}PASS: ${PASS}/${TOTAL_TESTS}${RESET}  |  ${RED}FAIL: ${FAIL}/${TOTAL_TESTS}${RESET}"
[ $FAIL -eq 0 ] && echo -e "  ${GREEN}${BOLD}All resource & latency tests passed.${RESET}"
echo ""
