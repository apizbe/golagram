package redisstorage_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	gg "github.com/apizbe/golagram"
	"github.com/apizbe/golagram/fsmtest"
	redisstorage "github.com/apizbe/golagram/storage/redis"
)

func newMiniredisClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return client, mr
}

func TestStorage_Conformance(t *testing.T) {
	client, _ := newMiniredisClient(t)
	fsmtest.Run(t, func(t *testing.T) gg.FSMStorage {
		return redisstorage.New(client, redisstorage.WithPrefix(t.Name()))
	})
}

func TestStorage_KeyLayout(t *testing.T) {
	client, mr := newMiniredisClient(t)
	s := redisstorage.New(client, redisstorage.WithPrefix("mybot"))
	ctx := context.Background()
	key := gg.StorageKey{ChatID: 10, UserID: 20, ThreadID: 3}

	if err := s.SetState(ctx, key, "waiting_name"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetData(ctx, key, map[string]any{"x": 1}); err != nil {
		t.Fatalf("SetData: %v", err)
	}

	keys := mr.Keys()
	wantState, wantData := "mybot:10:20:3:state", "mybot:10:20:3:data"
	var haveState, haveData bool
	for _, k := range keys {
		if k == wantState {
			haveState = true
		}
		if k == wantData {
			haveData = true
		}
	}
	if !haveState {
		t.Errorf("expected key %q in %v", wantState, keys)
	}
	if !haveData {
		t.Errorf("expected key %q in %v", wantData, keys)
	}
}

func TestStorage_WithTTL_SetsExpiry(t *testing.T) {
	client, mr := newMiniredisClient(t)
	s := redisstorage.New(client, redisstorage.WithPrefix("ttltest"), redisstorage.WithTTL(time.Minute))
	ctx := context.Background()
	key := gg.StorageKey{ChatID: 1, UserID: 2}

	if err := s.SetState(ctx, key, "step1"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	ttl := mr.TTL("ttltest:1:2:0:state")
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL = %v, want a positive duration <= 1m", ttl)
	}
}

func TestStorage_WithTTL_ReadsSlideExpiryForward(t *testing.T) {
	client, mr := newMiniredisClient(t)
	s := redisstorage.New(client, redisstorage.WithPrefix("slide"), redisstorage.WithTTL(time.Minute))
	ctx := context.Background()
	key := gg.StorageKey{ChatID: 1, UserID: 2}

	if err := s.SetState(ctx, key, "step1"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	mr.FastForward(45 * time.Second)
	if _, err := s.GetState(ctx, key); err != nil {
		t.Fatalf("GetState: %v", err)
	}
	// The read at t=45s should have refreshed the TTL back to a full
	// minute — advancing another 45s (t=90s total, past the original
	// deadline at t=60s) must not have expired the key.
	mr.FastForward(45 * time.Second)
	state, err := s.GetState(ctx, key)
	if err != nil {
		t.Fatalf("GetState after fast-forward: %v", err)
	}
	if state != "step1" {
		t.Errorf("state = %q, want %q — a read should have slid the TTL forward", state, "step1")
	}
}

func TestStorage_WithoutTTL_NeverExpires(t *testing.T) {
	client, mr := newMiniredisClient(t)
	s := redisstorage.New(client, redisstorage.WithPrefix("noexpiry"))
	ctx := context.Background()
	key := gg.StorageKey{ChatID: 1, UserID: 2}

	if err := s.SetState(ctx, key, "step1"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if ttl := mr.TTL("noexpiry:1:2:0:state"); ttl != 0 {
		t.Errorf("TTL = %v, want 0 (no expiry) when WithTTL isn't set", ttl)
	}
}

func TestStorage_UpdateData_MergesAcrossReads(t *testing.T) {
	client, _ := newMiniredisClient(t)
	s := redisstorage.New(client, redisstorage.WithPrefix("merge"))
	ctx := context.Background()
	key := gg.StorageKey{ChatID: 1, UserID: 2}

	if err := s.SetData(ctx, key, map[string]any{"a": float64(1)}); err != nil {
		t.Fatalf("SetData: %v", err)
	}
	merged, err := s.UpdateData(ctx, key, map[string]any{"b": float64(2)})
	if err != nil {
		t.Fatalf("UpdateData: %v", err)
	}
	if merged["a"] != float64(1) || merged["b"] != float64(2) {
		t.Errorf("merged = %v, want a=1 b=2", merged)
	}

	stored, err := s.GetData(ctx, key)
	if err != nil {
		t.Fatalf("GetData: %v", err)
	}
	if stored["a"] != float64(1) || stored["b"] != float64(2) {
		t.Errorf("stored = %v, want a=1 b=2", stored)
	}
}
