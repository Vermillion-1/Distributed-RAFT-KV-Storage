# [CLAUDE BROWSER PROMPT] — Generate PPTX: Fault-Tolerant Distributed KV Store

## Your Task

Write and execute Python code using `python-pptx` to generate a 4-slide PPTX presentation. After running the code, provide the file as a download named `raft-kv-presentation.pptx`.

Do NOT ask clarifying questions. Generate the complete file immediately.

---

## Global Design System

### Slide Dimensions
- Widescreen 16:9: width = 13.333 inches, height = 7.5 inches

### Color Palette
```
BACKGROUND     = "F8F9FF"   # near-white with faint blue tint
HEADER_BAR     = "6C5CE7"   # indigo/purple — slide title bar
HEADER_TEXT    = "FFFFFF"   # white — text on header bar
BODY_TEXT      = "1A1A2E"   # dark charcoal
ACCENT_BLUE    = "3D5A80"   # muted slate blue — section headings
ACCENT_GREEN   = "2D6A4F"   # forest green — positive callouts
ACCENT_ORANGE  = "D4870A"   # warm amber — challenge/chaos callouts
LEADER_BLUE    = "3B8FC7"   # bright blue — LEADER highlight
PILL_BG        = "EAE8FF"   # lavender — tag/pill backgrounds
PILL_TEXT      = "4A3F8A"   # deep purple — text on pills
TABLE_HEADER   = "3D5A80"   # slate blue — table header rows
TABLE_ROW_ALT  = "EDF2F7"   # light blue-gray — alternating table rows
DIVIDER        = "C8C8E8"   # lavender — subtle dividers
```

### Typography
- Title font: Calibri Bold, 32pt, white, on HEADER_BAR strip
- Slide heading (if no bar): Calibri Bold, 26pt, ACCENT_BLUE
- Body text: Calibri, 13pt, BODY_TEXT, line spacing 1.2
- Table header: Calibri Bold, 11pt, white on TABLE_HEADER
- Table body: Calibri, 11pt, BODY_TEXT on white / TABLE_ROW_ALT
- Callout box label: Calibri Bold, 11pt

### Layout Constants
- Header bar: top=0, left=0, width=full, height=0.7in
- Content area starts: top=0.85in
- Slide margins: left=0.4in, right=0.4in, bottom=0.2in

### Helper: colored rectangle
Use `slide.shapes.add_shape(MSO_SHAPE_TYPE.RECTANGLE, ...)` with `fill.fore_color.rgb` and `line.color.rgb` set to match. Text frames go inside these shapes.

---

## Slide 1: Title & Overview

### Layout
- Full-width HEADER_BAR strip at top (height 1.4in) with centered title text
- Below header: two columns
  - LEFT (55% width): "Why we built this" + system class + fault model
  - RIGHT (40% width): CAP triangle callout box

### Header bar content
```
Title:    Fault-Tolerant Distributed Key-Value Store
          using Raft Consensus
Subtitle: Raft Consensus  ·  GCP  ·  Go  ·  37/37 Tests Passing
```
Title: 28pt, Bold, White
Subtitle: 14pt, Regular, "#D0CAFF" (light lavender)
Both centered horizontally and vertically in the bar.

### LEFT column (left=0.4in, top=1.6in, width=7in, height=5.5in)

**Block 1 — The Problem** (ACCENT_BLUE label "THE PROBLEM", 10pt bold + small caps)
Body text (13pt, BODY_TEXT):
```
Cloud VMs crash and networks partition without warning.
We built a system that survives both — automatically,
without data loss, without human intervention.
```

**Block 2 — System Classification** (label "SYSTEM CLASS")
```
CP Store (CAP Theorem)
  Consistency + Partition Tolerance
  Minority side sacrifices availability
  to preserve data correctness
```
Render "CP" in a rounded rectangle badge: bg=LEADER_BLUE, text=white, 16pt bold, inline.

