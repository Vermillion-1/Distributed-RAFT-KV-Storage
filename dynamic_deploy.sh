#!/bin/bash
# dynamic_deploy.sh — Scalable N-Node Cluster Provisioning
# Usage: export GCP_PROJECT=your-project-id && bash dynamic_deploy.sh [NODE_COUNT]
set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
PROJECT="${GCP_PROJECT:?GCP_PROJECT env var is required}"
NODE_COUNT="${1:-3}"
REGION="us-central1"

echo "Scaling cluster to ${NODE_COUNT} nodes..."

# Dynamically generate nodes and wrap-around zones to distribute compute load
AVAILABLE_ZONES=("us-central1-a" "us-central1-c")
NODES=()
ZONES=()
for i in $(seq 0 $((NODE_COUNT - 1))); do
  NODES+=("node${i}")
  ZONES+=("${AVAILABLE_ZONES[$((i % ${#AVAILABLE_ZONES[@]}))]}")
done

MACHINE="e2-micro"
IMAGE_FAMILY="debian-12"
IMAGE_PROJECT="debian-cloud"
FIREWALL_RULE="raft-cluster"
TAG="raft-node"
BIN_DIR="$(pwd)/bin"

AGENT_PORT="9000"
DASHBOARD_PORT="8080"

# ── Step 0: Preflight ─────────────────────────────────────────────────────────
echo "🔍 Checking prerequisites..."
gcloud config set project "$PROJECT"
gcloud auth print-access-token > /dev/null 2>&1 || { echo "❌ Run: gcloud auth login"; exit 1; }
go version > /dev/null 2>&1 || { echo "❌ Go not found"; exit 1; }
echo "✅ Project: $PROJECT | Nodes: $NODE_COUNT"

# ── Step 1: Create VMs ────────────────────────────────────────────────────────
echo ""
echo "🖥  Creating $NODE_COUNT VMs..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  NODE="${NODES[$i]}"
  ZONE="${ZONES[$i]}"
  echo "  Creating $NODE in $ZONE (if not exists)..."
  gcloud compute instances create "$NODE" \
    --zone="$ZONE" \
    --machine-type="$MACHINE" \
    --image-family="$IMAGE_FAMILY" \
    --image-project="$IMAGE_PROJECT" \
    --boot-disk-size=10GB \
    --tags="$TAG" \
    --quiet || true
done
echo "✅ VMs created."

# ── Step 2: Firewall rule ─────────────────────────────────────────────────────
echo ""
echo "🔥 Configuring firewall..."
if gcloud compute firewall-rules describe "$FIREWALL_RULE" --quiet > /dev/null 2>&1; then
  echo "  Firewall rule '$FIREWALL_RULE' already exists — skipping."
else
  # NOTE: To scale flawlessly, we open broad ranges for Raft and gRPC
  # so nodes 0 through 99 all get ports 12000-12099 and 50050-50150 automatically
  gcloud compute firewall-rules create "$FIREWALL_RULE" \
    --allow="tcp:12000-12099,tcp:50051-50150,tcp:${AGENT_PORT},tcp:${DASHBOARD_PORT}" \
    --target-tags="$TAG" \
    --source-ranges="0.0.0.0/0" \
    --quiet
  echo "  ✅ Firewall rule created."
fi

# ── Step 3: Cross-compile binaries ───────────────────────────────────────────
echo ""
echo "🔨 Cross-compiling for linux/amd64..."
mkdir -p "$BIN_DIR"
GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/kv-store"     .
GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/kv-client"    ./cmd/client/
GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/kv-chaos"     ./cmd/chaos/
GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/kv-dashboard" ./cmd/dashboard/
GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/node-agent"   ./cmd/agent/
tar -czf "$BIN_DIR/binaries.tar.gz" -C "$BIN_DIR" kv-store kv-client kv-chaos kv-dashboard node-agent
echo "✅ Binaries built and bundled: binaries.tar.gz"

# ── Step 4: Get internal IPs ──────────────────────────────────────────────────
echo ""
echo "📡 Fetching VM internal IPs (waiting for VMs to be SSH-ready)..."
# Poll each VM until SSH is accepting connections (up to 90s).
# The previous flat sleep 15 was a race: on a loaded GCP zone sshd can take longer.
for i in $(seq 0 $((NODE_COUNT - 1))); do
  NODE="${NODES[$i]}"; ZONE="${ZONES[$i]}"
  echo "  Waiting for ${NODE} SSH..."
  for attempt in $(seq 1 18); do
    if gcloud compute ssh "$NODE" --zone="$ZONE" --ssh-flag="-T" --quiet -- "true" 2>/dev/null; then
      echo "    ${NODE} ready (attempt ${attempt})"
      break
    fi
    [ "$attempt" -eq 18 ] && { echo "❌ ${NODE} not SSH-ready after 90s"; exit 1; }
    sleep 5
  done
