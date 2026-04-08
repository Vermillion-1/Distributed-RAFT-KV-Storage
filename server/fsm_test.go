package server

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/hashicorp/raft"
)

// makeLog is a helper that creates a raft.Log from a command.
func makeLog(op, key, value, clientID string, seqNum uint64) *raft.Log {
	c := command{
		Op:       op,
		Key:      key,
		Value:    value,
		ClientID: clientID,
		SeqNum:   seqNum,
	}
	b, _ := json.Marshal(c)
	return &raft.Log{Data: b}
}

// --- Basic Operations ---

func TestSetAndGet(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "foo", "bar", "", 0))

	val, ok := kv.Get("foo")
	if !ok {
		t.Fatal("expected key 'foo' to exist")
	}
	if val != "bar" {
		t.Fatalf("expected 'bar', got %q", val)
	}
}

func TestGetMissingKey(t *testing.T) {
	kv := NewKVStore()

	_, ok := kv.Get("missing")
	if ok {
		t.Fatal("expected key 'missing' to not exist")
	}
}

func TestDelete(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "foo", "bar", "", 0))
	kv.Apply(makeLog("delete", "foo", "", "", 0))

	_, ok := kv.Get("foo")
	if ok {
		t.Fatal("expected key 'foo' to be deleted")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	// Deleting a key that doesn't exist should not panic or error
	kv := NewKVStore()
	kv.Apply(makeLog("delete", "nope", "", "", 0))

	_, ok := kv.Get("nope")
	if ok {
		t.Fatal("expected key 'nope' to not exist")
	}
}

func TestOverwrite(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "key", "v1", "", 0))
	kv.Apply(makeLog("set", "key", "v2", "", 0))

	val, ok := kv.Get("key")
	if !ok || val != "v2" {
		t.Fatalf("expected 'v2', got %q (found=%v)", val, ok)
	}
}

// --- Idempotency ---

func TestIdempotentSet(t *testing.T) {
	kv := NewKVStore()

	// First apply sets value
	kv.Apply(makeLog("set", "key", "first", "client-1", 1))
	val, _ := kv.Get("key")
	if val != "first" {
		t.Fatalf("expected 'first', got %q", val)
	}

	// Duplicate with same client+seq should be ignored
	kv.Apply(makeLog("set", "key", "duplicate", "client-1", 1))
	val, _ = kv.Get("key")
	if val != "first" {
		t.Fatalf("duplicate was not skipped: expected 'first', got %q", val)
	}
}

func TestIdempotentSetOlderSeq(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "key", "v1", "client-1", 5))

	// Older sequence number should also be skipped
	kv.Apply(makeLog("set", "key", "old", "client-1", 3))
	val, _ := kv.Get("key")
	if val != "v1" {
		t.Fatalf("older seq was not skipped: expected 'v1', got %q", val)
	}
}

func TestIdempotentSetNewSeq(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "key", "v1", "client-1", 1))

	// Higher sequence number should be applied
	kv.Apply(makeLog("set", "key", "v2", "client-1", 2))
	val, _ := kv.Get("key")
	if val != "v2" {
		t.Fatalf("new seq was not applied: expected 'v2', got %q", val)
	}
}

func TestIdempotentDelete(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "key", "value", "client-1", 1))
	kv.Apply(makeLog("delete", "key", "", "client-1", 2))

	_, ok := kv.Get("key")
	if ok {
		t.Fatal("expected key to be deleted")
	}

	// Re-set with newer seq, then try duplicate delete (old seq)
	kv.Apply(makeLog("set", "key", "back", "client-1", 3))
	kv.Apply(makeLog("delete", "key", "", "client-1", 2)) // should be skipped (old seq)

	val, ok := kv.Get("key")
	if !ok || val != "back" {
		t.Fatalf("duplicate delete was not skipped: found=%v, val=%q", ok, val)
	}
}

func TestDifferentClientsSameSeq(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("set", "key", "from-A", "clientA", 1))
	kv.Apply(makeLog("set", "key", "from-B", "clientB", 1))

	// Different clients should not interfere with each other's idempotency
	val, _ := kv.Get("key")
	if val != "from-B" {
		t.Fatalf("expected 'from-B', got %q", val)
	}
}

