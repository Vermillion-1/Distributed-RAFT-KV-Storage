package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/ankush/raft-kv/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:embed index.html
var indexHTML embed.FS

// NodeConfig holds static config for each node in the cluster
type NodeConfig struct {
	ID       string `json:"id"`
	RaftAddr string `json:"raft_addr"`
	GRPCAddr string `json:"grpc_addr"`
	DataDir  string `json:"data_dir"`
}

// NodeState holds live state for a node
type NodeState struct {
	Config       NodeConfig `json:"config"`
	State        string     `json:"state"` // Leader, Follower, Candidate, Dead
	LeaderAddr   string     `json:"leader_addr"`
	Term         uint64     `json:"term"` // Raft term number
	AppliedIndex uint64     `json:"applied_index"`
	NumPeers     uint32     `json:"num_peers"`
	Alive        bool       `json:"alive"`
}

// ClusterState is the full cluster snapshot returned to the frontend
type ClusterState struct {
	Nodes  []NodeState `json:"nodes"`
	Events []Event     `json:"events"`
}

// Event is a single timestamped event in the chaos log
type Event struct {
	Time    string `json:"time"`
	Level   string `json:"level"` // "info", "warn", "error", "success"
	Message string `json:"message"`
}

// ChaosProxy tracks a running chaos proxy process
type ChaosProxy struct {
	NodeID     string
	Cmd        *exec.Cmd
	ListenAddr string
}

// Manager orchestrates the cluster and chaos experiments
type Manager struct {
	mu         sync.RWMutex
	nodes      map[string]*nodeProcess
	proxies    map[string]*ChaosProxy
	configs    []NodeConfig
	events     []Event
	binDir     string
	freshStart bool              // tracks whether this is the initial startup (wipe data) vs restart
	agentAddrs map[string]string // B2 (GCP_TODO.md): nodeID → "ip:9000"; non-empty = GCP mode
}

type nodeProcess struct {
	config  NodeConfig
	cmd     *exec.Cmd
	logFile *os.File
}

func NewManager(binDir string, numNodes int) *Manager {
	configs := []NodeConfig{}
	for i := 0; i < numNodes; i++ {
		configs = append(configs, NodeConfig{
			ID:       fmt.Sprintf("node%d", i),
			RaftAddr: fmt.Sprintf("127.0.0.1:%d", 12000+i),
			GRPCAddr: fmt.Sprintf("127.0.0.1:%d", 50051+i),
			DataDir:  fmt.Sprintf("/tmp/raft-kv/node%d", i),
		})
	}
	return &Manager{
		nodes:   make(map[string]*nodeProcess),
		proxies: make(map[string]*ChaosProxy),
		configs: configs,
		binDir:  binDir,
	}
}

func (m *Manager) log(level, msg string) {
	e := Event{
		Time:    time.Now().Format("15:04:05.000"),
		Level:   level,
		Message: msg,
	}
	m.mu.Lock()
	m.events = append(m.events, e)
	// Keep last 500 events
	if len(m.events) > 500 {
		m.events = m.events[len(m.events)-500:]
	}
	m.mu.Unlock()
	log.Printf("[%s] %s", level, msg)
}

// hasBoltData returns true if any node data directory already contains a BoltDB file,
// meaning this is a restart rather than a first-ever boot.
func hasBoltData(configs []NodeConfig) bool {
	for _, cfg := range configs {
		if _, err := os.Stat(cfg.DataDir + "/raft-log.bolt"); err == nil {
			return true
		}
	}
	return false
}

