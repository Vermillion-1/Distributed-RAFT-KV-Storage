#!/bin/bash
# deploy.sh — GCP_TODO.md Block C1
# Provisions a 3-node Raft KV cluster on GCP VMs.
# Usage: export GCP_PROJECT=your-project-id && bash deploy.sh
set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
PROJECT="${GCP_PROJECT:?GCP_PROJECT env var is required}"
REGION="us-central1"
ZONES=("us-central1-a" "us-central1-b" "us-central1-c")
NODES=("node0" "node1" "node2")
MACHINE="e2-micro"
IMAGE_FAMILY="debian-12"
IMAGE_PROJECT="debian-cloud"
FIREWALL_RULE="raft-cluster"
TAG="raft-node"
BIN_DIR="$(pwd)/bin"

RAFT_PORTS="12000 12001 12002"
GRPC_PORTS="50051 50052 50053"
AGENT_PORT="9000"
DASHBOARD_PORT="8080"

# ── Step 0: Preflight ─────────────────────────────────────────────────────────
echo "🔍 Checking prerequisites..."
gcloud config set project "$PROJECT"
gcloud auth print-access-token > /dev/null 2>&1 || { echo "❌ Run: gcloud auth login"; exit 1; }
go version > /dev/null 2>&1 || { echo "❌ Go not found"; exit 1; }
echo "✅ Project: $PROJECT"

# ── Step 1: Create VMs ────────────────────────────────────────────────────────
echo ""
echo "🖥  Creating 3 VMs..."
for i in 0 1 2; do
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
  gcloud compute firewall-rules create "$FIREWALL_RULE" \
    --allow="tcp:12000-12002,tcp:50051-50053,tcp:${AGENT_PORT},tcp:${DASHBOARD_PORT}" \
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
echo "✅ Binaries built: $(ls "$BIN_DIR" | tr '\n' ' ')"

# ── Step 4: Get internal IPs ──────────────────────────────────────────────────
echo ""
echo "📡 Fetching VM internal IPs (waiting for VMs to be ready)..."
sleep 15  # give VMs time to initialise networking
INT_IPS=()
for i in 0 1 2; do
  IP=$(gcloud compute instances describe "${NODES[$i]}" \
    --zone="${ZONES[$i]}" \
    --format='get(networkInterfaces[0].networkIP)')
  INT_IPS+=("$IP")
  echo "  ${NODES[$i]} internal IP: $IP"
done

# ── Step 4.5: Cleanup existing processes & state (Idempotency) ───────────────
echo ""
echo "🧹 Cleaning up any existing cluster processes and old data..."
for i in 0 1 2; do
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "pkill -f '[n]ode-agent|[k]v-store|[k]v-dashboard|[k]v-chaos' || true; sudo rm -rf /data/raft-kv/*"
done
echo "✅ Cleanup complete."

