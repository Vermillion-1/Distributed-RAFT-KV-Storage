# Raft KV — GCP Distributed Handover State & Future Roadmap

**Date**: March 2026

When returning to this project, this document serves as the absolute ground truth regarding the progress, system architecture decisions, and remaining roadmap for the Distributed Raft KV Storage Prototype.

---

## 1. What We've Accomplished
- **Idempotent Setup:** Fixed a critical SSH `nohup` detachment race condition inside `deploy.sh` and implemented total idempotency around GCP deployment logic (wiping data, killing detached PID remnants automatically).
- **Core HTTP2/gRPC Node Bug Fix:** Repaired `server/node.go`. The node's `Join` redirect was initially returning the `Raft TCP` coordinate (`12000`) instead of the `gRPC` coordinate (`50051`). The followers attempted multiplexing gRPC traffic down the byte-level raw TCP port, bringing down the cluster instantly with `PROTOCOL_ERROR`.
- **GCP Validation Port:** 
  - Realized `verify_phase3.sh` and `verify_phase4.sh` were blindly attempting to read/write keys against `127.0.0.1` endpoints instead of the actual GCP Leader node IPs.
  - Built **`GCP_verify_phase1.sh`** through **`phase4.sh`**. These scripts dynamically unmarshal `grpc_addr` directly from the dashboard state endpoint to accurately route traffic across network boundaries.
  - Dropped the `R3 Packet Drop Proxy` tests. The `kv-chaos` proxy is not functionally able to cleanly drop packet segments on gRPC without violating HTTP/2 framing. Real `SIGSTOP/SIGCONT` logic is securely handling partition chaos testing instead.
- **N-Node Scalability Overhaul:** 
  - Left `deploy.sh` strictly alone (3 hardcoded nodes).
  - Engineered **`dynamic_deploy.sh`** capable of instantly parsing a `$1` environment variable and perfectly scaling instance counts, modulo-wrapping GCP Zones naturally, and automatically string-constructing the mesh-agent array for the dashboard.

## 2. Current State
- The original 3-node static cluster is fully deployed in GCP (`raft-kv-756`).
- All validation phases (Liveness, Minority/Majority Partitions, Latency Proxies, Total Cluster Wipes, Dirty Writes, Catch-ups) pass immaculately against the new `GCP_verify` bash suite.
- `dynamic_deploy.sh` represents the bleeding edge state of the system but has **not yet been executed** in real infrastructure testing. 

## 3. Future Action Plan

If/when picking this project back up, execution should be prioritized around advanced systems robustness and the newly built scaling features.

### A. Test N-Node Scaling
- **Goal:** Run `export GCP_PROJECT=raft-kv-756 && bash dynamic_deploy.sh 5`. 
- **Validation:** Execute the GCP Phase Validation scripts. The cluster's `pump_writes` will likely slow down due to sequential `AppendEntries` payload bottlenecks over a 5-voter consensus ring. Evaluate exactly how heavily throughput decays over Google's SDN.

### B. Patch the "Flapping Partition" (Enable Pre-Vote)
- **Goal:** A follower partition test that extends continuously will cause its local election timer to loop unrestrictedly. Its `Term` variable will artificially inflate. When the partition heals, the follower will force the healthy leader to instantly abdicate, needlessly hurting cluster availability.
- **Fix:** Hook into HashiCorp's config via `node.go` and investigate exposing the **Pre-Vote Raft Protocol Extension**.

### C. Prove Snapshot Syncing
- **Goal:** Phase 4 durability currently relies strictly on sequential *Log Replays* for node recovery. This proves BOLT-DB persistence, but it physically cannot scale.
- **Fix:** Drop the `SnapshotInterval` threshold heavily within the source code. Keep a follower offline indefinitely, inject 500 records into the Leader, boot the follower back on, and prove the Raft FSM handles monolithic `InstallSnapshot` RPC transitions cleanly over the wire without memory exhaustion.