**Block 3 — Fault Model** (label "FAULT MODEL — NON-BYZANTINE")
Render as 5 rows, each with a small colored bullet (●):
```
● Crash-Recovery      SIGKILL via /api/kill
● Process Freeze      SIGSTOP / SIGCONT via /api/pause · /api/resume
● Network Partition   Bidirectional iptables DROP via /api/chaos/partition
● Message Delay       tc netem via /api/chaos/netem
● Packet Loss         tc netem loss X% via /api/chaos/netem
```
Fault type: 12pt Bold, ACCENT_BLUE
Mechanism: 11pt, BODY_TEXT, right-aligned in same row

**Block 4 — Team / Course** (bottom, small, muted)
```
CMPT 756 — Fault-Tolerant Distributed Systems  ·  2026-04-01
```

### RIGHT column (left=7.6in, top=1.6in, width=5.3in, height=5.5in)

Rounded rectangle box, bg=PILL_BG, border=DIVIDER, 1pt:

**CAP Theorem Triangle** (draw with lines or text-art):
```
         ⬡ Consistency
        / \
       /   \
      / CP  \   ← highlighted
     /   ★   \
    /___________\
  Availability   Partition
                 Tolerance
```
Below triangle, bold label:
```
We chose: C + P
Availability sacrificed on minority partition
```

Small note below (italic, 11pt, muted):
```
"A minority replica correctly goes silent
rather than serving stale data."
```

---

## Slide 2: System Architecture & Key Design Decisions

### Layout
- HEADER_BAR strip (0.65in height), title left-aligned with padding
- LEFT half (width=6.8in): architecture diagram description
- RIGHT half (width=5.8in): design decision callouts
- BOTTOM strip (0.7in): quorum safety callout spanning full width

### Header
```
System Architecture & Key Design Decisions
```
18pt bold white, left-aligned, vertically centered in bar.

### LEFT half (left=0.4in, top=0.85in, width=6.6in, height=5.8in)

Draw a styled box (bg=white, border=DIVIDER, 1.5pt, rounded corners) containing the following architecture description as formatted text. Use monospace-like spacing with tab stops:

**Label above box** (ACCENT_BLUE, 10pt bold): "SYSTEM DIAGRAM — 3-NODE GCP CLUSTER"

**Inside the box**, use text with careful positioning to suggest a layered architecture:

```
  ┌─────────────────────────────────────────────────┐
  │  kv-dashboard :8080  —  Web UI · Chaos Orchestrator  │  ← bg: light lavender
  └──────────────┬──────────────────────┬───────────┘
        Health RPC (gRPC)        HTTP chaos ops
                 │                      │
  ┌──────────────▼──────────────────────▼──────────────┐
  │              GCP Cluster — us-central1              │
  │  ┌──────────┐   ┌──────────┐   ┌──────────┐        │
  │  │ node0    │   │ node1 👑 │   │ node2    │        │
  │  │ Follower │   │  LEADER  │   │ Follower │        │
  │  │          │   │          │   │          │        │
  │  │ gRPC Svr │◄──┤ gRPC Svr ├──►│ gRPC Svr │        │
  │  │:50051    │   │ :50052   │   │ :50053   │        │
  │  │──────────│   │──────────│   │──────────│        │
  │  │Raft Eng  │◄══╪══Raft Consensus══╪══►Raft Eng │  │
  │  │:12000    │   │ :12001   │   │ :12002   │        │
  │  │ FSM      │   │  FSM     │   │  FSM     │        │
  │  │ BoltDB   │   │  BoltDB  │   │  BoltDB  │        │
  │  │──────────│   │──────────│   │──────────│        │
  │  │🔧Sidecar │   │🔧Sidecar │   │🔧Sidecar │        │  ← bg: amber tint
  │  │:9000     │   │ :9000    │   │ :9000    │        │
  │  └──────────┘   └──────────┘   └──────────┘        │
  └────────────────────────────────────────────────────┘
         ▲
  kv-client (Smart Client)
  gRPC → auto-redirect to leader
  Exactly-once: clientID + seqNum
```

Render node boxes with colored borders:
- node1 (LEADER): LEADER_BLUE border, 2.5pt
- node0, node2 (Follower): ACCENT_BLUE border, 1pt
- Sidecar row inside each node: bg=amber tint "#FEF3DC"
- Dashboard box: bg=PILL_BG

