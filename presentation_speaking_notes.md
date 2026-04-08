# Presentation Speaking Notes

**Deck:** `submission_docs/756 PPT v2-proto.pptx` (4 slides)
**Team:** Group 15 — Aarish, Ankith, Dhwani, Ankush
**Course:** CMPT 756 — Fault-Tolerant Distributed Systems

---

## Slide 1 — Title: Fault-Tolerant Distributed Key-Value Store using Raft Consensus

### What's on the slide
- Project title
- Team members (Group 15: Aarish, Ankith, Dhwani, Ankush)
- CAP theorem triangle with CP highlighted
- Fault model: Non-Byzantine
- Tagline: "Orchestrating Consensus within our CP System"

### What to say

> "We built a fault-tolerant key-value store on top of the Raft consensus algorithm and deployed it on Google Cloud Platform. Our system takes a deliberate CP position in the CAP theorem — we prioritize consistency and partition tolerance over availability.
>
> What that means concretely: if the cluster loses network connectivity and splits into two groups, the minority group stops accepting reads and writes rather than risk serving stale data. Cloud VMs crash and networks partition without warning — we built a system that survives both without data loss or human intervention.
>
> We're only handling non-Byzantine faults — crash-stop failures and network faults. Nodes either follow the protocol or are offline. There are no malicious actors."

### Key points to emphasize
- The CP choice is **intentional** — not a limitation or oversight
- "Without human intervention" — fully automated recovery (~1.25s MTTR)
- Non-Byzantine scope is standard for production distributed databases (etcd, Consul, Zookeeper all make the same assumption)

### Anticipated questions
- **Q: Why CP over AP?** A: For a KV store used for coordination or configuration, stale data is worse than temporary unavailability. We'd rather refuse a request than silently return the wrong answer.
- **Q: What about multi-region?** A: All nodes are in `us-central1`. Cross-region would need different timeout profiles (~100ms RTT vs our ~15ms). Documented as a known limitation.

---

## Slide 2 — System Functionality & Design

### What's on the slide
- Section divider / architecture context slide

### What to say

> "The core architecture is two processes per VM: the Raft replica, which handles consensus and client requests, and a sidecar agent, which handles fault injection.
>
> Every replica exposes two ports. The Raft port — in the 12000 range — is for internal consensus traffic: heartbeats, AppendEntries, RequestVote. The gRPC port — in the 50051 range — is the client-facing API: Get, Set, Delete.
>
> The sidecar agent is completely independent of the replica. It operates at the OS level — it can kill the process, freeze it, block its network traffic with iptables, or add artificial latency with tc netem. The replica has no idea it's being tested. This was a deliberate design choice: we wanted fault injection to be as realistic as possible, not something the application can accidentally work around.
>
> On the client side, kv-client automatically redirects to the current leader. If a follower receives a write, it returns the leader's address and the client retries. This is transparent to the user."

### Key points to emphasize
- Sidecar = OS-level fault injection, not application-level mocking
- Dual-port separation has a real purpose: you can fault the Raft layer and the client layer independently
- Client auto-redirect means users don't need to know which node is the leader

### Anticipated questions
- **Q: Why not use a single port?** A: We can apply iptables rules to one port without affecting the other. It also mirrors production systems like etcd (peer port vs. client port).
- **Q: How does the client know the leader address?** A: The follower returns a redirect error containing the leader's gRPC address. The client catches this and retries.

---

## Slide 3 — Features: What It Does

### What's on the slide
6 feature cards split into two sections: Distributed Systems Primitives and Infrastructure & Fault Engineering.

### What to say — feature by feature

**1. Follower Reads (Read-Index)**
> "This is our v1.3 feature. Normally all reads go to the leader, which creates a bottleneck. With follower reads, a client can send a read to any follower. The follower asks the leader: 'what's the current commit index?' The leader confirms it's still the leader, returns that index, and the follower waits until it has applied all log entries up to that point before serving the read. It's linearizable — you can never read stale data — but the leader isn't in the read critical path. Proven by 6/6 scenarios in our verification script."

**2. Exactly-Once Semantics**
> "Every write has a client ID and sequence number. The FSM maintains a per-client dedup table. If the same write arrives twice — which happens naturally when a client retries after a leader failover — the second application is silently dropped. The value is applied exactly once, even across leader changes. This is critical for correctness: without it, a client that loses the connection to the leader mid-write and retries could commit the same value twice."

**3. Linearizable Reads (Leader)**
> "Every read on the leader path calls VerifyLeader() before returning data. This sends a heartbeat to the majority of followers. If the leader can't reach the majority — because it's been partitioned — it refuses the read. This prevents a deposed leader from serving stale data. It's the CP trade-off in action: the read is blocked during the partition, but it's never wrong."

**4. Deployment Engine**
> "We built a full GCP provisioning pipeline. It provisions N VMs, cross-compiles the binaries for linux/amd64 on macOS, copies them over SSH, starts the cluster, and polls until a leader is elected. The entire process takes under 2 minutes for a 3-node cluster. We tested both 3-node and 5-node configurations — same code, same scripts, just `N=5`."

