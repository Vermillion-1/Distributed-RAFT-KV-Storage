#!/bin/bash
# teardown.sh — Tears down N-node GCP cluster provisioned by dynamic_deploy.sh
# Usage: export GCP_PROJECT=your-project-id && bash teardown.sh [NODE_COUNT]
set -uo pipefail

PROJECT="${GCP_PROJECT:?GCP_PROJECT env var is required}"
NODE_COUNT="${1:-3}"
FIREWALL_RULE="raft-cluster"

# Must match dynamic_deploy.sh exactly
AVAILABLE_ZONES=("us-central1-a" "us-central1-c")
NODES=()
ZONES=()
for i in $(seq 0 $((NODE_COUNT - 1))); do
 NODES+=("node${i}")
 ZONES+=("${AVAILABLE_ZONES[$((i % ${#AVAILABLE_ZONES[@]}))]}")
done

gcloud config set project "$PROJECT"

echo " Deleting VMs..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
 gcloud compute instances delete "${NODES[$i]}" --zone="${ZONES[$i]}" --quiet
done

echo " Deleting firewall rule..."
gcloud compute firewall-rules delete "$FIREWALL_RULE" --quiet

echo "✅ Cluster torn down."