Use colored connector lines where possible (Raft arrows = ACCENT_BLUE; chaos arrows = ACCENT_ORANGE).

Note: If you cannot draw boxes programmatically at this fidelity, render the architecture as a clean text diagram in a monospace text box (Courier New 9pt) with the colored borders applied around named sections using shape overlays.

### RIGHT half (left=7.2in, top=0.85in, width=5.7in, height=5.8in)

**Label** (ACCENT_BLUE, 10pt bold): "KEY DESIGN DECISIONS"

Render 5 callout cards stacked vertically (each ~0.95in height, full width, rounded rectangle, bg=white, left border accent 4pt):

**Card 1** — left border LEADER_BLUE
```
Dual-Port Node Design
Raft TCP  :12000+i  →  internal consensus only
gRPC API  :50051+i  →  external client requests
Fault injection on client path ≠ disrupts consensus
```

**Card 2** — left border ACCENT_ORANGE
```
Sidecar Agent (per VM, independent process)
SIGKILL  ·  SIGSTOP  ·  SIGCONT
Bidirectional iptables DROP  ·  tc netem delay/loss
HTTP API :9000 — no replica cooperation needed
```

**Card 3** — left border ACCENT_BLUE
```
Smart Client — kv-client
Connects to any replica → auto-redirects to leader
Multi-address failover after leader death
```

**Card 4** — left border ACCENT_GREEN
```
Exactly-Once Semantics
Every write: (client_id, seq_num)
FSM dedup table → retried writes never applied twice
```

**Card 5** — left border ACCENT_BLUE
```
Linearizable Reads
VerifyLeader() heartbeat before every read
Prevents stale reads from deposed leaders
+1 RTT cost per read
```

### BOTTOM strip (full width, top=6.6in, height=0.75in)
Rounded rectangle, bg="#FEF3DC" (amber tint), border=ACCENT_ORANGE, 1.5pt:
```
Write commits after ACK from ⌊N/2⌋+1 = 2 replicas (majority)
Minority partition → writes blocked (CP Safety)   ▶   MTTR ~1.25s
```
Text: 12pt, ACCENT_ORANGE bold for "majority" and "CP Safety", rest BODY_TEXT.

---

## Slide 3: Implementation & Engineering Challenges

### Layout
- HEADER_BAR strip (0.65in)
- Tech stack row (0.8in, directly below header)
- Two columns below: LEFT = Deployment Engine, RIGHT = Sidecar v1.2 hardening
- Bottom strip: test suite results pills

### Header
```
Implementation & Engineering Challenges
```

### Tech Stack Row (top=0.75in, full width, height=0.75in)
Horizontal row of 4 pill-shaped badges centered, bg=PILL_BG, text=PILL_TEXT, 12pt bold:
```
[ Go 1.21 ]   [ gRPC / Protobuf ]   [ HashiCorp Raft v1.7.3 ]   [ BoltDB (bbolt) ]
```
Each pill: rounded rectangle, bg=PILL_BG, border=DIVIDER 1pt, padding 0.15in horizontal.
Add a thin DIVIDER line below this row.

### LEFT column (left=0.4in, top=1.7in, width=6.2in, height=4.5in)

**Section label** (ACCENT_BLUE, 10pt bold): "DEPLOYMENT ENGINE"

**Card** (bg=white, border=DIVIDER, 1pt, rounded):

```
dynamic_deploy.sh
```
Bold title (ACCENT_BLUE, 13pt), then bullets (12pt, BODY_TEXT, 1.3 line spacing):

```
• Provisions N GCP e2-micro VMs, configures firewall rules
• Cross-compiles linux/amd64 binaries on macOS (GOOS=linux)
• SSH readiness retry loop — polls until sshd reachable
  (BUG-1: blind sleep replaced with active health check)
• Bootstraps full N-node Raft quorum in < 2 minutes
• Injects peer Raft addresses per node at startup
```

