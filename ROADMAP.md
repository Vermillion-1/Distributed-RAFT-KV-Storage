# 🚀 V2.0 Roadmap: GCP-Grade Reliability & Observability

This document outlines the next phase of development for the Distributed Raft KV cluster. Our goal is to move from a **Functional Prototype** to a **GCP-Grade Production System**.

---

## 📊 1. Observability: "The Deep-Vision Dashboard"
Currently, the dashboard shows basic liveness and applied index. V2.0 focuses on exposing the internal Raft state.

*   **Raft Term Tracking:** Add the current `Term` to each node's status. This allows us to observe election cycles and "Stale Term" rejections in real-time.
*   **Heartbeat Heatmap:** Visualize the RTT (Round Trip Time) between the current Leader and all Follower nodes. This identifies "Grey Failures" where a node isn't dead but is too slow to maintain quorum.
*   **Live Log Streaming:** Implement WebSockets in the `node-agent` to stream the last 50 lines of `agent.log` directly to the browser, eliminating the need to `gcloud compute ssh` just to see errors.
*   **Election History:** Add a timeline showing who was Leader, when they were killed, and which node won the subsequent election.

---

## 🌩️ 2. Chaos Engineering: "Kernel-Level Fault Injection"
Currently, our `kv-chaos` tool is a **User-Space TCP Proxy**. While effective, it only intercepts *new* connections and adds overhead. V2.0 moves to **Kernel-Level NetEm**.

*   **Linux `tc` & `netem` Integration:** Replace the Go proxy with direct calls to Linux Traffic Control.
    *   **Real Latency:** Use `tc qdisc add dev eth0 root netem delay 100ms 10ms` to add base latency + jitter to all packets.
    *   **Packet Operations:** Support packet **corruption**, **reordering**, and **duplication**—faults that a TCP proxy cannot simulate.
*   **Deterministic Network Partitions:** Enhance the `iptables` partition logic to support **Asymmetric Partitions** (Node A can hear B, but B cannot hear A) to test complex Raft edge cases like "Pre-Vote" protocols.

---

## 🏗️ 3. Scaling & Architecture: "The TiKV Pattern"
We aim to adopt the architectural modularity seen in production systems like TiKV and CockroachDB.

*   **Gossip-Based Discovery (Serf/Consul):** Remove the need for the `-join=node0` flag. Use a Gossip protocol so that new nodes can join by talking to *any* existing member.
*   **Multi-Raft Sharding:** Instead of one big Raft group, split keys into **Regions**. Each Region is its own Raft group, allowing the cluster to scale horizontally across hundreds of nodes.
*   **Automated Leadership Rebalancing:** A "Placement Driver" service that detects when one VM is overloaded and transparently transfers Raft leadership to a less busy node.

---

## 🛠️ 4. Developer Experience
*   **One-Click Benchmarking:** Integrate `kv-client` performance tests into the dashboard to generate TPS (Transactions Per Second) graphs under chaotic conditions.
*   **Terraform Provider:** Replace the `gcloud compute instances create` bash loops with a formal Terraform configuration for professional infrastructure-as-code management.
