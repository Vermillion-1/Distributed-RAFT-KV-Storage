#!/bin/bash
# GCP_verify_phase7.sh — Phase 7: Read-Index Follower Reads (FEAT-RI, v1.3)
# Adapted for GCP: Queries the live Phase 1-6 cluster instead of a local loopback cluster.

set -euo pipefail

PASS=0
FAIL=0

pass() { echo -e "  ✅ [PASS] $1"; PASS=$((PASS+1)); }
fail() { echo -e "  ❌ [FAIL] $1"; FAIL=$((FAIL+1)); }

API="http://localhost:8080/api"
KV_CLIENT="${HOME}/kv-client"

echo "=== Gathering Cluster Network Info ==="
if ! curl -sf "${API}/cluster" > /dev/null 2>&1; then
    echo "ERROR: Dashboard API unreachable. Is the node agent running?"
    exit 1
fi

# Map out the Addresses
ALL_ADDRS=$(curl -s "${API}/cluster" | python3 -c "import sys,json; print(','.join([n['config']['grpc_addr'] for n in json.load(sys.stdin)['nodes'] if n.get('alive')]))")
LEADER_ADDR=$(curl -s "${API}/cluster" | python3 -c "import sys,json; l=[n['config']['grpc_addr'] for n in json.load(sys.stdin)['nodes'] if n.get('state')=='Leader']; print(l[0] if l else '')")
FOLLOWER_ADDR=$(curl -s "${API}/cluster" | python3 -c "import sys,json; f=[n['config']['grpc_addr'] for n in json.load(sys.stdin)['nodes'] if n.get('state')=='Follower']; print(f[0] if f else '')")

if [ -z "$LEADER_ADDR" ] || [ -z "$FOLLOWER_ADDR" ]; then
    echo "ERROR: Could not find both a Leader and a Follower in the live cluster."
    exit 1
fi

echo "All Nodes: $ALL_ADDRS"
echo "Leader:    $LEADER_ADDR"
echo "Follower:  $FOLLOWER_ADDR"


# ---- Test 1: Set key via smart client (all addrs) ----
echo ""
echo "=== Test 1: Set key via Leader ==="
if $KV_CLIENT -cmd=set -key=testkey -val=hello123 -addrs="$ALL_ADDRS" 2>/dev/null | grep -q "successful"; then
    pass "T1: Set testkey=hello123 via cluster"
else
    fail "T1: Set failed"
fi

sleep 0.5 # Small replication pause

# ---- Test 2: Follower read from specific follower node ----
echo ""
echo "=== Test 2: Follower read from $FOLLOWER_ADDR ==="
OUT=$($KV_CLIENT -cmd=get -key=testkey -addr="$FOLLOWER_ADDR" -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "hello123"; then
    pass "T2: Follower read returned correct value (hello123)"
else
    fail "T2: Follower read did not return hello123. Got: $OUT"
fi

# ---- Test 3: Follower read of missing key returns not-found ----
echo ""
echo "=== Test 3: Missing key via follower read ==="
OUT=$($KV_CLIENT -cmd=get -key=doesnotexist -addr="$FOLLOWER_ADDR" -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "not found"; then
    pass "T3: Missing key correctly returned 'not found' from follower"
else
    fail "T3: Expected 'not found', got: $OUT"
fi

# ---- Test 4: Multiple writes, follower read sees latest ----
echo ""
echo "=== Test 4: Follower read reflects latest write ==="
$KV_CLIENT -cmd=set -key=testkey -val=updated456 -addrs="$ALL_ADDRS" 2>/dev/null | grep -q "successful" || true
sleep 0.3
OUT=$($KV_CLIENT -cmd=get -key=testkey -addr="$FOLLOWER_ADDR" -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "updated456"; then
    pass "T4: Follower read reflects updated value (updated456)"
else
    fail "T4: Follower read stale or wrong. Got: $OUT"
fi

# ---- Test 5: Normal Get (no --follower-read) still works via leader redirect ----
echo ""
echo "=== Test 5: Normal Get (no --follower-read) still redirects correctly ==="
OUT=$($KV_CLIENT -cmd=get -key=testkey -addrs="$ALL_ADDRS" 2>/dev/null || true)
if echo "$OUT" | grep -q "updated456"; then
    pass "T5: Normal leader-redirect Get still returns correct value"
else
    fail "T5: Normal Get failed. Got: $OUT"
fi

# ---- Summary ----
echo ""
echo "========================================"
echo "  Follower Read Tests: $PASS passed, $FAIL failed"
echo "========================================"
if [ "$FAIL" -eq 0 ]; then
    exit 0
else
    exit 1
fi