**5. Sidecar Agent + Fault Injection**
> "The agent is where most of the interesting work happened. The two important fixes are BUG-4 and BUG-5. BUG-4: our initial iptables partition was unidirectional — we blocked inbound traffic but the partitioned node could still send heartbeats out. That meant the leader never detected the partition and all our partition tests were passing for the wrong reason. Fix: bidirectional DROP. BUG-5: our initial netem was scoped to the Raft port, but the client connects on gRPC ports. Fix: apply netem to the full NIC."

**6. Aggressive Snapshotting**
> "We snapshot every 10 log entries. This bounds recovery time: a restarting node never replays more than ~10 entries before its state is fully current. For a follower that was offline for 30 writes, the leader doesn't replay 30 log entries — it sends a single InstallSnapshot RPC that teleports the full FSM state. The follower then applies that snapshot atomically. We verified this in D3: 30 missed entries, 100% recovery, applied-index delta of zero."

### Closing this slide

> "All six features are proven by specific test IDs in our 6-phase suite — those are the codes in the bottom-right of each card. 37 tests, all passing, on GCP, April 4."

### Anticipated questions
- **Q: What's the overhead of VerifyLeader?** A: One round-trip per read, ~30ms cross-zone on GCP. For read-heavy workloads, follower reads eliminate this overhead.
- **Q: What if a client's sequence number overflows?** A: seqNum is uint64. At 1000 writes/second, that's ~585 million years. Not a concern in practice.

---

## Slide 4 — Results & Analysis v4: Evidence Matrix

### What's on the slide
A 5-column table: Result Category | Key Metric | Fault Method | Test Evidence | Status

13 rows across 4 sections: WRITE LATENCY, LEADER RECOVERY (MTTR), DURABILITY, CORRECTNESS.

### What to say — how to present the table

> "This is our evidence matrix. Every row is a concrete claim about the system's behavior under a specific fault, backed by a specific test run with specific numbers. Let me walk through the four sections."

**WRITE LATENCY**
> "Three rows. First: baseline — 15.9ms per write, 62.9 ops/sec. No faults. That's our ground truth.
>
> Second: slow follower — we put 2 seconds of artificial delay on one of the two followers. Throughput dropped by only 2.5%, to 61.3 ops/sec. This is the quorum bypass in action: the leader only needs ACK from one follower, and with N=3, one fast follower is enough. The slow one is completely off the critical path. This result is stronger than we expected.
>
> Third: slow leader — we put 500ms delay on the leader. Throughput dropped 99%, to 0.6 ops/sec. But this is correct behavior — the delay on the leader caused the followers to time out and fire an election. During the election, all writes block. After the new leader is up, throughput recovers. We're measuring the transition window, and the number is expected."

**LEADER RECOVERY (MTTR)**
> "Three rows, all showing ~1.25s MTTR. First: SIGKILL — process death via kill -9. Second: iptables partition — bidirectional DROP on the Raft port. Both give the same MTTR because the election timing dominates: 500ms heartbeat timeout + 750ms election window = ~1.25s. The fault mechanism doesn't matter; it's Raft's clock that determines recovery time.
>
> Third row: isolated leader rejects writes. This is the CP safety test. The isolated leader — the one cut off from followers — cannot reach quorum, so writes return an error. It correctly refuses to commit rather than risk a split-brain write."

**DURABILITY**
> "Three scenarios. D1: SIGKILL all 3 nodes simultaneously, restart all — 10/10 pre-crash keys recovered, 100%. D2: Kill the leader mid-write during a 50-write burst — 7 acknowledged keys are intact, 43 unacknowledged are absent. This is correct: an acknowledged write was committed to Raft quorum before the response was sent, so it survives. An unacknowledged write was not committed, so it's correctly absent — no phantom data. D3: Offline a follower for 30 writes, bring it back — InstallSnapshot delivers all 30 keys, applied-index delta of zero."

**CORRECTNESS — CP SAFETY + IDEMPOTENCY**
> "Four rows. Exactly-once writes: the duplicate write is silently dropped — never applied twice. Linearizable reads: no stale data from a deposed leader. Follower reads: 6/6 read-index scenarios, linearizable off-leader. And finally: 5-node generalization — 37/37 tests at N=5, quorum of 3, and the 3-kill threshold test (killing exactly ⌊5/2⌋+1 nodes makes the cluster unavailable at precisely the quorum boundary).
>
> Every single row: PASS."

### Closing

> "37 tests. Every test is a real fault on real GCP VMs. No mocks, no simulations. The system either behaves correctly or it doesn't. It does."

### Anticipated questions
- **Q: How long did the full test suite take to run?** A: Each phase is 5–15 minutes of wall time including provisioning. Full suite is about an hour end-to-end.
- **Q: Did any tests fail during development?** A: Yes — we went from 33/37 to 36/37 to 37/37 across three bug fixes. The bugs were in the test infrastructure itself (unidirectional iptables, wrong NIC for netem, follower selector picking the test node), not just the system.
- **Q: Why test both N=3 and N=5?** A: To demonstrate that the quorum math generalizes. At N=3, majority = 2. At N=5, majority = 3. The same code handles both — you just change the cluster size parameter.
