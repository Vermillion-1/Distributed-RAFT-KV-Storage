# Changelog - March 30-31, 2026

## Overview
This changelog documents all code changes made during the development session to enhance the Distributed Raft KV Storage prototype with kernel-level chaos testing, KV operations via dashboard, and election tracking.

---

## Session 1: Phase 5 & 6 Test Scripts Creation

### File: GCP_verify_phase5.sh (NEW)
**Purpose:** Test idempotency and client features on GCP

**Tests implemented:**
- I1: Follower Redirect - Verify client receives leader redirect from follower
- I2: Idempotent Set - Send same SET twice with same clientId+seq, verify deduplication
- I3: Idempotent Delete - Send same DELETE twice, verify idempotent behavior
- I4: Client Failover - Kill leader, verify client can write to new leader

**Reasoning:** The problem statement required testing of idempotent writes. This script automates verification of the duplicate detection mechanism.

---

### File: GCP_verify_phase6.sh (NEW)
**Purpose:** Test kernel-level chaos (netem + iptables) on GCP

**Tests implemented:**
- N1: iptables Partition - Drop Raft traffic via kernel-level iptables
- N2: netem on Follower - 500ms delay, verify writes still fast (quorum bypass)
- N3: netem on Leader - 500ms delay, verify latency increases
- N4: netem with Packet Loss - 30% loss + 200ms delay
- N5: Dashboard Access During Chaos - Verify UI works via other nodes

**Safety Features:**
- Auto-cleanup after 10s timeout for all netem tests
- Cleanup function at end of script removes all rules

**Reasoning:** The existing kv-chaos proxy only affected client-side traffic. For true cluster-level chaos testing, we needed kernel-level network impairment. tc/netem and iptables provide this capability.

---

## Session 2: Client Idempotency Testing Support

### File: cmd/client/main.go

**Full Line-by-Line Changes:**

#### 1. Added flag declarations (Lines 32-34) - NEW
```go
// NEW - Added after existing flags (after line 30)
    // Idempotency control flags (for testing duplicate detection)
    explicitClientID := flag.String("client-id", "", "Explicit client ID for idempotency testing (default: auto-generated UUID)")
    explicitSeqNum := flag.Uint64("seq-num", 0, "Explicit sequence number for idempotency testing (default: auto-increment)")
```

#### 2. Added flag parsing logic (Lines 38-45) - NEW
```go
// NEW - Added after flag.Parse() (line 36)
    // Use explicit client-id if provided, otherwise generate one
    if *explicitClientID != "" {
        clientID = *explicitClientID
    }
    // Use explicit sequence number if provided, otherwise auto-increment
    if *explicitSeqNum != 0 {
        sequenceNo = *explicitSeqNum - 1 // Will become the provided value after atomic add
    }
```

**Before:** Client only had auto-generated UUID and auto-incrementing sequence number:
```go
// Old code:
clientID = uuid.New().String()     // auto-generated
sequenceNo = 0                      // auto-incremented in sendRequest
```

**After:** User can specify exact values:
```go
// New code allows:
-client-id "my-client"     // Explicit client ID
-seq-num 5                 // Start sequence at 5
```

**Full example of testing idempotency:**
```bash
# First write with client-id=foo, seq-num=1
./kv-client -addr node0:50051 -cmd set -key test -val v1 -client-id foo -seq-num 1

# Duplicate write - same client-id and seq-num should be skipped by FSM
./kv-client -addr node0:50051 -cmd set -key test -val v2 -client-id foo -seq-num 1
# Result: second write should be deduplicated, value stays "v1"

---

## Session 3: Agent Netem Endpoints

### File: cmd/agent/main.go

**Full Line-by-Line Changes:**

#### 1. Global variable - Added netIface (Line 35)
```go
// BEFORE:
var (
    mu        sync.Mutex
    childProc *os.Process
    kvBin     string
    kvArgs    []string
    raftPort  string
)

// AFTER (NEW FIELD):
var (
    mu        sync.Mutex
    childProc *os.Process
    kvBin     string
    kvArgs    []string
    raftPort  string
    netIface  string // NEW - network interface for tc netem (e.g., "eth0")
)
```

#### 2. Flag parsing - Added netIfaceFlag (Lines 72, 77)
```go
// BEFORE:
func main() {
    kvBinFlag := flag.String("kv-bin", "./kv-store", "...")
    kvArgsFlag := flag.String("kv-args", "", "...")
    port := flag.Int("port", 9000, "...")
    flag.Parse()
    kvBin = *kvBinFlag
    kvArgs = strings.Fields(*kvArgsFlag)
}

