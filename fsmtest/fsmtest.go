// Package fsmtest is a reusable conformance suite for FSMStorage
// implementations. A storage backend (Redis, Postgres, ...) proves it
// honors the contract [gg.MemoryStorage] sets — including the JSON
// round-trip contract documented on [gg.FSMStorage] — with one test:
//
//	func TestMyStorage_Conformance(t *testing.T) {
//		fsmtest.Run(t, func(t *testing.T) golagram.FSMStorage {
//			return newMyEmptyStorage(t)
//		})
//	}
//
// The suite does not test cross-process atomicity of UpdateData: the bot
// serializes updates sharing a storage key through its per-key dispatch
// lock, so a storage only needs read-merge-write consistency within one
// process.
package fsmtest

import (
	"context"
	"testing"

	gg "github.com/apizbe/golagram"
)

// Run exercises factory's [gg.FSMStorage] against the interface contract.
// factory is called once per subtest and must return an empty storage.
func Run(t *testing.T, factory func(t *testing.T) gg.FSMStorage) {
	ctx := context.Background()
	key := gg.StorageKey{ChatID: 10, UserID: 20}

	t.Run("EmptyDefaults", func(t *testing.T) {
		s := factory(t)
		state, err := s.GetState(ctx, key)
		if err != nil || state != gg.NoState {
			t.Errorf("GetState on empty storage = (%q, %v), want (NoState, nil)", state, err)
		}
		data, err := s.GetData(ctx, key)
		if err != nil || len(data) != 0 {
			t.Errorf("GetData on empty storage = (%v, %v), want (empty, nil)", data, err)
		}
	})

	t.Run("SetGetState", func(t *testing.T) {
		s := factory(t)
		if err := s.SetState(ctx, key, "reg:waiting_name"); err != nil {
			t.Fatalf("SetState: %v", err)
		}
		if state, err := s.GetState(ctx, key); err != nil || state != "reg:waiting_name" {
			t.Errorf("GetState = (%q, %v), want (reg:waiting_name, nil)", state, err)
		}
		if err := s.SetState(ctx, key, "reg:waiting_age"); err != nil {
			t.Fatalf("SetState overwrite: %v", err)
		}
		if state, _ := s.GetState(ctx, key); state != "reg:waiting_age" {
			t.Errorf("GetState after overwrite = %q, want reg:waiting_age", state)
		}
	})

	t.Run("SetStateToNoStateIsAllowed", func(t *testing.T) {
		s := factory(t)
		s.SetState(ctx, key, "reg:waiting_name")
		if err := s.SetState(ctx, key, gg.NoState); err != nil {
			t.Fatalf("SetState(NoState): %v", err)
		}
		if state, err := s.GetState(ctx, key); err != nil || state != gg.NoState {
			t.Errorf("GetState = (%q, %v), want (NoState, nil)", state, err)
		}
	})

	t.Run("SetGetData", func(t *testing.T) {
		s := factory(t)
		if err := s.SetData(ctx, key, map[string]any{"name": "Alice", "age": 30}); err != nil {
			t.Fatalf("SetData: %v", err)
		}
		data, err := s.GetData(ctx, key)
		if err != nil {
			t.Fatalf("GetData: %v", err)
		}
		// Compare through FSMGet: a storage may return the JSON image of
		// what was stored (30 → float64), and FSMGet is the documented way
		// to read type-stably across backends.
		f := gg.NewFSMContext(ctx, s, key)
		if name, ok, err := gg.FSMGet[string](f, "name"); err != nil || !ok || name != "Alice" {
			t.Errorf("FSMGet[string](name) = (%q, %v, %v), want (Alice, true, nil)", name, ok, err)
		}
		if age, ok, err := gg.FSMGet[int](f, "age"); err != nil || !ok || age != 30 {
			t.Errorf("FSMGet[int](age) = (%d, %v, %v), want (30, true, nil)", age, ok, err)
		}
		if len(data) != 2 {
			t.Errorf("GetData returned %d entries, want 2", len(data))
		}
	})

	t.Run("SetDataReplacesEntirely", func(t *testing.T) {
		s := factory(t)
		s.SetData(ctx, key, map[string]any{"name": "Alice", "age": 30})
		s.SetData(ctx, key, map[string]any{"city": "Tashkent"})
		data, _ := s.GetData(ctx, key)
		if len(data) != 1 || data["city"] == nil {
			t.Errorf("SetData must replace the whole map, got %v", data)
		}
	})

	t.Run("UpdateDataMergesAndReturnsFullMap", func(t *testing.T) {
		s := factory(t)
		s.SetData(ctx, key, map[string]any{"name": "Alice"})
		merged, err := s.UpdateData(ctx, key, map[string]any{"age": 30})
		if err != nil {
			t.Fatalf("UpdateData: %v", err)
		}
		if len(merged) != 2 {
			t.Errorf("UpdateData returned %v, want both name and age", merged)
		}
		f := gg.NewFSMContext(ctx, s, key)
		if age, ok, _ := gg.FSMGet[int](f, "age"); !ok || age != 30 {
			t.Errorf("age after UpdateData = (%d, %v), want (30, true)", age, ok)
		}
		if name, ok, _ := gg.FSMGet[string](f, "name"); !ok || name != "Alice" {
			t.Errorf("name must survive UpdateData, got (%q, %v)", name, ok)
		}
	})

	t.Run("UpdateDataOnEmptyKey", func(t *testing.T) {
		s := factory(t)
		merged, err := s.UpdateData(ctx, key, map[string]any{"age": 30})
		if err != nil || len(merged) != 1 {
			t.Errorf("UpdateData on empty key = (%v, %v), want single-entry map", merged, err)
		}
	})

	t.Run("KeysAreIsolated", func(t *testing.T) {
		s := factory(t)
		others := []gg.StorageKey{
			{ChatID: 10, UserID: 99},              // same chat, other user
			{ChatID: 99, UserID: 20},              // other chat, same user
			{ChatID: 10, UserID: 20, ThreadID: 7}, // same pair, forum topic
			{UserID: 20},                          // global-user scope
			{ChatID: 10},                          // chat scope
		}
		s.SetState(ctx, key, "reg:waiting_name")
		s.SetData(ctx, key, map[string]any{"name": "Alice"})
		for _, other := range others {
			if state, _ := s.GetState(ctx, other); state != gg.NoState {
				t.Errorf("key %+v sees state %q set under %+v", other, state, key)
			}
			if data, _ := s.GetData(ctx, other); len(data) != 0 {
				t.Errorf("key %+v sees data %v set under %+v", other, data, key)
			}
		}
	})

	t.Run("ClearResetsStateAndData", func(t *testing.T) {
		s := factory(t)
		s.SetState(ctx, key, "reg:waiting_name")
		s.SetData(ctx, key, map[string]any{"name": "Alice"})
		if err := s.Clear(ctx, key); err != nil {
			t.Fatalf("Clear: %v", err)
		}
		if state, _ := s.GetState(ctx, key); state != gg.NoState {
			t.Errorf("state after Clear = %q, want NoState", state)
		}
		if data, _ := s.GetData(ctx, key); len(data) != 0 {
			t.Errorf("data after Clear = %v, want empty", data)
		}
	})

	t.Run("ClearOnMissingKeyIsNotAnError", func(t *testing.T) {
		s := factory(t)
		if err := s.Clear(ctx, key); err != nil {
			t.Errorf("Clear on a never-written key = %v, want nil", err)
		}
	})

	t.Run("DataMutationDoesNotLeakBack", func(t *testing.T) {
		s := factory(t)
		s.SetData(ctx, key, map[string]any{"name": "Alice"})
		got, _ := s.GetData(ctx, key)
		got["name"] = "Mallory"
		fresh, _ := s.GetData(ctx, key)
		f := gg.NewFSMContext(ctx, s, key)
		if name, _, _ := gg.FSMGet[string](f, "name"); name != "Alice" {
			t.Errorf("mutating a returned map changed stored data: %v", fresh)
		}
	})

	t.Run("JSONRoundTrip", func(t *testing.T) {
		s := factory(t)
		type profile struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		s.SetData(ctx, key, map[string]any{
			"n":       42,
			"pi":      3.14,
			"ok":      true,
			"word":    "hello",
			"list":    []string{"a", "b"},
			"profile": profile{Name: "Alice", Tags: []string{"admin"}},
		})
		f := gg.NewFSMContext(ctx, s, key)
		if v, _, err := gg.FSMGet[int](f, "n"); err != nil || v != 42 {
			t.Errorf("int round-trip = (%d, %v), want 42", v, err)
		}
		if v, _, err := gg.FSMGet[float64](f, "pi"); err != nil || v != 3.14 {
			t.Errorf("float round-trip = (%v, %v), want 3.14", v, err)
		}
		if v, _, err := gg.FSMGet[bool](f, "ok"); err != nil || !v {
			t.Errorf("bool round-trip = (%v, %v), want true", v, err)
		}
		if v, _, err := gg.FSMGet[string](f, "word"); err != nil || v != "hello" {
			t.Errorf("string round-trip = (%q, %v), want hello", v, err)
		}
		if v, _, err := gg.FSMGet[[]string](f, "list"); err != nil || len(v) != 2 || v[0] != "a" {
			t.Errorf("slice round-trip = (%v, %v), want [a b]", v, err)
		}
		if v, _, err := gg.FSMGet[profile](f, "profile"); err != nil || v.Name != "Alice" || len(v.Tags) != 1 {
			t.Errorf("struct round-trip = (%+v, %v), want {Alice [admin]}", v, err)
		}
	})
}
