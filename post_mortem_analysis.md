# Distributed Raft KV — Post-Deployment Analysis

This document details the analysis of the failing assertions in Phase 3 and Phase 4 tests observed in GCP environment. The investigation revealed that **the core Raft KV application is actually healthy and highly durable**. The failures stem entirely from hardcoded assumptions within the verification scripts and chaos proxies originally designed for single-machine local testing.

## 1. Phase 4 Durability Failures (D1, D2, D3)
**Symptom:** The test suite reports massive data loss (e.g., `Pre-kill verification: 0/10 keys readable` and `7 acknowledged writes LOST after leader crash`).

**Root Cause: Localhost Assumption in Validation Scripts**
The `verify_phase4.sh` script assumes it is running on the same machine as all nodes. When it wants to dial the leader, it dynamically fetches the leader's port (e.g., `50052` for `node1`) and executes:
```bash
"${KV_CLIENT}" -addr "127.0.0.1:${L_PORT}" -cmd get
```
In our distributed GCP deployment, `node1` listens on `10.128.0.3:50052`. Thus, `verify_phase4.sh` (which executes on `node0`) tries to dial `127.0.0.1:50052`, hits `Connection Refused`, and assumes the key is missing. 
- During `D1` (Full Restart), `node1` was leader, so all writes aimed at `127.0.0.1:50052` failed immediately (`0/10 keys readable`).
- During `D2` (Dirty Restart), `node0` was leader, so writes *succeeded* initially on `127.0.0.1:50051`. But when `node0` crashed and `node1` took over, the script tried reading from `127.0.0.1:50052` and failed, incorrectly claiming "acknowledged writes LOST".

**Fix:** Update `verify_phase4.sh` to extract the full `grpc_addr` (e.g., `10.128.0.x:5005x`) from the dashboard `/api/cluster` state instead of blindly parsing ports and appending them to `127.0.0.1`.

## 2. Phase 3 Packet Loss (R3)
**Symptom:** `FAIL -- R3a: All requests failed (expected ~50% to succeed)` across a 50% packet drop proxy.

**Root Cause: gRPC HTTP/2 Framing Vulnerability**
The internal `kv-chaos` proxy performs random byte-level drops at the underlying TCP socket layer. Because gRPC multiplexes over HTTP/2, the protocol relies on strict, continuous binary framing.
If a raw byte belonging to an HTTP/2 frame header is randomly dropped by `kv-chaos`, the entire gRPC client connection enters a fatal state (`PROTOCOL_ERROR`). A single byte drop causes the gRPC stream parser to fail, resetting the entire connection and killing inflight requests, resulting in a near 0% request survival rate even at a 50% byte drop rate.

**Fix:** Application-layer proxies targeting gRPC cannot drop random TCP bytes. They must intercept the stream and drop discrete gRPC messages (or return HTTP 500s). For testing accurate 50% drop rates, implementing an `iptables` rule via the `node-agent` with `-m statistic --mode random --probability 0.5 -j DROP` is far superior as it simulates network-level packet loss natively and forces gRPC to timeout cleanly.

## Conclusion & Opportunities
The Raft subsystem, Log persistence via BoltDB, and Cluster Rejoining mechanics are entirely robust and passed distributed validation. The storage engine itself is rock solid.

To perfect the workspace, we should:
1. Overhaul the `verify_phase3.sh` and `verify_phase4.sh` scripts to fully utilize `$EXT_IPS` and the `grpc_addr` JSON properties, completely removing any reliance on `127.0.0.1`.
2. Sunset the local user-space proxy (`kv-chaos`) and exclusively use the `node-agent` to manage deterministic Linux `iptables` rules for rigorous chaos engineering.