// StartAll starts the cluster. It wipes data directories only when no existing
// BoltDB data is found — preserving durability across dashboard restarts.
// Previously this used an in-memory freshStart flag which would wipe data if
// the dashboard process was restarted mid-demo (Bug 3 fix).
func (m *Manager) StartAll() {
	if len(m.agentAddrs) > 0 {
		m.log("info", "GCP mode: deferring startup to node-agents (no local spawn)")
		return
	}

	if hasBoltData(m.configs) {
		// Existing data found — preserve it (durability test or dashboard restart)
		os.MkdirAll("/tmp/raft-kv/", 0755)
		m.freshStart = true
		m.log("info", "Restart: existing BoltDB data found, preserving data directories")
	} else {
		// No existing data — clean slate startup
		os.RemoveAll("/tmp/raft-kv/")
		os.MkdirAll("/tmp/raft-kv/", 0755)
		m.freshStart = true
		m.log("info", "Fresh start: no existing data found, created clean directories")
	}

	for i, cfg := range m.configs {
		time.Sleep(200 * time.Millisecond)
		joining := ""
		if i > 0 {
			joining = m.configs[0].GRPCAddr
		}
		m.startNode(cfg, joining)
		if i == 0 {
			time.Sleep(2 * time.Second) // wait for bootstrap
		}
	}
	m.log("success", fmt.Sprintf("Cluster started: %d nodes", len(m.configs)))
}

func (m *Manager) startNode(cfg NodeConfig, joinAddr string) {
	os.MkdirAll(cfg.DataDir, 0755)
	logPath := fmt.Sprintf("/tmp/raft-kv/%s.log", cfg.ID)
	logFile, _ := os.Create(logPath)

	args := []string{
		"-id=" + cfg.ID,
		"-raft=" + cfg.RaftAddr,
		"-grpc=" + cfg.GRPCAddr,
		"-data=" + cfg.DataDir,
	}
	if joinAddr != "" {
		args = append(args, "-join="+joinAddr)
	}

	cmd := exec.Command(m.binDir+"/kv-store", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		m.log("error", fmt.Sprintf("Failed to start %s: %v", cfg.ID, err))
		return
	}

	m.mu.Lock()
	m.nodes[cfg.ID] = &nodeProcess{config: cfg, cmd: cmd, logFile: logFile}
	m.mu.Unlock()

	m.log("info", fmt.Sprintf("Started %s (gRPC: %s, Raft: %s)", cfg.ID, cfg.GRPCAddr, cfg.RaftAddr))
}

func (m *Manager) KillNode(nodeID string) error {
	// B2 (GCP_TODO.md): GCP mode — route to node-agent instead of local PID
	if len(m.agentAddrs) > 0 {
		if err := m.agentPost(nodeID, "kill"); err != nil {
			return fmt.Errorf("agent kill failed: %w", err)
		}
		m.log("error", fmt.Sprintf("💀 KILLED node %s via agent (GCP mode)", nodeID))
		return nil
	}
	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}
	if np.cmd.Process == nil {
		return fmt.Errorf("node %s not running", nodeID)
	}
	if err := np.cmd.Process.Kill(); err != nil {
		return err
	}
	m.log("error", fmt.Sprintf("💀 KILLED node %s (Crash/Fail-stop failure)", nodeID))
	return nil
}

// PauseNode sends SIGSTOP — freezes the process in place, simulating a
// network partition. Other nodes timeout waiting for heartbeats and elect
// a new leader. The node retains all state and recovers fully on Resume.
func (m *Manager) PauseNode(nodeID string) error {
	// B2 (GCP_TODO.md): GCP mode — route to node-agent instead of local PID
	if len(m.agentAddrs) > 0 {
		if err := m.agentPost(nodeID, "pause"); err != nil {
			return fmt.Errorf("agent pause failed: %w", err)
		}
		m.log("warn", fmt.Sprintf("⏸️  PAUSED node %s via agent (GCP mode — SIGSTOP)", nodeID))
		return nil
	}
	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}
	if np.cmd.Process == nil {
		return fmt.Errorf("node %s not running", nodeID)
	}
	if err := np.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		return err
	}
	m.log("warn", fmt.Sprintf("⏸️  PAUSED node %s (SIGSTOP — simulating network partition)", nodeID))
	return nil
}

