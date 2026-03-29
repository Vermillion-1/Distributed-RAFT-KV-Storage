# GCP Port TODO — 1-2 Day Sprint

## Context (read this first when resuming cold)

**What this project is:** A 3-node fault-tolerant distributed KV store built in Go using HashiCorp Raft + BoltDB + gRPC. Core is solid and fully working locally. Goal is to port it to 3 GCP VMs and run the 4-phase chaos test suite on real hardware.

**What's done:** Core Raft implementation, FSM, idempotency, snapshotting, gRPC API, chaos proxy, 4-phase bash test suite (`verify.sh`, `verify_phase2.sh`, `verify_phase3.sh`, `verify_phase4.sh`), web dashboard.

**What's missing for GCP:** node-agent sidecar (Block B — the critical unblocker), deploy.sh (Block C), plus small code fixes (Block A) and script adaptations (Block D).

**Key files:**
- `server/node.go` — Raft setup + gRPC handlers (edit for timeouts, context deadlines)
- `main.go` — node entrypoint (edit for backoff)
- `verify.sh` — edit for L2 skip fix
- `cmd/agent/main.go` — **NEW FILE to create** (node-agent sidecar)
- `cmd/dashboard/main.go` — update to route kill/pause/restart to agent HTTP endpoints
- `deploy.sh` + `teardown.sh` — **NEW FILES to create**

**Companion doc:** `GCP_DEPLOY.md` — how to actually run the deployment and tests once code is done.

**Monday deadline:** GCP cluster live + Phase 1 + Phase 2 results for professor meeting.

---

## Block A — Quick Code Fixes (~2h)
> Batch into one LLM prompt per file. Paste the exact function you want changed.

### A1. Tune Raft timeouts
- **File:** `server/node.go` → `NewNode()` (~line 54)
- **Change:**
  ```go
  config.HeartbeatTimeout   = 500 * time.Millisecond
  config.ElectionTimeout    = 750 * time.Millisecond
  config.CommitTimeout      = 100 * time.Millisecond
  config.LeaderLeaseTimeout = 400 * time.Millisecond
  ```
- **Why:** 150ms LeaderLeaseTimeout is too tight for GCP inter-VM latency
- [x] Done

### A2. Add context deadline to `joinCluster`
- **File:** `main.go` → `joinCluster()` (~line 107)
- **Change:** Replace `ctx := context.Background()` with:
  ```go
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()
  ```
- **Why:** Unbounded gRPC call hangs forever if target VM is unreachable
- [x] Done

### A3. Exponential backoff in join retry loop
- **File:** `main.go` → `main()` join retry loop (~line 65)
- **Change:** Replace `time.Sleep(1 * time.Second)` with:
  ```go
  backoff := time.Duration(1<<uint(i)) * time.Second
  if backoff > 30*time.Second { backoff = 30 * time.Second }
  time.Sleep(backoff)
  ```
- **Why:** Flat retry causes thundering herd on new leader
- [x] Done

### A4. Fix verify.sh L2 false FAIL
- **File:** `verify.sh` (~line 108)
- **Change:** Add `skip() { echo "⏭  SKIP — $1"; ((SKIP++)); }` helper. Replace the `fail` in L2 with `skip "L2: Expected — quorum lost with 2/3 dead (correct CP behavior)"`
- [x] Done

> **LLM prompt for A1-A3:**
> ```
> I have a HashiCorp Raft KV store. Make only these targeted changes:
> 1. In server/node.go NewNode(): set HeartbeatTimeout=500ms, ElectionTimeout=750ms,
>    CommitTimeout=100ms, LeaderLeaseTimeout=400ms
> 2. In main.go joinCluster(): replace context.Background() with context.WithTimeout 5s
> 3. In main.go join retry loop: replace flat 1s sleep with exponential backoff capped at 30s
> Show only changed lines with 3 lines of surrounding context. Don't refactor anything else.
> [paste current content of both files]
> ```

---

## Block B — Node-Agent Sidecar (~3h) ⚠️ MOST CRITICAL
> Without this, dashboard cannot SIGSTOP/SIGKILL/restart nodes on remote GCP VMs.
> Phase 2 partition tests are blocked until this exists.

### B1. Create `cmd/agent/main.go`
- A standalone Go HTTP server on `:9000` per VM
- Spawns `kv-store` as a child (receives kv-store args via its own `-kv-args` flag)
- **Endpoints:**
  ```
  POST /kill         → SIGKILL child PID
  POST /pause        → SIGSTOP child PID
  POST /resume       → SIGCONT child PID
  POST /restart      → SIGKILL + re-exec with same args
  GET  /health       → {pid, alive bool}
  POST /partition    → iptables DROP on Raft port (optional — see B3)
  POST /unpartition  → remove iptables DROP rule (optional — see B3)
  ```
