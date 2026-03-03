package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	pb "github.com/ankush/raft-kv/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
	mu      sync.RWMutex
	nodes   map[string]*nodeProcess
	proxies map[string]*ChaosProxy
	configs []NodeConfig
	events  []Event
	binDir  string
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

func (m *Manager) StartAll() {
	// Clean up old data
	os.RemoveAll("/tmp/raft-kv/")
	os.MkdirAll("/tmp/raft-kv/", 0755)

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

func (m *Manager) RestartNode(nodeID string) error {
	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Find a live node to join
	joinAddr := ""
	m.mu.RLock()
	for id, n := range m.nodes {
		if id != nodeID && n.cmd.Process != nil {
			joinAddr = n.config.GRPCAddr
			break
		}
	}
	m.mu.RUnlock()

	m.startNode(np.config, joinAddr)
	m.log("success", fmt.Sprintf("♻️  Restarted %s — recovering via Raft log replay", nodeID))
	return nil
}

func (m *Manager) StartChaosProxy(nodeID string, dropRate float64, delayMs int) error {
	m.mu.Lock()
	np, ok := m.nodes[nodeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Kill existing proxy for this node if any
	m.StopChaosProxy(nodeID)

	listenPort := 22000 + len(m.proxies)
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
		}
	}
	for _, p := range m.proxies {
		if p.Cmd.Process != nil {
			p.Cmd.Process.Kill()
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

func main() {
	numNodes := flag.Int("nodes", 3, "Number of nodes to start (must be odd: 3, 5, 7...)")
	port := flag.Int("port", 8080, "Dashboard HTTP port")
	flag.Parse()

	// Find binary dir
	binDir, _ := os.Getwd()
	mgr := NewManager(binDir, *numNodes)

	log.Printf("Starting %d-node Raft cluster...", *numNodes)
	go func() {
		time.Sleep(500 * time.Millisecond)
		mgr.StartAll()
	}()

	// API routes
	mux := http.NewServeMux()

	// Serve the dashboard HTML
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, binDir+"/cmd/dashboard/index.html")
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

	// POST /api/run-tests → runs the full chaos test suite
	mux.HandleFunc("/api/run-tests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		mgr.log("info", "🧪 Starting chaos test suite via run_chaos_test.sh...")
		mgr.StopAll() // stop managed cluster so test script can start fresh

		cmd := exec.Command("bash", binDir+"/run_chaos_test.sh")
		cmd.Dir = binDir
		out, err := cmd.CombinedOutput()
		passed := err == nil

		outStr := string(out)
		if passed {
			mgr.log("success", "✅ All chaos tests PASSED")
		} else {
			mgr.log("error", "❌ Chaos tests FAILED: "+err.Error())
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"passed": passed,
			"output": outStr,
		})

		// Restart cluster after tests
		go func() {
			time.Sleep(2 * time.Second)
			mgr.StartAll()
		}()
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("🚀 Chaos Dashboard running at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
