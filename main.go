package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ankush/raft-kv/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/ankush/raft-kv/proto"
)

func main() {
	nodeID := flag.String("id", "node0", "Node ID")
	raftAddr := flag.String("raft", "127.0.0.1:12000", "Raft communication address")
	grpcAddr := flag.String("grpc", "127.0.0.1:50051", "gRPC client address")
	dataDir := flag.String("data", "/tmp/raft-kv/node0", "Directory to store Raft data")
	join := flag.String("join", "", "gRPC address of an existing node to join")

	flag.Parse()

	log.Printf("Starting Node %s", *nodeID)
	log.Printf("Raft Addr: %s | gRPC Addr: %s | Data Dir: %s", *raftAddr, *grpcAddr, *dataDir)

	// In a real application, you might use DNS to find peers.
	// For this prototype, we'll start a single node and have others join it via HTTP/gRPC.
	peers := []string{}

	n, err := server.NewNode(*nodeID, *raftAddr, *grpcAddr, *dataDir, peers)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}

	if err := n.StartGRPCServer(); err != nil {
		log.Fatalf("Failed to start gRPC server: %v", err)
	}

	// Wait a moment for network to bind before bootstrapping or joining
	// time.Sleep(1 * time.Second)

	if *join == "" {
		// We are the first node, bootstrap the cluster
		log.Println("Bootstrapping new Raft cluster")
		if err := n.Bootstrap(); err != nil {
			log.Fatalf("Failed to bootstrap node: %v", err)
		}
	} else {
		// We need to tell the existing cluster to add us
		log.Printf("Joining existing cluster at %s", *join)

		retries := 10
		for i := 0; i < retries; i++ {
			err = joinCluster(*join, *nodeID, *raftAddr, *grpcAddr)
			if err == nil {
				log.Println("Successfully joined cluster")
				break
			}
			log.Printf("Failed to join cluster (attempt %d/%d): %v. Retrying in 1s...", i+1, retries, err)
			time.Sleep(1 * time.Second)
		}

		if err != nil {
			log.Fatalf("Fatal: could not join cluster after %d attempts", retries)
		}
	}

	// Wait for an interrupt signal (e.g., Ctrl+C) to gracefully shut down the node
	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, os.Interrupt, syscall.SIGTERM)
	<-terminate

	log.Println("Node shutting down...")
	n.Stop()
	log.Println("Node stopped.")
}

// joinCluster connects to an existing node and asks to be added via a custom Join RPC
func joinCluster(targetGrpc, nodeID, raftAddr, grpcAddr string) error {
	// For this prototype, we'll simply connect via gRPC to the existing node.
	// We need to implement a Join Request in our KV protobuf. Let's add that.
	conn, err := grpc.NewClient(targetGrpc, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewKVStoreClient(conn)

	// Since we haven't defined Join in PROTO yet, we will do it via the node's internal raft method
	// for the purpose of the prototype, we will expose an API for it shortly.

	// Create request
	req := &pb.JoinRequest{
		NodeId:   nodeID,
		RaftAddr: raftAddr,
		GrpcAddr: grpcAddr,
	}

	// Try to join
	ctx := context.Background()
	resp, err := c.Join(ctx, req)
	if err != nil {
		return fmt.Errorf("could not join: %v", err)
	}

	if !resp.Success {
		if resp.LeaderAddr != "" {
			log.Printf("Redirected to leader at %s for join", resp.LeaderAddr)
			return joinCluster(resp.LeaderAddr, nodeID, raftAddr, grpcAddr)
		}
		return fmt.Errorf("join failed, no leader available")
	}

	return nil
}