// ResumeNode sends SIGCONT — unfreezes the process, healing the partition.
// The node rejoins the cluster and catches up via Raft log replication.
func (m *Manager) ResumeNode(nodeID string) error {
	// B2 (GCP_TODO.md): GCP mode — route to node-agent instead of local PID
	if len(m.agentAddrs) > 0 {
		if err := m.agentPost(nodeID, "resume"); err != nil {
			return fmt.Errorf("agent resume failed: %w", err)
		}
		m.log("success", fmt.Sprintf("▶️  RESUMED node %s via agent (GCP mode — SIGCONT)", nodeID))
		return nil
	}
	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}
	if np.cmd.Process == nil {
		return fmt.Errorf("node %s not running", nodeID)
	}
	if err := np.cmd.Process.Signal(syscall.SIGCONT); err != nil {
		return err
	}
	m.log("success", fmt.Sprintf("▶️  RESUMED node %s (SIGCONT — partition healed)", nodeID))
	return nil
}

// PartitionNode uses iptables to drop ALL Raft traffic to/from the node.
// Unlike SIGSTOP (Pause), the process keeps running but cannot communicate.
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

// UnpartitionNode removes iptables rules to heal the partition.
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

// ApplyNetem applies network impairment via tc/netem (latency + packet loss).
// delay: base delay in milliseconds (e.g., "500")
// loss: packet loss percentage (e.g., "30")
// jitter: random variation in ms (e.g., "10")
func (m *Manager) ApplyNetem(nodeID, delay, loss, jitter string) error {
	if len(m.agentAddrs) > 0 {
		url := fmt.Sprintf("netem?delay=%s&loss=%s&jitter=%s", delay, loss, jitter)
		if err := m.agentPost(nodeID, url); err != nil {
			return fmt.Errorf("agent netem failed: %w", err)
		}
		desc := ""
		if delay != "" {
			desc += "delay=" + delay + "ms"
		}
		if loss != "" {
			if desc != "" {
				desc += ", "
			}
			desc += "loss=" + loss + "%"
		}
		m.log("warn", fmt.Sprintf("🌩️  NETEM applied on %s: %s", nodeID, desc))
		return nil
	}
	return fmt.Errorf("netem requires agent mode (GCP)")
}

// RemoveNetem removes tc/netem rules to restore normal networking.
func (m *Manager) RemoveNetem(nodeID string) error {
	if len(m.agentAddrs) > 0 {
		if err := m.agentPost(nodeID, "unnetem"); err != nil {
			return fmt.Errorf("agent unnetem failed: %w", err)
		}
		m.log("success", fmt.Sprintf("✅ NETEM removed on %s (network normal)", nodeID))
		return nil
	}
	return fmt.Errorf("unnetem requires agent mode (GCP)")
}

func (m *Manager) RestartNode(nodeID string) error {
	// B2 (GCP_TODO.md): GCP mode — route to node-agent instead of local PID
	if len(m.agentAddrs) > 0 {
		if err := m.agentPost(nodeID, "restart"); err != nil {
			return fmt.Errorf("agent restart failed: %w", err)
		}
		m.log("success", fmt.Sprintf("♻️  RESTARTED node %s via agent (GCP mode)", nodeID))
		return nil
	}
	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Wait for the old process to fully exit before starting a new one
	// to avoid port conflicts (the old process may still hold the port).
	if np.cmd != nil && np.cmd.Process != nil {
		np.cmd.Wait() // blocks until process exits; safe even if already dead
	}

	// Find a live node to join. Check with signal(0) — a non-nil Process can still
	// be a dead/reaped process, so we verify it's actually running before using it.
	joinAddr := ""
	m.mu.RLock()
	for id, n := range m.nodes {
		if id != nodeID && n.cmd.Process != nil {
			if err := n.cmd.Process.Signal(syscall.Signal(0)); err == nil {
				joinAddr = n.config.GRPCAddr
				break
			}
		}
	}
	m.mu.RUnlock()

	m.startNode(np.config, joinAddr)
	m.log("success", fmt.Sprintf("Restarted %s — recovering via Raft log replay", nodeID))
	return nil
}

