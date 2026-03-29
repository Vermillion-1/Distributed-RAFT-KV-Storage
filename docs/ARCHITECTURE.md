# System Architecture: Distributed Raft KV Design Defense

Our system is an implementation of a **Distributed, Highly-Available, and Strongly Consistent (CP)** Key-Value Store based on the Raft consensus protocol.

---

## 🏗️ 1. Core Principles: Decentralization & Quorum
Unlike a "Server/Client" MONOLITH, our system is entirely symmetrical. Every GCP VM runs the exact same `kv-store` binary. No single node "owns" the data; rather, they form a **3-node Quorum** (`N/2 + 1 = 2`).

### Distributed Logic:
-   **No Static Leader:** If any leader dies, the other two nodes recognize the failure (via 500ms heartbeat timeout) and elect a new leader.
-   **Linearizable Consistency:** Every write is replicated to at least two nodes before the client is acknowledged.

---

## 📡 2. Networking Design: The "Dual-Port" Pattern
We solve the "Chaos vs. Control" problem by separating our networking onto two logic separate ports.

1.  **Port 12000 (Raft Internal):** This is the high-priority "Heartbeat" port. It is only used for leader election, log replication, and configuration changes. It never touches user data directly. 
2.  **Port 50051 (Client gRPC):** This is the user-facing API. It provides a clean gRPC interface for `Get`, `Set`, and `Delete`.

### Reasoning & Defense (The Prof's Question):
*   **Why separate?** We allow the **Chaos Proxy** to intentionally delay or drop traffic on port 50051 to simulate a slow application layer *without* breaking the underlying Raft heartbeats on port 12000. This is a crucial distinction between "Application-Level Partition" and "Infrastructure-Level Partition."

---

## ⚡ 3. Persistence: The Write-Ahead Log (WAL)
We use **BoltDB** for persistent on-disk storage.
-   **Why it's durable:** When a leader appends a log entry, it is `fsync`'d to the physical disk of at least two VMs. This ensures the system survives the "Total Wipeout" scenario (Phase 4).
-   **Log Compaction (Snapshots):** To prevent the Raft log from growing boundlessly, we've tuned our system to take **Binary Snapshots** after every 10 operations. This keeps memory usage constant even in high-throughput environments.

---

## 👺 4. Fault Model & Resilience
Our implementation addresses the exact fault model required by the **CMPT 756** rubric.

| Fault Type | Mitigation Strategy | Verification Phase |
| :--- | :--- | :--- |
| **Crash-Failure** | Periodic disk-flushing via BoltDB. | **Phase 4** (Durability) |
| **Network Partition** | Raft majority quorum selection. | **Phase 2** (Partitions) |
| **Message Latency** | Configurable timeouts for elections and heartbeats. | **Phase 3** (Latency) |
| **Split-Brain** | Strict Raft term-number incrementing prevents dual-leadership. | **Phase 2** (Liveness) |

---

## 🌍 5. GCP Cross-Zone Deployment
Industrial distributed systems don't run in a single rack. We deploy our nodes across **3 separate availability zones** (`us-central1-a, b, c`). This protects the cluster from a physical fire or power failure in a single Google data center.
