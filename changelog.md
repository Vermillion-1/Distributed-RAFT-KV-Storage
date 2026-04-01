# Changelog: March 29, 2026
**Sprint Goal:** Finalize GCP verification, generate metrics, and overhaul project documentation.

### 🚀 Major Accomplishments
1.  **Resolved Snapshot Recovery Bug:** Lowered `SnapshotInterval` and `SnapshotThreshold` in `server/node.go` to force aggressive log compaction. This successfully validated **Phase 4 (Durability)** by triggering `InstallSnapshot` RPCs during recovery.
2.  **Fixed Client Redirect Loop:** Updated `dynamic_deploy.sh` and `deploy.sh` to bind gRPC services to explicit **GCP Internal IPs** instead of `0.0.0.0`. This allowed follower nodes to accurately resolve the Leader's address for client-side redirection.
3.  **100% Verification Pass:** Successfully executed and passed all 4 phases of the GCP Verification Suite (Liveness, Partitions, Latency, and Durability).
4.  **Metric Visualization:** Created `analysis/generate_graphs.py` to produce academic-grade charts for Latency Quorum, Durability, and MTTR.
5.  **Scientific Analysis:** Authored `analysis/results_analysis.md` interpreting the metric data from a distributed systems perspective.
6.  **Documentation Overhaul:** Created a new `docs/` suite with exhaustive guides: `HOW_TO_RUN`, `ARCHITECTURE`, `DELIVERABLES`, and `FILE_MANIFEST`.
7.  **Version Control Synchronization:** Initialized Git and pushed the "Submission-Grade" codebase to a new `stable-gcp` branch on GitHub.

### 🛠️ Technical Fixes
- **`dynamic_deploy.sh`**: Added `shopt -s nullglob` to `chmod` commands to prevent script crashes when `.sh` files are missing on a target node.
- **`GCP_verify_phase3.sh`**: Silenced stdout/stderr in the `time_writes` loop to prevent output pollution in bash arithmetic variables.
- **`node-agent`**: Verified the sidecar's ability to reliably signal child processes under heavy load.

---

*End of Log.*