# ── Step 5: SCP binaries to each VM ──────────────────────────────────────────
echo ""
echo "📦 Uploading binaries to VMs..."
for i in 0 1 2; do
  echo "  → ${NODES[$i]}..."
  gcloud compute scp "$BIN_DIR"/* "${NODES[$i]}":~/ \
    --zone="${ZONES[$i]}" \
    --quiet
  
  # node0 needs the dashboard HTML file and the verify scripts
  if [ "$i" -eq 0 ]; then
    gcloud compute ssh "${NODES[0]}" --zone="${ZONES[0]}" --quiet -- "mkdir -p ~/cmd/dashboard"
    gcloud compute scp cmd/dashboard/index.html "${NODES[0]}":~/cmd/dashboard/ \
      --zone="${ZONES[0]}" \
      --quiet
    gcloud compute scp verify.sh verify_phase2.sh verify_phase3.sh verify_phase4.sh GCP_verify_phase1.sh GCP_verify_phase2.sh GCP_verify_phase3.sh GCP_verify_phase4.sh "${NODES[0]}":~/ \
      --zone="${ZONES[0]}" \
      --quiet
  fi
done
echo "✅ Binaries uploaded."

# ── Step 5b: Ensure binaries are executable on each VM ───────────────────────
echo ""
echo "🔑 Setting execute permissions on each VM..."
for i in 0 1 2; do
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "chmod +x ~/node-agent ~/kv-store ~/kv-dashboard ~/kv-chaos ~/kv-client"
done
echo "✅ Permissions set."

# ── Step 6: Prepare data directory on each VM ────────────────────────────────
echo ""
echo "💾 Creating data directory on each VM..."
for i in 0 1 2; do
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "sudo mkdir -p /data/raft-kv && sudo chmod 777 /data/raft-kv"
done
echo "✅ Data directories ready."

# ── Step 7: Start node-agent on node0 first ──────────────────────────────────
# node0 bootstraps the cluster; node1/2 join it, so node0 must be healthy first.
echo ""
echo "🚀 Starting node-agent on node0 (bootstrap node)..."
KV_ARGS0="-id=node0 -raft=${INT_IPS[0]}:12000 -grpc=${INT_IPS[0]}:50051 -data=/data/raft-kv"
gcloud compute ssh "${NODES[0]}" --zone="${ZONES[0]}" --quiet -- \
  "nohup ./node-agent -kv-bin=./kv-store -kv-args='${KV_ARGS0}' > agent.log 2>&1 </dev/null & sleep 1"

# Poll node0 /health until alive (up to 60s)
echo "  Waiting for node0 agent to report alive..."
EXT0=$(gcloud compute instances describe "${NODES[0]}" \
  --zone="${ZONES[0]}" \
  --format='get(networkInterfaces[0].accessConfigs[0].natIP)')
DEADLINE=$((SECONDS + 60))
until curl -sf "http://${EXT0}:${AGENT_PORT}/health" 2>/dev/null | grep -q '"alive":true'; do
  [ $SECONDS -ge $DEADLINE ] && { echo "❌ node0 agent did not come up in 60s"; exit 1; }
  echo "    ...waiting (${SECONDS}s elapsed)"
  sleep 3
done
echo "  ✅ node0 alive."

# ── Step 8: Start node-agent on node1 and node2 ───────────────────────────────
echo ""
echo "🚀 Starting node-agent on node1 and node2..."
for i in 1 2; do
  RAFT_PORT=$((12000 + i))
  GRPC_PORT=$((50051 + i))
  KV_ARGS="-id=${NODES[$i]} -raft=${INT_IPS[$i]}:${RAFT_PORT} -grpc=${INT_IPS[$i]}:${GRPC_PORT} -data=/data/raft-kv -join=${INT_IPS[0]}:50051"
  echo "  Starting ${NODES[$i]}..."
  gcloud compute ssh "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet -- \
    "nohup ./node-agent -kv-bin=./kv-store -kv-args='${KV_ARGS}' > agent.log 2>&1 </dev/null & sleep 1"
done

# Poll node1 and node2 /health until all alive
echo "  Waiting for node1 and node2 agents..."
EXT_IPS=("$EXT0")
for i in 1 2; do
  EXT=$(gcloud compute instances describe "${NODES[$i]}" \
    --zone="${ZONES[$i]}" \
    --format='get(networkInterfaces[0].accessConfigs[0].natIP)')
  EXT_IPS+=("$EXT")
done

for i in 1 2; do
  DEADLINE=$((SECONDS + 60))
  until curl -sf "http://${EXT_IPS[$i]}:${AGENT_PORT}/health" 2>/dev/null | grep -q '"alive":true'; do
    [ $SECONDS -ge $DEADLINE ] && { echo "❌ ${NODES[$i]} agent did not come up in 60s"; exit 1; }
    echo "    ...${NODES[$i]} not yet alive (${SECONDS}s elapsed)"
    sleep 3
  done
  echo "  ✅ ${NODES[$i]} alive."
done

# ── Step 9: Start dashboard on node0 ─────────────────────────────────────────
echo ""
echo "📊 Starting kv-dashboard on node0..."
AGENT_ADDRS="node0=${INT_IPS[0]}:${AGENT_PORT},node1=${INT_IPS[1]}:${AGENT_PORT},node2=${INT_IPS[2]}:${AGENT_PORT}"
gcloud compute ssh "${NODES[0]}" --zone="${ZONES[0]}" --quiet -- \
  "nohup ./kv-dashboard -nodes=3 -port=${DASHBOARD_PORT} -agent-addrs='${AGENT_ADDRS}' > dashboard.log 2>&1 </dev/null & sleep 1"
sleep 2
echo "✅ Dashboard started."

# ── Step 10: Print cluster IP table ──────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo " Cluster Ready"
echo "═══════════════════════════════════════════════════════════════"
for i in 0 1 2; do
  RAFT_PORT=$((12000 + i))
  GRPC_PORT=$((50051 + i))
  echo " ✅ ${NODES[$i]}  internal=${INT_IPS[$i]}  external=${EXT_IPS[$i]}"
  echo "           Raft=:${RAFT_PORT}  gRPC=:${GRPC_PORT}  Agent=:${AGENT_PORT}"
done
echo ""
echo " Dashboard: http://${EXT_IPS[0]}:${DASHBOARD_PORT}"
echo ""
echo " To run Phase 1 tests from VM-0:"
echo "   gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify.sh ${DASHBOARD_PORT}'"
echo "═══════════════════════════════════════════════════════════════"