Below card, a small code snippet box (bg="#F0F0F0", monospace 9pt, Courier New):
```
# SSH readiness — not a blind sleep
wait_ssh() {
  until ssh -o StrictHostKeyChecking=no \
    "$1" "echo ok" 2>/dev/null; do
    sleep 2
  done
}
```

**Section label** (ACCENT_BLUE, 10pt bold), below code box: "DURABILITY & SNAPSHOTTING"

Bullets:
```
• BoltDB WAL: survives SIGKILL, power loss, process crash
• SnapshotThreshold = 10 entries
• InstallSnapshot RPC teleports full FSM to lagging replicas
• Recovery: load snapshot → replay log → rejoin → catch up
```

### RIGHT column (left=6.8in, top=1.7in, width=6.1in, height=4.5in)

**Section label** (ACCENT_ORANGE, 10pt bold): "SIDECAR AGENT — v1.2 HARDENING"

**Challenge Card 1** (bg="#FEF3DC", border=ACCENT_ORANGE, 1.5pt, rounded):
```
BUG-4: One-Directional Partition (FIXED)
```
Bold title (ACCENT_ORANGE, 12pt), then:
```
Before: INPUT DROP only on Raft port
→ isolated leader still sent heartbeats outbound
→ followers never timed out → no election fired
→ CP safety violation (isolated leader accepted writes)

After: Bidirectional iptables
  INPUT  DROP  own-raft-port     ← stop receiving ACKs
  OUTPUT DROP  each peer's port  ← stop sending heartbeats
→ followers timeout in 500ms → new leader elected
```
Use monospace 9pt for the iptables lines.

**Challenge Card 2** (bg="#F0FFF4", border=ACCENT_GREEN, 1.5pt, rounded):
```
BUG-5: Wrong NIC for netem (FIXED)
```
Bold title (ACCENT_GREEN, 12pt), then:
```
Before: tc netem scoped to Raft port via u32 filter
→ client gRPC traffic unaffected by delay tests

After: Apply netem to full NIC
  NIC=$(ip route get 8.8.8.8 | awk '{...}')
  tc qdisc add dev $NIC root netem delay ${ms}ms
→ NIC-agnostic (eth0, ens4, any Linux distro)
→ 10s auto-cleanup prevents stray rules
```

**Impact** (below both cards, ACCENT_GREEN, 12pt bold):
```
These two fixes: 33/36 → 37/37 test suite
```

### BOTTOM strip (full width, top=6.3in, height=0.85in)
Rounded rectangle bg=PILL_BG, border=DIVIDER:

```
Test Suite Coverage:
```
Then 6 pills in a row (each pill: bg=ACCENT_GREEN text=white for PASS, 10pt bold):
```
[P1  6/6 ✅]  [P2  9/9 ✅]  [P3  3/3 ✅]  [P4  4/4 ✅]  [P5  8/8 ✅]  [P6  13/13 ✅]
```
Then centered:
```
= 37 / 37 Total  ✅  Liveness · Partition · Latency · Durability · Idempotency · Kernel Chaos
```
"37 / 37" in 16pt Bold ACCENT_GREEN.

---

## Slide 4: Results & Analysis

### Layout
- HEADER_BAR strip (0.65in)
- Three result tables in the upper 2/3 of the slide (compact)
- Technical Challenge callout in the lower 1/3

### Header
```
Results & Analysis
```

### Table 1 — Write Latency & Throughput (left=0.4in, top=0.85in, width=5.8in)

**Label above** (ACCENT_BLUE, 10pt bold): "WRITE LATENCY UNDER FAULT INJECTION (Phase 3)"

Table (3 rows + header, compact 0.28in row height):

| Condition | Fault | Latency (ms/op) | Throughput (ops/s) | Change |
|---|---|---|---|---|
| Baseline | — | 19.6 | 51 | — |
| Slow Follower | 2000ms netem on 1 replica | 23.8 | 42 | −18% |
| Slow Leader | 500ms netem on leader | **502** | **2** | **−96%** |