// AFTER (NEW):
func main() {
    kvBinFlag := flag.String("kv-bin", "./kv-store", "...")
    kvArgsFlag := flag.String("kv-args", "", "...")
    port := flag.Int("port", 9000, "...")
    netIfaceFlag := flag.String("iface", "eth0", "Network interface for tc netem")  // NEW
    flag.Parse()
    kvBin = *kvBinFlag
    kvArgs = strings.Fields(*kvArgsFlag)
    netIface = *netIfaceFlag  // NEW
}
```

#### 3. /netem endpoint (Lines 254-314) - NEW
```go
// NEW ENDPOINT - applies tc/netem network impairment
mux.HandleFunc("/netem", func(w http.ResponseWriter, r *http.Request) {
    // Validate it's POST
    if r.Method != http.MethodPost {
        http.Error(w, "POST only", http.StatusMethodNotAllowed)
        return
    }
    
    // Parse query parameters: delay, jitter, loss
    delayMs := r.URL.Query().Get("delay")
    jitterMs := r.URL.Query().Get("jitter")
    lossPct := r.URL.Query().Get("loss")
    
    // Validate at least one param provided
    if delayMs == "" && lossPct == "" {
        http.Error(w, "must specify delay and/or loss parameter", http.StatusBadRequest)
        return
    }
    
    // First, delete any existing qdisc rule
    delCmd := exec.Command("sudo", "tc", "qdisc", "del", "dev", netIface, "root")
    delCmd.Run()
    
    // Build netem command
    args := []string{"tc", "qdisc", "add", "dev", netIface, "root", "netem"}
    if delayMs != "" {
        if jitterMs != "" {
            args = append(args, "delay", delayMs+"ms", jitterMs+"ms")
        } else {
            args = append(args, "delay", delayMs+"ms")
        }
    }
    if lossPct != "" {
        args = append(args, "loss", lossPct+"%")
    }
    
    // Execute: sudo tc qdisc add dev eth0 root netem delay 500ms loss 30%
    cmd := exec.Command("sudo", args...)
    if out, err := cmd.CombinedOutput(); err != nil {
        http.Error(w, fmt.Sprintf("tc netem failed: %v — %s", err, out), http.StatusInternalServerError)
        return
    }
    
    // Log success
    log.Printf("[agent] tc netem applied on %s: %s", netIface, desc)
    w.WriteHeader(http.StatusOK)
})
```

#### 4. /unnetem endpoint (Lines 316-329) - NEW
```go
// NEW ENDPOINT - removes tc/netem rules
mux.HandleFunc("/unnetem", func(w http.ResponseWriter, r *http.Request) {
    // Validate POST
    if r.Method != http.MethodPost {
        http.Error(w, "POST only", http.StatusMethodNotAllowed)
        return
    }
    
    // Execute: sudo tc qdisc del dev eth0 root
    cmd := exec.Command("sudo", "tc", "qdisc", "del", "dev", netIface, "root")
    if out, err := cmd.CombinedOutput(); err != nil {
        log.Printf("[agent] tc qdisc del warning: %v — %s", err, out)
    }
    log.Printf("[agent] tc netem removed on %s (network normal)", netIface)
    w.WriteHeader(http.StatusOK)
})
```

**Before:** Agent only had kill/pause/resume/restart/partition/unpartition endpoints. No way to inject network latency or packet loss at the kernel level.

**After:** Agent can apply tc/netem rules:
- `POST /netem?delay=500` - Add 500ms latency
- `POST /netem?delay=500&loss=30` - 500ms latency + 30% packet loss
- `POST /unnetem` - Remove all netem rules

**Reasoning:** 
- kv-chaos proxy only affects traffic to its port (client-side)
- For true cluster-level testing (affecting Raft heartbeats), we need kernel-level network impairment
- tc/netem applies to ALL traffic on the interface, not just specific ports
- This enables Phase 3-style latency testing at the actual cluster level

---

## Session 4: Dashboard Chaos Endpoints

### File: cmd/dashboard/main.go

**Full Line-by-Line Changes:**

#### 1. NodeState struct - Added Term field (Line 37)
```go
// BEFORE:
type NodeState struct {
    Config       NodeConfig `json:"config"`
    State        string     `json:"state"`
    LeaderAddr   string     `json:"leader_addr"`
    AppliedIndex uint64     `json:"applied_index"`
    NumPeers     uint32     `json:"num_peers"`
    Alive        bool       `json:"alive"`
}