done
INT_IPS=()
for i in $(seq 0 $((NODE_COUNT - 1))); do
  IP=$(gcloud compute instances describe "${NODES[$i]}" \
    --zone="${ZONES[$i]}" \
    --format='get(networkInterfaces[0].networkIP)')
  INT_IPS+=("$IP")
  echo "  ${NODES[$i]} internal IP: $IP"
done

# ── Step 4.5: Cleanup existing processes & state (Idempotency) ───────────────
echo ""
echo "🧹 Cleaning up any existing cluster processes and old data..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "pkill -f '[n]ode-agent|[k]v-store|[k]v-dashboard|[k]v-chaos' || true; sudo rm -rf /data/raft-kv/*"
done
echo "✅ Cleanup complete."

# ── Step 5: SCP binaries to each VM ──────────────────────────────────────────
echo ""
echo "📦 Uploading binaries to VMs..."
SCP_FLAGS="--scp-flag=-oServerAliveInterval=30 --scp-flag=-oServerAliveCountMax=5"
for i in $(seq 0 $((NODE_COUNT - 1))); do
  echo "  → ${NODES[$i]}..."
  gcloud compute scp "$BIN_DIR/binaries.tar.gz" "${NODES[$i]}":~/ \
    --zone="${ZONES[$i]}" \
    $SCP_FLAGS \
    --quiet
  
  # Extract binaries
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "tar -xzf ~/binaries.tar.gz -C ~/ && rm ~/binaries.tar.gz"
  
  # Each node needs the dashboard HTML file for decentralized dashboard (S5a)
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- "mkdir -p ~/cmd/dashboard"
  gcloud compute scp cmd/dashboard/index.html "${NODES[$i]}":~/cmd/dashboard/ \
    --zone="${ZONES[$i]}" \
    --quiet

  # Only node0 needs the verify scripts (for running tests)
  if [ "$i" -eq 0 ]; then
    gcloud compute scp verify.sh verify_phase2.sh verify_phase3.sh verify_phase4.sh \
      GCP_verify_phase1.sh GCP_verify_phase2.sh GCP_verify_phase3.sh GCP_verify_phase4.sh \
      GCP_verify_phase5.sh GCP_verify_phase6.sh \
      "${NODES[0]}":~/ \
      --zone="${ZONES[0]}" \
      --quiet
  fi
done
echo "✅ Binaries uploaded."

# ── Step 5b: Ensure binaries are executable on each VM ───────────────────────
echo ""
echo "🔑 Setting execute permissions on each VM..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "chmod +x ~/node-agent ~/kv-store ~/kv-dashboard ~/kv-chaos ~/kv-client; find ~/ -maxdepth 1 -name '*.sh' -exec chmod +x {} +"
done
echo "✅ Permissions set."

# ── Step 6: Prepare data directory on each VM ────────────────────────────────
echo ""
echo "💾 Creating data directory on each VM..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "sudo mkdir -p /data/raft-kv && sudo chmod 777 /data/raft-kv"
done
echo "✅ Data directories ready."

# ── Step 7: Start node-agent on node0 first ──────────────────────────────────
# node0 bootstraps the cluster; node1/N join it, so node0 must be healthy first.
echo ""
echo "🚀 Starting node-agent on node0 (bootstrap node)..."
KV_ARGS0="-id=${NODES[0]} -raft=${INT_IPS[0]}:12000 -grpc=${INT_IPS[0]}:50051 -data=/data/raft-kv"
# Build peer addresses: all nodes EXCEPT node0
PEER_ADDRS0=""
for j in $(seq 1 $((NODE_COUNT - 1))); do
  PEER_ADDRS0+="${INT_IPS[$j]}:$((12000 + j)),"
done
PEER_ADDRS0="${PEER_ADDRS0%,}"
gcloud compute ssh "${NODES[0]}" --zone="${ZONES[0]}" --quiet -- \
  "nohup ./node-agent -kv-bin=./kv-store -kv-args='${KV_ARGS0}' -peer-raft-addrs='${PEER_ADDRS0}' > agent.log 2>&1 </dev/null & sleep 1"

# Poll node0 /health until alive (up to 60s)
echo "  Waiting for node0 agent to report alive..."
EXT0=$(gcloud compute instances describe "${NODES[0]}" \
  --zone="${ZONES[0]}" \
  --format='get(networkInterfaces[0].accessConfigs[0].natIP)')
EXT_IPS=("$EXT0")
DEADLINE=$((SECONDS + 60))
until curl -sf "http://${EXT0}:${AGENT_PORT}/health" 2>/dev/null | grep -q '"alive":true'; do
  [ $SECONDS -ge $DEADLINE ] && { echo "❌ node0 agent did not come up in 60s"; exit 1; }
  echo "    ...waiting (${SECONDS}s elapsed)"
  sleep 3
done
echo "  ✅ node0 alive."

# ── Step 8: Start node-agent on node1 through nodeN ───────────────────────────
if [ "$NODE_COUNT" -gt 1 ]; then
  echo ""
  echo "🚀 Starting node-agent on remaining $((NODE_COUNT - 1)) nodes..."
  for i in $(seq 1 $((NODE_COUNT - 1))); do
    RAFT_PORT=$((12000 + i))
    GRPC_PORT=$((50051 + i))
    KV_ARGS="-id=${NODES[$i]} -raft=${INT_IPS[$i]}:${RAFT_PORT} -grpc=${INT_IPS[$i]}:${GRPC_PORT} -data=/data/raft-kv -join=${INT_IPS[0]}:50051"
    # Build peer addresses: all nodes except node i
    PEER_ADDRS=""
    for j in $(seq 0 $((NODE_COUNT - 1))); do
      if [ "$j" -ne "$i" ]; then
        PEER_ADDRS+="${INT_IPS[$j]}:$((12000 + j)),"
      fi
    done
    PEER_ADDRS="${PEER_ADDRS%,}"
    echo "  Starting ${NODES[$i]}..."
    gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
      "nohup ./node-agent -kv-bin=./kv-store -kv-args='${KV_ARGS}' -peer-raft-addrs='${PEER_ADDRS}' > agent.log 2>&1 </dev/null & sleep 1"
  done

  # Poll remaining nodes
  echo "  Waiting for remaining agents..."
  for i in $(seq 1 $((NODE_COUNT - 1))); do
    EXT=$(gcloud compute instances describe "${NODES[$i]}" \
      --zone="${ZONES[$i]}" \
      --format='get(networkInterfaces[0].accessConfigs[0].natIP)')
    EXT_IPS+=("$EXT")
  done

  for i in $(seq 1 $((NODE_COUNT - 1))); do
    DEADLINE=$((SECONDS + 60))
    until curl -sf "http://${EXT_IPS[$i]}:${AGENT_PORT}/health" 2>/dev/null | grep -q '"alive":true'; do
      [ $SECONDS -ge $DEADLINE ] && { echo "❌ ${NODES[$i]} agent did not come up in 60s"; exit 1; }
      echo "    ...${NODES[$i]} not yet alive (${SECONDS}s elapsed)"
      sleep 3
    done
    echo "  ✅ ${NODES[$i]} alive."
  done
fi

# ── Step 9: Start dashboard on ALL nodes (Decentralized Dashboard - S5a) ─────
echo ""
echo "📊 Starting kv-dashboard on ALL nodes (decentralized mode)..."
AGENT_ADDRS=""
for i in $(seq 0 $((NODE_COUNT - 1))); do
  AGENT_ADDRS+="${NODES[$i]}=${INT_IPS[$i]}:${AGENT_PORT},"
done
AGENT_ADDRS=${AGENT_ADDRS%,} # trim trailing comma

# Start dashboard on each node so the UI is available from any node
for i in $(seq 0 $((NODE_COUNT - 1))); do
  echo "  Starting dashboard on ${NODES[$i]}..."
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "nohup ./kv-dashboard -nodes=${NODE_COUNT} -port=${DASHBOARD_PORT} -agent-addrs='${AGENT_ADDRS}' > dashboard.log 2>&1 </dev/null & sleep 1"
done
sleep 2
echo "✅ Dashboard started on all nodes."

# ── Step 10: Print cluster IP table ──────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo " Cluster Ready (${NODE_COUNT} Nodes)"
echo "═══════════════════════════════════════════════════════════════"
for i in $(seq 0 $((NODE_COUNT - 1))); do
  RAFT_PORT=$((12000 + i))
  GRPC_PORT=$((50051 + i))
  echo " ✅ ${NODES[$i]}  internal=${INT_IPS[$i]}  external=${EXT_IPS[$i]}"
  echo "           Raft=:${RAFT_PORT}  gRPC=:${GRPC_PORT}  Agent=:${AGENT_PORT}"
done
echo ""
echo " 📊 Dashboard (available on ANY node):"
for i in $(seq 0 $((NODE_COUNT - 1))); do
  echo "   http://${EXT_IPS[$i]}:${DASHBOARD_PORT}  (${NODES[$i]})"
done
echo "═══════════════════════════════════════════════════════════════"