// kvGRPCConn opens an insecure gRPC connection to addr, executes fn, and closes it.
func kvGRPCConn(addr string, fn func(pb.KVStoreClient, context.Context) error) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return fn(pb.NewKVStoreClient(conn), ctx)
}

// DirectKVSet performs a SET operation via gRPC. If the target is a follower it
// automatically follows the leader redirect once so the dashboard KV panel works
// even if a leadership change happens between the /api/cluster poll and the write.
func (m *Manager) DirectKVSet(addr, key, val string) (bool, error) {
	var result bool
	err := kvGRPCConn(addr, func(c pb.KVStoreClient, ctx context.Context) error {
		resp, err := c.Set(ctx, &pb.SetRequest{Key: key, Value: val})
		if err != nil {
			return fmt.Errorf("SET failed: %w", err)
		}
		if !resp.Success && resp.LeaderAddr != "" {
			// Follow redirect to actual leader
			return kvGRPCConn(resp.LeaderAddr, func(c2 pb.KVStoreClient, ctx2 context.Context) error {
				resp2, err2 := c2.Set(ctx2, &pb.SetRequest{Key: key, Value: val})
				if err2 != nil {
					return fmt.Errorf("SET (redirected) failed: %w", err2)
				}
				result = resp2.Success
				return nil
			})
		}
		result = resp.Success
		return nil
	})
	return result, err
}

// DirectKVGet performs a GET operation via gRPC, following a leader redirect if needed.
func (m *Manager) DirectKVGet(addr, key string) (string, bool, error) {
	var value string
	var found bool
	err := kvGRPCConn(addr, func(c pb.KVStoreClient, ctx context.Context) error {
		resp, err := c.Get(ctx, &pb.GetRequest{Key: key})
		if err != nil {
			return fmt.Errorf("GET failed: %w", err)
		}
		if resp.LeaderAddr != "" {
			// Follow redirect to actual leader
			return kvGRPCConn(resp.LeaderAddr, func(c2 pb.KVStoreClient, ctx2 context.Context) error {
				resp2, err2 := c2.Get(ctx2, &pb.GetRequest{Key: key})
				if err2 != nil {
					return fmt.Errorf("GET (redirected) failed: %w", err2)
				}
				value = resp2.Value
				found = resp2.Found
				return nil
			})
		}
		value = resp.Value
		found = resp.Found
		return nil
	})
	return value, found, err
}

// DirectKVDelete performs a DELETE operation via gRPC, following a leader redirect if needed.
func (m *Manager) DirectKVDelete(addr, key string) (bool, error) {
	var result bool
	err := kvGRPCConn(addr, func(c pb.KVStoreClient, ctx context.Context) error {
		resp, err := c.Delete(ctx, &pb.DeleteRequest{Key: key})
		if err != nil {
			return fmt.Errorf("DELETE failed: %w", err)
		}
		if !resp.Success && resp.LeaderAddr != "" {
			// Follow redirect to actual leader
			return kvGRPCConn(resp.LeaderAddr, func(c2 pb.KVStoreClient, ctx2 context.Context) error {
				resp2, err2 := c2.Delete(ctx2, &pb.DeleteRequest{Key: key})
				if err2 != nil {
					return fmt.Errorf("DELETE (redirected) failed: %w", err2)
				}
				result = resp2.Success
				return nil
			})
		}
		result = resp.Success
		return nil
	})
	return result, err
}

