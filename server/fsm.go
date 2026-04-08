package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/hashicorp/raft"
)

// clientEntry tracks both the last sequence number and when it was last seen,
// allowing periodic cleanup of stale entries to prevent unbounded memory growth.
type clientEntry struct {
	SeqNum   uint64
	LastSeen time.Time
}

// KVStore represents the in-memory key-value store and implements the raft.FSM interface.
// It tracks per-client sequence numbers for idempotent request handling (Duplicate Message prevention).
type KVStore struct {
	mu          sync.RWMutex
	m           map[string]string       // The actual map holding the data
	Peers       map[string]string       // Maps Raft Address -> gRPC Address for client redirects
	lastApplied map[string]*clientEntry // Maps ClientID -> last applied entry (idempotency with TTL)

	// read-index tracking — separate mutex so WaitForIndex never blocks concurrent Gets
	indexMu      sync.Mutex
	indexCond    *sync.Cond
	appliedIndex uint64
}

// NewKVStore creates a new KVStore
func NewKVStore() *KVStore {
	kv := &KVStore{
		m:           make(map[string]string),
		Peers:       make(map[string]string),
		lastApplied: make(map[string]*clientEntry),
	}
	kv.indexCond = sync.NewCond(&kv.indexMu)
	// Start background goroutine to evict stale client idempotency entries (older than 10 minutes).
	// This prevents unbounded memory growth from the lastApplied map.
	go kv.evictStaleClients(10*time.Minute, 1*time.Minute)
	return kv
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

// WaitForIndex blocks until the FSM has applied at least idx, or until timeout.
// Used by the follower read-index path to ensure the local FSM is caught up
// before serving a linearizable read.
func (s *KVStore) WaitForIndex(idx uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	// AfterFunc wakes the cond when the timeout fires so Wait() never blocks indefinitely.
	timer := time.AfterFunc(timeout, func() { s.indexCond.Broadcast() })
	defer timer.Stop()
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	for s.appliedIndex < idx {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for index %d (current applied: %d)", idx, s.appliedIndex)
		}
		s.indexCond.Wait()
	}
	return nil
}

// AppliedIndex returns the last Raft log index applied to this FSM.
func (s *KVStore) AppliedIndex() uint64 {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	return s.appliedIndex
}

// isDuplicate checks if a command with the given client ID and sequence number
// has already been applied. This prevents duplicate message processing.
// Note: Clients should send monotonically increasing sequence numbers.
// - Same seq (retries): duplicate, skip
// - Lower seq: client bug/duplicate, skip
// - Higher seq: new request, apply
func (s *KVStore) isDuplicate(clientID string, seqNum uint64) bool {
	if clientID == "" {
		return false // No client ID means no idempotency tracking (backward compatible)
	}
	entry, exists := s.lastApplied[clientID]
	return exists && seqNum <= entry.SeqNum
}

// recordApplied records a client's latest applied sequence number
func (s *KVStore) recordApplied(clientID string, seqNum uint64) {
	if clientID != "" {
		s.lastApplied[clientID] = &clientEntry{SeqNum: seqNum, LastSeen: time.Now()}
	}
}

// evictStaleClients periodically removes client entries older than maxAge
// to prevent the lastApplied map from growing without bound.
func (s *KVStore) evictStaleClients(maxAge, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		cutoff := time.Now().Add(-maxAge)
		for id, entry := range s.lastApplied {
			if entry.LastSeen.Before(cutoff) {
				delete(s.lastApplied, id)
			}
		}
		s.mu.Unlock()
	}
}

// The following methods implement the raft.FSM interface

// Apply applies a Raft log entry to the state machine.
// For set/delete operations, it checks for duplicate requests using client ID and sequence numbers
// to ensure idempotent behavior (exactly-once semantics on retries).
func (s *KVStore) Apply(l *raft.Log) interface{} {
	var c command
	if err := json.Unmarshal(l.Data, &c); err != nil {
		log.Panicf("failed to unmarshal command: %s", err.Error())
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
		log.Printf("[FSM] WARNING: unrecognized command op: %q (ignored)", c.Op)
	}

	// Update applied index for read-index reads. Done after the data write so
	// WaitForIndex callers always see the data when they unblock.
	s.indexMu.Lock()
	s.appliedIndex = l.Index
	s.indexMu.Unlock()
	s.indexCond.Broadcast()

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
		la[k] = v.SeqNum
	}
	s.indexMu.Lock()
	ai := s.appliedIndex
	s.indexMu.Unlock()
	return &fsmSnapshot{store: o, peers: p, lastApplied: la, appliedIndex: ai}, nil
}

// Restore restores the KVStore from a snapshot
func (s *KVStore) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var snap struct {
		Store        map[string]string `json:"store"`
		Peers        map[string]string `json:"peers"`
		LastApplied  map[string]uint64 `json:"last_applied"`
		AppliedIndex uint64            `json:"applied_index"`
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
		s.lastApplied = make(map[string]*clientEntry, len(snap.LastApplied))
		for k, v := range snap.LastApplied {
			s.lastApplied[k] = &clientEntry{SeqNum: v, LastSeen: time.Now()}
		}
	}
	// Restore the applied index so WaitForIndex callers don't time out on a
	// quiescent cluster where no new Apply() calls arrive after restore.
	s.indexMu.Lock()
	s.appliedIndex = snap.AppliedIndex
	s.indexMu.Unlock()
	s.indexCond.Broadcast()
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
	store        map[string]string
	peers        map[string]string
	lastApplied  map[string]uint64
	appliedIndex uint64
}

// Persist writes the snapshot to a sink
func (f *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode data into JSON
		snap := struct {
			Store        map[string]string `json:"store"`
			Peers        map[string]string `json:"peers"`
			LastApplied  map[string]uint64 `json:"last_applied"`
			AppliedIndex uint64            `json:"applied_index"`
		}{
			Store:        f.store,
			Peers:        f.peers,
			LastApplied:  f.lastApplied,
			AppliedIndex: f.appliedIndex,
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