func TestNoClientIDBypassesIdempotency(t *testing.T) {
	kv := NewKVStore()

	// Without client ID, every apply should go through (backward compatible)
	kv.Apply(makeLog("set", "key", "v1", "", 0))
	kv.Apply(makeLog("set", "key", "v2", "", 0))

	val, _ := kv.Get("key")
	if val != "v2" {
		t.Fatalf("expected 'v2' (no idempotency without client ID), got %q", val)
	}
}

// --- Peer Registration ---

func TestRegisterNode(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("register", "127.0.0.1:12000", "127.0.0.1:50051", "", 0))

	addr := kv.GetGrpcAddr("127.0.0.1:12000")
	if addr != "127.0.0.1:50051" {
		t.Fatalf("expected '127.0.0.1:50051', got %q", addr)
	}
}

func TestRegisterNodeOverwrite(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLog("register", "127.0.0.1:12000", "127.0.0.1:50051", "", 0))
	kv.Apply(makeLog("register", "127.0.0.1:12000", "127.0.0.1:60051", "", 0))

	addr := kv.GetGrpcAddr("127.0.0.1:12000")
	if addr != "127.0.0.1:60051" {
		t.Fatalf("expected overwritten address '127.0.0.1:60051', got %q", addr)
	}
}

// --- Unknown Command ---

func TestUnknownCommandDoesNotPanic(t *testing.T) {
	kv := NewKVStore()

	// Should log a warning but not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unknown command caused a panic: %v", r)
		}
	}()

	kv.Apply(makeLog("foobar", "key", "value", "", 0))
}

// --- Snapshot & Restore ---

func TestSnapshotAndRestore(t *testing.T) {
	kv := NewKVStore()

	// Populate state
	kv.Apply(makeLog("set", "key1", "val1", "client-1", 1))
	kv.Apply(makeLog("set", "key2", "val2", "client-2", 5))
	kv.Apply(makeLog("register", "127.0.0.1:12000", "127.0.0.1:50051", "", 0))

	// Take snapshot
	snap, err := kv.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	// Serialize snapshot
	var buf bytes.Buffer
	sink := &mockSink{Writer: &buf}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("persist failed: %v", err)
	}

	// Restore into a fresh KVStore
	kv2 := NewKVStore()
	if err := kv2.Restore(io.NopCloser(&buf)); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	// Verify data
	val, ok := kv2.Get("key1")
	if !ok || val != "val1" {
		t.Fatalf("expected key1=val1, got %q (found=%v)", val, ok)
	}
	val, ok = kv2.Get("key2")
	if !ok || val != "val2" {
		t.Fatalf("expected key2=val2, got %q (found=%v)", val, ok)
	}

	// Verify peers
	addr := kv2.GetGrpcAddr("127.0.0.1:12000")
	if addr != "127.0.0.1:50051" {
		t.Fatalf("expected peer addr '127.0.0.1:50051', got %q", addr)
	}

	// Verify idempotency state was restored: duplicate should be skipped
	kv2.Apply(makeLog("set", "key1", "should-be-skipped", "client-1", 1))
	val, _ = kv2.Get("key1")
	if val != "val1" {
		t.Fatalf("idempotency not restored: expected 'val1', got %q", val)
	}

	// But a new seq from the same client should work
	kv2.Apply(makeLog("set", "key1", "updated", "client-1", 2))
	val, _ = kv2.Get("key1")
	if val != "updated" {
		t.Fatalf("new seq after restore not applied: expected 'updated', got %q", val)
	}
}

// --- Multiple Keys ---

func TestMultipleKeys(t *testing.T) {
	kv := NewKVStore()

	for i := 0; i < 100; i++ {
		key := "key" + string(rune('A'+i%26))
		val := "val" + string(rune('0'+i%10))
		kv.Apply(makeLog("set", key, val, "", 0))
	}

	// Spot-check a few
	val, ok := kv.Get("keyA")
	if !ok {
		t.Fatal("expected keyA to exist")
	}
	_ = val // value was overwritten multiple times, just check it exists
}

// --- Mock Sink for Snapshot Testing ---

type mockSink struct {
	Writer  *bytes.Buffer
	closed  bool
	cancel  bool
}

func (m *mockSink) Write(p []byte) (n int, err error) {
	return m.Writer.Write(p)
}

func (m *mockSink) Close() error {
	m.closed = true
	return nil
}

func (m *mockSink) Cancel() error {
	m.cancel = true
	return nil
}

func (m *mockSink) ID() string {
	return "mock-snapshot"
}

