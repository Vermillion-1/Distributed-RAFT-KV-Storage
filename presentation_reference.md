# Presentation Reference: Fault-Tolerant Distributed Raft KV Store
**Purpose:** Use this document as the prompt for Gemini/Claude to generate your final Google Slides / PPT.

---

## 📽️ SLIDE 1: Title & Overview
**Focus:** Introduction & Problem Statement (30 Seconds)

### Content:
*   **Project Title:** Implementing a Highly-Available, Fault-Tolerant, Distributed Key-Value Store.
*   **The Team:** [Your Names Here]
*   **The "Why":** Standard cloud applications face arbitrary network partitions and VM crashes. We built a system that survives these without data corruption or human manual intervention.
*   **Fault Model:** Asynchronous environment with Crash-Recovery and Network partitions (IP-tables simulation). Non-Byzantine.

### Visual Cues:
*   Title in bold, modern font.
*   Background image: A distributed network mesh or a stylized server cluster on GCP.

### 🎙️ Speaker Script (0:00 - 0:30):
"Hello everyone. Our project tackles one of the hardest problems in cloud computing: maintaining a consistent state across physically isolated machines. We built a distributed Key-Value store based on the Raft consensus protocol. Our goal wasn't just to store keys, but to build a 'CP' system—one that prioritizes Consistency and Partition Tolerance—ensuring that even if a data center's network is literally cut in half, your data remains safe and accurate."

---

## 📽️ SLIDE 2: System Functionality & Design
**Focus:** Architectural Depth (90 Seconds)

### Content:
*   **Consensus Algorithm:** Raft (Strongly Consistent - CP).
*   **Symmetrical Architecture:** Every VM runs the same binary; No "Master" node bottleneck.
*   **Dual-Port Design (Crucial Innovation):** 
    *   **Port 12000:** Internal Raft traffic (Leader Election, Log Heartbeats).
    *   **Port 50051:** User gRPC API (Get, Set, Delete).
*   **Service Discovery:** Distributed state machine records dynamic GCP Internal IPs for seamless cross-VM client redirects.
*   **Exactly-Once Semantics:** Client-ID and Sequence-Num tracking in the FSM prevents duplicate writes during retries.

### Visual Cues:
*   **Diagram:** A simplified 3-node triangle.
*   Show nodes labeled "GCP VM" with two arrows: a red one between nodes (Raft) and a green one from the outside (Client gRPC).

### 🎙️ Speaker Script (0:30 - 2:00):
"Our design follows a strictly symmetrical architecture across three GCP virtual machines. To handle faults precisely, we designed a dual-port node. One port is dedicated to the 'Raft heartbeat'—the mathematical core where nodes vote and replicate logs. The second port is the gRPC API for the user. We did this so we can inject massive network delays on the user's data port without accidentally killing the underlying cluster consensus. We also solved the 'Service Discovery' problem: if a client hits a follower, the follower knows the exact GCP internal IP of the leader and issues an automatic redirect. Finally, we implemented 'Exactly-Once Semantics'—this means even if a network drop causes a client to send the same write twice, our state machine recognizes the sequence number and ensures only one update is ever applied."

---

## 📽️ SLIDE 3: Implementation Details
**Focus:** The "Hard" Engineering (60 Seconds)

### Content:
*   **The Stack:** Go (Golang), gRPC, Protobuf, BoltDB (Persistent Log store).
*   **GCP Orchestration:** Custom `dynamic_deploy.sh` script—provisions N VMs, cross-compiles for Linux/AMD64, and bootstraps a global quorum across availability zones.
*   **Chaos Engineering Suite:**
    *   **Sidecar Agent:** A monitoring process that manages the KV-Store.
    *   **Fault Injection:** Remotely triggering `SIGKILL` (power loss), `SIGSTOP` (freeze), and `IP-Tables` (partition) via our dashboard API.
*   **State Compaction:** Tuning `SnapshotThreshold` aggressively (10 entries) to force binary snapshots over the wire.

### Visual Cues:
*   Logos: GCP, Go, gRPC.
*   Small screenshot snippet of the `dynamic_deploy` terminal output showing cross-compilation and IP mapping.

### 🎙️ Speaker Script (2:00 - 3:00):
"For implementation, we used Go for its high-performance concurrency. We didn't just run this locally; we built a dynamic deployment engine that spins up a cluster on GCP in under two minutes. To prove our fault tolerance, we built a 'Chaos Sidecar Agent.' This allows us to remotely send POSIX signals like SIGSTOP to a specific VM to freeze it, or SIGKILL to simulate a total power loss. A technical highlight of our implementation is 'Aggressive Compaction.' Instead of letting logs grow forever, we tuned our Raft thresholds to force binary snapshots after every 10 writes. When a node that was 'dead' for a month wakes up, the leader doesn't replay tiny logs; it 'teleports' the entire state via an InstallSnapshot RPC."

---

## 📽️ SLIDE 4: Results & Analysis
**Focus:** Quantitative Proof (90 Seconds)

### Content:
*   **Result 1: MTTR (Mean Time to Recovery):** 
    *   Leader failure detected in 500ms; Election resolved in 750ms.
    *   **Total Outage:** ~1.25s (Predictable & Bounded).
*   **Result 2: Quorum Bypass:** 
    *   A 2000ms delay on a follower only increased baseline write latency by 4.2ms.
    *   **Proves:** N/2 + 1 consensus works perfectly by ignoring lagging minority nodes.
*   **Result 3: Durability:** 100% Key-Recovery ratio across all 4 phases (No data loss).

### Visual Cues:
*   **Embed Graph 1:** `availability_mttr.png` (The timeline).
*   **Embed Graph 2:** `latency_quorum_proof.png` (The log-scale bars).

### 🎙️ Speaker Script (3:00 - 4:30):
"Finally, the results. Our evaluation phase proved three key things. First, our MTTR—or Mean Time To Recovery. When we killed the leader, the cluster detected the failure in 500ms and seated a new leader in 750ms. That entire 1.25-second window is perfectly reflected in this chart. Second, we proved the 'Quorum Bypass' property. When we artificially slowed down one follower by 2 seconds, our client write latency barely budged. This is the mathematical beauty of Raft: the system effectively 'cuts out the slow part' to maintain high throughput. Lastly, our durability experiments resulted in zero data loss. Every write acknowledged to a client was recovered from the physical disk across every failure mode we tested."

---

## 📽️ [EXTRA] Q&A PREP (The 2-3 Peer Questions)
*   **Q: Why choose Raft over Paxos?** 
    *   *Ans:* Raft is designed for understandability and has a clear 'Strong Leader' model, which makes implementing our Client Redirection mechanism much more straightforward in Go.
*   **Q: How do you handle a total network partition (split split brain)?** 
    *   *Ans:* Since we use a Quorum (N/2 + 1), the minority side of the partition will realize it can't get enough votes and will block all writes. This ensures we never branch the data into two conflicting states.
*   **Q: What happens if the persistence disk (BoltDB) is full?** 
    *   *Ans:* This is where our 'Snapshotting' comes in. It flattens the infinite log into a single binary file to keep storage usage constant regardless of uptime.
