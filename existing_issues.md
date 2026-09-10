# Existing Issues

A working list of known problems in this project, found during an adversarial review of the
documentation against the source code, the test scripts, the analysis data, and the
`hashicorp/raft` library itself.

Every item below was verified directly against a file or a measurement. Items marked **FIXED**
have been corrected in `docs/index.html` and the affected documents; items marked **OPEN** need a
decision or new work from the author.

Ordered by severity within each section.

---

## 1. Factual errors in the documentation

### 1.1 The test count is 42, not 43 — Phase 7 has five tests, not six · **FIXED**

`GCP_verify_phase7.sh` defines exactly five tests, `T1` through `T5` (ten `pass`/`fail` calls,
five of each). There is no `T6` anywhere in the script.

`analysis/results_analysis.md:37` reports the follower-read suite as **6/6**, and its coverage
table lists a `T6` whose description ("Follower read reflects latest write") is a duplicate of what
`T4` actually asserts (`GCP_verify_phase7.sh:75` — "T4: Follower read reflects updated value").

The phantom test propagated into every summary document: `docs/index.html`, `README.md`, and
`detailed_walkthrough.md` all quote **43/43**. The correct total is **37 + 5 = 42**.

> Fix applied: all headline counts changed to 42, and the `T6` row removed at the source — see 2.5.

### 1.2 MTTR is described as deterministic; it is randomized · **FIXED**

The page claimed:

> "Theoretical minimum MTTR = heartbeat + election = **1250 ms**. Observed MTTR matched it almost
> exactly, because detection is deterministic: the election timer fires a fixed interval after the
> last heartbeat."

This is wrong on mechanism. In `hashicorp/raft@v1.7.3`:

- `util.go:33` — `randomTimeout` "returns a value that is between the minVal and 2x minVal".
- `raft.go:163` — the follower's heartbeat timer is `randomTimeout(HeartbeatTimeout)`, so with
  `HeartbeatTimeout=500ms` detection fires uniformly in **[500 ms, 1000 ms)**.
- `raft.go:310` — the candidate's election timer is `randomTimeout(electionTimeout)`, drawn from
  **[750 ms, 1500 ms)**.

Raft randomizes both timers *deliberately*, to avoid split-vote livelock. Describing detection as
deterministic inverts a core design property of the algorithm.

The arithmetic is also wrong independently of randomization: `ElectionTimeout` is the deadline for
an election to complete before retrying, **not** the duration an election takes. On a quiet
three-node cluster the vote completes in roughly one round-trip, so adding 750 ms to the detection
time models something that does not happen.

**Measured, 7 local trials** (kill the leader, poll until a new leader is observed):

| | ms |
|---|---|
| Trials | 777 · 817 · 1007 · 1127 · 1137 · 1211 · 1997 |
| Min | 777 |
| Max | 1997 |
| Mean | 1153 |

**Five of seven trials landed below 1250 ms**, which cannot happen if 1250 ms is a minimum. The
2.6× spread is the randomization showing up directly.

Caveat on this measurement: it is localhost on an M-series Mac, not GCP cross-zone, so the absolute
numbers are not comparable to the reported GCP figures. What it establishes is the *shape* —
failover time is a distribution, not a constant.

### 1.3 The write path does not write to BoltDB · **FIXED**

The write-path sequence diagram showed `FSM -> write to BoltDB`. `FSM.Apply()` writes to an
in-memory map — `s.m[c.Key] = c.Value` (`server/fsm.go:170`) — and never touches BoltDB.

Durability of the *log entry* is provided earlier and separately by the Raft library, which fsyncs
to `raft-log.bolt` before `Apply` is invoked. The KV map itself has no direct disk backing; its
durability is derivative, via log replay and snapshots.

`detailed_walkthrough.md` repeats the same misconception ("writes key→value to BoltDB KV bucket").
There is no such bucket in the code.

### 1.4 The dedup table has a different name and shape than documented · **FIXED**

