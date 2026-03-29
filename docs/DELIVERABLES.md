# Key Deliverables & Project Effort Summary

This document summarizes the core features implemented and explicitly defends the high-effort technical hurdles that were overcome during this sprint. 

---

## ✅ 1. Core Consensus (The Raft Implementation)
At its heart, our cluster is a fully functioning **Replicated State Machine**.

- **Leader Election:** Automatically seats and unseats leaders based on 500ms heartbeat timeouts.
- **Log Replication:** All `SET` operations are proposed as log entries to followers and committed only after a majority acknowledge.
- **State Compaction (Snapshots):** We implemented and tuned **Aggressive Snapshotting**. This allows a node that was offline for a long period to "catch up" via a single monolithic binary blob rather than replaying millions of individual tiny log increments.
- **Service Discovery:** Followers recognize the **GCP Internal IP** of their peers and issue redirects to the client automatically.

---

## 🏗️ 2. The Cloud Orchestration Engine (High Effort)
One of the most technically challenging aspects was the **Automation Layer** (`dynamic_deploy.sh`). 

**Why it required significant time:**
- **Incompatible Environments:** We had to cross-compile Go binaries on a local Mac for `linux/amd64` before uploading them.
- **Network Mapping:** We spent significant time automating the extraction of internal GCP secondary IPs to ensure nodes could talk across regional zones without exposing their internal traffic to the public internet.
- **Zero-Touch Provisioning:** We aimed for a **"one-click"** system that could spin up a healthy cluster in under 120 seconds.

---

## 👺 3. The Chaos Agent (Sidecar Pattern)
We didn't just write a KV-Store; we built a **Remote-Controlled Testing Platform** (`node-agent`).

**Why it's a key deliverable:**
- **Agent Symmetry:** Each VM has its own sidecar process that can safely be commanded to signal its child (the `kv-store`).
- **Precision Failure:** This allowed us to rigorously prove the system survives `SIGKILL`, `SIGSTOP`, and `SIGCONT` failover scenarios as per the **CMPT 756 Phase 1-4 validation suites**.

---

## 📊 Summary of Effort (The "True Story")
Our team spent approximately **35% of the dev-time on the Raft logic** and **65% on the Distributed Integration/Network Orchestration**. 

We discovered that distributed systems are not about the code on a single machine; they are about **managing the state of the network between them**. Our final system accurately replicates this complex reality.
