package golagram

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type fsmEntry struct {
	state      State
	data       map[string]any
	lastAccess time.Time
}

// MemoryStorage is an in-process [FSMStorage]. It's the default storage
// every [TelegramBot] starts with — state is lost on restart, which is fine
// for development and many simple bots; swap in a persistent FSMStorage via
// [TelegramBot.SetFSMStorage] for production use across restarts.
//
// By default entries live forever until a handler calls [MemoryStorage.Clear]
// — fine for short-lived dev bots, but a long-running one will accumulate
// an entry for every user who ever touched FSM and never finished. Use
// [NewMemoryStorageWithTTL] to evict idle conversations automatically.
type MemoryStorage struct {
	mu          sync.Mutex
	entries     map[StorageKey]*fsmEntry
	ttl         time.Duration
	stopCleanup chan struct{}
	closeOnce   sync.Once
}

// NewMemoryStorage creates an in-process [MemoryStorage] with no expiry —
// entries live until a handler calls [MemoryStorage.Clear] or the process
// exits.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		entries: make(map[StorageKey]*fsmEntry),
	}
}

// NewMemoryStorageWithTTL evicts an entry once it's gone untouched for ttl.
// The TTL is sliding, not a fixed countdown from creation: every read or
// write on a key resets its clock, so a user actively working through a
// conversation never gets cut off mid-flow — only one that's gone idle for
// the full ttl gets cleaned up. Call [MemoryStorage.Close] when done to
// stop the background sweep goroutine.
func NewMemoryStorageWithTTL(ttl time.Duration) *MemoryStorage {
	s := &MemoryStorage{
		entries:     make(map[StorageKey]*fsmEntry),
		ttl:         ttl,
		stopCleanup: make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Close stops the background eviction goroutine started by
// [NewMemoryStorageWithTTL]. Safe to call on a storage without TTL enabled,
// and safe to call more than once.
func (s *MemoryStorage) Close() {
	if s.stopCleanup == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.stopCleanup)
	})
}

func (s *MemoryStorage) cleanupLoop() {
	interval := s.ttl / 2
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCleanup:
			return
		case <-ticker.C:
			s.evictExpired()
		}
	}
}

func (s *MemoryStorage) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, e := range s.entries {
		if now.Sub(e.lastAccess) > s.ttl {
			delete(s.entries, key)
		}
	}
}

func (s *MemoryStorage) get(key StorageKey) *fsmEntry {
	e, ok := s.entries[key]
	if !ok {
		e = &fsmEntry{data: make(map[string]any)}
		s.entries[key] = e
	}
	e.lastAccess = time.Now()
	return e
}

// SetState implements [FSMStorage].
func (s *MemoryStorage) SetState(_ context.Context, key StorageKey, state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.get(key).state = state
	return nil
}

// GetState implements [FSMStorage].
func (s *MemoryStorage) GetState(_ context.Context, key StorageKey) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return "", nil
	}
	e.lastAccess = time.Now()
	return e.state, nil
}

// SetData implements [FSMStorage].
func (s *MemoryStorage) SetData(_ context.Context, key StorageKey, data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied, err := copyData(data)
	if err != nil {
		return err
	}
	s.get(key).data = copied
	return nil
}

// GetData implements [FSMStorage].
func (s *MemoryStorage) GetData(_ context.Context, key StorageKey) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return map[string]any{}, nil
	}
	e.lastAccess = time.Now()
	return copyData(e.data)
}

// UpdateData implements [FSMStorage].
func (s *MemoryStorage) UpdateData(_ context.Context, key StorageKey, partial map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(key)

	merged := make(map[string]any, len(e.data)+len(partial))
	for k, v := range e.data {
		merged[k] = v
	}
	for k, v := range partial {
		merged[k] = v
	}
	// Canonicalize storage itself through the round-trip too — otherwise a
	// mutable value handed in via partial would sit in e.data by reference,
	// letting the caller mutate stored state after the fact without going
	// through the storage API.
	copied, err := copyData(merged)
	if err != nil {
		return nil, err
	}
	e.data = copied
	return copyData(e.data)
}

// Clear implements [FSMStorage], resetting state and data and ending the
// conversation.
func (s *MemoryStorage) Clear(_ context.Context, key StorageKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

// copyData deep-copies data via a JSON round-trip rather than a shallow
// top-level copy — a nested map/slice in FSM data must not be shared by
// reference between caller and store, or a handler mutating it in place
// corrupts stored state without going through the storage API, and races
// the per-key dispatch lock if the same value leaks to two handlers. This
// also enforces, at write time, the "must be JSON-marshalable" contract
// persistent [FSMStorage] backends already impose via their own JSON
// round-trip — so code that works against MemoryStorage in dev doesn't
// discover a non-marshalable value only after switching backends.
func copyData(data map[string]any) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("golagram: FSM data is not JSON-marshalable: %w", err)
	}
	out := make(map[string]any, len(data))
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("golagram: FSM data round-trip failed: %w", err)
	}
	return out, nil
}