Documented as `lastSeq[clientID] = seqNum` and "a `(client_id → last_seq)` table". The actual field
is:

```go
lastApplied map[string]*clientEntry   // server/fsm.go:27
type clientEntry struct { SeqNum uint64; LastSeen time.Time }   // server/fsm.go:16
```

It stores a struct per client with a last-seen timestamp for TTL eviction, not a bare sequence
number. The substance of the claim is correct — it does live in the FSM and it *is* included in
snapshot and restore (`fsm.go:209-216`, `271-289`, `243-248`) — only the name and shape were wrong.

### 1.5 Cross-zone latency is quoted as two different numbers · **FIXED**

- `docs/index.html:550` — "`VerifyLeader()` costs one round-trip (**~30 ms** cross-zone on GCP)"
- `docs/index.html:714` — "Tuned for a **~15 ms** cross-zone RTT"

RTT already means round-trip time, so one round-trip at ~15 ms RTT should cost ~15 ms, not 30 ms.
`analysis/results_analysis.md:156` supports ~15 ms ("cross-zone jitter ~15ms"). The 30 ms figure is
unexplained and appears nowhere in any measurement.

Neither number is measured — there is no `ping` or RTT probe anywhere in the repo. Both are
asserted.

> Fix applied: standardised on ~15 ms and labelled it as an estimate rather than a measurement.

### 1.6 The "spurious election" was a deliberate fault injection · **FIXED**

The Limitations table said the aggressive election timeout was "empirically stable... with one
observed spurious election under heavy load."

That election was **R2b**, caused by intentionally injecting a 500 ms `tc netem` delay onto the
leader's NIC. `analysis/results_analysis.md:53` describes the outcome as "the correct Raft behavior
under network stress" — an expected result of a test designed to provoke exactly that, not an
anomaly, and not "heavy load".

---

## 2. Contradictions between documents — **OPEN, needs an author decision**

These cannot be fixed by editing one file, because the source documents disagree with each other
and I cannot tell from the repo which version is true.

### 2.1 "BUG-6" describes two entirely different bugs

- `detailed_walkthrough.md:397` — **"BUG-6: Follower Selector Picked the Test Node."** Root cause:
  `random.choice` could select `node0`. Credited with taking the suite from 36/37 to 37/37 on
  April 4.
- `analysis/results_analysis.md:53` — **"BUG-6 (R2b election not accepted as valid result)."** Root
  cause: the test script treated any election as a failure. A different fix entirely.

Same ID, different symptom, different root cause, different fix — and each is credited with closing
the final gap. **Decide which is BUG-6, and give the other its own number.**

### 2.2 The 5-node run is reported as both 37/37 and 36/37

Within a single file:

- `analysis/results_analysis.md:35` — "Test suite pass rate (5-node) | **37/37** | April 1, 2026"
- `analysis/results_analysis.md:55` — "Run 2 (April 1, 2026) — 5-node — **36/37**"

`detailed_walkthrough.md:105` sides with 37/37 on April 1, but its own `:405` says the 36→37 fix
happened on April 4 — so it contradicts itself too.

### 2.3 `docs/ARCHITECTURE.md` dates the 5-node run wrongly

`docs/ARCHITECTURE.md:62` attributes the 5-node validation to **April 4**. Every other document
assigns April 4 to the 3-node v1.3 run and the 5-node run to **April 1**.

### 2.4 `docs/ARCHITECTURE.md:36` claims BoltDB stores the KV state machine

> "BoltDB provides durable on-disk storage for both the Raft log and the KV state machine"

Contradicted by the code. BoltDB backs the log store and stable store only
(`server/node.go:89,94`); snapshots use `raft.NewFileSnapshotStore` (`server/node.go:83`); the KV
map is in-memory. See 1.3.

### 2.5 The phantom T6 originated in `analysis/results_analysis.md` · **FIXED**

