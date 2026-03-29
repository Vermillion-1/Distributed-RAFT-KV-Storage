# Project Architecture & Complexity Report
**Topic:** Distributed, Scalable, Fault-Tolerant Key-Value Store (CP System)

## The Fault Model
Our system is designed to withstand **Crash-Failures** and **Network Partitions** in an asynchronous environment. We assumed:
- **Crash-Recovery:** Nodes can die at any moment and lose in-memory state, but can recover at an indeterminate time in the future.
- **Network Unreliability:** Messages can be dropped, delayed arbitrarily, or reordered (simulated via IP-tables and Chaos Proxies).
- **Split-Brain Partitions:** The network can slice the cluster in half, prohibiting node communication. 
- **Non-Byzantine:** Nodes do not actively act maliciously or corrupt packets.

---

## Strict Evaluation: What the Libraries Provided
To be critical and truthful: we *did not* implement the mathematical proof and raw state-transition arrays of the Raft algorithm ourselves. We relied on existing battle-tested libraries for the mathematical foundation.

1. **HashiCorp Raft (`github.com/hashicorp/raft`)**
   - Provided the core Leader Election mechanics (timers, randomized timeouts).
   - Provided the raw Quorum mechanics (calculating `N/2 + 1`).
   - Handled the internal heartbeat packets between nodes.
   - Handled the physical truncation of log structures when thresholds were met.
2. **BoltDB/Bbolt (`go.etcd.io/bbolt`)**
   - Handled physical disk-writing guarantees (fsync) to ensure Raft's log actually survived sudden SIGKILL power-loss events.

---

## What WE Built: Complexity Categorization 

Everything surrounding the libraries that elevates this from a "math library" into a **functioning, horizontally scalable, distributed GCP product** had to be engineered by us. 

### 🔴 HARD 
*These components required deep distributed systems understanding, complex networking manipulation, and orchestration.*

1. **Distributed Dynamic Deployment Engine (`dynamic_deploy.sh`)**
   - *Why it's hard:* Writing a script to spin up scalable `N` nodes on Google Cloud Platform, auto-configure firewall rules, cross-compile OS-specific binaries, distribute payloads via SCP, and auto-bootstrap a quorum is a massive infrastructure challenge.
2. **True Network Partitions (IP-Tables & Chaos Proxy)**
   - *Why it's hard:* Standard testing just "kills" a node. We built a proxy (`kv-chaos`) and an iptables daemon in our agent to simulate *real* network partitions, packet drops, and artificial latency (5000ms+ delays) without actually killing the Raft process. This rigorously tested that the Leader Election timers accurately decoupled from the Client gRPC connections.
3. **Internal GCP Service Discovery & Redirection**
   - *Why it's hard:* If a client dynamically connects to `node2`, but `node1` is the leader, `node2` cannot just drop the request. We built a distributed mapping system where nodes record their dynamic GCP internal IPs (`10.128.0.x`) into the *shared Raft state machine itself*, allowing followers to perfectly redirect client traffic to the remote leader across the physical network.

### 🟡 MEDIUM
*These components required solid software engineering and OS-level integrations.*

1. **The Node-Agent Sidecar Architecture**
   - *Why it's medium:* Rather than just running `kv-store`, we built an HTTP `node-agent` wrapper that spawned `kv-store` as a child process. This allowed our Dashboard to remotely send OS signals (`SIGSTOP`, `SIGCONT`, `SIGKILL`) over the network to freeze/resume the processes and simulate failures.
2. **Idempotency & Exactly-Once Semantics**
   - *Why it's medium:* When networks drop, clients retry requests. If a `Set(x=5)` request was applied but the response was dropped, a blind retry applies it twice. We engineered sequence tracking within the FSM using `client_id` and `sequence_num` to enforce idempotent state transitions.
3. **The Chaos Dashboard Verification Suite**
   - *Why it's medium:* We built a centralized visual orchestrator and automated Bash validation suite (`GCP_verify_phaseX.sh`) to query health endpoints, trigger dirty-restarts, and parse numeric latency assertions against the distributed quorum in real-time.

### 🟢 EASY
*These components were relatively straightforward to wire up based on standard examples.*

1. **The Basic In-Memory Key-Value Store**
   - *Why it's easy:* The core application logic is just a standard Go map (`map[string]string`) sitting behind a Mutex wrapped within the HashiCorp `Apply()` interface.
2. **gRPC Protocol Buffers**
   - *Why it's easy:* Defining `Get`/`Set`/`Delete` in a `.proto` file and generating the boilerplate server code is standard practice with minimal edge cases. 
3. **File Snapshots Structs**
   - *Why it's easy:* Capturing a snapshot consisted merely of copying our in-memory map arrays into a generic JSON byte slice (`Restore()` / `Persist()`) and writing it to disk. 

---

### Conclusion
By relying on HashiCorp for the internal mathematics, we successfully devoted all our engineering time to the **macro-architecture**—deploying out to physical separate GCP machines, orchestrating chaotic network disruptions, tuning aggressive recovery heuristics, and enforcing strict redirection protocols—which is the hardest part of bringing a theoretical algorithm to a true distributed environment.
