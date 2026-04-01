#!/bin/bash

# --- GCP Resource Auditor ---
# Run this script to verify that your GCP project is clean and not incurring costs.

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

echo "🔍 Auditing GCP Project Resources..."
echo "-----------------------------------"

# 1. Check for Running Compute Instances
INSTANCE_COUNT=$(gcloud compute instances list --format="value(name)" | wc -l | tr -d ' ')

if [ "$INSTANCE_COUNT" -gt 0 ]; then
    echo -e "${RED}⚠️  ALERT: You have $INSTANCE_COUNT active Compute Engine instance(s) running.${NC}"
    gcloud compute instances list --format="table(name,zone,status,networkInterfaces[0].accessConfigs[0].natIP:label=EXTERNAL_IP)"
else
    echo -e "${GREEN}✅ No active Compute Engine instances found.${NC}"
fi

# 2. Check for Custom Firewall Rules
FIREWALL_COUNT=$(gcloud compute firewall-rules list --filter="name~'raft-kv' OR name~'node-agent-dash'" --format="value(name)" | wc -l | tr -d ' ')

if [ "$FIREWALL_COUNT" -gt 0 ]; then
    echo -e "${RED}⚠️  ALERT: You have $FIREWALL_COUNT custom firewall rule(s) remaining.${NC}"
    gcloud compute firewall-rules list --filter="name~'raft-kv' OR name~'node-agent-dash'" --format="table(name,allow)"
else
    echo -e "${GREEN}✅ No project-specific firewall rules remaining.${NC}"
fi

echo "-----------------------------------"

# Summary
if [ "$INSTANCE_COUNT" -eq 0 ] && [ "$FIREWALL_COUNT" -eq 0 ]; then
    echo -e "${GREEN}🌟 VERDICT: Your GCP project is 100% idle. No Compute Engine costs are being incurred.${NC}"
else
    echo -e "${RED}❌ VERDICT: You have active resources. Please run ./teardown.sh to stop billing.${NC}"
fi
