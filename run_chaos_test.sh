#!/bin/bash
# ⚠️  RETIRED (GCP_TODO.md Block D3) — localhost only. Spawns its own local cluster.
# Use deploy.sh to bring up the GCP cluster, then run verify_phase*.sh from VM-0.
# Kept in repo for local development reference only. Do NOT run on GCP.
# ============================================================================
# Fault-Tolerance Chaos Test Suite
# Tests: Normal Operation, Crash Failure, Recovery, Receive Omission, Send Omission
# Maps to CMPT 756 Fault Tolerance concepts (03-756-FT.pdf)
# ============================================================================

PASS_COUNT=0
FAIL_COUNT=0

pass() {
    echo "✅ PASS: $1"
    PASS_COUNT=$((PASS_COUNT + 1))
}

fail() {
    echo "❌ FAIL: $1"
    FAIL_COUNT=$((FAIL_COUNT + 1))
}

echo "Building binaries..."
go build -o kv-store
go build -o kv-client ./cmd/client/
go build -o kv-chaos ./cmd/chaos/

echo "Starting 3-Node Raft Cluster..."
./start_cluster.sh &
CLUSTER_PID=$!
sleep 5 # Wait for leader election and cluster stabilization

echo ""
echo "=========================================="
echo "TEST 1: Basic Set and Get (Normal Operation)"
echo "  Demonstrates: Replicated State Machine, Leader Election (Slide 17, 30)"
echo "=========================================="
./kv-client -cmd=set -key=foo -val=bar -addr=127.0.0.1:50051
sleep 1

# Assert: Get from a different node (tests cross-node redirect via leader)
RESULT=$(./kv-client -cmd=get -key=foo -addr=127.0.0.1:50052 2>/dev/null)
if echo "$RESULT" | grep -q "foo=bar"; then
    pass "Normal set/get works with cross-node redirect"
else
    fail "Expected 'foo=bar', got '$RESULT'"
fi

echo ""
echo "=========================================="
echo "TEST 2: Crash Failure (Leader Dies)"
echo "  Demonstrates: Fail-stop/Crash failure model, Leader Election (Slide 3-4, 30-32)"
echo "=========================================="

# Find the actual leader using Health endpoint
LEADER_PORT=""
for PORT in 50051 50052 50053; do
    HEALTH=$(./kv-client -cmd=health -key=dummy -addr=127.0.0.1:$PORT 2>/dev/null)
    if echo "$HEALTH" | grep -q "State: Leader"; then
        LEADER_PORT=$PORT
        break
    fi
done

if [ -z "$LEADER_PORT" ]; then
    fail "Could not find leader"
else
    echo "Leader detected on port $LEADER_PORT"
    echo "Killing Leader (Port $LEADER_PORT)..."
    ELECTION_START=$(python3 -c "import time; print(time.time())")
    kill $(lsof -t -i:$LEADER_PORT) 2>/dev/null
    sleep 3 # Wait for Raft to elect a new leader

    # Find a surviving node
    SURVIVING_PORT=""
    for PORT in 50051 50052 50053; do
        if [ "$PORT" != "$LEADER_PORT" ]; then
            SURVIVING_PORT=$PORT
            break
        fi
    done

    # Measure MTTR: time from kill to new leader available (Resiliency metric, Slide 8)
    echo "Attempting to write to surviving node $SURVIVING_PORT..."
    WRITE_RESULT=$(./kv-client -cmd=set -key=foo -val=baz -addr=127.0.0.1:$SURVIVING_PORT 2>/dev/null)
    ELECTION_END=$(python3 -c "import time; print(time.time())")
    MTTR=$(python3 -c "print(f'{$ELECTION_END - $ELECTION_START:.2f}')")
    echo "  MTTR (approx): ${MTTR}s"

    if echo "$WRITE_RESULT" | grep -q "successful"; then
        pass "Write succeeded after leader crash (MTTR: ${MTTR}s)"
    else
        fail "Write failed after leader crash: '$WRITE_RESULT'"
    fi

    # Verify read from another surviving node
    OTHER_PORT=""
    for PORT in 50051 50052 50053; do
        if [ "$PORT" != "$LEADER_PORT" ] && [ "$PORT" != "$SURVIVING_PORT" ]; then
            OTHER_PORT=$PORT
            break
        fi
    done

    if [ -n "$OTHER_PORT" ]; then
        RESULT=$(./kv-client -cmd=get -key=foo -addr=127.0.0.1:$OTHER_PORT 2>/dev/null)
        if echo "$RESULT" | grep -q "foo=baz"; then
            pass "Updated value readable from other surviving node"
        else
            fail "Expected 'foo=baz' from node $OTHER_PORT, got '$RESULT'"
        fi
    fi
