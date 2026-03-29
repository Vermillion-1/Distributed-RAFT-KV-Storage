# 5h — `cmd/dashboard/index.html` (Frontend)

**File:** `store/cmd/dashboard/index.html`  
**Role:** Single-file web app served by the dashboard. Polls the backend every second, renders the cluster topology on a `<canvas>`, and exposes all chaos controls as buttons.

---

## Structure

```
index.html
├── <style>        Dark theme CSS, button variants, grid layout
├── <body>
│   ├── Header     Title + status dot (🟢 Live / 🔴 Disconnected)
│   ├── .canvas-panel
│   │   └── <canvas id="topology">   Cluster topology diagram
│   ├── .controls-panel
│   │   ├── <select id="node-select">  Target node dropdown
│   │   └── .controls-grid            8 chaos buttons
│   ├── .health-panel                 Leader / Raft Term / Applied Index / Alive / MTTR
│   ├── .events-panel                 Event log (timestamped, auto-scroll)
│   └── .log-panel                    Raw node log viewer
└── <script>       All JavaScript (no external dependencies)
```

---

## Polling Loop

```javascript
// Runs every 1000ms
setInterval(async () => {
    const data = await fetch('/api/cluster').then(r => r.json());
    liveNodes = data.nodes;      // shared state for canvas + metrics + dropdown
    updateMetrics(liveNodes);    // update health panel
    drawFrame();                 // redraw canvas
    updateNodeSelect();          // refresh dropdown options
    updateEventLog(data.events); // append new events
}, 1000);
```

---

## Canvas Renderer (`drawFrame`)

Nodes are arranged in a **circle** on the canvas:

```javascript
// Position calculation
const angle = (2 * Math.PI / N) * i;
const x = cx + radius * Math.cos(angle);
const y = cy + radius * Math.sin(angle);
```

**Node colors:**
- 🟡 Gold (`#FFD700`) — Leader
- 🔵 Blue (`#4FC3F7`) — Follower
- 🔴 Red (`#EF5350`) — Dead / unreachable

**Leader pulse animation:** Leader node has a subtle growing outer ring that oscillates with `Math.sin(Date.now() / 400)`, giving a breathing effect.

**Edges:** Lines are drawn between every pair of nodes, colored `rgba(255,255,255,0.1)` — dim unless a node is alive.

---

## Button Wiring

Each button calls a helper `apiPost(url, logMessage)`:

```javascript
async function apiPost(url, msg) {
    const res = await fetch(url, { method: 'POST' });
    logEvent(msg, res.ok ? 'info' : 'error');
}
```

| Button ID | API Call |
|---|---|
| `btn-kill-leader` | `POST /api/kill/<current-leader-id>` |
| `btn-kill-node` | `POST /api/kill/<selected>` |
| `btn-restart-node` | `POST /api/restart/<selected>` |
| `btn-pause-node` | `POST /api/pause/<selected>` — SIGSTOP |
| `btn-resume-node` | `POST /api/resume/<selected>` — SIGCONT |
| `btn-drop-packets` | `POST /api/chaos/drop/<selected>?rate=1.0` |
| `btn-add-latency` | `POST /api/chaos/delay/<selected>?ms=3000` |
| `btn-stop-chaos` | `POST /api/chaos/stop/<selected>` |
| `btn-run-tests` | `POST /api/run-tests` |
| `btn-refresh-log` | `GET /api/logs/<selected>` |

---

## MTTR Tracking

```javascript
let killTime = null;

// Set when Kill Leader or Pause Node is pressed
killTime = Date.now();

// Cleared when a new leader is detected in updateMetrics()
if (leader && killTime) {
    const mttr = ((Date.now() - killTime) / 1000).toFixed(1);
    document.getElementById('mttr').textContent = mttr + 's';
    killTime = null;
}
```

---

## Button Styles (CSS)

| Class | Color | Used For |
|---|---|---|
| `.btn.danger` | Red | Kill actions |
| `.btn.warn` | Yellow | Chaos proxy (drop/delay) |
| `.btn.success` | Green | Restart, Resume |
| `.btn.partition` | Orange | Pause Node (SIGSTOP) |
| `.btn.purple` | Purple | Run Tests |
| `.btn` (default) | Grey | Stop Chaos, View Log |
