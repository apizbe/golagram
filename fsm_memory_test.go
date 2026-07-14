package golagram

import (
	"context"
	"testing"
	"time"
)

// entryExists checks the map directly instead of going through GetState/
// GetData, both of which refresh lastAccess on every call — polling with
// those would itself keep the entry alive and the test would never see it
// expire.
func entryExists(s *MemoryStorage, key StorageKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[key]
	return ok
}

// waitUntil polls cond until it returns true or the deadline passes,
// avoiding flaky fixed-sleep assumptions about background goroutine timing.
func waitUntil(t *testing.T, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %v", deadline)
	}
}

func TestMemoryStorageWithTTL_EvictsAfterInactivity(t *testing.T) {
	s := NewMemoryStorageWithTTL(40 * time.Millisecond)
	defer s.Close()

	key := StorageKey{ChatID: 1, UserID: 1}
	s.SetState(context.Background(), key, "waiting_name")
	s.SetData(context.Background(), key, map[string]any{"name": "Alice"})

	waitUntil(t, time.Second, func() bool {
		return !entryExists(s, key)
	})

	data, _ := s.GetData(context.Background(), key)
	if len(data) != 0 {
		t.Errorf("expected data to be evicted alongside state, got %v", data)
	}
}

func TestMemoryStorageWithTTL_SlidingRenewalKeepsActiveEntryAlive(t *testing.T) {
	ttl := 80 * time.Millisecond
	s := NewMemoryStorageWithTTL(ttl)
	defer s.Close()

	key := StorageKey{ChatID: 1, UserID: 1}
	s.SetState(context.Background(), key, "waiting_name")

	// Keep "using" the key for longer than ttl by touching it faster than
	// it can expire. If TTL weren't sliding, this would still get evicted
	// partway through since the total exceeds ttl.
	deadline := time.Now().Add(3 * ttl)
	for time.Now().Before(deadline) {
		if _, err := s.GetState(context.Background(), key); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		time.Sleep(ttl / 4)
	}

	state, err := s.GetState(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "waiting_name" {
		t.Errorf("expected actively-touched entry to survive past ttl, got state=%q", state)
	}
}

func TestMemoryStorageWithTTL_GoesIdleAfterActivityStillExpires(t *testing.T) {
	ttl := 40 * time.Millisecond
	s := NewMemoryStorageWithTTL(ttl)
	defer s.Close()

	key := StorageKey{ChatID: 1, UserID: 1}
	s.SetState(context.Background(), key, "waiting_name")
	s.GetState(context.Background(), key) // a bit of activity, then go idle
	s.GetState(context.Background(), key)

	waitUntil(t, time.Second, func() bool {
		return !entryExists(s, key)
	})
}

func TestMemoryStorage_DefaultHasNoTTL_NeverExpires(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}
	s.SetState(context.Background(), key, "waiting_name")

	time.Sleep(100 * time.Millisecond)

	state, err := s.GetState(context.Background(), key)
	if err != nil || state != "waiting_name" {
		t.Errorf("default MemoryStorage should never expire entries, got (%q, %v)", state, err)
	}
}

func TestMemoryStorage_Close_SafeWithoutTTL(t *testing.T) {
	s := NewMemoryStorage()
	s.Close() // should be a no-op, not panic
}

func TestMemoryStorageWithTTL_CloseIsSafeToCallTwice(t *testing.T) {
	s := NewMemoryStorageWithTTL(time.Second)
	s.Close()
	s.Close() // must not panic on double-close
}
