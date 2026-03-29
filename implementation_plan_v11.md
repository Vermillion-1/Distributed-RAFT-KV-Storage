# V1.1 Implementation Plan: Distributed Reliability & Self-Healing

This plan targets the **Rank 1 (Client Entry-Point)** and **Rank 2 (Dashboard Centralization)** SPOFs identified in our architectural audit. By moving from a "Hardcoded Seed" model to a "Cluster-Aware" model, we align our system with industry standards like **etcd** and **TiKV**.

## User Review Required

> [!IMPORTANT]
> This plan changes the CLI arguments for `kv-client`. Instead of `-addr=string`, it will now accept `-addrs=string` (comma-separated). Any automated scripts using the old flag must be updated once this is merged.

## Deep-Dive Architectural Audit

### 1. The Client Entry-Point SPOF (Rank 1)
*   **The Problem:** The `kv-client` and verification scripts currently target a single, hard-coded IP (usually `node0`).
*   **Why we must fix this:** In a CP system (like ours), if a node crashes, the cluster remains 100% consistent and available via a new leader. However, if the client is hard-coded to talk to the node that just died, it will receive a "Connection Refused." To the user, the entire system appears offline even though 2/3 of the cluster is perfectly healthy.
*   **Was this Intentional?** No. It was a limitation of our V1.0 CLI design where we prioritized a single-connection string for ease of use.
*   **Industry Giant Solutions:** 
    *   **etcd (Smart Client):** The `etcdctl` client accepts a list of endpoints. It uses a **Round-Robin** failover strategy internally. If node A is down, the library transparently falls back to node B or C without the user ever seeing a 404 or Timeout.
    *   **TiDB/TiKV (PD Discovery):** Clients query a separate **Placement Driver (PD)** cluster to get the latest healthy node map, which they then cache locally.

### 2. The Dashboard Control-Plane SPOF (Rank 2)
*   **The Problem:** The `kv-dashboard` UI process and `index.html` only exist on `node0`.
*   **Why we must fix this:** Our project is a **Chaos Testing Framework**. If our primary test (Phase 4) involves killing `node0` to see if the cluster survives, we lose our "eyes" (the dashboard) exactly when we need them most. 
*   **Was this Intentional?** Yes. Hosting it on one node provided a stable, predictable URL for the user.
*   **Industry Giant Solutions:** 
    *   **Consul (Gossip UI):** Consul agents run a gossip protocol (**Serf**). Every node in the cluster is capable of serving the UI and member-list. There is no "Master" UI node; any node is a portal to the whole cluster state.
    *   **Kubernetes (Distributed Controller):** Controllers (like the Dashboard) are replicated and use **Leader Election** themselves; if the UI pod dies, a new one is instantly spawned on a healthy worker node.

---

## Proposed Changes

---

### Phase 1: Cluster-Aware Client (Industry Pattern: "Smart Clients")
Currently, the client is "brittle." If the initial node it attempts to connect to is dead, the whole request fails even if the cluster is healthy. We are adopting the **etcd Round-Robin Failover** pattern.

#### [MODIFY] [client/main.go](file:///Users/ankushsingh/Desktop/CMPT%20756/Distributed-RAFT-KV-Storage-prototype/cmd/client/main.go)
*   Replace `addr := flag.String("addr", ...)` with `addrs := flag.String("addrs", ...)`.
*   Parse the input into a `[]string` slice.
*   Update the `main` loop to iterate through the slice until a successful connection or a "Leader Redirect" is received.
*   *Benefit:* A 3-node system now survives the loss of the entry-point node without the user knowing.

---

### Phase 2: Decentralized Dashboard (Industry Pattern: "Symmetric Management")
Currently, the Dashboard only lives on `node0`. If `node0` is killed for chaos testing, the "eye in the sky" (UI) is lost.

#### [MODIFY] [dynamic_deploy.sh](file:///Users/ankushsingh/Desktop/CMPT%20756/Distributed-RAFT-KV-Storage-prototype/dynamic_deploy.sh)
*   Update the upload loop to `scp` the `index.html` to **all** nodes, not just `node0`.
*   Start the `kv-dashboard` process on all nodes as well (binding to local agents).

#### [MODIFY] [dashboard/main.go](file:///Users/ankushsingh/Desktop/CMPT%20756/Distributed-RAFT-KV-Storage-prototype/cmd/dashboard/main.go)
*   Update the dashboard to query its "local" agent by default rather than a hardcoded `node0` IP.
*   *Benefit:* The system management interface remains available at any node's IP (e.g., `http://<node1-ip>:8080`).

---

### Phase 3: "Future Work" Roadmap (The Presentation Defense)
We are explicitly leaving Rank 3 (Gossip Bootstrap) as future work for the **V2.0 Roadmap** to avoid breaking the core Raft quorum logic 48 hours before the presentation.

#### [NEW] [ROADMAP.md](file:///Users/ankushsingh/Desktop/CMPT%20756/Distributed-RAFT-KV-Storage-prototype/ROADMAP.md)
Document the technical justification for "Future Improvements":
1.  **Gossip-based Bootstrap (Consul Pattern):** Moving away from `-join=node0` to a `memberlist` gossip approach.
2.  **Multi-Raft Sharding (TiKV Pattern):** Splitting data into Regions to allow horizontal scaling past the limits of a single Raft quorum.
3.  **Automatic Data Re-balancing (PD Pattern):** Using a background controller to move snapshots between nodes to balance disk I/O.

## Verification Plan

### Automated Tests
1.  **Client Failover Test:** Kill `node0`. Attempt to run `kv-client -addrs="node0:50051,node1:50052,node2:50053"`. 
    *   *Success Criterion:* Client prints "Connection to node0 failed, trying node1..." and successfully completes the write.
2.  **Dashboard Availability Test:** Kill `node0`.
    *   *Success Criterion:* User can still access the Dashboard UI via node1's external IP on port 8080.

### Manual Verification
*   Verify the presentation slides can now truthfully claim: **"No Single Point of Failure in either the Data Plane or the Control Plane."**
