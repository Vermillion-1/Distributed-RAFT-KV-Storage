# Part 5 — SD Bridge: Local → Enterprise → GCP

## The Three Environments

| | Local Prototype | Enterprise App | GCP Target |
|---|---|---|---|
| **Node identity** | `127.0.0.1:12000` hardcoded | DNS name / service discovery | GCE internal IP, passed as startup flag |
| **Process lifecycle** | Dashboard spawns child PIDs | K8s manages pods | node-agent sidecar per VM handles local PID |
| **Failure injection** | SIGSTOP/SIGKILL to local PIDs | Chaos Engineering tool (Chaos Monkey, LitmusChaos) | SSH exec or agent HTTP endpoint |
| **Networking** | Loopback, no TLS | mTLS between all services | GCP VPC + firewall rules, TLS optional for prototyped phase |
| **Data persistence** | `/tmp/raft-kv/` (survives process restarts, not VM reimages) | Persistent volume (K8s PV) | GCE persistent disk mounted at `/data/raft-kv/` |
| **Observability** | `log.Printf` to stderr | Prometheus + Grafana + Jaeger | Cloud Monitoring + Cloud Logging (free tier) |
| **Client discovery** | Hardcoded `127.0.0.1:50051` | Service mesh (Envoy) or DNS | Static VM IPs passed as CLI args to kv-client |
| **Cluster bootstrap** | Sequential sleep in start_cluster.sh | Init containers + readiness probes | `deploy.sh` with `gcloud compute ssh` per VM |
| **Recovery** | Manual `/api/restart` click | K8s restartPolicy + liveness probe | agent sidecar auto-respawns kv-store on crash |
| **Multi-AZ** | N/A (single machine) | Nodes mandatorily across AZs | GCE zones: us-central1-a, -b, -c (one VM each) |

---

## Full Component Classification

### ✅ KEEP — Works As-Is (minor config only)

