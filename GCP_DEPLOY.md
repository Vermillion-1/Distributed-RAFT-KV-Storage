# GCP Deployment — Step by Step (Absolute Beginner Guide)

This guide assumes you have never used Google Cloud before.
Follow every step in order. Do not skip anything.

---

## Step 1 — Create a GCP Account and Project

1. Go to [https://cloud.google.com](https://cloud.google.com) and sign in with your Google account.
2. Click **"Console"** in the top right corner.
3. At the top of the Console, click the project dropdown → **"New Project"**.
4. Give it a name (e.g. `raft-kv-demo`) and click **Create**.
5. Note your **Project ID** — it looks like `raft-kv-demo-123456`.
   - This is NOT the project name. Get the exact ID from the top dropdown.

> **⚠️ Billing:** GCP requires a billing account to create VMs.  
> Go to **Billing → Link a billing account** and connect a card.  
> e2-micro VMs cost ~$0.01/hour each — 3 VMs for a 2-hour session ≈ **$0.06 total**.  
> Always run `bash teardown.sh` when done to avoid charges.

---

## Step 2 — Install gcloud CLI on Your Mac

Open Terminal and run:

```bash
# Download and install gcloud
curl https://sdk.cloud.google.com | bash
```

When it asks "Modify profile to update PATH?" → type **Y** and press Enter.

Then **close and reopen Terminal** (or run `exec -l $SHELL`), then verify:

```bash
gcloud version
# Should print: Google Cloud SDK 460.0.0 (or similar)
```

---

## Step 3 — Authenticate gcloud

```bash
gcloud auth login
```

- A browser window will open. Log in with the same Google account you used for GCP.
- Come back to Terminal — it should say "You are now logged in as: your@gmail.com"

```bash
gcloud auth application-default login
```

- Same thing — opens browser, log in again.
- This step is needed for deployment scripts to work.

---

## Step 4 — Enable Required APIs

GCP has features that must be "turned on" per-project. Run:

```bash
# Replace YOUR_PROJECT_ID with your actual Project ID from Step 1
export GCP_PROJECT=YOUR_PROJECT_ID

gcloud config set project $GCP_PROJECT

gcloud services enable compute.googleapis.com
# Should print: Operation finished successfully
```

---

## Step 5 — Navigate to the Project Folder

```bash
cd "/Users/ankushsingh/Desktop/CMPT 756/Distributed-RAFT-KV-Storage-prototype"
```

Confirm you're in the right place:

```bash
ls
# You should see: deploy.sh  teardown.sh  main.go  GCP_DEPLOY.md  etc.
```

---

## Step 6 — Run the Deploy Script

```bash
export GCP_PROJECT=YOUR_PROJECT_ID    # paste your Project ID here
bash deploy.sh
```

**This will take 5–10 minutes.** You will see output like:

```
🖥  Creating 3 VMs...
  Creating node0 in us-central1-a...
  Creating node1 in us-central1-b...
  Creating node2 in us-central1-c...
✅ VMs created.
🔥 Configuring firewall...
🔨 Cross-compiling for linux/amd64...
✅ kv-store
✅ node-agent
...
🚀 Starting node-agent on node0 (bootstrap node)...
  Waiting for node0 agent to report alive...
  ✅ node0 alive.
🚀 Starting node-agent on node1 and node2...
  ✅ node1 alive.
  ✅ node2 alive.
📊 Starting kv-dashboard on node0...
```

At the end it will print a table like:
```
═══════════════════════════════════════════════════════════════
 Cluster Ready
═══════════════════════════════════════════════════════════════
 ✅ node0  internal=10.128.0.2  external=34.66.x.x
 ✅ node1  internal=10.128.0.3  external=35.184.x.x
 ✅ node2  internal=10.128.0.4  external=34.72.x.x

 Dashboard: http://34.66.x.x:8080
═══════════════════════════════════════════════════════════════
```

**Write down the `external` IPs — you will need them.**

---

## Step 7 — Open the Dashboard

Copy the Dashboard URL from the output above and paste it into your browser.

```
http://<node0-external-ip>:8080
```

You should see the Chaos Dashboard with 3 nodes — one marked **Leader**.

> If the page doesn't load after 30 seconds, see Troubleshooting below.

---

## Step 8 — Run the Test Suite from VM-0

The test scripts must run from inside the GCP network (from VM-0), because the
gRPC addresses used internally are private IPs that aren't reachable from your laptop.

SSH into VM-0:

```bash
gcloud compute ssh node0 --zone=us-central1-a
```

- The first time, it will generate SSH keys. When asked "Enter passphrase" → just press **Enter** twice (no passphrase).
- You are now inside VM-0. Your terminal prompt will change to something like `ankushsingh@node0:~$`.

Upload the test scripts to VM-0 (run this from your laptop in a **separate terminal tab**):

```bash
cd "/Users/ankushsingh/Desktop/CMPT 756/Distributed-RAFT-KV-Storage-prototype"

gcloud compute scp verify.sh verify_phase2.sh verify_phase3.sh verify_phase4.sh \
  node0:~/ --zone=us-central1-a
```

Now go back to your SSH terminal (inside VM-0) and run each phase:

```bash
# Phase 1 — Liveness and Election (~2 min)
bash ~/verify.sh 8080
# Expected: L1 PASS, L2 SKIP, L3 PASS

# Phase 2 — Network Partitions (~3 min)
bash ~/verify_phase2.sh 8080
# Expected: 7/7 PASS

# Phase 3 — Latency and Resource Faults (~3 min)
bash ~/verify_phase3.sh 8080
# Expected: R1 PASS, R2 PASS, R3 PASS (may vary slightly)

# Phase 4 — Durability (~5 min)
bash ~/verify_phase4.sh 8080
# Expected: D1 PASS, D2 PASS, D3 PASS
```

> **Record your MTTR from Phase 1** (time to elect a new leader after kill).
> On GCP this is typically 1–3s instead of ~1s locally — due to real network latency.
> This number goes in your report's evaluation section.

---

## Step 9 — Teardown (IMPORTANT — stops billing)

When you are done with all testing, **always** run teardown to delete the VMs:

```bash
# Back on your laptop (exit the SSH session first with: exit)
export GCP_PROJECT=YOUR_PROJECT_ID
bash teardown.sh
```

Output:
```
🗑  Deleting VMs...
🔥 Deleting firewall rule...
✅ Cluster torn down.
```

Confirm in the GCP Console: **Compute Engine → VM instances** → list should be empty.

---

## Troubleshooting

| Problem | What to do |
|---|---|
| `deploy.sh` fails with "API not enabled" | Run: `gcloud services enable compute.googleapis.com` |
| `deploy.sh` fails with "quota exceeded" | Go to GCP Console → IAM → Quotas, request more e2-micro quota |
| Dashboard page doesn't load | Run `curl http://<node0-ext-ip>:9000/health` — if that works, dashboard may still be starting (wait 30s and refresh) |
| SSH hangs on "Waiting for SSH" | VM is still booting — wait 30s and retry |
| `gcloud compute ssh` asks for a passphrase | Just press Enter (no passphrase needed) |
| All nodes show as "Dead" in dashboard | Agent isn't running — SSH into VM-0 and check: `cat ~/agent.log` |
| Phase 2 SIGSTOP tests fail | Confirm Block B dashboard update is in place — check `cmd/dashboard/main.go` has `-agent-addrs` flag |
| Node-1 or Node-2 never comes up | Node-0 may not have finished leader election — deploy.sh waits, but if it timed out, SSH into VM-1 and check `~/agent.log` |

---

## Quick Reference — Key Commands

```bash
# Deploy
export GCP_PROJECT=YOUR_PROJECT_ID && bash deploy.sh

# SSH into VM-0
gcloud compute ssh node0 --zone=us-central1-a

# Check agent health on any VM
curl http://<vm-external-ip>:9000/health

# Teardown (stops all billing)
bash teardown.sh

# View VM list in terminal
gcloud compute instances list
```
