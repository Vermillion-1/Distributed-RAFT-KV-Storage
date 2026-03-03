package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	pb "github.com/ankush/raft-kv/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Global client identity for idempotent requests (Duplicate Message prevention)
var (
	clientID   = uuid.New().String()
	sequenceNo uint64
)

func main() {
	cmd := flag.String("cmd", "get", "Command to run: get, set, delete, health")
	key := flag.String("key", "", "Key to get/set/delete")
	val := flag.String("val", "", "Value to set")
	addr := flag.String("addr", "127.0.0.1:50051", "Address of a cluster node")

	flag.Parse()

	// Health command doesn't require a key
	if *cmd == "health" {
		queryHealth(*addr)
		return
	}

	if *key == "" {
		log.Fatalf("Error: key is required")
	}

	// Retry loop with leader-redirect support (Retry resiliency pattern)
	for i := 0; i < 5; i++ {
		success, leaderAddr := sendRequest(*cmd, *key, *val, *addr)

		if success {
			return
		}

		if leaderAddr != "" {
			log.Printf("Redirecting to leader at %s...", leaderAddr)
			*addr = leaderAddr
			time.Sleep(100 * time.Millisecond) // Give it a moment before retry
			continue
		}

		log.Printf("Request failed or node unavailable. Retrying in 1s...")
		time.Sleep(1 * time.Second)
	}

	log.Fatalf("Operation failed entirely.")
}

func sendRequest(cmd, key, val, addr string) (bool, string) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect: %v", err)
		return false, ""
	}
	defer conn.Close()

	c := pb.NewKVStoreClient(conn)
	// Client timeout acts as a basic Circuit Breaker pattern (Slide 9)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	switch cmd {
	case "get":
		resp, err := c.Get(ctx, &pb.GetRequest{Key: key})
		if err != nil {
			log.Printf("RPC Error: %v", err)
			return false, ""
		}
		if resp.LeaderAddr != "" {
			return false, resp.LeaderAddr
		}
		if resp.Found {
			fmt.Printf("%s=%s\n", key, resp.Value)
		} else {
			fmt.Printf("Key %s not found\n", key)
		}
		return true, ""

	case "set":
		seqNum := atomic.AddUint64(&sequenceNo, 1)
		resp, err := c.Set(ctx, &pb.SetRequest{
			Key:         key,
			Value:       val,
			ClientId:    clientID,
			SequenceNum: seqNum,
		})
		if err != nil {
			log.Printf("RPC Error: %v", err)
			return false, ""
		}
		if !resp.Success {
			return false, resp.LeaderAddr
		}
		fmt.Printf("Set %s=%s successful\n", key, val)
		return true, ""

	case "delete":
		seqNum := atomic.AddUint64(&sequenceNo, 1)
		resp, err := c.Delete(ctx, &pb.DeleteRequest{
			Key:         key,
			ClientId:    clientID,
			SequenceNum: seqNum,
		})
		if err != nil {
			log.Printf("RPC Error: %v", err)
			return false, ""
		}
		if !resp.Success {
			return false, resp.LeaderAddr
		}
		fmt.Printf("Deleted %s successfully\n", key)
		return true, ""
	default:
		log.Fatalf("Unknown command: %s", cmd)
		return false, ""
	}
}

// queryHealth calls the Health endpoint and displays node status
func queryHealth(addr string) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewKVStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		log.Fatalf("Health RPC Error: %v", err)
	}

	fmt.Printf("Node: %s | State: %s | Leader: %s | Applied: %d | Peers: %d\n",
		resp.NodeId, resp.State, resp.LeaderAddr, resp.AppliedIndex, resp.NumPeers)
}