Highlight "−96%" cell: bg=ACCENT_ORANGE tint "#FDEBD0", text=ACCENT_ORANGE bold.
Highlight "Slow Follower" row with note: tiny text "(quorum bypass — majority ignores slow replica)"

Table style: header row bg=TABLE_HEADER (white text, bold), alternating rows bg=white / TABLE_ROW_ALT.

### Table 2 — MTTR Breakdown (left=6.4in, top=0.85in, width=6.5in)

**Label above** (ACCENT_BLUE, 10pt bold): "MTTR — LEADER FAILURE (Phase 1 L1 + Phase 6 N6a)"

Table (3 rows + header):

| Window | Duration | Event |
|---|---|---|
| Detection | 0 – 500 ms | Followers miss heartbeats |
| Election | 500 – 1,250 ms | Candidate collects majority votes |
| **Total MTTR** | **~1.25 s** | **Same via SIGKILL and iptables** |

Highlight "Total MTTR" row: bg=LEADER_BLUE tint "#D6EAF8", text=LEADER_BLUE bold.
Highlight "~1.25 s" cell: LEADER_BLUE bold, 13pt.

### Table 3 — Durability (left=0.4in, top=3.15in, width=6.0in)

**Label above** (ACCENT_BLUE, 10pt bold): "KEY RECOVERY — PHASE 4 DURABILITY"

Table (3 rows + header):

| Scenario | Acknowledged | Recovered | Ratio |
|---|---|---|---|
| D1: Total cluster wipe + restart | 10 | 10 | **100%** |
| D2: Dirty leader crash mid-write | 7 | 7 | **100%** |
| D3: Snapshot catch-up (30 missed) | 30 | 30 | **100%** |

All "100%" cells: bg=ACCENT_GREEN tint "#D5F5E3", text=ACCENT_GREEN bold.

### Quorum Proof (right of Table 3, left=6.6in, top=3.15in, width=6.3in)

**Label** (ACCENT_BLUE, 10pt bold): "QUORUM PROOF — ⌊N/2⌋+1 FORMULA"

Small 2-row table:

| N | Quorum | Tolerates | Liveness | Unavailability trigger |
|---|---|---|---|---|
| 3 | 2 nodes | 1 failure | L2 ✅ | 2 kills → writes blocked |
| 5 | 3 nodes | 2 failures | L2 ✅ | 3 kills → writes blocked |

Below table, italic note (11pt, BODY_TEXT):
```
N=5 run (April 1, 2026) confirms the formula generalizes
beyond minimal 3-node case.
```

### Technical Challenge Callout Box (full width, top=5.0in, height=2.25in)
Rounded rectangle, bg="#FEF3DC", border=ACCENT_ORANGE, 2pt:

**Title** (ACCENT_ORANGE, 13pt bold): "KEY TECHNICAL CHALLENGE: Bidirectional Partition Fix"

Two sub-columns inside:

**LEFT sub-column** — label "BEFORE (BUG-4)" in red-tinted box:
```
INPUT DROP on own Raft port only
→ isolated leader still sends heartbeats outbound
→ followers never time out → no election
→ isolated leader accepts writes
→ CP safety VIOLATED  ✗
Result: N6c test FAIL (33/36)
```

**RIGHT sub-column** — label "AFTER (v1.2 FIX)" in green-tinted box:
```
Bidirectional iptables DROP:
  INPUT  DROP  own-raft-port
  OUTPUT DROP  each peer's raft-port
→ followers stop receiving heartbeats
→ election timeout fires in ~500ms
→ new leader elected, former leader steps down
→ CP safety PRESERVED  ✓
Result: 37/37 ✅  (+4 tests recovered)
```

Center divider between sub-columns: thin DIVIDER line with "→ FIX →" label in ACCENT_ORANGE badge.

---

## Output Instructions

1. Run the complete python-pptx code
2. Save as `raft-kv-presentation.pptx`
3. Provide it as a downloadable file
4. Print "Done! 4 slides generated." when complete

The file must have exactly 4 slides, in order: Title, Architecture, Implementation, Results.
Do not add any extra slides, table of contents, or appendix slides.
