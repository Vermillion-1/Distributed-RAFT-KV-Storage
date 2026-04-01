# Future Planning & Evolutionary Roadmap: Raft KV Store

This document is a frank and honest "North Star" for where this project could go after the academic submission. It is designed to maximize **Developer Learning**, **Recruiter Appeal**, and **Open Source Viability**.

---

## 🏗️ 1. Scaling the "Developer Learning" (Deep Systems Engineering)
To truly "master" distributed systems, the next steps are about the edge cases that separate a prototype from a production system.

### A. Dynamic Membership Changes (Joint Consensus)
*   **The Problem:** Currently, if you want a 5-node cluster instead of 3, you have to restart the whole cluster.
*   **The Learning:** Implement **"Joint Consensus" (Raft Chapter 6)**. This allows a cluster to transition from `N` to `M` nodes safely without ever risking a split-brain. It is one of the most intellectually difficult parts of the Raft algorithm.
*   **Analogy:** This is like changing the tires while the car is moving at 60mph.

### B. High-Performance Persistence (Pluggable LSM Trees)
*   **The Problem:** BoltDB is simple but has its limits in high-write scenarios.
*   **The Learning:** Create a "Persistence Interface" and implement **LevelDB** or **BadgerDB**. Learning how **Log-Structured Merge (LSM) Trees** differ from B-Trees (like BoltDB) is essential for any backend engineer.

---

## 💼 2. Maximizing Recruiter Appeal (The "WOW" Factors)
If you want this on your resume or top-tier GitHub, you need "Hard Evidence" of reliability and performance.

### A. Jepsen Testing (Automated Correctness Verification)
*   **The Strategy:** Use the **Jepsen** framework (or a Go-port) to rigorously prove your system's consistency claims. 
*   **The Appeal:** Showing a recruiter a Jepsen report that says *"Experimental proof of Linearizability under partial network failure"* is an instant hire for a Cloud Infrastructure or Database team.

### B. Observability (Prometheus & Grafana)
*   **The Strategy:** Instrument the nodes with a `metrics/` endpoint. 
    *   Track `raft_term_number`, `log_commit_index`, `request_latency_ms`.
*   **The Appeal:** It shows you build systems that can be **monitored in production**. It’s not just code; it’s an **observable service**.

### C. Benchmarking & Performance Profiling
*   **The Strategy:** Use `go test -bench` to measure operations per second. 
    *   Compare 1-node throughput vs 3-node throughput.
*   **The Appeal:** Proves you care about the **cost of consensus**. Recruiters love to see "Reduced latency by X% via Y optimization."

---

## 🌐 3. Open Source Viability: Making it "Actually Useful"
For a KV store to be useful to others, it needs to be **interoperable**.

### A. S3/Cloud-Backup for Snapshots
*   **The Idea:** Automatically sync Raft snapshots to an S3 bucket or Google Cloud Storage.
*   **The Value:** Instant Disaster Recovery. If all 3 VMs vanish, a new cluster can "Hydrate" its state from the cloud bucket.

### B. Multi-Language Client SDKs
*   **The Idea:** Since we use gRPC, generate client libraries for **Python, Java, and Node.js**.
*   **The Value:** Developers don't want to use your Go binary; they want to use an `npm install raft-kv-client` package.

### C. "Kubernetes Operator"
*   **The Idea:** Write a K8s Operator that manages the Raft cluster nodes as a StatefulSet. 
*   **The Value:** This moves the project into the "Cloud-Native Integration" space. It becomes part of a larger ecosystem.

---

## 🏁 4. Critical Self-Reflection: The "Frank & Honest" Truth
Let's be critical: Why *wouldn't* someone use this today?

1.  **Discovery SPOF:** As we identified, the `node0` join logic is a "toy" setup. To be viable, we need a **Gossip membership** (Serf) so nodes are truly equal.
2.  **Lack of Authentication:** There is zero security. Anyone with the IP can `Set` a key. To be OSS-viable, it needs **mTLS (Mutual TLS)** for node-to-node and client-to-node security.
3.  **Read Latency:** Currently, even `GET` requests go to the leader to ensure strong consistency (Raft's "Strict Consistency" model). We could implement **Read-Only Followers** (with Lease-based reads) to massively scale read throughput.

---

### Final "North Star" Recommendation
If you have **one weekend** after this class ends: 
Implement **Gossip-based Membership**. It turns your "Academic Assignment" into a "Peer-to-Peer Cluster Engine." It is the most impressive single technical change you could make for your long-term developer growth. 
