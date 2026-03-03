#!/bin/bash
set -e

# Cleanup previous data and zombie processes
killall kv-store kv-chaos 2>/dev/null || true
lsof -ti:50051,50052,50053,12000,12001,12002 | xargs kill -9 2>/dev/null || true

rm -rf /tmp/raft-kv/
mkdir -p /tmp/raft-kv/

# Build binaries
go build -o kv-store
go build -o kv-client ./cmd/client/
go build -o kv-chaos ./cmd/chaos/

echo "Starting Node 0 (Leader)..."
./kv-store -id=node0 -raft=127.0.0.1:12000 -grpc=127.0.0.1:50051 -data=/tmp/raft-kv/node0 > /tmp/raft-kv/node0.log 2>&1 &
NODE0_PID=$!
sleep 2

echo "Starting Node 1..."
./kv-store -id=node1 -raft=127.0.0.1:12001 -grpc=127.0.0.1:50052 -data=/tmp/raft-kv/node1 -join=127.0.0.1:50051 > /tmp/raft-kv/node1.log 2>&1 &
NODE1_PID=$!
sleep 1

echo "Starting Node 2..."
./kv-store -id=node2 -raft=127.0.0.1:12002 -grpc=127.0.0.1:50053 -data=/tmp/raft-kv/node2 -join=127.0.0.1:50051 > /tmp/raft-kv/node2.log 2>&1 &
NODE2_PID=$!
sleep 1

echo "Cluster started with PIDs: $NODE0_PID, $NODE1_PID, $NODE2_PID"
echo "Check logs at /tmp/raft-kv/nodeX.log"
echo "You can now test client writes with './kv-client -cmd=set -key=hello -val=world'"

# Trap SIGINT and shutdown processes gracefully
cleanup() {
    echo "Killing nodes..."
    killall kv-store kv-chaos 2>/dev/null || true
    lsof -ti:50051,50052,50053,12000,12001,12002 | xargs kill -9 2>/dev/null || true
    exit 0
}

trap cleanup SIGINT SIGTERM
wait