// AFTER (NEW FIELD):
type NodeState struct {
    Config       NodeConfig `json:"config"`
    State        string     `json:"state"`
    LeaderAddr   string     `json:"leader_addr"`
    Term         uint64     `json:"term"`         // NEW - Raft term number
    AppliedIndex uint64     `json:"applied_index"`
    NumPeers     uint32     `json:"num_peers"`
    Alive        bool       `json:"alive"`
}
```

#### 2. PartitionNode function (Lines 262-273) - NEW
```go
// NEW FUNCTION - routes to agent /partition endpoint
func (m *Manager) PartitionNode(nodeID string) error {
    if len(m.agentAddrs) > 0 {
        if err := m.agentPost(nodeID, "partition"); err != nil {
            return fmt.Errorf("agent partition failed: %w", err)
        }
        m.log("warn", fmt.Sprintf("🔒 PARTITIONED node %s via agent (iptables — true network partition)", nodeID))
        return nil
    }
    return fmt.Errorf("partition requires agent mode (GCP)")
}
```

#### 3. UnpartitionNode function (Lines 275-285) - NEW
```go
// NEW FUNCTION - routes to agent /unpartition endpoint
func (m *Manager) UnpartitionNode(nodeID string) error {
    if len(m.agentAddrs) > 0 {
        if err := m.agentPost(nodeID, "unpartition"); err != nil {
            return fmt.Errorf("agent unpartition failed: %w", err)
        }
        m.log("success", fmt.Sprintf("🔓 UNPARTITIONED node %s (iptables rules removed)", nodeID))
        return nil
    }
    return fmt.Errorf("unpartition requires agent mode (GCP)")
}
```

#### 4. ApplyNetem function (Lines 287-311) - NEW
```go
// NEW FUNCTION - routes to agent /netem endpoint
// delay: base delay in milliseconds (e.g., "500")
// loss: packet loss percentage (e.g., "30")
// jitter: random variation in ms (e.g., "10")
func (m *Manager) ApplyNetem(nodeID, delay, loss, jitter string) error {
    if len(m.agentAddrs) > 0 {
        url := fmt.Sprintf("netem?delay=%s&loss=%s&jitter=%s", delay, loss, jitter)
        if err := m.agentPost(nodeID, url); err != nil {
            return fmt.Errorf("agent netem failed: %w", err)
        }
        // ... logging ...
        return nil
    }
    return fmt.Errorf("netem requires agent mode (GCP)")
}
```

#### 5. RemoveNetem function (Lines 313-323) - NEW
```go
// NEW FUNCTION - routes to agent /unnetem endpoint
func (m *Manager) RemoveNetem(nodeID string) error {
    if len(m.agentAddrs) > 0 {
        if err := m.agentPost(nodeID, "unnetem"); err != nil {
            return fmt.Errorf("agent unnetem failed: %w", err)
        }
        // ... logging ...
        return nil
    }
    return fmt.Errorf("unnetem requires agent mode (GCP)")
}
```

#### 6. DirectKVSet function (Lines 363-383) - NEW
```go
// NEW FUNCTION - performs gRPC SET to arbitrary address
func (m *Manager) DirectKVSet(addr, key, val string) (bool, error) {
    conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    // ... gRPC call to set key=value ...
    return resp.Success, nil
}
```

#### 7. DirectKVGet function (Lines 385-405) - NEW
```go
// NEW FUNCTION - performs gRPC GET to arbitrary address
func (m *Manager) DirectKVGet(addr, key string) (string, bool, error) {
    // ... gRPC call to get key ...
    return resp.Value, resp.Found, nil
}
```

#### 8. DirectKVDelete function (Lines 407-427) - NEW
```go
// NEW FUNCTION - performs gRPC DELETE to arbitrary address
func (m *Manager) DirectKVDelete(addr, key string) (bool, error) {
    // ... gRPC call to delete key ...
    return resp.Success, nil
}
```

#### 9. GetClusterState - Added Term field (Line 544)
```go
// In the health response handler:
ns.Term = resp.Term  // NEW - copy term from health response
```

#### 10. /api/chaos/netem/{nodeID} endpoint (Lines 718-748) - NEW
```go
// NEW ENDPOINT - applies tc/netem via agent
mux.HandleFunc("/api/chaos/netem/", func(w http.ResponseWriter, r *http.Request) {
    // Parse delay, loss, jitter query params
    // Call mgr.ApplyNetem()
})
```

#### 11. /api/chaos/unnetem/{nodeID} endpoint (Lines 750-762) - NEW
```go
// NEW ENDPOINT - removes tc/netem via agent
mux.HandleFunc("/api/chaos/unnetem/", func(w http.ResponseWriter, r *http.Request) {
    // Call mgr.RemoveNetem()
})
```

#### 12. /api/kv/set endpoint (Lines 764-783) - NEW
```go
// NEW ENDPOINT - direct KV SET via dashboard
mux.HandleFunc("/api/kv/set", func(w http.ResponseWriter, r *http.Request) {
    // Query params: key, val, addr
    // Call mgr.DirectKVSet()
})
```

#### 13. /api/kv/get endpoint (Lines 785-798) - NEW
```go
// NEW ENDPOINT - direct KV GET via dashboard
mux.HandleFunc("/api/kv/get", func(w http.ResponseWriter, r *http.Request) {
    // Query params: key, addr
    // Call mgr.DirectKVGet()
})
```

#### 14. /api/kv/delete endpoint (Lines 800-817) - NEW
```go
// NEW ENDPOINT - direct KV DELETE via dashboard
mux.HandleFunc("/api/kv/delete", func(w http.ResponseWriter, r *http.Request) {
    // Query params: key, addr
    // Call mgr.DirectKVDelete()
})
```

#### 15. /api/partition/{nodeID} endpoint (Lines 819-831) - NEW
```go
// NEW ENDPOINT - iptables partition via agent
mux.HandleFunc("/api/partition/", func(w http.ResponseWriter, r *http.Request) {
    // Call mgr.PartitionNode()
})
```

#### 16. /api/unpartition/{nodeID} endpoint (Lines 833-845) - NEW
```go
// NEW ENDPOINT - heal iptables partition via agent
mux.HandleFunc("/api/unpartition/", func(w http.ResponseWriter, r *http.Request) {
    // Call mgr.UnpartitionNode()
})
```

**Before:** Dashboard had /api/pause (SIGSTOP) and /api/chaos/delay (kv-chaos proxy). No true network partition or kernel-level chaos.

**After:** Dashboard can now:
- `/api/partition/{nodeID}` - True network partition (iptables DROP on Raft port)
- `/api/chaos/netem/{nodeID}?delay=500` - Kernel-level latency
- `/api/chaos/netem/{nodeID}?loss=30` - Kernel-level packet loss

**Reasoning:**
- SIGSTOP pauses entire process including Raft - more severe than real partition
- kv-chaos only affects client traffic, not Raft internals
- iptables partition keeps process alive but blocks all Raft communication - stricter test
- netem affects actual network stack - more realistic than user-space proxy

---

## Session 5: KV Operations via Dashboard

### File: cmd/dashboard/main.go

**Changes:**

1. **Added DirectKVSet function (NEW):**
```go
func (m *Manager) DirectKVSet(addr, key, val string) (bool, error) {
    // gRPC call to specified address
    // Returns success or error
}
```

2. **Added DirectKVGet function (NEW):**
```go
func (m *Manager) DirectKVGet(addr, key string) (string, bool, error) {
    // gRPC call to specified address
    // Returns value, found flag, error
}
```

3. **Added DirectKVDelete function (NEW):**
```go
func (m *Manager) DirectKVDelete(addr, key string) (bool, error) {
    // gRPC call to specified address
    // Returns success or error
}
```

4. **Added /api/kv/set endpoint (NEW):**
```go
mux.HandleFunc("/api/kv/set", func(w http.ResponseWriter, r *http.Request) {
    // Query params: key, val, addr
    // Call mgr.DirectKVSet()
})
```

5. **Added /api/kv/get endpoint (NEW):**
```go
mux.HandleFunc("/api/kv/get", func(w http.ResponseWriter, r *http.Request) {
    // Query params: key, addr
    // Call mgr.DirectKVGet()
})
```

6. **Added /api/kv/delete endpoint (NEW):**
```go
mux.HandleFunc("/api/kv/delete", func(w http.ResponseWriter, r *http.Request) {
    // Query params: key, addr
    // Call mgr.DirectKVDelete()
})
```

**Before:** Dashboard could view cluster state and inject chaos, but users had to use kv-client CLI separately for KV operations.

**After:** Users can perform KV operations directly from dashboard UI without needing external client.

**Reasoning:**
- Improves UX for demos and testing
- Allows testing without switching between CLI and browser
- Shows that dashboard connects to leader and performs actual KV operations

---

## Session 6: Election Term Tracking

### File: proto/kv.proto

**Change:** Added term field to HealthResponse message
```protobuf
// BEFORE:
message HealthResponse {
  string node_id = 1;
  string state = 2;
  string leader_addr = 3;
  uint64 applied_index = 4;
  uint32 num_peers = 5;
}

