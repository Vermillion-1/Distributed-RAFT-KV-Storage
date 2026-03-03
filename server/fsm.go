package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/hashicorp/raft"
)

// KVStore represents the in-memory key-value store and implements the raft.FSM interface.
// It tracks per-client sequence numbers for idempotent request handling (Duplicate Message prevention).
type KVStore struct {
	mu          sync.RWMutex
	m           map[string]string // The actual map holding the data
	Peers       map[string]string // Maps Raft Address -> gRPC Address for client redirects
	lastApplied map[string]uint64 // Maps ClientID -> last applied SequenceNum (idempotency)
}

// NewKVStore creates a new KVStore
func NewKVStore() *KVStore {
	return &KVStore{
		m:           make(map[string]string),
		Peers:       make(map[string]string),
		lastApplied: make(map[string]uint64),
	}
}

// Get returns the value for a given key
func (s *KVStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.m[key]
	return val, ok
}

// Set sets the value for a given key
func (s *KVStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
}

// Delete removes a key
func (s *KVStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}

// RegisterNode maps a Raft address to a gRPC address
func (s *KVStore) RegisterNode(raftAddr, grpcAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Peers[raftAddr] = grpcAddr
}

// GetGrpcAddr retrieves the gRPC address for a node's Raft address
func (s *KVStore) GetGrpcAddr(raftAddr string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Peers[raftAddr]
}

// isDuplicate checks if a command with the given client ID and sequence number
// has already been applied. This prevents duplicate message processing (Slide 6: Duplicate Messages).
func (s *KVStore) isDuplicate(clientID string, seqNum uint64) bool {
	if clientID == "" {
		return false // No client ID means no idempotency tracking (backward compatible)
	}
	lastSeq, exists := s.lastApplied[clientID]
	return exists && seqNum <= lastSeq
}

// recordApplied records a client's latest applied sequence number
func (s *KVStore) recordApplied(clientID string, seqNum uint64) {
	if clientID != "" {
		s.lastApplied[clientID] = seqNum
	}
}

// The following methods implement the raft.FSM interface

// Apply applies a Raft log entry to the state machine.
// For set/delete operations, it checks for duplicate requests using client ID and sequence numbers
// to ensure idempotent behavior (exactly-once semantics on retries).
func (s *KVStore) Apply(l *raft.Log) interface{} {
	var c command
	if err := json.Unmarshal(l.Data, &c); err != nil {
		panic(fmt.Sprintf("failed to unmarshal command: %s", err.Error()))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch c.Op {
	case "set":
		// Idempotency check: skip if this client+sequence was already applied
		if s.isDuplicate(c.ClientID, c.SeqNum) {
			log.Printf("[FSM] Skipping duplicate set: client=%s seq=%d", c.ClientID, c.SeqNum)
			return nil
		}
		s.m[c.Key] = c.Value
		s.recordApplied(c.ClientID, c.SeqNum)
	case "delete":
		if s.isDuplicate(c.ClientID, c.SeqNum) {
			log.Printf("[FSM] Skipping duplicate delete: client=%s seq=%d", c.ClientID, c.SeqNum)
			return nil
		}
		delete(s.m, c.Key)
		s.recordApplied(c.ClientID, c.SeqNum)
	case "register":
		s.Peers[c.Key] = c.Value // Key=RaftAddr, Value=GrpcAddr
	default:
		panic(fmt.Sprintf("unrecognized command op: %s", c.Op))
	}

	return nil
}

// Snapshot returns a point-in-time snapshot of the KVStore's data
func (s *KVStore) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Clone all maps so that snapshotting doesn't block future writes
	o := make(map[string]string)
	for k, v := range s.m {
		o[k] = v
	}
	p := make(map[string]string)
	for k, v := range s.Peers {
		p[k] = v
	}
	la := make(map[string]uint64)
	for k, v := range s.lastApplied {
		la[k] = v
	}
	return &fsmSnapshot{store: o, peers: p, lastApplied: la}, nil
}

// Restore restores the KVStore from a snapshot
func (s *KVStore) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var snap struct {
		Store       map[string]string `json:"store"`
		Peers       map[string]string `json:"peers"`
		LastApplied map[string]uint64 `json:"last_applied"`
	}

	if err := json.NewDecoder(rc).Decode(&snap); err != nil {
		return err
	}

	// Set the state from the snapshot, overriding current state
	s.mu.Lock()
	defer s.mu.Unlock()
	if snap.Store != nil {
		s.m = snap.Store
	}
	if snap.Peers != nil {
		s.Peers = snap.Peers
	}
	if snap.LastApplied != nil {
		s.lastApplied = snap.LastApplied
	}
	return nil
}

// --- Raft State Machine Types ---

type command struct {
	Op       string `json:"op,omitempty"`
	Key      string `json:"key,omitempty"`
	Value    string `json:"value,omitempty"`
	ClientID string `json:"client_id,omitempty"` // For idempotency
	SeqNum   uint64 `json:"seq_num,omitempty"`   // For idempotency
}

type fsmSnapshot struct {
	store       map[string]string
	peers       map[string]string
	lastApplied map[string]uint64
}

// Persist writes the snapshot to a sink
func (f *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode data into JSON
		snap := struct {
			Store       map[string]string `json:"store"`
			Peers       map[string]string `json:"peers"`
			LastApplied map[string]uint64 `json:"last_applied"`
		}{
			Store:       f.store,
			Peers:       f.peers,
			LastApplied: f.lastApplied,
		}

		b, err := json.Marshal(snap)
		if err != nil {
			return err
		}

		// Write to sink
		if _, err := sink.Write(b); err != nil {
			return err
		}
		return sink.Close()
	}()

	if err != nil {
		sink.Cancel()
	}
	return err
}

// Release releases any resources used by the snapshot (in this memory-only case, nothing)
func (f *fsmSnapshot) Release() {}