func (m *Manager) StartChaosProxy(nodeID string, dropRate float64, delayMs int) error {
	// kv-chaos is a local TCP proxy — not applicable in GCP mode where nodes are remote.
	// Use netem (/api/chaos/netem) or partition (/api/partition) for GCP chaos testing.
	if len(m.agentAddrs) > 0 {
		return fmt.Errorf("chaos proxy (drop/delay) is not available in GCP mode — use 'Netem' or 'Partition' buttons instead")
	}

	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Kill existing proxy for this node if any
	m.StopChaosProxy(nodeID)

	// Assign a stable proxy port based on the node's position in configs,
	// not the current map size. This avoids port collisions when proxies
	// are stopped and restarted, and makes the port predictable for tests.
	nodeIndex := 0
	for i, cfg := range m.configs {
		if cfg.ID == nodeID {
			nodeIndex = i
			break
		}
	}
	listenPort := 22000 + nodeIndex
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	args := []string{
		"-listen=" + listenAddr,
		"-target=" + np.config.GRPCAddr,
		fmt.Sprintf("-drop=%f", dropRate),
		fmt.Sprintf("-delay=%d", delayMs),
	}

	cmd := exec.Command(m.binDir+"/kv-chaos", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	m.mu.Lock()
	m.proxies[nodeID] = &ChaosProxy{NodeID: nodeID, Cmd: cmd, ListenAddr: listenAddr}
	m.mu.Unlock()

	desc := ""
	if dropRate > 0 {
		desc += fmt.Sprintf("drop=%.0f%%", dropRate*100)
	}
	if delayMs > 0 {
		desc += fmt.Sprintf(" delay=%dms", delayMs)
	}
	m.log("warn", fmt.Sprintf("🌩️  Chaos proxy started for %s (%s) → proxy at %s", nodeID, desc, listenAddr))
	return nil
}

func (m *Manager) StopChaosProxy(nodeID string) {
	m.mu.Lock()
	p, ok := m.proxies[nodeID]
	if ok {
		if p.Cmd.Process != nil {
			p.Cmd.Process.Kill()
			p.Cmd.Wait() // reap zombie process
		}
		delete(m.proxies, nodeID)
	}
	m.mu.Unlock()
	if ok {
		m.log("info", fmt.Sprintf("✅ Chaos proxy removed for %s", nodeID))
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, np := range m.nodes {
		if np.cmd.Process != nil {
			np.cmd.Process.Kill()
			np.cmd.Wait() // reap zombie process
		}
	}
	for _, p := range m.proxies {
		if p.Cmd.Process != nil {
			p.Cmd.Process.Kill()
			p.Cmd.Wait() // reap zombie process
		}
	}
}

// GetClusterState polls every node's Health RPC and returns the live state
func (m *Manager) GetClusterState() ClusterState {
	m.mu.RLock()
	configs := m.configs
	events := make([]Event, len(m.events))
	copy(events, m.events)
	m.mu.RUnlock()

	states := make([]NodeState, len(configs))
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(i int, cfg NodeConfig) {
			defer wg.Done()
			ns := NodeState{Config: cfg, State: "Dead", Alive: false}

			conn, err := grpc.NewClient(cfg.GRPCAddr,
				grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err == nil {
				defer conn.Close()
				c := pb.NewKVStoreClient(conn)
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				resp, err := c.Health(ctx, &pb.HealthRequest{})
				if err == nil {
					ns.State = resp.State
					ns.LeaderAddr = resp.LeaderAddr
					ns.AppliedIndex = resp.AppliedIndex
					ns.NumPeers = resp.NumPeers
					ns.Term = resp.Term
					ns.Alive = true
				}
			}
			states[i] = ns
		}(i, cfg)
	}
	wg.Wait()

	return ClusterState{Nodes: states, Events: events}
}

// --- HTTP Handlers ---

// parseAgentAddrs parses "node0=ip:9000,node1=ip:9000,..." into a map.
// B2 (GCP_TODO.md): used to configure GCP mode agent routing.
func parseAgentAddrs(s string) map[string]string {
	m := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] != "" && kv[1] != "" {
			m[kv[0]] = kv[1]
		}
	}
	return m
}