Its coverage table listed a `T6` that does not exist in `GCP_verify_phase7.sh` and reported Phase 7
as 6/6 — the origin of the 43-vs-42 error in 1.1.

Its Phase 7 table was also misaligned with the script in a second way: it invented a "T3 = follower
read on node2" and shifted every later ID by one, so the documented assertions did not match what
the script asserts. Both are now corrected against `GCP_verify_phase7.sh` (T1–T5), along with the
grand total (42) and the accompanying commentary.

### 2.6 `docs/FILE_MANIFEST.md` is stale and references files that do not exist

| Reference | Line | Status |
|---|---|---|
| `start_cluster.sh` | 26 | Missing — renamed to `local_deploy.sh` |
| `verify_follower_read.sh` | 29 | Missing — renamed to `GCP_verify_phase7.sh` |
| `bin/node-agent` | 37 | Missing — no `bin/` directory in the public repo |
| `submission_docs/REPORT.md` | 50 | Missing — deliberately untracked |
| `presentation_speaking_notes.md` | 49 | Missing — course-internal |

It also predates v1.3: it describes a "6-phase" suite at "37/37" and never mentions Phase 7,
`docs/index.html`, or itself. **Either regenerate it or delete it** — as the only stale document
sitting beside an otherwise current set, it does more harm than good.

### 2.7 `kv-chaos` is documented as deprecated but presented as live infrastructure

`docs/FILE_MANIFEST.md:16` — "**Chaos proxy (deprecated).** Early fault injection mechanism,
superseded by sidecar agent in v1.2."

`docs/index.html` listed it in the components table and under "What was engineered on top" as
though it were load-bearing. Neither `detailed_walkthrough.md` nor `analysis/results_analysis.md`
references it in any of the 42 passing tests — all network fault injection goes through
`node-agent`'s iptables/netem API.

> Partially fixed: the page now marks it as superseded. **Decide whether to keep the source** (it
> is a working userspace TCP proxy and a reasonable thing to show) or remove it.

---

## 3. Measurement and rigour gaps — **OPEN**

### 3.1 MTTR is reported to three significant figures but measured to the nearest second

`GCP_verify_phase1.sh:87,92` and `GCP_verify_phase6.sh:377,387` time the election using bash's
`$SECONDS`, which is integer-valued. A harness with ±1 s granularity cannot produce "1.25 s".

**Fix:** use `date +%s%N` or `python3 -c 'import time;print(time.time())'` for millisecond
resolution, run 20+ trials per fault type, and report median plus min/max instead of a single
figure. Combined with 1.2, this is the single highest-value correction available.

### 3.2 The throughput numbers exist only as constants in the plotting script

The six headline figures (62.9 / 61.3 / 0.6 ops/s and 15.9 / 16.3 / 1645 ms/op) appear nowhere in
the repo except hardcoded in `analysis/generate_graphs.py:24-27`, under a comment attributing them
to the April 4 run. There is no raw timing log, CSV, or captured output backing them.

They are internally consistent (1/0.0159 ≈ 62.9, and so on), and the *relative* result — the quorum
bypass — is the interesting claim and is robust. But the figures are not currently reproducible.

**Fix:** have the phase scripts write raw timings to a CSV committed alongside the analysis, and
have `generate_graphs.py` read that file instead of literals.

### 3.3 Phase 6's test count varies by run

`GCP_verify_phase6.sh` contains 16 `pass` calls but only 8 `fail` calls. Eight checks can report
success but have no failure branch — their `else` only logs `info` — so they silently drop out of
the denominator instead of counting as failures. A flat "37/37" implies a fixed denominator that
the script does not actually guarantee. `analysis/results_analysis.md:339` concedes as much,
labelling Phase 6 "8+6 = variable by run".

**Fix:** give every check a `fail` branch.

### 3.4 "7/7 acknowledged writes" is one sample of a range