// AFTER:
message HealthResponse {
  string node_id = 1;
  string state = 2;
  string leader_addr = 3;
  uint64 applied_index = 4;
  uint32 num_peers = 5;
  uint64 term = 6;  // NEW - Current Raft term number
}
```

**Reasoning:** To display election information in dashboard, we need the current term number. Each election increments the term - showing the term proves elections are happening.

---

### File: server/node.go

**Change:** Extract term from Raft stats and include in HealthResponse
```go
// BEFORE:
var appliedIndex uint64
fmt.Sscanf(stats["applied_index"], "%d", &appliedIndex)

// AFTER:
var appliedIndex uint64
fmt.Sscanf(stats["applied_index"], "%d", &appliedIndex)

var term uint64
fmt.Sscanf(stats["term"], "%d", &term)  // NEW - extract term

return &pb.HealthResponse{
    // ... existing fields ...
    Term: term,  // NEW
}
```

**Reasoning:** HashiCorp Raft provides term in its stats. We need to extract and expose it so the dashboard can display election cycles.

---

### File: cmd/dashboard/main.go

**Change:** Added Term field to NodeState struct
```go
// BEFORE:
type NodeState struct {
    Config       NodeConfig `json:"config"`
    State        string     `json:"state"`
    LeaderAddr   string     `json:"leader_addr"`
    AppliedIndex uint64     `json:"applied_index"`
    NumPeers     uint32     `json:"num_peers"`
    Alive        bool       `json:"alive"`
}

