#!/bin/bash
# verify_follower_read.sh — Local 3-node test for read-index follower reads (FEAT-RI)
#
# Starts a 3-node local cluster, writes a key via the leader, then confirms that
# followers with --follower-read serve the correct value without redirecting.
# Also verifies a missing-key read from a follower returns "not found" (not a redirect).
#
# Usage: bash verify_follower_read.sh
# Requires: kv-store and kv-client binaries built in project root.

set -euo pipefail

PASS=0
FAIL=0

pass() { echo "[PASS] $1"; PASS=$((PASS+1)); }
fail() { echo "[FAIL] $1"; FAIL=$((FAIL+1)); }

# ---- Cleanup & Build ----
echo "=== Building binaries ==="
killall kv-store 2>/dev/null || true
lsof -ti:50051,50052,50053,12000,12001,12002 2>/dev/null | xargs kill -9 2>/dev/null || true
rm -rf /tmp/raft-kv-fri/
mkdir -p /tmp/raft-kv-fri/

go build -o kv-store . 2>&1
go build -o kv-client ./cmd/client/ 2>&1
echo "Build complete."

# ---- Start Cluster ----
echo ""
echo "=== Starting 3-node cluster ==="

./kv-store -id=node0 -raft=127.0.0.1:12000 -grpc=127.0.0.1:50051 -data=/tmp/raft-kv-fri/node0 \
    > /tmp/raft-kv-fri/node0.log 2>&1 &
sleep 2

./kv-store -id=node1 -raft=127.0.0.1:12001 -grpc=127.0.0.1:50052 -data=/tmp/raft-kv-fri/node1 \
    -join=127.0.0.1:50051 > /tmp/raft-kv-fri/node1.log 2>&1 &
sleep 1

./kv-store -id=node2 -raft=127.0.0.1:12002 -grpc=127.0.0.1:50053 -data=/tmp/raft-kv-fri/node2 \
    -join=127.0.0.1:50051 > /tmp/raft-kv-fri/node2.log 2>&1 &
sleep 2

echo "Cluster started."

# ---- Helper: wait for leader ----
wait_for_leader() {
    local max_attempts=20
    for i in $(seq 1 $max_attempts); do
        if ./kv-client -cmd=health -addrs=127.0.0.1:50051,127.0.0.1:50052,127.0.0.1:50053 2>/dev/null | grep -q "Leader"; then
            return 0
        fi
        sleep 0.5
    done
    echo "ERROR: No leader elected after ${max_attempts} attempts"
    return 1
}

echo "Waiting for leader election..."
wait_for_leader
echo "Leader elected."

# ---- Test 1: Set key via leader path ----
echo ""
echo "=== Test 1: Set key via leader ==="
if ./kv-client -cmd=set -key=testkey -val=hello123 -addrs=127.0.0.1:50051,127.0.0.1:50052,127.0.0.1:50053 2>/dev/null | grep -q "successful"; then
    pass "T1: Set testkey=hello123 via leader"
else
    fail "T1: Set failed"
fi

# Small pause for replication to followers
sleep 0.5

# ---- Test 2: Follower read from node1 ----
echo ""
echo "=== Test 2: Follower read from node1 (50052) ==="
OUT=$(./kv-client -cmd=get -key=testkey -addr=127.0.0.1:50052 -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "hello123"; then
    pass "T2: node1 follower read returned correct value (hello123)"
else
    fail "T2: node1 follower read did not return hello123. Got: $OUT"
fi

# ---- Test 3: Follower read from node2 ----
echo ""
echo "=== Test 3: Follower read from node2 (50053) ==="
OUT=$(./kv-client -cmd=get -key=testkey -addr=127.0.0.1:50053 -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "hello123"; then
    pass "T3: node2 follower read returned correct value (hello123)"
else
    fail "T3: node2 follower read did not return hello123. Got: $OUT"
fi

# ---- Test 4: Follower read of missing key returns not-found (not redirect) ----
echo ""
echo "=== Test 4: Missing key via follower read ==="
OUT=$(./kv-client -cmd=get -key=doesnotexist -addr=127.0.0.1:50052 -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "not found"; then
    pass "T4: Missing key correctly returned 'not found' from follower"
else
    fail "T4: Expected 'not found', got: $OUT"
fi

# ---- Test 5: Normal (non-follower-read) Get still works via leader redirect ----
echo ""
echo "=== Test 5: Normal Get (no --follower-read) still redirects correctly ==="
OUT=$(./kv-client -cmd=get -key=testkey -addrs=127.0.0.1:50051,127.0.0.1:50052,127.0.0.1:50053 2>/dev/null || true)
if echo "$OUT" | grep -q "hello123"; then
    pass "T5: Normal leader-redirect Get still returns correct value"
else
    fail "T5: Normal Get failed. Got: $OUT"
fi

# ---- Test 6: Multiple writes, follower read sees latest ----
echo ""
echo "=== Test 6: Follower read reflects latest write ==="
./kv-client -cmd=set -key=testkey -val=updated456 -addrs=127.0.0.1:50051,127.0.0.1:50052,127.0.0.1:50053 2>/dev/null | grep -q "successful" || true
sleep 0.3
OUT=$(./kv-client -cmd=get -key=testkey -addr=127.0.0.1:50052 -follower-read 2>/dev/null || true)
if echo "$OUT" | grep -q "updated456"; then
    pass "T6: Follower read reflects updated value (updated456)"
else
    fail "T6: Follower read stale or wrong. Got: $OUT"
fi

# ---- Cleanup ----
echo ""
echo "=== Shutting down cluster ==="
killall kv-store 2>/dev/null || true

# ---- Summary ----
echo ""
echo "========================================"
echo "  Follower Read Tests: $PASS passed, $FAIL failed"
echo "========================================"
if [ "$FAIL" -eq 0 ]; then
    echo "  ALL TESTS PASSED"
    exit 0
else
    echo "  SOME TESTS FAILED"
    exit 1
fi
