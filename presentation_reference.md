# Presentation Reference: Fault-Tolerant Distributed Raft KV Store — v1.2
**Purpose:** Use this document as the prompt for Gemini/Claude to generate your final Google Slides / PPT.
**Version:** Updated for v1.2 (3-node GCP, bidirectional iptables, NIC auto-detect, 37/37 test suite)
**Deadline:** April 2, 2026, 9:00 AM (slides PDF submission)
**Presentation:** 5 minutes total — 4 slides

---

## 📽️ SLIDE 1: Title & Overview
**Target time:** ~30 seconds

### Content Bullets:
- **Project Title:** Fault-Tolerant Distributed Key-Value Store using Raft Consensus
- **The Team:** [Your Names Here]
- **The "Why":** Cloud VMs crash and networks partition without warning. We built a system that survives both without data loss or human intervention — automatically.
- **System Class:** CP (CAP theorem) — Consistency + Partition Tolerance. Minority side sacrifices availability to preserve data correctness.
- **Fault Model:** Asynchronous, Non-Byzantine — crash-recovery, network partition (iptables), process freeze (SIGSTOP), message delay (netem)

### Visual Cues:
- Title bold, modern sans-serif
- Subtitle: "Raft Consensus · GCP · Go · 37/37 Tests Passing"
- Background: abstract network mesh or GCP datacenter visual
- Small CAP theorem triangle with "CP" highlighted

### 🎙️ Speaker Script (0:00 – 0:30):
"Hi everyone. Our project tackles a fundamental problem in cloud computing: what happens when your distributed system's network gets cut in half, or a VM crashes mid-write? We built a key-value store based on the Raft consensus protocol that survives these exact failures — automatically, without data loss, and without a human touching a config file. We classify it as a CP system: we chose consistency over availability, meaning the minority side of a partition correctly goes offline rather than serving stale data."

---

## 📽️ SLIDE 2: System Functionality & Design
**Target time:** ~90–120 seconds

### Content Bullets:
- **Consensus Algorithm:** Raft — Strong Leader model, CP semantics
- **Replicated State Machine:** Every VM runs an identical `kv-store` replica; no single permanent master
- **Dual-Port Node Design (key innovation):**
  - Port `12000+i` — Raft TCP: leader election, log replication, heartbeats (internal only)
  - Port `50051+i` — gRPC API: Get/Set/Delete for clients (external)
  - Separation allows fault injection on client path without disrupting consensus
- **Sidecar Agent (per VM):** Independent control plane process — injects SIGKILL, SIGSTOP, bidirectional iptables partition, tc netem — without the replica's cooperation
- **Smart Client:** Connects to any replica; auto-redirects to leader on follower response. Multi-address failover after leader death.
- **Exactly-Once Semantics:** `(client_id, seq_num)` dedup in FSM — retried writes never apply twice
- **Linearizable Reads:** `VerifyLeader()` heartbeat before every read — no stale data from deposed leaders

### Architecture Diagram Description (for slide designer):
Draw a **3-node diagram** (equilateral triangle arrangement):
- **3 VM boxes**, each labeled "GCP e2-micro · Replica + Sidecar Agent"
- **Red arrows** between all replicas (bidirectional): labeled "Raft TCP :12000–12002 (heartbeat · AppendEntries · vote)"
- **Green arrow** from "Client (kv-client)" entering from outside to one replica: labeled "gRPC :50051–50053 → auto-redirect to Leader"
- **Orange badge** on each VM box: "Sidecar Agent" — small sub-box, connected to "iptables · netem · SIGKILL" icons below
- **Crown icon** on the current leader replica
- **Key callout box at bottom:** "Write commits on Quorum Majority (2 of 3) · Minority partition → writes blocked (CP Safety)"

### 🎙️ Speaker Script (0:30 – 2:00):
"Our design uses a strictly symmetrical architecture — every VM runs the exact same binary. There is no special master node. The 'leader' role is determined by Raft election and can move to any replica at any time.

The most important design decision was the dual-port layout. Each replica has a dedicated Raft TCP port for consensus traffic — heartbeats, log replication, leader votes — and a separate gRPC port for client requests. This separation lets us inject massive network delays on the client path during testing without accidentally killing the underlying consensus protocol.

On each VM, we run a Sidecar Agent — a completely independent process that gives us remote control over the replica via HTTP API. We can SIGKILL it to simulate power loss, SIGSTOP it to freeze it, or apply kernel-level iptables rules to cut it off from the network entirely — at the TCP level, not by killing the process.

For clients, we built a smart client that connects to any replica and automatically redirects to the leader. We also implemented exactly-once semantics: every write carries a client ID and sequence number, and the FSM's dedup table ensures a retried write is never applied twice, even after a leader change."

---

## 📽️ SLIDE 3: Implementation Details
**Target time:** ~60–90 seconds

