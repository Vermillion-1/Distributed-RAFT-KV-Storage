package main

import (
	"flag"
	"io"
	"log"
	"math/rand"
	"net"
	"time"
)

// ChaosProxy sits between two port endpoints and randomly drops or delays packets
func main() {
	listenAddr := flag.String("listen", "127.0.0.1:12001", "Proxy address to listen on")
	targetAddr := flag.String("target", "127.0.0.1:12000", "Target Raft node address")
	dropRate := flag.Float64("drop", 0.0, "Probability of dropping a connection (0.0 to 1.0)")
	delayMs := flag.Int("delay", 0, "Max random delay in ms to add to connections")

	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("Chaos Proxy listening on %s, forwarding to %s", *listenAddr, *targetAddr)
	log.Printf("Chaos applied: Drop rate %f, Max delay %d ms", *dropRate, *delayMs)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(clientConn, *targetAddr, *dropRate, *delayMs)
	}
}

func handleConnection(clientConn net.Conn, targetAddr string, dropRate float64, delayMs int) {
	defer clientConn.Close()

	// Simulate connection drop (Send/Receive Omission)
	if rand.Float64() < dropRate {
		log.Printf("Chaos: Intercepted and dropped connection to %s", targetAddr)
		return
	}

	// Simulate latency
	if delayMs > 0 {
		delay := time.Duration(rand.Intn(delayMs)) * time.Millisecond
		time.Sleep(delay)
	}

	// Connect to the actual target
	targetConn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		log.Printf("Failed to connect to target: %v", err)
		return
	}
	defer targetConn.Close()

	// Bi-directional pipe
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(targetConn, clientConn)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, targetConn)
		errc <- err
	}()

	<-errc // Wait for one side to close or error
}
