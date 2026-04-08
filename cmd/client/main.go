package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
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
	addr := flag.String("addr", "", "Address of a single cluster node (DEPRECATED: use -addrs)")
	addrs := flag.String("addrs", "", "Comma-separated addresses (e.g., node0:50051,node1:50052,node2:50053)")
	followerRead := flag.Bool("follower-read", false, "Serve Gets from followers via read-index (linearizable, distributes read load)")

	// Idempotency control flags (for testing duplicate detection)
	explicitClientID := flag.String("client-id", "", "Explicit client ID for idempotency testing (default: auto-generated UUID)")
	explicitSeqNum := flag.Uint64("seq-num", 0, "Explicit sequence number for idempotency testing (default: auto-increment)")

	flag.Parse()

	// Use explicit client-id if provided, otherwise generate one
	if *explicitClientID != "" {
		clientID = *explicitClientID
	}
	// Use explicit sequence number if provided, otherwise auto-increment.
	// LIMITATION: -seq-num=0 cannot be detected as "explicitly set" because 0 is the
	// uint64 zero value. If you pass -seq-num=0, it is silently ignored and auto-increment
	// is used. Use -seq-num=1 as the minimum testable sequence number.
	if *explicitSeqNum != 0 {
		sequenceNo = *explicitSeqNum - 1 // Will become the provided value after atomic add
		log.Printf("Using explicit seq-num: %d (for idempotency testing)", *explicitSeqNum)
	}

	// Parse addresses: -addrs takes priority over -addr
	var addressList []string
	if *addrs != "" {
		addressList = strings.Split(*addrs, ",")
		// Trim whitespace from each address
		for i := range addressList {
			addressList[i] = strings.TrimSpace(addressList[i])
		}
		log.Printf("Smart client initialized with %d endpoints: %v", len(addressList), addressList)
	} else if *addr != "" {
		addressList = []string{*addr}
		log.Printf("Using single endpoint (legacy mode): %s", *addr)
	} else {
		log.Fatalf("Error: either -addr or -addrs must be specified")
	}

	// Health command - try each address
	if *cmd == "health" {
		for _, a := range addressList {
			if ok := tryHealth(a); ok {
				return
			}
			log.Printf("Health check failed for %s, trying next...", a)
		}
		log.Fatalf("Health check failed for all addresses")
		return
	}

	if *key == "" {
		log.Fatalf("Error: key is required")
	}

	// Smart client main loop with failover support
	success := smartRequestLoop(*cmd, *key, *val, addressList, *followerRead)
	if !success {
		log.Fatalf("Operation failed entirely after trying all addresses")
	}
}

// smartRequestLoop tries each address in order until one succeeds
func smartRequestLoop(cmd, key, val string, addresses []string, followerRead bool) bool {
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		for i, addr := range addresses {
			log.Printf("Trying address %d/%d: %s", i+1, len(addresses), addr)

			success, leaderAddr := sendRequest(cmd, key, val, addr, followerRead)

			if success {
				return true
			}

			// Handle leader redirect - use the redirected address
			if leaderAddr != "" {
				log.Printf("Redirected to leader at %s, using it...", leaderAddr)
				// Use leader address for next attempt
				leaderSuccess, _ := sendRequest(cmd, key, val, leaderAddr, followerRead)
				if leaderSuccess {
					return true
				}
			}

			log.Printf("Address %s failed, trying next...", addr)
			time.Sleep(100 * time.Millisecond)
		}

		if attempt < maxRetries-1 {
			log.Printf("All addresses failed, retrying in 1s... (attempt %d/%d)", attempt+1, maxRetries)
			time.Sleep(1 * time.Second)
		}
	}

	return false
}

// tryHealth attempts to query health from a single address
func tryHealth(addr string) bool {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to %s: %v", addr, err)
		return false
	}
	defer conn.Close()

	c := pb.NewKVStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		log.Printf("Health RPC failed for %s: %v", addr, err)
		return false
	}

	fmt.Printf("Node: %s | State: %s | Leader: %s | Applied: %d | Peers: %d\n",
		resp.NodeId, resp.State, resp.LeaderAddr, resp.AppliedIndex, resp.NumPeers)
	return true
}

func sendRequest(cmd, key, val, addr string, followerRead bool) (bool, string) {
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
		resp, err := c.Get(ctx, &pb.GetRequest{Key: key, FollowerRead: followerRead})
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