### Content Bullets:
- **Stack:** Go 1.21 · gRPC / Protobuf · HashiCorp Raft v1.7.3 · BoltDB (`go.etcd.io/bbolt`)
- **Deployment Engine (`dynamic_deploy.sh`):**
  - Provisions N GCP VMs, configures firewall rules, cross-compiles `linux/amd64` binaries on macOS
  - SSH readiness retry loop — polls until `sshd` reachable, not a blind sleep (BUG-1 fix)
  - Bootstraps full N-node quorum in < 2 minutes
- **Sidecar Agent — v1.2 hardening:**
  - Bidirectional iptables partition: `INPUT DROP` on own Raft port + `OUTPUT DROP` per peer Raft port (BUG-4)
  - NIC auto-detection: `ip route get 8.8.8.8` → works on `eth0`, `ens4`, any Linux NIC (BUG-5)
- **Aggressive Snapshotting:** `SnapshotThreshold=10` entries → `InstallSnapshot` RPC teleports full FSM state to lagging replicas
- **Binary portability:** `go:embed` bundles dashboard HTML into binary — no filesystem path dependency
- **Test Suite:** 6 phases · 37 tests · liveness · partition · latency · durability · idempotency · kernel chaos

### Visual Cues:
- Tech stack logos row: Go gopher, GCP cloud icon, gRPC logo, BoltDB
- Code snippet (small, left column): the `wait_ssh()` retry loop
- Code snippet (small, right column): the bidirectional iptables block
- Test suite coverage pills at bottom: "P1 6/6 ✅ · P2 9/9 ✅ · P3 3/3 ✅ · P4 4/4 ✅ · P5 8/8 ✅ · P6 13/13 ✅ = 37/37"

### 🎙️ Speaker Script (2:00 – 3:00):
"For implementation, we used Go for high-performance concurrency, with gRPC for the client API, HashiCorp Raft for consensus, and BoltDB for durable log storage on disk.

One of our hardest engineering challenges was the deployment engine. Our script spins up N virtual machines on GCP, cross-compiles the binaries for Linux from macOS, uploads them over SSH, and bootstraps a live quorum — all in under two minutes. We had to build a proper SSH readiness retry loop because GCP e2-micro VMs sometimes take 30-50 seconds for their SSH daemon to start, and a blind sleep was crashing the deploy.

The second major challenge was getting fault injection right. Our initial partition implementation only blocked incoming Raft traffic — which meant an isolated leader could still send outbound heartbeats to followers, stay leader, and accept writes. This violated CP safety. We fixed this with bidirectional iptables. We also discovered GCP Debian uses `ens4` as the network interface name, not `eth0` — so our netem delay wasn't actually applying. Auto-detection fixed this. These two fixes pushed our test score from 33/36 to 37/37."

---

## 📽️ SLIDE 4: Results & Analysis
**Target time:** ~60–90 seconds

### Content — Four Result Tables:

**Table 1: Write Latency & Throughput Under Fault Injection (Phase 3)**

| Condition | Fault | Latency (ms/op) | Throughput (ops/sec) | Change |
|-----------|-------|-----------------|----------------------|--------|
| Baseline | — | 19.6 | 51 | — |
| Slow follower | 2000ms netem on 1 replica | 23.8 | 42 | −18% |
| Slow leader | 500ms netem on leader | 502 | 2 | −96% |

**Table 2: MTTR Breakdown — Leader Failure (Phase 1 L1 + Phase 6 N6a)**

| Window | Duration | Event |
|--------|----------|-------|
| Detection | 0 – 500ms | Followers miss heartbeats |
| Election | 500 – 1250ms | Candidate collects majority votes |
| **Total MTTR** | **~1.25s** | Same result via SIGKILL and iptables |

**Table 3: Durability — Key Recovery Ratio (Phase 4)**

| Scenario | Acknowledged | Recovered | Ratio |
|----------|-------------|-----------|-------|
| D1: Total cluster wipe + restart | 10 | 10 | **100%** |
| D2: Dirty leader crash mid-write | 7 | 7 | **100%** |
| D3: Snapshot catch-up (30 missed entries) | 30 | 30 | **100%** |

**Table 4: Quorum Proof**

| N | Quorum | Tolerates | L2 | L3 trigger |
|---|--------|-----------|-----|-----------|
| 3 | 2 | 1 failure | ✅ | 2 kills |

### Technical Challenge (required by rubric):
**Challenge encountered:** Our partition implementation initially only blocked *incoming* Raft traffic (`INPUT DROP`). An isolated leader could still send outbound heartbeats to followers → followers never timed out → no election fired → isolated leader stayed leader and accepted writes (CP safety violation — N6c test: write to isolated leader should fail).

**Root cause:** A symmetric network partition requires blocking *both* directions: incoming ACKs *and* outgoing heartbeats. Only blocking one direction creates a "receive omission" fault, not a full partition.

**Fix:** Added `OUTPUT DROP` per peer Raft port. Both directions cut → followers stop receiving heartbeats → election timeout fires in ~1.25s → new leader elected. N6 suite now passes all 6 assertions including N6c (isolated leader rejects writes) and N6e (former leader rejoins as Follower).