| Component | Why it's fine | What to change |
|---|---|---|
| [server/fsm.go](file:///Users/ankushsingh/Desktop/CMPT%20756/Distributed-RAFT-KV-Storage-prototype/server/fsm.go) — Raft FSM | Correct implementation; idempotency, snapshot, restore all sound | Nothing |
| [server/node.go](file:///Users/ankushsingh/Desktop/CMPT%20756/Distributed-RAFT-KV-Storage-prototype/server/node.go) — Raft setup + gRPC handlers | Two-port design, VerifyLeader reads, graceful transfer all correct | Bind gRPC to `0.0.0.0` not `127.0.0.1` |
| BoltDB log + stable store | Production-grade persistence library | Change data dir flag from `/tmp` to `/data` |
| Idempotency (clientID + seqNum) | Correctly prevents duplicate writes across retries | Nothing |
| `Health()` gRPC endpoint | Production-grade health monitoring pattern | Nothing |
| gRPC/Protobuf API | Already the right protocol for internal RPC | Add TLS credentials if time permits |
| `verify_phase*.sh` scripts | Good test logic; use dashboard API correctly | Replace `127.0.0.1` with VM IPs in the dashboard URL |

---

### ⚠️ MINOR TWEAK — Right idea, needs small fix

| Component | Problem | Fix |
|---|---|---|
| Raft timeouts in `node.go` | 150ms `LeaderLeaseTimeout` too tight for GCP (~1ms+ latency) | Bump to: Heartbeat=500ms, Election=750ms, LeaderLease=400ms |
| `main.go` default flag values | Default `--raft=127.0.0.1:12000` etc. will silently bind to loopback | Keep flags, change deploy script to always pass explicit IPs |
| `joinCluster()` retry loop | Flat 1s sleep, 10 retries — fine for 3 nodes | Add 2x exponential backoff: `sleep = min(1<<attempt, 30)` |
| `verify.sh` L2 FAIL counter | Counts an expected quorum-loss as FAIL in total | Add a `skip()` helper and mark L2 as `skip()` not `fail()` |
| `cmd/dashboard/main.go` | `/api/pause`, `/api/kill` send signals to local child PIDs only | Route these through node-agent HTTP endpoints (see ADD below) |
| Data directory | `/tmp/raft-kv/` | Change to `/data/raft-kv/` (persistent disk mount point) |

---

### 🔧 MAJOR TWEAK — Concept right, implementation must change significantly

| Component | Problem | New Implementation |
|---|---|---|
| **Process lifecycle management** | Dashboard uses `exec.Command` to spawn sub-processes and tracks PIDs locally. On GCP with separate VMs, this breaks entirely. | Dashboard becomes a **control plane only**: no spawning. It calls `POST /control/{kill,restart,pause,resume}` on each VM's node-agent. The node-agent is a tiny HTTP server that manages the local `kv-store` PID. |
| **`start_cluster.sh`** | Starts all nodes on localhost sequentially with sleep. No awareness of remote VMs. | Replace with `deploy.sh`: uses `gcloud compute ssh` to start `kv-store` on each VM with correct IPs. Waits for Health RPC to respond before moving to next node. |
| **Chaos proxy port routing** | `proxy_port()` in Phase 3 computes `22000 + index` as a local port. On GCP, proxy runs on each VM's own port space. | Each node's chaos proxy still listens on `:22000` locally on its VM. Dashboard calls node-agent to start/stop the proxy. Test scripts talk to the proxy via the VM's external IP, not localhost. |

---

### ❌ REMOVE / RETIRE

| Component | Reason |
|---|---|
| `run_chaos_test.sh` | Manages its own local cluster. Cannot be adapted for GCP VMs. Superseded by the `verify_phase*.sh` suite which correctly uses the dashboard API. |
| `start_cluster.sh` | Localhost-only. Replaced by `deploy.sh`. |
| `docs/intern/` (for production discussion) | Onboarding content; irrelevant to production FT argument. Don't cite this in your design discussion. |

---

### 🆕 MUST ADD — Critical gaps for any cloud deployment

| What | Why Critical | Implementation |
|---|---|---|
| **Node-agent sidecar** | Without it, the dashboard cannot kill/pause/restart nodes on remote VMs. Phase 2 (SIGSTOP) tests are completely blocked. | Small Go HTTP server (`/kill`, `/restart`, `/pause`, `/resume`) that manages the local `kv-store` process. ~100 lines. |
| **`deploy.sh`** | No automated way to provision and start the cluster on GCP VMs. | Bash script: `gcloud compute instances create` × 3, `gcloud compute firewall-rules create`, `gcloud compute scp` to push binary, `gcloud compute ssh` to start node-agent and kv-store with correct IPs. |
| **Firewall rules** | GCP blocks all inter-VM traffic by default. Raft TCP (`:12000-12002`) and gRPC (`:50051-50053`) will silently fail. | `gcloud compute firewall-rules create raft-internal --allow tcp:12000-12002,tcp:50051-50053,tcp:9000 --source-ranges=<VPC CIDR>` |
| **Persistent disk mount** | `/tmp` survives process restarts but NOT VM reimages. Durability test requires data to outlive the process, not the machine. | Attach a 10GB persistent disk to each VM, mount at `/data/raft-kv/`. Pass `--data=/data/raft-kv/node0` as flag. |
| **Context deadlines on gRPC** | `Join` and `Get` use `context.Background()` — unbounded wait. A hung node blocks the goroutine forever. | Wrap all outbound RPCs: `ctx, cancel := context.WithTimeout(ctx, 2*time.Second); defer cancel()` |
| **Structured health metrics** | Currently zero observability beyond stderr logs. Hard to measure MTTR precisely without timestamped events. | Minimum: add `time.Time` to the node-agent's `/health` response. Better: emit one Prometheus counter per state transition (leader→follower, follower→candidate). |

---

## To Be Reasonably FT Across All Layers on GCP

Working from the bottom up:

```
Layer 0 (Consensus):      ✅ Done — Raft with BoltDB
Layer 1 (Application):    ✅ Idempotency done. ADD: context timeouts, backoff
Layer 2 (Network/Proxy):  ✅ kv-chaos proxy done. NEEDS: GCP firewall rules + per-VM agent
Layer 3 (Platform):       PARTIAL — dashboard restart exists. ADD: node-agent for remote control
Layer 4 (Infrastructure): ADD: persistent disk, multi-zone VM placement
Layer 5 (Operational):    SKIP for prototype — document MTTR and quorum math in writeup
```

**Minimum viable cloud-FT work order:**
1. Node-agent sidecar (unblocks all remote lifecycle + Phase 2 tests)
2. `deploy.sh` (makes cluster formation repeatable)
3. Firewall rules (makes inter-VM Raft traffic work at all)
4. Persistent disk + flag update (makes D1 durability test valid)
5. Context timeouts on gRPC (prevents goroutine leaks under fault)
