# File Manifest: Roles and Industry Analogies

This document maps the project's file structure to its functional role within the distributed system.

---

## Core Source Files

| File / Path | Functional Role | Industry Analogy |
|-------------|-----------------|------------------|
| `server/node.go` | **Consensus engine.** Leader election, log replication, `VerifyLeader()` on reads, `AddVoter` join handler. | etcd server core — the "brain" that orchestrates consensus |
| `server/fsm.go` | **State machine.** Applies committed log entries to the KV store, manages idempotency table, handles snapshots. | Redis / TiKV storage layer — where the actual data lives |
| `cmd/agent/` | **Sidecar process.** HTTP API for OS-level fault injection: kill, pause, partition (iptables), netem, restart. | Kubernetes node agent — manages the lifecycle of the workload process |
| `cmd/client/` | **Smart client CLI.** Multi-address failover, auto-redirect to leader, idempotency (`client_id` + `seq_num`), follower reads. | etcdctl / redis-cli with built-in leader discovery |
| `cmd/dashboard/` | **Cluster UI.** Web interface for cluster health, fault injection controls, and KV operations. | Consul UI / Kubernetes Dashboard — single portal for cluster management |
| `cmd/chaos/` | **Chaos proxy (deprecated).** Early fault injection mechanism, superseded by sidecar agent in v1.2. | — |
| `proto/kv.proto` | **gRPC interface definition.** Defines `Get`, `Set`, `Delete`, `Health`, `Join` RPCs. | Protobuf / Apache Thrift — the communication contract between services |

---

## Scripts and Deployment

| File | Role | Industry Analogy |
|------|------|------------------|
| `dynamic_deploy.sh` | Provisions N GCP VMs, cross-compiles binaries for `linux/amd64`, bootstraps the Raft quorum. Full cluster in ~2 minutes. | Terraform + Ansible — Infrastructure as Code |
| `start_cluster.sh` | Starts a local 3-node cluster for development/testing without GCP. | Docker Compose for local dev |
| `teardown.sh` | Deletes all provisioned GCP VMs. | `terraform destroy` |
| `GCP_verify_phase[1-6].sh` | Automated 6-phase fault injection test suite. 37/37 on GCP (April 4, 2026). | Chaos Monkey (Netflix) — proves the system matches its fault model |
| `verify_follower_read.sh` | Dedicated verification for v1.3 follower read-index feature. 6/6 scenarios. | Feature integration test |

---

## Binaries and Generated Code

| File | Role |
|------|------|
| `bin/node-agent` | Pre-compiled `node-agent` binary (`linux/amd64`) for direct GCP deployment |
| `proto/kv.pb.go` | Auto-generated protobuf serialization code (do not edit manually) |
| `proto/kv_grpc.pb.go` | Auto-generated gRPC service stubs (do not edit manually) |

---

## Documentation

| File | Role |
|------|------|
| `README.md` | GitHub landing page: architecture, features, test results, quick start |
| `detailed_walkthrough.md` | Step-by-step technical walkthrough: write/read paths, fault injection, test suite, bugs |
| `presentation_speaking_notes.md` | Slide-by-slide speaking guide for the 4-slide presentation deck |
| `submission_docs/REPORT.md` | Full technical report (955 lines): design, implementation, performance, 37/37 results |
| `docs/ARCHITECTURE.md` | Architecture design defense: dual-port, quorum, fault model table |
| `docs/HOW_TO_RUN.md` | Detailed deployment and usage guide (this file's sibling) |

---

## Design Defense: Why the Sidecar is Separate

The `node-agent` is a completely independent process from `kv-store`. It operates at the OS level (signals, iptables, tc netem) and has no runtime dependency on the application.

**Why this matters for testing:** If the `kv-store` hangs, deadlocks, or enters a crash loop, the agent can still report health status and forcibly kill the process. More importantly, the application cannot accidentally detect or work around the fault injection — a `SIGKILL` is not catchable, and `iptables DROP` is invisible to the process. Every test in the 6-phase suite exercises a real fault that would affect a production deployment.

This pattern mirrors production cloud-native platforms: Kubernetes separates the Kubelet (node agent) from the application Pods. Consul separates the health-checking agent from the service. The agent is the controller; the store is the worker. They are distinct by design.
