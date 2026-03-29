# Core User Guide: How to Run the Distributed KV Cluster

This document explains how to provision, deploy, and interact with the 3-node Raft Key-Value Store on Google Cloud Platform (GCP).

## 🛠️ Prerequisites
Before starting, ensure you have:
1.  **A GCP Project:** (Ours is `raft-kv-756`).
2.  **`gcloud` CLI installed:** Logged in and set to your project:
    ```bash
    gcloud auth login
    gcloud config set project [YOUR_PROJECT_ID]
    ```
3.  **Go (Golang)** (1.20+): For cross-compiling the cluster binaries.
4.  **SSH Key:** Ensure your `ssh-agent` is running for automated VM access.

---

## 🚀 Step 1: Automated Deployment
We have built a **one-click orchestrator** that handles the complex job of provisioning VMs across zones, cross-compiling binaries for Linux, setting up firewall rules, and bootstrapping the Raft quorum.

Run the following command from this directory:
```bash
./dynamic_deploy.sh 3
```
*   **What this does:**
    1.  Provisioning 3 Google Compute Engine (GCE) VMs in `us-central1-a, b, c`.
    2.  Compiling the `kv-store` and `node-agent` binaries for Linux/AMD64.
    3.  Uploading files and bootstrapping the cluster via Internal IPs.
    4.  Providing a URL to the **Chaos Dashboard**.

---

## 📡 Step 2: Interacting with the Cluster
Once the cluster is "Ready," you can communicate with it using our **`kv-client`**.

### Read/Write Operations (Idempotent)
The client allows you to `Set` or `Get` keys. It automatically handles **Leader Redirects** (if you hit a follower, it will find the leader for you).

```bash
# Set a key
# Replace XXX with the External IP of node0 provided by the deploy script
./kv-client -addr=XXX:50051 -cmd=set -key=hello -val=world

# Get a key
./kv-client -addr=XXX:50051 -cmd=get -key=hello
```

### Checking Cluster Health
You can query the health status of a specific node:
```bash
./kv-client -addr=XXX:50051 -cmd=health
```

---

## 📊 Step 3: Monitoring & Chaos Testing
Open the **Chaos Dashboard** URL provided at the end of the deployment script (usually `http://[NODE0_EXTERNAL_IP]:8080`).

1.  **View State:** See which node is the "Leader," current Term, and applied log index.
2.  **Inject Faults:** Use buttons to kill/restart nodes or partition them to test Raft's resilience.

---

## 🔬 Step 4: Running Verification Scripts
We have provided automated test suites for each phase of validation. To run these, log into `node0`:
```bash
gcloud compute ssh node0 --zone=us-central1-a
# Inside node0, run a phase script (args: dashboard port)
bash ~/GCP_verify_phase1.sh 8080
```

---

## 🗑️ Step 5: Teardown (CRITICAL)
Always tear down your cluster to avoid incurring Google Cloud costs once you are done with testing:
```bash
./teardown.sh
```
*   **Note:** This script only iterates through 3 nodes. If you deployed more, you may need to delete them manually via the GCP console.
