# AI Image Generator Prompts — nanobanana2
**Project:** Distributed Raft KV Store v1.2
**Tool:** nanobanana2 (AI image generator)
**Node count:** 3-node for all diagrams (cleaner on slides; concepts fully provable at N=3)

---

## Workflow (Read This First)

AI image generators produce the **visual skeleton** — shapes, colors, layout, arrows. They cannot reliably render text labels (they hallucinate or garble them).

**The right process:**
1. Give nanobanana2 the prompt below — ask for NO text in the image
2. Download the generated image
3. Import it into Google Slides as a background image on the relevant slide
4. Add text labels manually as Slides text boxes on top of the image
5. Result: professional visual structure + accurate technical labels

Each diagram below has a **"Manual labels to add"** checklist for exactly this step.

---

## D1 — Architecture Topology
**Goes on:** Slide 2 (full width, primary visual) + Report system design section
**Slide caption:** *"Fig. 1: Three-replica Raft consensus group on GCP. The Leader role is elected — any replica can become leader. Each VM runs a Sidecar Agent as an independent control plane."*

### Prompt

```
Technical distributed systems architecture diagram, clean flat vector design, pure white background.

Layout: Three rounded-rectangle server node boxes arranged in a triangle.
- Top center: one node filled bright gold/yellow with a small crown symbol — this is the leader
- Bottom left: one node in light grey — follower
- Bottom right: one node in light grey — follower
- Below each of the three nodes: a small orange rounded-rectangle badge (the sidecar agent)

Connections:
- Three thick red bidirectional arrows forming a triangle connecting all three server nodes to each other (leader to follower-1, leader to follower-2, follower-1 to follower-2), with arrowheads on both ends of each arrow
- One green arrow entering from the left side of the image, from a small green laptop or monitor icon, pointing to the gold leader node

Style: Minimal, flat design, white background, professional tech conference presentation slide. No gradients, no shadows, no 3D effects. Sharp clean edges. The overall color palette is white background, gold/yellow leader node, light grey follower nodes, orange agent badges, red consensus arrows, green client arrow. No text or labels anywhere inside the image — labels will be added separately in slide software.
```

### Manual Labels to Add in Google Slides

Place these as text boxes over the generated image:

| Element | Label text |
|---------|-----------|
| Gold (top) node | `node0` on first line, `Leader` on second line (bold) |
| Bottom-left grey node | `node1` / `Follower` |
| Bottom-right grey node | `node2` / `Follower` |
| Orange badge under each node | `Sidecar Agent` (small font) |
| Label on one red arrow | `Raft TCP — AppendEntries · Heartbeat · Vote` |
| Label on green arrow | `gRPC  Get / Set / Delete` |
| Label on laptop icon | `kv-client` |
| Top of slide (above diagram) | `Raft Consensus Group — 3 Replicas on GCP` |
| Below diagram | `Writes commit on ⌊N/2⌋+1 ACKs · Minority partition → writes blocked (CP Safety)` |

---

## D2 — Write Path Sequence Diagram
**Goes on:** Report system design section
**Note:** Sequence diagrams are 80% text. If this prompt produces garbled results, use the Mermaid code in `diagram_specs.md` rendered at mermaid.live — paste the `sequenceDiagram` block, export as PNG, and add labels on top. The AI prompt below generates the visual skeleton only.

### Prompt

```
A UML sequence diagram, clean minimal flat design, pure white background.

Five vertical swimlane columns from left to right, each with a colored rectangle header at the top and a vertical dashed line extending downward for the full height of the image:
- Column 1 (leftmost): header rectangle filled green
- Column 2: header rectangle filled light grey
- Column 3 (center): header rectangle filled bright gold/yellow, slightly taller than the others to indicate importance
- Column 4: header rectangle filled light grey
- Column 5 (rightmost): header rectangle filled light grey

Seven horizontal arrows at evenly spaced vertical positions, going top to bottom:
1. Solid arrow with arrowhead pointing right, from Column 1 to Column 2
2. Dashed arrow with arrowhead pointing left, from Column 2 back to Column 1
3. Solid arrow with arrowhead pointing right, from Column 1 directly to Column 3 (longer, skipping Column 2)
4. Two parallel solid arrows at the exact same vertical height — one from Column 3 to Column 4, one from Column 3 to Column 5 — both pointing right. Show these as simultaneous by placing them at the same y-position with a small curly bracket on Column 3's left side
5. Dashed arrow with arrowhead pointing left, from Column 4 back to Column 3
6. A small looping arrow that curves from Column 3 back to itself (self-loop)
7. Dashed arrow with arrowhead pointing left, from Column 3 all the way back to Column 1 (longest arrow)

Between arrows 4 and 5, place a small rectangular note box with a light yellow background and thin border, floating in the center of the diagram

Style: Classic clean UML sequence diagram. White background. Thin precise lines. Solid arrows for requests, dashed for responses. No decorative elements, no gradients, no background color. No text anywhere in the image.
```