- [x] File created

### B2. Update dashboard routing
- **File:** `cmd/dashboard/main.go`
- **Find:** handlers for `/api/kill/:id`, `/api/pause/:id`, `/api/resume/:id`, `/api/restart/:id`
- **Change:** Route to `http://<agent-ip>:9000/{kill,pause,resume,restart}` via `http.Post`
- **Config:** Add `-agent-addrs=node0=<ip>:9000,node1=<ip>:9000,node2=<ip>:9000` flag to dashboard
- [x] Done

### B3. ⚡ Required — True iptables Network Partition (~45 min, only if B1+B2 done early)
> Upgrades Phase 2 from "process freeze" to "true network partition". The isolated node
> stays alive and Raft keeps running — more rigorously tests split-brain prevention.

- **Add to `cmd/agent/main.go`** — two extra handlers:
  ```go
  // POST /partition — drop Raft traffic at kernel level
  exec.Command("sudo", "iptables", "-I", "INPUT",  "-p", "tcp", "--dport", "12000", "-j", "DROP").Run()
  exec.Command("sudo", "iptables", "-I", "OUTPUT", "-p", "tcp", "--sport", "12000", "-j", "DROP").Run()

  // POST /unpartition — remove the rules
  exec.Command("sudo", "iptables", "-D", "INPUT",  "-p", "tcp", "--dport", "12000", "-j", "DROP").Run()
  exec.Command("sudo", "iptables", "-D", "OUTPUT", "-p", "tcp", "--sport", "12000", "-j", "DROP").Run()
  ```
- **Update `verify_phase2.sh`**: replace `pause_node` → `/partition`, `resume_node` → `/unpartition`
- **Report line to add:** *"SIGSTOP simulates fail-stop; iptables DROP simulates true network partition — the process stays alive but is isolated, which is the more rigorous split-brain test."*
- [x] Required — implemented in cmd/agent/main.go

> **LLM prompt for B3:**
> ```
> Add two HTTP handlers to cmd/agent/main.go:
> POST /partition: run these two iptables commands via exec.Command:
>   sudo iptables -I INPUT  -p tcp --dport 12000 -j DROP
>   sudo iptables -I OUTPUT -p tcp --sport 12000 -j DROP
> POST /unpartition: run the same commands with -D instead of -I (delete rule).
> Log the command output. Return 200 on success, 500 on error.
> Show only the new handler code to append to the file.
> ```

> **LLM prompt for B1:**
> ```
> Create cmd/agent/main.go for Go module github.com/ankush/raft-kv.
> It's an HTTP server on :9000 that manages one kv-store child process.
> Accepts -kv-bin (path to kv-store binary, default ./kv-store) and 
> -kv-args (space-separated args to pass to it) flags.
> On startup, spawns kv-store as a child process.
> Endpoints: POST /kill (SIGKILL), POST /pause (SIGSTOP), POST /resume (SIGCONT),
> POST /restart (kill + re-exec same args), GET /health (returns JSON: pid, alive).
> Keep it under 130 lines. Use os/exec for spawn, os.FindProcess + Signal for signaling.
> ```

> **LLM prompt for B2 — paste the 4 relevant handler functions then say:**
> ```
> Update these 4 handlers to route to agent HTTP endpoints instead of local signals.
> Add a flag: -agent-addrs (comma-separated node0=ip:9000,node1=ip:9000,...).
> Parse it into a map[string]string at startup. In each handler, POST to
> http://<agent-addr>/{kill,pause,resume,restart}. Return 502 if agent unreachable.
> Don't change anything else.
> ```

---

## Block C — deploy.sh (~1.5h)

### C1. Create `deploy.sh` in project root
- Cross-compiles all binaries for linux/amd64
- Creates 3 e2-micro VMs in us-central1-a/b/c
- Creates firewall rules for ports 12000-12002, 50051-50053, 9000, 8080
- SCPs binaries to each VM
- SSHes to start node-agent (which starts kv-store) with correct IPs
- Polls `:9000/health` until all 3 agents report alive
- Prints final cluster IP table

### C2. Create `teardown.sh`
- Deletes all 3 VMs and the firewall rules
- [x] Both files created

