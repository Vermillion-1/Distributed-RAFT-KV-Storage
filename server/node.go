package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	pb "github.com/ankush/raft-kv/proto"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Node represents a single server in the Raft cluster
type Node struct {
	pb.UnimplementedKVStoreServer
	raft          *raft.Raft
	fsm           *KVStore
	nodeID        string
	raftAddr      string
	grpcAddr      string
	server        *grpc.Server
	peers         []string
	logger        *log.Logger
	mu            sync.RWMutex
	logStore      raft.LogStore
	stableStore   raft.StableStore
	snapshotStore raft.SnapshotStore
}

// NewNode creates a new KV Store Node
func NewNode(nodeID, raftAddr, grpcAddr, dataDir string, peers []string) (*Node, error) {
	logger := log.New(os.Stderr, fmt.Sprintf("[Node %s] ", nodeID), log.LstdFlags)

	n := &Node{
		fsm:      NewKVStore(),
		nodeID:   nodeID,
		raftAddr: raftAddr,
		grpcAddr: grpcAddr,
		peers:    peers,
		logger:   logger,
	}

	// 1. Setup Raft Configuration
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(nodeID)
	// Tuned for GCP inter-VM latency (~1ms RTT). See GCP_TODO.md Block A1.
	config.HeartbeatTimeout = 500 * time.Millisecond
	config.ElectionTimeout = 750 * time.Millisecond
	config.CommitTimeout = 100 * time.Millisecond
	config.LeaderLeaseTimeout = 400 * time.Millisecond

	// NEW: Aggressive snapshotting for rapid testing of InstallSnapshot recovery
	config.SnapshotInterval = 10 * time.Second
	config.SnapshotThreshold = 10
	config.TrailingLogs = 10

	// 2. Setup Raft Communication (Transport)
	addr, err := net.ResolveTCPAddr("tcp", raftAddr)
	if err != nil {
		return nil, err
	}
	transport, err := raft.NewTCPTransport(raftAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, err
	}

	// 3. Setup Raft Storage
	os.MkdirAll(dataDir, 0700)

	// Create the snapshot store. This allows the Raft to truncate the log.
	snapshots, err := raft.NewFileSnapshotStore(dataDir, 2, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("file snapshot store: %s", err)
	}

	// Create the log store and stable store.
	logStore, err := raftboltdb.NewBoltStore(fmt.Sprintf("%s/raft-log.bolt", dataDir))
	if err != nil {
		return nil, fmt.Errorf("new bolt store: %s", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(fmt.Sprintf("%s/raft-stable.bolt", dataDir))
	if err != nil {
		return nil, fmt.Errorf("new bolt store: %s", err)
	}

	// 4. Instantiate the Raft system
	rt, err := raft.NewRaft(config, n.fsm, logStore, stableStore, snapshots, transport)
	if err != nil {
		return nil, err
	}
	n.raft = rt
	n.logStore = logStore
	n.stableStore = stableStore
	n.snapshotStore = snapshots

	return n, nil
}

// Bootstrap brings up the node and discovers peers
func (n *Node) Bootstrap() error {
	hasState, err := raft.HasExistingState(n.logStore, n.stableStore, n.snapshotStore)
	if err != nil {
		return fmt.Errorf("failed to check existing state: %v", err)
	}

	if !hasState {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:       raft.ServerID(n.nodeID),
					Address:  raft.ServerAddress(n.raftAddr),
					Suffrage: raft.Voter,
				},
			},
		}
		f := n.raft.BootstrapCluster(configuration)
		if f.Error() != nil {
			return fmt.Errorf("failed to bootstrap cluster: %v", f.Error())
		}
	}

	// Always register ourselves once we're up and we are the leader (or when we become leader, but for bootstrap this is enough)
	go func() {
		// Wait to become leader
		timeout := time.After(5 * time.Second)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				n.logger.Printf("timed out waiting to become leader to register self")
				return
			case <-ticker.C:
				if n.raft.State() == raft.Leader {
					c := &command{
						Op:    "register",
						Key:   n.raftAddr,
						Value: n.grpcAddr,
					}
					b, err := json.Marshal(c)
					if err != nil {
						n.logger.Printf("Failed to marshal register command: %v", err)
						return
					}
					f := n.raft.Apply(b, 500*time.Millisecond)
					if f.Error() != nil {
						n.logger.Printf("Failed to apply register command: %v", f.Error())
					}
					return
				}
			}
		}
	}()

	return nil
}

// StartGRPCServer begins listening for client requests
func (n *Node) StartGRPCServer() error {
	lis, err := net.Listen("tcp", n.grpcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", n.grpcAddr, err)
	}

	n.server = grpc.NewServer()
	pb.RegisterKVStoreServer(n.server, n)
	reflection.Register(n.server) // Help for debugging with grpcurl

	n.logger.Printf("gRPC server listening on %s", n.grpcAddr)
	go func() {
		if err := n.server.Serve(lis); err != nil {
			n.logger.Fatalf("failed to serve gRPC: %v", err)
		}
	}()
	return nil
}

func (n *Node) Stop() {
	if n.server != nil {
		n.server.GracefulStop()
	}
	// Graceful leadership transfer before shutdown to minimize cluster disruption (Failover pattern)
	if n.raft.State() == raft.Leader {
		n.logger.Printf("Transferring leadership before shutdown...")
		future := n.raft.LeadershipTransfer()
		if err := future.Error(); err != nil {
			n.logger.Printf("Leadership transfer failed: %v (proceeding with shutdown)", err)
		}
	}
	future := n.raft.Shutdown()
	if err := future.Error(); err != nil {
		n.logger.Printf("Error shutting down raft: %v", err)
	}
}

