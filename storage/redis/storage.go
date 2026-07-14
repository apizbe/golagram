// Package redisstorage is a Redis-backed [gg.FSMStorage] implementation —
// the first persistent conversation-state backend for golagram bots
// (MemoryStorage, the only other implementation, doesn't survive a
// restart). Requires Redis >= 6.2.0 (uses GETEX).
//
//	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	storage := redisstorage.New(client, redisstorage.WithPrefix("mybot"), redisstorage.WithTTL(24*time.Hour))
//	bot, _ := gg.NewTelegramBot(token, gg.WithFSMStorage(storage))
//
// Storage never closes client — the caller constructed it and owns its
// lifecycle (connection pooling, Close, ...).
package redisstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/redis/go-redis/v9"

	gg "github.com/apizbe/golagram"
)

// Storage implements [gg.FSMStorage] against a Redis server.
type Storage struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// Option configures a [Storage] built by [New].
type Option func(*Storage)

// WithPrefix sets the namespace prefix for every key this Storage writes
// (default: "golagram"). Use a distinct prefix per bot if multiple bots
// share one Redis server/database.
func WithPrefix(prefix string) Option {
	return func(s *Storage) { s.prefix = prefix }
}

// WithTTL sets a sliding expiration for stored state and data: every read
// (GetState/GetData, and the read half of UpdateData) refreshes the TTL,
// so an active conversation never expires mid-flow, but an abandoned one
// is reclaimed automatically. Default is 0, meaning no expiration.
func WithTTL(ttl time.Duration) Option {
	return func(s *Storage) { s.ttl = ttl }
}

// New builds a Storage using client. client's connection lifecycle
// (pooling, Close) remains the caller's responsibility.
func New(client *redis.Client, opts ...Option) *Storage {
	s := &Storage{client: client, prefix: "golagram"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Storage) stateKey(key gg.StorageKey) string {
	return fmt.Sprintf("%s:%d:%d:%d:state", s.prefix, key.ChatID, key.UserID, key.ThreadID)
}

func (s *Storage) dataKey(key gg.StorageKey) string {
	return fmt.Sprintf("%s:%d:%d:%d:data", s.prefix, key.ChatID, key.UserID, key.ThreadID)
}

// SetState implements [gg.FSMStorage]. Setting [gg.NoState] deletes the key
// instead of storing an empty string.
func (s *Storage) SetState(ctx context.Context, key gg.StorageKey, state gg.State) error {
	if state == gg.NoState {
		return s.client.Del(ctx, s.stateKey(key)).Err()
	}
	return s.client.Set(ctx, s.stateKey(key), string(state), s.ttl).Err()
}

// GetState implements [gg.FSMStorage]. A missing key returns
// ([gg.NoState], nil), not an error.
func (s *Storage) GetState(ctx context.Context, key gg.StorageKey) (gg.State, error) {
	val, err := s.client.GetEx(ctx, s.stateKey(key), s.ttl).Result()
	if err == redis.Nil {
		return gg.NoState, nil
	}
	if err != nil {
		return "", err
	}
	return gg.State(val), nil
}

// SetData implements [gg.FSMStorage], storing data as one JSON blob.
func (s *Storage) SetData(ctx context.Context, key gg.StorageKey, data map[string]any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.dataKey(key), encoded, s.ttl).Err()
}

// GetData implements [gg.FSMStorage]. A missing key returns an empty,
// non-nil map, not an error.
func (s *Storage) GetData(ctx context.Context, key gg.StorageKey) (map[string]any, error) {
	val, err := s.client.GetEx(ctx, s.dataKey(key), s.ttl).Result()
	if err == redis.Nil {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	return data, nil
}

// UpdateData implements [gg.FSMStorage] via read-merge-write: GetEx
// (refreshing the TTL), merge partial over the existing map, marshal, and
// Set with the same TTL. This is not atomic across processes — per
// [gg.FSMStorage]'s documented contract, that's fine, because the bot's own
// per-key dispatch lock already serializes updates that share a storage
// key within one process.
func (s *Storage) UpdateData(ctx context.Context, key gg.StorageKey, partial map[string]any) (map[string]any, error) {
	existing, err := s.GetData(ctx, key)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]any, len(existing)+len(partial))
	maps.Copy(merged, existing)
	maps.Copy(merged, partial)
	if err := s.SetData(ctx, key, merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// Clear implements [gg.FSMStorage], deleting both the state and data keys.
// Clearing an already-empty key is not an error.
func (s *Storage) Clear(ctx context.Context, key gg.StorageKey) error {
	return s.client.Del(ctx, s.stateKey(key), s.dataKey(key)).Err()
}