fi

echo ""
echo "=========================================="
echo "TEST 3: Restarting Crashed Node (Recovery)"
echo "  Demonstrates: Log-based Recovery, Checkpointing (Slide 10-12)"
echo "=========================================="
echo "Restarting crashed node on port $LEADER_PORT..."

# Map port back to node config
case $LEADER_PORT in
    50051)
        ./kv-store -id=node0 -raft=127.0.0.1:12000 -grpc=127.0.0.1:50051 -data=/tmp/raft-kv/node0 -join=127.0.0.1:$SURVIVING_PORT > /tmp/raft-kv/node0-restart.log 2>&1 &
        ;;
    50052)
        ./kv-store -id=node1 -raft=127.0.0.1:12001 -grpc=127.0.0.1:50052 -data=/tmp/raft-kv/node1 -join=127.0.0.1:$SURVIVING_PORT > /tmp/raft-kv/node1-restart.log 2>&1 &
        ;;
    50053)
        ./kv-store -id=node2 -raft=127.0.0.1:12002 -grpc=127.0.0.1:50053 -data=/tmp/raft-kv/node2 -join=127.0.0.1:$SURVIVING_PORT > /tmp/raft-kv/node2-restart.log 2>&1 &
        ;;
esac
NODE_RESTART_PID=$!
sleep 3

echo "Verifying recovered node caught up (Raft log replay)..."
RESULT=$(./kv-client -cmd=get -key=foo -addr=127.0.0.1:$LEADER_PORT 2>/dev/null)
if echo "$RESULT" | grep -q "foo=baz"; then
    pass "Recovered node has correct state after rejoining"
else
    fail "Expected 'foo=baz' from recovered node, got '$RESULT'"
fi

echo ""
echo "=========================================="
echo "TEST 4: Receive Omission (100% Drop Rate)"
echo "  Demonstrates: Receive Omission failure model (Slide 3-4)"
echo "  Message type: Lost Messages (Slide 6)"
echo "=========================================="
echo "Starting Chaos Proxy dropping 100% of packets to Node 2..."
./kv-chaos -listen=127.0.0.1:22002 -target=127.0.0.1:50053 -drop=1.0 &
PROXY_PID=$!
sleep 2

echo "Attempting to query through proxy (should timeout, not hang)..."
RESULT=$(timeout 5 ./kv-client -cmd=get -key=foo -addr=127.0.0.1:22002 2>&1)
EXIT_CODE=$?

# Client should timeout gracefully (Circuit Breaker pattern, Slide 9)
if [ $EXIT_CODE -ne 0 ] || echo "$RESULT" | grep -qi "error\|fail\|timeout"; then
    pass "Client handled receive omission gracefully (timeout/error, no hang)"
else
    fail "Expected timeout/error, got: '$RESULT'"
fi

kill $PROXY_PID 2>/dev/null

echo ""
echo "=========================================="
echo "TEST 5: Send Omission (High Latency Delay)"
echo "  Demonstrates: Send Omission failure model (Slide 3-4)"
echo "  Message type: Delayed Messages (Slide 6)"
echo "=========================================="
echo "Starting Chaos Proxy adding 3000ms delay to Node 2..."
./kv-chaos -listen=127.0.0.1:22002 -target=127.0.0.1:50053 -delay=3000 &
PROXY_PID2=$!
sleep 2

echo "Attempting to query through proxy (should be slow or timeout)..."
DELAY_START=$(python3 -c "import time; print(time.time())")
RESULT=$(timeout 10 ./kv-client -cmd=get -key=foo -addr=127.0.0.1:22002 2>&1)
DELAY_END=$(python3 -c "import time; print(time.time())")
ELAPSED=$(python3 -c "print(f'{$DELAY_END - $DELAY_START:.2f}')")

echo "  Elapsed time: ${ELAPSED}s"
# Either the request took > 1s (delay applied) or it timed out entirely
if python3 -c "exit(0 if float($ELAPSED) > 1.0 else 1)"; then
    pass "Send omission caused measurable delay (${ELAPSED}s)"
else
    fail "Expected significant delay, but completed in ${ELAPSED}s"
fi

kill $PROXY_PID2 2>/dev/null

echo ""
echo "=========================================="
echo "Test Complete. Shutting down."
echo "=========================================="
echo ""
echo "Results: $PASS_COUNT passed, $FAIL_COUNT failed"
echo ""

# Cleanup
kill $CLUSTER_PID 2>/dev/null
kill $NODE_RESTART_PID 2>/dev/null
killall kv-store kv-chaos 2>/dev/null || true

if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
exit 0