// --- GRPC handlers ---

func (n *Node) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	if n.raft.State() != raft.Leader {
		leaderAddr, _ := n.raft.LeaderWithID()
		grpcAddr := n.fsm.GetGrpcAddr(string(leaderAddr))
		n.logger.Printf("[DEBUG Get] Not leader. Leader is %s, resolving to grpc %s", leaderAddr, grpcAddr)
		return &pb.GetResponse{Found: false, LeaderAddr: grpcAddr}, nil
	}

	// Strictly consistent read: Ensure we are still the leader
	if err := n.raft.VerifyLeader().Error(); err != nil {
		return nil, fmt.Errorf("failed to verify leader: %v", err)
	}

	val, ok := n.fsm.Get(req.Key)
	return &pb.GetResponse{
		Found: ok,
		Value: val,
	}, nil
}

func (n *Node) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	if n.raft.State() != raft.Leader {
		leaderAddr, _ := n.raft.LeaderWithID()
		grpcAddr := n.fsm.GetGrpcAddr(string(leaderAddr))
		return &pb.SetResponse{Success: false, LeaderAddr: grpcAddr}, nil
	}

	c := &command{
		Op:       "set",
		Key:      req.Key,
		Value:    req.Value,
		ClientID: req.ClientId,
		SeqNum:   req.SequenceNum,
	}

	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	f := n.raft.Apply(b, 500*time.Millisecond)
	if f.Error() != nil {
		return nil, f.Error()
	}

	return &pb.SetResponse{Success: true}, nil
}

func (n *Node) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if n.raft.State() != raft.Leader {
		leaderAddr, _ := n.raft.LeaderWithID()
		grpcAddr := n.fsm.GetGrpcAddr(string(leaderAddr))
		return &pb.DeleteResponse{Success: false, LeaderAddr: grpcAddr}, nil
	}

	c := &command{
		Op:       "delete",
		Key:      req.Key,
		ClientID: req.ClientId,
		SeqNum:   req.SequenceNum,
	}

	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	f := n.raft.Apply(b, 500*time.Millisecond)
	if f.Error() != nil {
		return nil, f.Error()
	}

	return &pb.DeleteResponse{Success: true}, nil
}

func (n *Node) Join(ctx context.Context, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	if n.raft.State() != raft.Leader {
		leaderAddr, _ := n.raft.LeaderWithID()
		grpcAddr := n.fsm.GetGrpcAddr(string(leaderAddr))
		return &pb.JoinResponse{Success: false, LeaderAddr: grpcAddr}, nil
	}

	n.logger.Printf("Received join request for remote node %s at %s", req.NodeId, req.RaftAddr)

	configFuture := n.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		n.logger.Printf("failed to get raft configuration: %v", err)
		return nil, err
	}

	for _, srv := range configFuture.Configuration().Servers {
		// If a node already exists with either the joining node's ID or address,
		// that node may need to be removed and added again. We will just return true
		// if it's already there to keep it idempotent.
		if srv.ID == raft.ServerID(req.NodeId) || srv.Address == raft.ServerAddress(req.RaftAddr) {

			// However if both ID and Address are the same, it's already a member.
			if srv.Address == raft.ServerAddress(req.RaftAddr) && srv.ID == raft.ServerID(req.NodeId) {
				n.logger.Printf("node %s at %s already member of cluster, ignoring join request", req.NodeId, req.RaftAddr)
				return &pb.JoinResponse{Success: true}, nil
			}

			future := n.raft.RemoveServer(srv.ID, 0, 0)
			if err := future.Error(); err != nil {
				return nil, fmt.Errorf("error removing existing node %s at %s: %s", req.NodeId, req.RaftAddr, err)
			}
		}
	}

	f := n.raft.AddVoter(raft.ServerID(req.NodeId), raft.ServerAddress(req.RaftAddr), 0, 0)
	if f.Error() != nil {
		return nil, f.Error()
	}

	// Register the new node's mappings via the FSM so all followers learn it
	regCmd := &command{
		Op:    "register",
		Key:   req.RaftAddr,
		Value: req.GrpcAddr,
	}
	b, err := json.Marshal(regCmd)
	if err == nil {
		n.raft.Apply(b, 500*time.Millisecond)
	}

	n.logger.Printf("[DEBUG Join] Registered peer %s with grpc addr %s", req.RaftAddr, req.GrpcAddr)
	n.logger.Printf("node %s at %s joined successfully", req.NodeId, req.RaftAddr)
	return &pb.JoinResponse{Success: true}, nil
}

// Health implements the Health Endpoint Monitoring resiliency pattern (Slide 9).
// It returns the node's current state, leader address, applied log index, and peer count.
func (n *Node) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	leaderAddr, _ := n.raft.LeaderWithID()
	grpcAddr := n.fsm.GetGrpcAddr(string(leaderAddr))

	state := n.raft.State().String()
	stats := n.raft.Stats()

	var appliedIndex uint64
	fmt.Sscanf(stats["applied_index"], "%d", &appliedIndex)

	var term uint64
	fmt.Sscanf(stats["term"], "%d", &term)

	configFuture := n.raft.GetConfiguration()
	var numPeers uint32
	if configFuture.Error() == nil {
		numPeers = uint32(len(configFuture.Configuration().Servers))
	}

	return &pb.HealthResponse{
		NodeId:       n.nodeID,
		State:        state,
		LeaderAddr:   grpcAddr,
		AppliedIndex: appliedIndex,
		NumPeers:     numPeers,
		Term:         term,
	}, nil
}
