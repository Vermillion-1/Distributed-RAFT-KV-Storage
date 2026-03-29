#!/bin/bash
# teardown.sh — GCP_TODO.md Block C2
# Deletes all 3 cluster VMs and the firewall rule.
# Usage: bash teardown.sh
set -euo pipefail

PROJECT="${GCP_PROJECT:?GCP_PROJECT env var is required}"
FIREWALL_RULE="raft-cluster"

gcloud config set project "$PROJECT"

echo "🗑  Deleting VMs..."
gcloud compute instances delete node0 --zone=us-central1-a --quiet
gcloud compute instances delete node1 --zone=us-central1-b --quiet
gcloud compute instances delete node2 --zone=us-central1-c --quiet

echo "🔥 Deleting firewall rule..."
gcloud compute firewall-rules delete "$FIREWALL_RULE" --quiet

echo "✅ Cluster torn down. Verify in GCP console: Compute Engine → VM instances → empty."
