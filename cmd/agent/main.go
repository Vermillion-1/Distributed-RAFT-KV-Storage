// cmd/agent/main.go — Node-Agent Sidecar
// B1 + B3 (GCP_TODO.md Block B)
//
// Runs on each GCP VM. Spawns kv-store as a child process and exposes
// HTTP endpoints so the dashboard can manage it remotely.
//
// Usage:
//   ./node-agent \
//     -kv-bin=./kv-store \
//     -kv-args="-id=node0 -raft=10.0.0.1:12000 -grpc=0.0.0.0:50051 -data=/data/raft-kv" \
//     -port=9000

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	mu        sync.Mutex
	childProc *os.Process // owned by the agent; signalled for kill/pause/resume
	kvBin     string
	kvArgs    []string
	raftPort  string // parsed from -kv-args for B3 iptables rules
	netIface  string // network interface for tc netem (e.g., "eth0")
)

// spawnKV starts kv-store as a child process.
// Stores the Process reference for signal operations.
// Launches cmd.Wait() in a goroutine to reap the zombie on exit.
func spawnKV() error {
	cmd := exec.Command(kvBin, kvArgs...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	mu.Lock()
	childProc = cmd.Process
	mu.Unlock()
	go func() {
		cmd.Wait() // reap zombie; keeps OS process table clean
	}()
	log.Printf("[agent] spawned kv-store PID %d", cmd.Process.Pid)
	return nil
}

// alive checks if the child process is still running by sending signal 0.
// Signal 0 doesn't kill the process — it just checks if it exists.
func alive(p *os.Process) bool {
	if p == nil {
		return false
	}
	err := p.Signal(syscall.Signal(0))
	return err == nil
}

func main() {
	kvBinFlag := flag.String("kv-bin", "./kv-store", "Path to the kv-store binary")
	kvArgsFlag := flag.String("kv-args", "", "Space-separated args to pass to kv-store")
	port := flag.Int("port", 9000, "Agent HTTP port")
	netIfaceFlag := flag.String("iface", "eth0", "Network interface for tc netem (e.g., eth0)")
	flag.Parse()

	kvBin = *kvBinFlag
	kvArgs = strings.Fields(*kvArgsFlag)
	netIface = *netIfaceFlag

	// B3: auto-parse raft port from "-raft=<ip>:<port>" in kv-args
	// Used by the /partition and /unpartition iptables handlers.
	for _, arg := range kvArgs {
		if strings.HasPrefix(arg, "-raft=") {
			// "-raft=10.0.0.1:12000" → split on ":" → last element is port
			val := strings.TrimPrefix(arg, "-raft=")
			parts := strings.Split(val, ":")
			if len(parts) == 2 {
				raftPort = parts[1]
				log.Printf("[agent] raft port parsed from -kv-args: %s", raftPort)
			}
		}
	}

	// Spawn kv-store on agent startup
	log.Printf("[agent] launching: %s %v", kvBin, kvArgs)
	if err := spawnKV(); err != nil {
		log.Fatalf("[agent] failed to spawn kv-store: %v", err)
	}

	mux := http.NewServeMux()

	// GET /health → {"pid": N, "alive": true/false}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		p := childProc
		mu.Unlock()
		pid := 0
		if p != nil {
			pid = p.Pid
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"pid":   pid,
			"alive": alive(p),
		})
	})

	// POST /kill → SIGKILL child
	mux.HandleFunc("/kill", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		p := childProc
		mu.Unlock()
		if p == nil {
			http.Error(w, "no child process", http.StatusBadRequest)
			return
		}
		if err := p.Kill(); err != nil {
			http.Error(w, fmt.Sprintf("kill failed: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[agent] SIGKILL → PID %d", p.Pid)
		w.WriteHeader(http.StatusOK)
	})

	// POST /pause → SIGSTOP child (simulates network partition — freezes all I/O)
	mux.HandleFunc("/pause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		p := childProc
		mu.Unlock()
		if p == nil {
			http.Error(w, "no child process", http.StatusBadRequest)
			return
		}
		if err := p.Signal(syscall.SIGSTOP); err != nil {
			http.Error(w, fmt.Sprintf("pause failed: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[agent] SIGSTOP → PID %d", p.Pid)
		w.WriteHeader(http.StatusOK)
	})

	// POST /resume → SIGCONT child (heals SIGSTOP partition)
	mux.HandleFunc("/resume", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		p := childProc
		mu.Unlock()
		if p == nil {
			http.Error(w, "no child process", http.StatusBadRequest)
			return
		}
		if err := p.Signal(syscall.SIGCONT); err != nil {
			http.Error(w, fmt.Sprintf("resume failed: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[agent] SIGCONT → PID %d", p.Pid)
		w.WriteHeader(http.StatusOK)
	})

	// POST /restart → SIGKILL + wait for port release + re-exec with same args
	mux.HandleFunc("/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		p := childProc
		mu.Unlock()
		// Kill the old process (ignore error — it may already be dead)
		if p != nil {
			p.Kill()
		}
		// Wait for OS to release the Raft and gRPC ports before re-spawning.
		// 500ms is sufficient for the kernel to reclaim TCP ports after SIGKILL.
		time.Sleep(500 * time.Millisecond)
		if err := spawnKV(); err != nil {
			http.Error(w, fmt.Sprintf("restart failed: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[agent] restarted kv-store")
		w.WriteHeader(http.StatusOK)
	})

	// POST /partition — B3: drop Raft TCP traffic at kernel level via iptables.
	// The process stays alive (unlike SIGSTOP) but cannot send/receive on its Raft port.
	// This is a stricter split-brain test: the node keeps running Raft internally
	// but is invisible to the rest of the cluster.
	mux.HandleFunc("/partition", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if raftPort == "" {
			http.Error(w, "raft port not parsed from -kv-args; cannot partition", http.StatusBadRequest)
			return
		}
		inCmd := exec.Command("sudo", "iptables", "-I", "INPUT", "-p", "tcp", "--dport", raftPort, "-j", "DROP")
		outCmd := exec.Command("sudo", "iptables", "-I", "OUTPUT", "-p", "tcp", "--sport", raftPort, "-j", "DROP")
		if out, err := inCmd.CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("iptables INPUT failed: %v — %s", err, out), http.StatusInternalServerError)
			return
		}
		if out, err := outCmd.CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("iptables OUTPUT failed: %v — %s", err, out), http.StatusInternalServerError)
			return
		}
		log.Printf("[agent] iptables DROP applied on Raft port %s (true network partition)", raftPort)
		w.WriteHeader(http.StatusOK)
	})

	// POST /unpartition — B3: remove iptables DROP rules to heal the partition
	mux.HandleFunc("/unpartition", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if raftPort == "" {
			http.Error(w, "raft port not parsed from -kv-args", http.StatusBadRequest)
			return
		}
		inCmd := exec.Command("sudo", "iptables", "-D", "INPUT", "-p", "tcp", "--dport", raftPort, "-j", "DROP")
		outCmd := exec.Command("sudo", "iptables", "-D", "OUTPUT", "-p", "tcp", "--sport", raftPort, "-j", "DROP")
		// -D returns non-zero if the rule didn't exist — not fatal, log and continue
		if out, err := inCmd.CombinedOutput(); err != nil {
			log.Printf("[agent] iptables -D INPUT warning: %v — %s", err, out)
		}
		if out, err := outCmd.CombinedOutput(); err != nil {
			log.Printf("[agent] iptables -D OUTPUT warning: %v — %s", err, out)
		}
		log.Printf("[agent] iptables DROP removed on Raft port %s (partition healed)", raftPort)
		w.WriteHeader(http.StatusOK)
	})

	// POST /netem?delay=500&loss=30&jitter=10 — apply network impairment via tc/netem
	// delay: base delay in milliseconds (e.g., "500" = 500ms)
	// jitter: optional random variation in ms (e.g., "10" = ±10ms)
	// loss: packet loss percentage (e.g., "30" = 30%)
	// Examples:
	//   /netem?delay=500          → 500ms constant delay
	//   /netem?delay=500&jitter=10 → 500ms ± 10ms jitter
	//   /netem?delay=500&loss=30  → 500ms delay + 30% packet loss
	mux.HandleFunc("/netem", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		delayMs := r.URL.Query().Get("delay")
		jitterMs := r.URL.Query().Get("jitter")
		lossPct := r.URL.Query().Get("loss")

		if delayMs == "" && lossPct == "" {
			http.Error(w, "must specify delay and/or loss parameter", http.StatusBadRequest)
			return
		}

		// First, delete any existing qdisc rule
		delCmd := exec.Command("sudo", "tc", "qdisc", "del", "dev", netIface, "root")
		delCmd.Run() // ignore error if none exists

		// Build the netem command
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

		cmd := exec.Command("sudo", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("tc netem failed: %v — %s", err, out), http.StatusInternalServerError)
			return
		}

		desc := ""
		if delayMs != "" {
			desc += "delay=" + delayMs + "ms"
			if jitterMs != "" {
				desc += "±" + jitterMs + "ms"
			}
		}
		if lossPct != "" {
			if desc != "" {
				desc += ", "
			}
			desc += "loss=" + lossPct + "%"
		}
		log.Printf("[agent] tc netem applied on %s: %s", netIface, desc)
		w.WriteHeader(http.StatusOK)
	})

	// POST /unnetem — remove all tc netem rules and restore normal networking
	mux.HandleFunc("/unnetem", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		cmd := exec.Command("sudo", "tc", "qdisc", "del", "dev", netIface, "root")
		if out, err := cmd.CombinedOutput(); err != nil {
			// Not an error if no qdisc exists
			log.Printf("[agent] tc qdisc del warning: %v — %s", err, out)
		}
		log.Printf("[agent] tc netem removed on %s (network normal)", netIface)
		w.WriteHeader(http.StatusOK)
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("[agent] listening on %s — managing: %s %v", addr, kvBin, kvArgs)
	log.Fatal(http.ListenAndServe(addr, mux))
}