`analysis/results_analysis.md:187` — "Writes acknowledged before kill: 7 (varies by timing, but
consistently in 5–10 range)". The docs presented 7/7 as a fixed statistic. The *ratio* (100% of
acknowledged writes recovered) is the real result and holds regardless.

### 3.5 Headline numbers are single-run, not aggregated

`analysis/generate_graphs.py:6` — "All numbers from the final GCP run — April 4, 2026." The page
implied figures reflected all three runs. The runs materially disagree: the March 29 run measured
**82%** throughput retention for the slow-follower case versus **97%** in the featured run
(`analysis/results_analysis.md:147`).

### 3.6 The 5-node run is not reproducible from the repository

`dynamic_deploy.sh` genuinely parameterises node count, so the capability is real. But no logs or
output artifacts from a 5-node run are committed. The claim rests on the analysis documents, which
disagree with each other about its result (2.2).

### 3.7 No linearizability checker

Correctness rests on hand-designed scenarios. Recording operation histories and checking them with
Porcupine or Jepsen would convert "these specific scenarios passed" into "no history violated
linearizability". Already noted in the Limitations section; repeated here because it is the
highest-value remaining engineering work.

---

## 4. Smaller engineering and documentation defects

| # | Issue | Status |
|---|---|---|
| 4.1 | Mermaid multi-line node labels rendered as run-on strings ("node0 — Leaderkv-storeRaft :12000 · gRPC :50051") because mermaid v11 folds `<br>` and strips `<small>`. Fixed by moving to backtick markdown-string syntax. | **FIXED** |
| 4.2 | Fault table documented `iptables -A`; the code uses `iptables -I` (`cmd/agent/main.go:254,261`). Functionally similar, literally different. | **FIXED** |
| 4.3 | "Bootstraps a quorum in under two minutes" is asserted, never measured — `dynamic_deploy.sh` has no timing instrumentation. | **FIXED** (softened) |
| 4.4 | "Distributes them over SCP with an SSH retry loop" — the retry loop (`dynamic_deploy.sh:90-100`) polls SSH readiness *before* upload. The `gcloud compute scp` calls have no retry, and under `set -euo pipefail` a failed transfer aborts the run. | **FIXED** (reworded) |
| 4.5 | The BUG-5 narrative conflated two separate facts: the actual root cause was a `tc filter u32 match ip dport 12001` port scope (`detailed_walkthrough.md:389`), while the `eth0`/`ens4` mismatch was a related but distinct discovery. | **FIXED** |
| 4.6 | `LeaderLeaseTimeout = 400ms` (`server/node.go:62`) is set but documented nowhere, despite being relevant to read safety. | **FIXED** (added to config table) |
| 4.7 | No Prometheus metrics endpoint for term, commit index, or replication lag. | **OPEN** |
| 4.8 | No pre-vote — a partitioned node rejoining with an inflated term can disrupt a stable leader. | **OPEN** |

---

## 5. Suggested order of work

1. **Re-measure MTTR properly** (3.1) — millisecond timing, 20+ trials, report a distribution.
   This is cheap and turns the weakest claim into the strongest one.
2. **Resolve the document contradictions** (2.1, 2.2, 2.3) — only the author knows which version is
   true, and they undermine the credibility of the rest.
3. **Regenerate or delete `FILE_MANIFEST.md`** (2.6).
4. **Commit raw timing data** and make `generate_graphs.py` read it (3.2).
5. **Give every Phase 6 check a `fail` branch** (3.3).
6. **Add a linearizability checker** (3.7) — the highest-value new engineering.

---

*Compiled from a review of `docs/index.html`, `README.md`, `docs/ARCHITECTURE.md`,
`docs/HOW_TO_RUN.md`, `docs/FILE_MANIFEST.md`, `detailed_walkthrough.md`,
`analysis/results_analysis.md`, `analysis/generate_graphs.py`, `GCP_verify_phase1-7.sh`,
`server/`, `cmd/`, and `hashicorp/raft@v1.7.3`.*
