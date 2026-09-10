# Known Limitations & Roadmap

What this system does not do, where the measurements are weaker than they look, and what would be
built next. Kept alongside the code because the boundary of a result is part of the result.

Every limitation below was verified against the source, the test scripts, or a measurement — not
assumed.

---

## 1. Measurement rigour

### 1.1 Failover time is a distribution, and the harness measures it coarsely

The most commonly quoted number for this project is a ~1.2 s leader failover. Two things bound it.

**It is not a constant.** HashiCorp Raft randomizes both relevant timers to prevent split votes:
`randomTimeout` returns a value between `minVal` and `2 × minVal`
(`hashicorp/raft@v1.7.3 util.go:33`). With `HeartbeatTimeout=500ms` and `ElectionTimeout=750ms`:

| Phase | Actual interval | Source |
|---|---|---|
| Detecting the leader is gone | uniform in [500 ms, 1000 ms) | `raft.go:163` |
| Election deadline before retry | uniform in [750 ms, 1500 ms) | `raft.go:310` |
| The vote itself, on a quiet cluster | ≈ one round-trip | — |

Note also that `ElectionTimeout` is the deadline for an election to *complete*, not the time an
election takes — so failover is dominated by the randomized detection window, not by the sum of the
two timeouts.

A local reproduction over 7 leader kills measured **777, 817, 1007, 1127, 1137, 1211 and 1997 ms** —
a 2.6× spread, which is the randomization made visible. (Localhost, so the absolute values are not
comparable to cross-zone GCP figures; what it demonstrates is the shape.)

**The GCP harness cannot resolve it precisely.** `GCP_verify_phase1.sh:87,92` and
`GCP_verify_phase6.sh:377,387` time the election with bash's `$SECONDS`, which is integer-valued.
A ±1 s instrument cannot support a figure quoted to three significant figures. Read ~1.2 s as an
order-of-magnitude result.

**Fix:** millisecond timing (`date +%s%N`), 20+ trials per fault type, report median with min/max.
This is cheap and is the single highest-value improvement available — it turns the weakest claim in
the project into one of the strongest.

### 1.2 Throughput figures are not reproducible from committed data

The six headline latency/throughput numbers (62.9 / 61.3 / 0.6 ops/s and 15.9 / 16.3 / 1645 ms/op)
exist only as constants in `analysis/generate_graphs.py:24-27`, attributed there to the April 4 GCP
run. No raw timing log or CSV backs them.

They are internally consistent (1/0.0159 ≈ 62.9, and so on), and the *relative* result — the quorum
bypass — is robust and is the interesting finding. But the measurements themselves cannot currently
be re-derived from the repository.

**Fix:** have the phase scripts emit raw timings to a committed CSV, and have `generate_graphs.py`
read that file rather than literals.

### 1.3 Headline numbers are single-run, not aggregated

Figures come from the final GCP run (3-node v1.3, April 4 2026), not averaged across runs.
Run-to-run variance was material where it was measured: an earlier run showed 82% throughput
retention for the slow-follower case against 97% in the featured run. Single figures should be read
as representative rather than tight.

### 1.4 Phase 6's denominator is not fixed

`GCP_verify_phase6.sh` contains 16 `pass` calls but only 8 `fail` calls. Eight checks can report
success but have no failure branch — their `else` only logs `info` — so they drop out of the count
rather than registering as failures. A flat pass count implies a fixed denominator that the script
does not guarantee.

**Fix:** give every check a `fail` branch.

### 1.5 The 5-node run is not reproducible from the repository

`dynamic_deploy.sh` genuinely parameterises node count, so the capability is real and both 3-node
and 5-node clusters were deployed. But no logs or output artifacts from a 5-node run are committed,
so that configuration cannot be re-verified from what is here.

---

## 2. Correctness verification

### 2.1 No linearizability checker

This is the clearest gap. Correctness is validated by hand-designed scenarios that assert specific
expected outcomes. That establishes "these scenarios behaved correctly" — it does not establish
"no execution violated linearizability."

**Fix:** record operation histories (invocation and response timestamps per operation) and check
them mechanically with [Porcupine](https://github.com/anishathalye/porcupine) or a Jepsen-style
harness. This is the highest-value remaining engineering work in the project.

### 2.2 Empirically verified, not formally

No TLA+ specification or machine-checked proof. Confidence rests on the design and the test suite.
The consensus algorithm itself is HashiCorp's, which is well-tested — but the composition built on
top of it is not formally specified.

---

## 3. Protocol and feature gaps

| Gap | Detail |
|---|---|
| **No pre-vote** | A partitioned node that rejoins with an inflated term can disrupt a stable leader by forcing an unnecessary election. A well-known Raft extension; not implemented here. |
| **Aggressive election timeout** | `ElectionTimeout` is 1.5× `HeartbeatTimeout`, below HashiCorp's recommended 5–10×. Empirically stable under the tested conditions, but not validated at scale or under sustained adversarial load. |
| **No metrics endpoint** | Cluster state is observable through logs and the dashboard, but there is no Prometheus surface for term, commit index, or replication lag — which is what you would actually operate this with. |
| **No batching or pipelining** | `AppendEntries` are neither batched nor pipelined, which caps absolute throughput at ~63 ops/s on `e2-micro`. The interesting result here is relative behaviour, not the raw number. |
| **Byzantine faults out of scope** | Raft assumes crash-stop participants. Malicious nodes and packet corruption are outside the fault model. |

---

## 4. Smaller defects

| # | Issue | Status |
|---|---|---|
| 4.1 | Mermaid multi-line node labels rendered as run-on strings, because mermaid v11 folds `<br>` and strips `<small>` in node labels. Fixed with backtick markdown-string syntax plus `markdownAutoWrap: false`. | **FIXED** |
| 4.2 | The fault table documented `iptables -A`; the code uses `iptables -I` (`cmd/agent/main.go:254,261`). | **FIXED** |
| 4.3 | "Bootstraps a quorum in under two minutes" was asserted but never measured — `dynamic_deploy.sh` has no timing instrumentation. | **FIXED** (claim softened) |
| 4.4 | The SSH retry loop (`dynamic_deploy.sh:90-100`) guards pre-flight connectivity, not the SCP transfer; the `gcloud compute scp` calls have no retry, and under `set -euo pipefail` a failed transfer aborts the run. | **FIXED** (reworded) |
| 4.5 | `LeaderLeaseTimeout = 400ms` (`server/node.go:62`) is set but was documented nowhere, despite bearing on read safety. | **FIXED** (added to config table) |
| 4.6 | `cmd/chaos` (`kv-chaos`) was presented as current infrastructure; it was superseded by the sidecar agent in v1.2 and no reported result depends on it. | **FIXED** (marked superseded) |

---

## 5. Roadmap, in order

1. **Re-measure failover properly** — millisecond timing, 20+ trials, reported as a distribution
   (§1.1). Cheap, and it converts the weakest claim into a strong one.
2. **Commit raw timing data** and drive `generate_graphs.py` from it (§1.2).
3. **Give every Phase 6 check a `fail` branch** (§1.4).
4. **Add a linearizability checker** — record histories, verify with Porcupine (§2.1).
5. **Implement pre-vote** (§3).
6. **Expose Prometheus metrics** for term, commit index and replication lag (§3).
7. **Batch and pipeline `AppendEntries`** to lift absolute throughput (§3).

---

*Verified against `server/`, `cmd/`, `GCP_verify_phase1-7.sh`, `analysis/`, and
`hashicorp/raft@v1.7.3`.*