// --- Read-Index: appliedIndex tracking ---

// makeLogIdx creates a raft.Log with an explicit Raft log index (needed for read-index tests).
func makeLogIdx(op, key, value, clientID string, seqNum uint64, index uint64) *raft.Log {
	c := command{Op: op, Key: key, Value: value, ClientID: clientID, SeqNum: seqNum}
	b, _ := json.Marshal(c)
	return &raft.Log{Data: b, Index: index}
}

// TestAppliedIndexTracking verifies that Apply() updates AppliedIndex() with the log's Index field.
func TestAppliedIndexTracking(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLogIdx("set", "k", "v1", "", 0, 1))
	if got := kv.AppliedIndex(); got != 1 {
		t.Fatalf("expected appliedIndex=1, got %d", got)
	}
	kv.Apply(makeLogIdx("set", "k", "v2", "", 0, 2))
	if got := kv.AppliedIndex(); got != 2 {
		t.Fatalf("expected appliedIndex=2, got %d", got)
	}
	kv.Apply(makeLogIdx("set", "k", "v3", "", 0, 3))
	if got := kv.AppliedIndex(); got != 3 {
		t.Fatalf("expected appliedIndex=3, got %d", got)
	}
}

// TestWaitForIndexImmediate verifies that WaitForIndex returns immediately when the FSM
// is already at or past the requested index.
func TestWaitForIndexImmediate(t *testing.T) {
	kv := NewKVStore()

	kv.Apply(makeLogIdx("set", "k", "v", "", 0, 5))

	if err := kv.WaitForIndex(3, 1*time.Second); err != nil {
		t.Fatalf("WaitForIndex(3) should succeed immediately when at index 5: %v", err)
	}
	if err := kv.WaitForIndex(5, 1*time.Second); err != nil {
		t.Fatalf("WaitForIndex(5) should succeed immediately when at index 5: %v", err)
	}
}

// TestWaitForIndexTimeout verifies that WaitForIndex returns an error when no Apply()
// calls arrive within the deadline.
func TestWaitForIndexTimeout(t *testing.T) {
	kv := NewKVStore()

	// No Apply calls — FSM stays at index 0 forever.
	err := kv.WaitForIndex(5, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestWaitForIndexCatchUp verifies that a goroutine blocked in WaitForIndex unblocks
// once Apply() calls bring the FSM up to the required index.
func TestWaitForIndexCatchUp(t *testing.T) {
	kv := NewKVStore()

	done := make(chan error, 1)
	go func() {
		done <- kv.WaitForIndex(3, 2*time.Second)
	}()

	// Small delay to let the goroutine enter WaitForIndex and block.
	time.Sleep(20 * time.Millisecond)

	kv.Apply(makeLogIdx("set", "k", "v1", "", 0, 1))
	kv.Apply(makeLogIdx("set", "k", "v2", "", 0, 2))
	kv.Apply(makeLogIdx("set", "k", "v3", "", 0, 3))

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForIndex should have succeeded after catch-up: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForIndex did not unblock after Apply() catch-up")
	}
}

// TestSnapshotRestoreAppliedIdx verifies that appliedIndex is persisted in the snapshot
// and correctly restored, so WaitForIndex works on a fresh KVStore after restore.
func TestSnapshotRestoreAppliedIdx(t *testing.T) {
	kv := NewKVStore()

	// Apply logs up to index 7.
	for i := uint64(1); i <= 7; i++ {
		kv.Apply(makeLogIdx("set", "k", "v", "", 0, i))
	}
	if got := kv.AppliedIndex(); got != 7 {
		t.Fatalf("expected appliedIndex=7 before snapshot, got %d", got)
	}

	// Snapshot and serialize.
	snap, err := kv.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	var buf bytes.Buffer
	sink := &mockSink{Writer: &buf}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("persist failed: %v", err)
	}

	// Restore into a fresh KVStore.
	kv2 := NewKVStore()
	if err := kv2.Restore(io.NopCloser(&buf)); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	if got := kv2.AppliedIndex(); got != 7 {
		t.Fatalf("expected appliedIndex=7 after restore, got %d", got)
	}
	// WaitForIndex(7) should return immediately.
	if err := kv2.WaitForIndex(7, 100*time.Millisecond); err != nil {
		t.Fatalf("WaitForIndex(7) should succeed immediately after restore: %v", err)
	}
}