// agentPost sends a POST request to a node's agent endpoint.
// B2 (GCP_TODO.md): returns an error with context on non-200 or network failure.
func (m *Manager) agentPost(nodeID, endpoint string) error {
	addr, ok := m.agentAddrs[nodeID]
	if !ok {
		return fmt.Errorf("no agent address configured for %s", nodeID)
	}
	url := fmt.Sprintf("http://%s/%s", addr, endpoint)
	resp, err := http.Post(url, "application/json", bytes.NewReader(nil))
	if err != nil {
		return fmt.Errorf("agent unreachable at %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned HTTP %d for %s", resp.StatusCode, url)
	}
	return nil
}

func main() {
	numNodes := flag.Int("nodes", 3, "Number of nodes to start (must be odd: 3, 5, 7...)")
	port := flag.Int("port", 8080, "Dashboard HTTP port")
	// B2 (GCP_TODO.md): when set, kill/pause/resume/restart route to agent HTTP endpoints.
	// Format: node0=<ip>:9000,node1=<ip>:9000,node2=<ip>:9000
	// Leave empty for local mode (dashboard manages child processes directly).
	agentAddrs := flag.String("agent-addrs", "", "GCP mode: comma-separated node=ip:port agent addresses")
	flag.Parse()

	// Find binary dir
	binDir, _ := os.Getwd()
	mgr := NewManager(binDir, *numNodes)
	if *agentAddrs != "" {
		mgr.agentAddrs = parseAgentAddrs(*agentAddrs)
		for i, cfg := range mgr.configs {
			if agentAddr, ok := mgr.agentAddrs[cfg.ID]; ok {
				parts := strings.Split(agentAddr, ":")
				if len(parts) >= 1 {
					ip := parts[0]
					mgr.configs[i].RaftAddr = fmt.Sprintf("%s:%d", ip, 12000+i)
					mgr.configs[i].GRPCAddr = fmt.Sprintf("%s:%d", ip, 50051+i)
				}
			}
		}
		log.Printf("GCP mode: routing lifecycle commands to %d node-agents", len(mgr.agentAddrs))
	}

	log.Printf("Starting %d-node Raft cluster...", *numNodes)
	go func() {
		time.Sleep(500 * time.Millisecond)
		mgr.StartAll()
	}()

	// API routes
	mux := http.NewServeMux()

	// Serve the dashboard HTML — embedded at compile time so it works from any directory
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := indexHTML.ReadFile("index.html")
		if err != nil {
			http.Error(w, "dashboard UI not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// GET /api/cluster → live cluster state
	mux.HandleFunc("/api/cluster", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		state := mgr.GetClusterState()
		json.NewEncoder(w).Encode(state)
	})

	// POST /api/kill/{nodeID}
	mux.HandleFunc("/api/kill/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/kill/"):]
		if err := mgr.KillNode(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "killed", "node": nodeID})
	})

	// POST /api/restart/{nodeID}
	mux.HandleFunc("/api/restart/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/restart/"):]
		if err := mgr.RestartNode(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "restarted", "node": nodeID})
	})

	// POST /api/chaos/drop/{nodeID}?rate=1.0
	mux.HandleFunc("/api/chaos/drop/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/chaos/drop/"):]
		rateStr := r.URL.Query().Get("rate")
		rate := 1.0
		if rateStr != "" {
			rate, _ = strconv.ParseFloat(rateStr, 64)
		}
		if err := mgr.StartChaosProxy(nodeID, rate, 0); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "proxy_started", "node": nodeID})
	})

	// POST /api/chaos/delay/{nodeID}?ms=3000
	mux.HandleFunc("/api/chaos/delay/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/chaos/delay/"):]
		msStr := r.URL.Query().Get("ms")
		ms := 3000
		if msStr != "" {
			ms, _ = strconv.Atoi(msStr)
		}
		if err := mgr.StartChaosProxy(nodeID, 0, ms); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "proxy_started", "node": nodeID})
	})

	// POST /api/chaos/stop/{nodeID}
	mux.HandleFunc("/api/chaos/stop/", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Path[len("/api/chaos/stop/"):]
		mgr.StopChaosProxy(nodeID)
		json.NewEncoder(w).Encode(map[string]string{"status": "proxy_stopped", "node": nodeID})
	})

	// POST /api/chaos/netem/{nodeID}?delay=500&loss=30&jitter=10
	// Applies tc/netem network impairment (real latency/packet loss at kernel level)
	mux.HandleFunc("/api/chaos/netem/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/chaos/netem/"):]
		delay := r.URL.Query().Get("delay")
		loss := r.URL.Query().Get("loss")
		jitter := r.URL.Query().Get("jitter")

		if err := mgr.ApplyNetem(nodeID, delay, loss, jitter); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		desc := ""
		if delay != "" {
			desc += "delay=" + delay + "ms"
			if jitter != "" {
				desc += "±" + jitter + "ms"
			}
		}
		if loss != "" {
			if desc != "" {
				desc += ", "
			}
			desc += "loss=" + loss + "%"
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "netem_applied", "node": nodeID, "params": desc})
	})

	// POST /api/chaos/unnetem/{nodeID} — remove tc/netem rules
	mux.HandleFunc("/api/chaos/unnetem/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/chaos/unnetem/"):]
		if err := mgr.RemoveNetem(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "netem_removed", "node": nodeID})
	})

	// KV Operations API - SET/GET/DELETE via dashboard (uses gRPC to leader)
	mux.HandleFunc("/api/kv/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("val")
		addr := r.URL.Query().Get("addr")
		if key == "" || val == "" || addr == "" {
			http.Error(w, "key, val, and addr are required", 400)
			return
		}
		result, err := mgr.DirectKVSet(addr, key, val)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": result})
	})

	mux.HandleFunc("/api/kv/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		addr := r.URL.Query().Get("addr")
		if key == "" || addr == "" {
			http.Error(w, "key and addr are required", 400)
			return
		}
		value, found, err := mgr.DirectKVGet(addr, key)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"found": false, "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"found": found, "value": value})
	})

	mux.HandleFunc("/api/kv/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		key := r.URL.Query().Get("key")
		addr := r.URL.Query().Get("addr")
		if key == "" || addr == "" {
			http.Error(w, "key and addr are required", 400)
			return
		}
		result, err := mgr.DirectKVDelete(addr, key)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": result})
	})

	// POST /api/partition/{nodeID} — iptables-based true network partition (drops Raft traffic)
	mux.HandleFunc("/api/partition/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/partition/"):]
		if err := mgr.PartitionNode(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "partitioned", "node": nodeID})
	})

	// POST /api/unpartition/{nodeID} — heal iptables partition
	mux.HandleFunc("/api/unpartition/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/unpartition/"):]
		if err := mgr.UnpartitionNode(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "unpartitioned", "node": nodeID})
	})

	// POST /api/pause/{nodeID} — SIGSTOP: simulate network partition
	mux.HandleFunc("/api/pause/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/pause/"):]
		w.Header().Set("Content-Type", "application/json")
		if err := mgr.PauseNode(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "paused", "node": nodeID})
	})

	// POST /api/resume/{nodeID} — SIGCONT: heal partition
	mux.HandleFunc("/api/resume/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		nodeID := r.URL.Path[len("/api/resume/"):]
		w.Header().Set("Content-Type", "application/json")
		if err := mgr.ResumeNode(nodeID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "resumed", "node": nodeID})
	})

	// GET /api/logs/{nodeID} → last N lines of node log
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Path[len("/api/logs/"):]
		logPath := fmt.Sprintf("/tmp/raft-kv/%s.log", nodeID)
		data, err := os.ReadFile(logPath)
		if err != nil {
			http.Error(w, "log not found", 404)
			return
		}
		// Return last 8KB
		if len(data) > 8192 {
			data = data[len(data)-8192:]
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("🚀 Chaos Dashboard running at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