> **LLM prompt for C1+C2:**
> ```
> Write deploy.sh and teardown.sh for a 3-node Raft KV cluster on GCP.
> 
> Setup: node0→us-central1-a, node1→us-central1-b, node2→us-central1-c
> Machine type: e2-micro. Project from $GCP_PROJECT env var.
> Ports: Raft=1200X, gRPC=5005X, agent=9000, dashboard=8080 (X=node index)
> 
> kv-store flags per node:
>   -id=nodeX -raft=<internal-ip>:1200X -grpc=0.0.0.0:5005X \
>   -data=/data/raft-kv -join=<node0-internal-ip>:50051  (omit -join for node0)
>
> agent flags per node:
>   -kv-bin=./kv-store -kv-args="<above flags>"
>
> Steps: create VMs → wait ready → create firewall rules → 
> cross-compile (GOOS=linux GOARCH=amd64) → build 4 binaries (kv-store, kv-client, kv-chaos, kv-dashboard, node-agent) →
> scp to each VM → ssh to start agent → poll /health until alive →
> on VM-0: start kv-dashboard with -agent-addrs flag → print IP table.
> Use gcloud CLI. Add error handling with set -e.
> ```

---

## Block D — Script Adaptations for GCP (~30 min)
> Small but required — tests will fail or connect to wrong hosts without these.

### D1. Update `verify_phase3.sh` proxy_port routing
- **File:** `verify_phase3.sh` → `proxy_port()` function
- **Problem:** Computes `22000 + node_index` as `localhost` port. On GCP the chaos proxy runs on each VM's own `:22000` — test must reach it via the VM's external IP.
- **Change:** When running against GCP, pass `CHAOS_HOST=<vm-external-ip>` as env var, and have `proxy_port()` return `${CHAOS_HOST:-127.0.0.1}:22000`
- **Or simpler:** just note in report that Phase 3 chaos proxy tests were run from within the VPC where `localhost` still maps correctly via the dashboard proxy
- [x] Handled (CHAOS_HOST env var added to verify_phase3.sh)

### D2. Point test scripts at GCP dashboard
- **All `verify_phase*.sh`** use `API="http://localhost:${PORT}/api"` at the top
- **Change:** When running from your laptop, set the PORT env var and also override the API base:
  ```bash
  API="http://<node0-external-ip>:8080/api" bash verify.sh
  ```
- Alternatively SSH into VM-0 and run the scripts from there — `localhost:8080` works natively
- [x] Decided approach (DASHBOARD_HOST env var — works from laptop or VM-0)

### D3. Retire localhost-only scripts
- [x] RETIRED comment added to start_cluster.sh
- [x] RETIRED comment added to run_chaos_test.sh
- These are superseded by `deploy.sh` + `verify_phase*.sh`. They can stay in the repo but should not be invoked during GCP testing.

---

## Day 2 — GCP Deploy + Collect Results

- [ ] `export GCP_PROJECT=<your-project-id>`
- [ ] `bash deploy.sh` — fix any startup errors
- [ ] `./kv-client -addr <node0-ip>:50051 -cmd health` — confirm cluster alive
- [ ] SSH into VM-0: `gcloud compute ssh node0 -- 'bash ~/verify.sh 8080'` (runs Phase 1)
- [ ] Phase 1: record MTTR from output
- [ ] Phase 2: `bash verify_phase2.sh 8080` from VM-0 → confirm 7/7 pass
- [ ] Phase 3: `bash verify_phase3.sh 8080` from VM-0
- [ ] Phase 4: `bash verify_phase4.sh 8080` from VM-0
- [ ] Fill in results table: localhost MTTR=1s vs GCP MTTR=?
- [ ] `bash teardown.sh`

---

## Hard Skips — Note in Report, Don't Implement
- TLS/mTLS → "production would use mTLS via cert-manager"
- Prometheus metrics → noted as observability gap; node-agent `/health` timestamp is minimum
- Circuit breaker → noted as gap; kv-client retries blindly (fine at 3-node scale)
- Persistent disk → VM root disk at `/data/raft-kv/` is sufficient for proc restarts
- Multi-region → out of scope; nodes are already in separate GCP zones (a/b/c)

---

## LLM Prompting Rules
1. **One file per prompt.** Never ask LLM to edit 2+ files simultaneously.
2. **Always paste the current code** of the function/block you want changed.
3. **Say "show only changed lines + 3 lines context"** — prevents full rewrites.
4. **For new files** (agent, deploy.sh), ask for the full file — no existing code to corrupt.
5. **Verify LLM output against the actual file** before applying — it invents line numbers.
6. **If output is wrong**, paste the actual current code back and say "this is what it actually looks like, try again."