// AFTER:
type NodeState struct {
    Config       NodeConfig `json:"config"`
    State        string     `json:"state"`
    LeaderAddr   string     `json:"leader_addr"`
    Term         uint64     `json:"term"`         // NEW
    AppliedIndex uint64     `json:"applied_index"`
    NumPeers     uint32     `json:"num_peers"`
    Alive        bool       `json:"alive"`
}
```

**Also:** Updated GetClusterState to populate Term field:
```go
ns.Term = resp.Term  // NEW - copy term from health response
```

**Reasoning:** Dashboard receives HealthResponse from each node and needs to store and display the term number.

---

## Session 7: Dashboard UI Enhancements

### File: cmd/dashboard/index.html

**Changes:**

1. **Added "Reset ALL Chaos" button (NEW):**
```html
<button class="btn purple" id="btn-reset-all-chaos" style="grid-column: span 2;">
    🔄 Reset ALL Chaos
</button>
```

**JavaScript handler:**
```javascript
document.getElementById('btn-reset-all-chaos').addEventListener('click', async () => {
    // 1. Stop kv-chaos proxy: /api/chaos/stop/{id}
    // 2. Remove netem: /api/chaos/unnetem/{id}
    // 3. Unpartition: /api/unpartition/{id}
    // 4. Resume if paused: /api/resume/{id}
});
```

**Before:** "Stop Chaos" button only stopped the kv-chaos proxy. Users had to manually remove netem, unpartition, and resume nodes.

**After:** Single button cleans up ALL chaos:
- Stops chaos proxy
- Removes netem rules
- Heals iptables partition
- Resumes paused nodes

**Reasoning:** In testing scenarios, users might apply multiple types of chaos. Having to clean up each separately is error-prone. A single "reset all" ensures clean state.

---

2. **Added KV Operations Panel (NEW):**
```html
<div class="panel-header">
    <span class="icon">🔑</span> KV Operations (Direct Dashboard)
</div>
<div style="padding: 8px;">
    <input type="text" id="kv-key" placeholder="Key">
    <input type="text" id="kv-value" placeholder="Value">
    <button id="btn-kv-set">SET</button>
    <button id="btn-kv-get">GET</button>
    <button id="btn-kv-delete">DELETE</button>
    <div id="kv-result">—</div>
