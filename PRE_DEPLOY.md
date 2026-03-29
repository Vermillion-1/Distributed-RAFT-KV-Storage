# Pre-Deployment Checklist

Complete all three local tests before running `bash deploy.sh`.
Each test should take 2–5 minutes. Once all pass, readiness is **9/10**.

---

## Changes Made (What We Built)

| Block | Files Changed | What It Does |
|---|---|---|
| **A** | `server/node.go`, `main.go`, `verify.sh` | Raft timeouts for GCP latency, 5s context deadline on join, exponential backoff, L2 skip fix |
| **B** | `cmd/agent/main.go` *(new)*, `cmd/dashboard/main.go` | node-agent sidecar + dashboard GCP routing via `-agent-addrs` flag |
| **C** | `deploy.sh` *(new)*, `teardown.sh` *(new)* | One-command GCP cluster bring-up and teardown |
| **D** | All `verify_phase*.sh`, `verify.sh` | `DASHBOARD_HOST` + `CHAOS_HOST` env vars, retired localhost-only scripts |

---

## Deployment Readiness: 7/10 (before tests) → 9/10 (after tests pass)

**Risk areas:**
- `node-agent` has never been run — highest-risk new component
- `deploy.sh` has never been executed against a real GCP project
- Dashboard GCP mode (`-agent-addrs`) untested end-to-end

---

## Test 1 — Smoke-test node-agent locally ⚠️ Most Important

```bash
# Build binaries
go build -o node-agent ./cmd/agent/
go build -o kv-store .

# Start agent (it will spawn kv-store as a child)
./node-agent -kv-bin=./kv-store \
  -kv-args="-id=node0 -raft=127.0.0.1:12000 -grpc=127.0.0.1:50051 -data=/tmp/raft-kv/node0" &
sleep 2

# Check health
curl http://localhost:9000/health
# Expected: {"pid":<N>,"alive":true}

# Test pause/resume
curl -X POST http://localhost:9000/pause
curl -X POST http://localhost:9000/resume

# Test kill
curl -X POST http://localhost:9000/kill
sleep 1
curl http://localhost:9000/health
# Expected: {"pid":<N>,"alive":false}

# Cleanup
pkill node-agent
pkill kv-store 2>/dev/null || true
```

**Pass criteria:** `/health` returns `alive:true` on start, `alive:false` after kill.

---

## Test 2 — Cross-compile all binaries (exact commands used by deploy.sh)

```bash
mkdir -p bin
GOOS=linux GOARCH=amd64 go build -o bin/kv-store      .
GOOS=linux GOARCH=amd64 go build -o bin/kv-client     ./cmd/client/
GOOS=linux GOARCH=amd64 go build -o bin/kv-chaos      ./cmd/chaos/
GOOS=linux GOARCH=amd64 go build -o bin/kv-dashboard  ./cmd/dashboard/
GOOS=linux GOARCH=amd64 go build -o bin/node-agent    ./cmd/agent/
ls -lh bin/
```

**Pass criteria:** All 5 binaries present in `./bin/`, no build errors.

---

## Test 3 — Local cluster regression with new Raft timeouts

```bash
# Build dashboard
go build -o kv-dashboard ./cmd/dashboard/

# Start 3-node cluster
./kv-dashboard -nodes=3 -port=8080 &
sleep 12   # wider timeouts mean slightly longer bootstrap than before

# Run Phase 1
bash verify.sh 8080

# Cleanup
pkill -f kv-dashboard; pkill -f kv-store
```

**Pass criteria:** L1 PASS, L2 SKIP (not FAIL), L3 PASS.

> **Note:** MTTR may show ~1.5–2s locally instead of ~1s. This is expected — timeouts
> are tuned for GCP latency. Behaviour is correct, just slightly slower on loopback.

---

## Pending Day-2 Tasks (After Deployment)

```bash
export GCP_PROJECT=<your-project-id>
bash deploy.sh                          # bring up the cluster

# SSH into VM-0 to run tests from inside the VPC
gcloud compute ssh node0 --zone=us-central1-a

# From VM-0:
bash ~/verify.sh 8080                   # Phase 1 — record MTTR
bash ~/verify_phase2.sh 8080            # Phase 2 — confirm 7/7 pass
bash ~/verify_phase3.sh 8080            # Phase 3
bash ~/verify_phase4.sh 8080            # Phase 4

bash teardown.sh                        # cleanup when done
```

Fill in MTTR comparison table for the report:

| Metric | localhost | GCP |
|---|---|---|
| MTTR after leader kill | ~1s | ? |
| Write throughput (ops/sec) | (measure) | (measure) |
| Phase 1 result | PASS | ? |
| Phase 2 result | 7/7 | ? |
| Phase 3 result | pending | ? |
| Phase 4 result | pending | ? |

---

## Known Issue to Watch On Deployment

The `pump_writes` helper in `verify_phase2.sh` dials `127.0.0.1:<grpc-port>`. When
running from VM-0 over SSH, `127.0.0.1` only reaches VM-0's own kv-store (node0).
For writes to node1 or node2, the leader redirect will handle it — but if the test
picks node1's port and tries to pump writes through `127.0.0.1:50052`, it will fail.

**Workaround:** Run Phase 2 while node0 is the leader (it usually is after a fresh
start), or accept that pump_writes may show fewer than 20 writes succeeding and
check that the ones that did succeed are correctly replicated.
