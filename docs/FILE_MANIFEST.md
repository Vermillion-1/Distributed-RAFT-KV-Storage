# File Manifest: Roles & Industry Analogies

This document maps our project's file structure to its functional role within a distributed system. We have modeled our components after **Industrial Scale** patterns.

---

| File Path | Functional Role | Industry Analogy (Analogy) |
| :--- | :--- | :--- |
| **`server/node.go`** | **Consensus Partition Engine.** Handles leader election and log synchronization logic. | **Etcd Core / Raft (CoreOS).** Like the etcd server inside Kubernetes, this is the "Brain." |
| **`server/fsm.go`** | **State Machine Replicator.** Maps the Raft log into the actually readable/writable key-value storage. | **Redis / TiKV Layer.** This is where the "Application" actually lives. |
| **`cmd/agent/main.go`** | **Distributed Process Sidecar.** Monitors and manages the node's local health and signals. | **Kubernetes Kubelet.** It ensures the node is "Healthy" and reports back to the Control Plane. |
| **`cmd/dashboard/`** | **Cluster Controller & UI.** Central orchestrator that provides a unified "Health" view. | **Consul UI / Kubernetes Dashboard.** The single "Portal" for cluster management. |
| **`cmd/client/main.go`** | **Smart Client with Redirects.** Automatically retries and follows the leader across the network. | **Etcdctl / Redis-CLI.** The standard tool for end-users to query the distributed cluster. |
| **`dynamic_deploy.sh`** | **Cloud-Native Provisioner.** Automates VM creation, binary distribution, and bootstrapping. | **Terraform / Ansible.** These tools define "Infrastructure as Code" (IaC). |
| **`proto/kv.proto`** | **Interface Definition Language (IDL).** Defines the strict gRPC communication contract between systems. | **Standard Protobuf / Apache Thrift.** The universal language of microservices. |
| **`GCP_verify_*.sh`** | **Validation Suite.** Automated integration tests that confirm the system's resilience under chaos. | **Chaos Monkey (Netflix) Testing.** Proves that the system matches its "Fault Model." |

---

## 🏗️ 1. Why this File Structure? (Design Defense)
We deliberately separated the **`node-agent`** from the **`kv-store`**. 

**The Distributed Logic:** 
If the `kv-store` hangs or enters a deadlock, the **Agent** can still report its health and forcefully "SIGKILL" the process. This **Symmetrical Separation** is common in cloud-native platforms like Kubernetes, where the Kubelet manages the lifecycle of the actual workload (Pods). 

By building our system this way, we've demonstrated an understanding of **Decoupled Architecture**, where the "Controller" (Agent) and the "Worker" (Store) are distinct entities.