### Visual Cues:
- Embed: `analysis/graphs/latency_quorum_proof.png` (bar chart: baseline vs slow-follower vs slow-leader)
- Embed: `analysis/graphs/availability_mttr.png` (timeline chart)
- Bold/highlight: "100%" in Table 3, "~1.25s" in Table 2, "−96%" in Table 1
- Technical challenge as a callout box with before/after iptables rule snippet

### 🎙️ Speaker Script (3:00 – 4:30):
"Our results prove three key properties.

First, latency. The slow-follower result shows the quorum bypass working: a 2000ms delay on one of three replicas only added 4.2ms to write latency. The leader only needs acknowledgment from a majority — 2 of 3 — so the slow replica is simply ignored for the commit. The slow-leader result shows the expected bottleneck: every write must go through the leader, so a 500ms leader delay added 482ms of latency per write.

Second, MTTR. When we killed the leader — with SIGKILL and separately with iptables — the cluster detected the failure in 500ms and elected a new leader within 750ms. Total downtime: ~1.25 seconds. The fact that both fault methods produce identical MTTR confirms the mechanism is working correctly: Raft's timeout-based detection is truly independent of how the failure happened.

Third, durability. Across every failure scenario — total wipe, dirty crash mid-write, and snapshot catch-up — we recovered 100% of acknowledged writes. Nothing that got a 'success' response to the client was ever lost.

Our biggest technical challenge was the partition fix. We initially only blocked incoming traffic, which meant an isolated leader could still heartbeat its followers and stay leader — a CP safety violation. The fix required bidirectional iptables rules, and this is what closed the gap from 33/36 to 37/37 in our test suite."

---

## 📽️ Q&A Preparation (2–3 minutes, 2–3 questions)

### Q1: Why Raft over Paxos?
**Answer:** Raft was specifically designed for understandability with a clear strong-leader model. This made our client redirect implementation straightforward — clients always need to find exactly one leader. Paxos is more general but significantly harder to implement correctly in Go, and the HashiCorp Raft library gives us a battle-tested foundation. The correctness guarantees are equivalent.

### Q2: How does your partition actually work — isn't it just killing the process?
**Answer:** No, and this distinction matters. We use kernel-level `iptables DROP` rules that block TCP packets at the OS level while the process keeps running. The replica is alive and willing to respond, but its packets are silently dropped. This tests whether Raft's timeout-based failure detection works correctly — which is the real mechanism used in production networks. In v1.2, we made the partition bidirectional: both incoming ACKs and outgoing heartbeats are blocked. One direction alone creates a "receive omission" fault, not a true partition.

### Q3: What determines the MTTR and can you make it faster?
**Answer:** MTTR = heartbeat timeout (500ms) + election timeout (750ms) = 1.25s. These are pure configuration choices. You could set the heartbeat to 50ms and the election timeout to 500ms for sub-second MTTR. The trade-off is spurious elections: a single delayed heartbeat on a congested network would trigger an election unnecessarily. We tuned for GCP cross-zone RTT of ~15ms with a conservative buffer for e2-micro scheduling jitter.

### Q4: Why a 3-node cluster?
**Answer:** 3 nodes is the operational minimum to prove Raft's core value: high availability through quorum. With 3 nodes, the system can tolerate exactly one failure while remaining fully operational. It demonstrates the fundamental logic of `⌊N/2⌋+1` quorum—where 2 of 3 nodes are required to commit—providing the simplest and clearest proof of how the algorithm generalizes to any odd-numbered cluster size.

### Q5: What are the known limitations?
**Answer:** Three main ones we're transparent about. First, static cluster membership — the cluster size is fixed at deploy time. A permanently failed node cannot be replaced without tearing the whole cluster down. Production systems use joint consensus for this. Second, single-leader write ceiling — all writes go through one replica, so write throughput doesn't scale horizontally. Sharding would be required. Third, single-region — all VMs are in `us-central1`. A geo-distributed cluster would require much higher election timeouts to account for cross-continent RTTs.

---

## Timing Guide

| Section | Content | Target |
|---------|---------|--------|
| Slide 1 | Title + Fault Model | 0:00 – 0:30 |
| Slide 2 | Architecture + Design | 0:30 – 2:00 |
| Slide 3 | Implementation | 2:00 – 3:00 |
| Slide 4 | Results + Challenge | 3:00 – 4:30 |
| Buffer | Transitions | 4:30 – 5:00 |
| Q&A | 2–3 peer questions | 5:00 – 7:30 |

**Hard constraint:** PDF submitted by April 2, 2026 at 9:00 AM. Exactly 4 slides — no more, no fewer.

---

## Terminology Cheat Sheet

| ❌ Avoid | ✅ Use instead |
|---------|--------------|
| Core Server | Raft Replica / RSM Node |
| Server | Replica |
| Master node | Leader (elected, not permanent) |
| The cluster | Consensus group |
| State machine | Replicated State Machine (RSM) |
| Chaos Proxy | Sidecar Agent (kv-chaos is deprecated in v1.2) |
| 3-node cluster | The production-ready minimum quorum |
| "nodes fail" | "replicas crash" / "network partition" |