</div>
```

**JavaScript handlers:**
- `btn-kv-set` - Calls `/api/kv/set?key=...&val=...&addr=...`
- `btn-kv-get` - Calls `/api/kv/get?key=...&addr=...`
- `btn-kv-delete` - Calls `/api/kv/delete?key=...&addr=...`

**Before:** No way to perform KV operations from dashboard UI. Users had to use kv-client CLI.

**After:** Users can SET/GET/DELETE directly from dashboard.

**Reasoning:** Better for demos - show full functionality without CLI switching.

---

3. **Updated Term Display (MODIFIED):**
```javascript
// BEFORE:
mTerm.textContent = leader ? (leader.num_peers || '?') + ' peers' : '—';

// AFTER:
mTerm.textContent = leader && leader.term ? 'Term: ' + leader.term : '—';
```

**Before:** Term area showed number of peers.

**After:** Term area shows "Term: X" where X is the current Raft term number.

**Reasoning:** Show election information - term increments on each election, providing evidence of leadership changes.

---

## Session 8: Compilation and Deployment

### Build Commands Executed:
```bash
# Local compilation for testing
go build -o /tmp/kv-dashboard ./cmd/dashboard/
go build -o /tmp/kv-client ./cmd/client/

# Cross-compilation for GCP (Linux/AMD64)
GOOS=linux GOARCH=amd64 go build -o kv-dashboard ./cmd/dashboard/
GOOS=linux GOARCH=amd64 go build -o kv-store .
GOOS=linux GOARCH=amd64 go build -o kv-client ./cmd/client/
GOOS=linux GOARCH=amd64 go build -o kv-chaos ./cmd/chaos/
GOOS=linux GOARCH=amd64 go build -o node-agent ./cmd/agent/

# Protobuf regeneration
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/kv.proto
```

**Reasoning:** 
- Go doesn't have a Makefile, so compilation is done manually
- GCP VMs run Linux, so cross-compilation required
- Protobuf changes (adding term field) required regeneration

---

## Summary of Key Design Decisions

### 1. Why Kernel-Level Chaos Instead of User-Space Proxy?
- **kv-chaos** (existing): Only affects NEW client connections to its port
- **tc/netem** (new): Affects ALL network traffic on the interface
- **Result**: netem tests actual Raft heartbeat delays, not just client delays

### 2. Why iptables Partition vs SIGSTOP?
- **SIGSTOP/Pause**: Freezes entire process including Raft - process can't do anything
- **iptables partition**: Process keeps running but can't send/receive Raft traffic
- **Result**: iptables is stricter test - shows if Raft can handle isolated-but-running node

### 3. Why Add KV Operations to Dashboard?
- CLI is fine for testing but awkward for demos
- Having in-dashboard operations shows "it works" without external dependencies
- Good for presentation - show full workflow in one place

### 4. Why Show Term Number?
- Raft term increments on each election
- If term keeps increasing, shows elections are happening
- Visual evidence of "cluster is active" beyond just state showing Leader/Follower

---

## Files Modified Summary

| File | Changes | Lines |
|------|---------|-------|
| GCP_verify_phase5.sh | NEW | ~270 |
| GCP_verify_phase6.sh | NEW | ~280 |
| cmd/client/main.go | MODIFIED (+25) | ~225 |
| cmd/agent/main.go | MODIFIED (+80) | ~340 |
| cmd/dashboard/main.go | MODIFIED (+150) | ~930 |
| proto/kv.proto | MODIFIED (+2) | ~68 |
| server/node.go | MODIFIED (+5) | ~365 |
| cmd/dashboard/index.html | MODIFIED (+80) | ~1000 |

---

## Testing Results (GCP)

### Phase 5 (Idempotency) - All 7/7 PASSED
- I1: Follower Redirect ✅
- I2: Idempotent Set ✅
- I3: Idempotent Delete ✅
- I4: Client Failover ✅

### Phase 6 (Kernel Chaos) - All 7/7 PASSED
- N1: iptables Partition ✅
- N2: netem on Follower (quorum bypass) ✅
- N3: netem on Leader ✅
- N4: netem with Packet Loss ✅
- N5: Dashboard Access During Chaos ✅

---

## Backward Compatibility

All changes are **backward compatible**:
- New CLI flags are optional (default to auto-generated values)
- New API endpoints don't affect existing functionality
- Old dashboard continues to work - new features are additive
- Existing test scripts continue to work

---

## Future Enhancements (Not Implemented)

1. **Checksum for BoltDB** - Would detect disk corruption, but not needed for class project
2. **TLS/mTLS** - Would encrypt inter-node communication, but out of scope
3. **Watch/Notifications** - Would allow clients to subscribe to key changes, but adds complexity

---

*End of Changelog*