### Manual Labels to Add in Google Slides

| Element | Label text |
|---------|-----------|
| Column 1 header | `kv-client` |
| Column 2 header | `Follower (any)` |
| Column 3 header | `Leader` |
| Column 4 header | `Follower-1` |
| Column 5 header | `Follower-2` |
| Arrow 1 | `Set(key, val, client_id=C1, seq_num=42)` |
| Arrow 2 | `{not_leader, leader_addr}` |
| Arrow 3 | `Set(...) — retry to leader` |
| Arrows 4 (both) | `AppendEntries{index=N}` |
| Arrow 5 | `ACK` |
| Self-loop (arrow 6) | `Commit + Apply to FSM` |
| Arrow 7 | `{success: true}` |
| Yellow note box | `Majority reached (self + Follower-1). Follower-2 ACK not required for commit.` |

---

## D3 — Partition Fix: Before vs After
**Goes on:** Slide 4 (technical challenge callout, half-width) + Report implementation section
**Slide caption:** *"Fig. 3: BUG-4 — one-directional iptables (left) left the leader able to heartbeat followers, preventing election. Bidirectional fix (right) creates a true symmetric partition, forcing election in ~1.25s."*

### Prompt

```
A clean side-by-side two-panel comparison diagram on a pure white background, suitable for a technical presentation slide.

LEFT PANEL (occupies the left half of the image):
- A thin rounded-rectangle border in red color enclosing the entire left half
- Inside the panel: three circle nodes arranged in a triangle formation
  - Left circle: gold or yellow color, slightly larger than the other two
  - Top-right circle: light grey
  - Bottom-right circle: light grey
- Two solid thick green arrows pointing outward FROM the gold circle TO each of the grey circles (arrowheads land on the grey circles, indicating successful delivery)
- Two red X marks sitting on top of thin dashed grey lines that would go from each grey circle back toward the gold circle — the X marks clearly indicate these return paths are blocked and dropped

RIGHT PANEL (occupies the right half of the image):
- A thin rounded-rectangle border in green color enclosing the entire right half
- Inside the panel: three circle nodes in the same triangle arrangement, but now ALL THREE circles are light grey (no gold, no leader)
- Red X marks on ALL four arrow paths in BOTH directions between the three circles — every single connection is blocked with a dashed line and red X
- A small green checkmark symbol in the top-right corner of the right panel

Center: A thin vertical dashed line separating the two panels in the middle

Style: Flat design, minimal, white background, professional. Circles are simple solid filled shapes. Arrows have clear arrowheads. No text anywhere in the image. No gradients, no shadows.
```

### Manual Labels to Add in Google Slides

| Element | Label text |
|---------|-----------|
| Left panel header | `❌ Before — BUG-4: Receive Omission Only` (red text) |
| Right panel header | `✅ After — v1.2: Bidirectional Partition` (green text) |
| Left gold circle | `Leader (partitioned)` |
| Left grey circles | `Follower-1` and `Follower-2` |
| Left green arrows | `Heartbeat — still reaching followers` |
| Left X marks | `ACKs blocked (INPUT DROP)` |
| Left result box (draw as red text box in Slides) | `Followers receive heartbeats → no election fires → leader stays leader → writes ACCEPTED` + `= CP SAFETY VIOLATION` |
| Right X marks | `All traffic blocked (both directions)` |
| Right result box (draw as green text box in Slides) | `Followers miss heartbeats → election timeout fires → new leader in ~1.25s → writes REJECTED` + `= CP Safety preserved ✅` |
| Below left panel (small monospace font) | `iptables -A INPUT -p tcp --dport 12000 -j DROP` |
| Below right panel (small monospace font) | `iptables -A INPUT  -p tcp --dport 12000 -j DROP` (line break) `iptables -A OUTPUT -p tcp --dport 12001 -j DROP` (line break) `iptables -A OUTPUT -p tcp --dport 12002 -j DROP` |

---

## Diagram Placement Summary

| Diagram | Slide | Report Section | Size |
|---------|-------|---------------|------|
| D1 Architecture | Slide 2 — full width | System Design §1 | Full width |
| D2 Write path | — | System Design §4 | Half page |
| D3 Partition fix | Slide 4 — half width, right side | Implementation §6 | Half page |
| `latency_quorum_proof.png` | Slide 4 — embed | Results | Quarter page |
| `availability_mttr.png` | Slide 4 — embed | Results | Quarter page |
| `durability_proof.png` | — | Results | Quarter page |
