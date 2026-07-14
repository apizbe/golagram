package golagram

import (
	"context"
	"testing"
)

func TestMemoryStorage_StateDefaultsToNoState(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}

	state, err := s.GetState(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != NoState {
		t.Errorf("GetState on unseen key = %q, want NoState", state)
	}
}

func TestMemoryStorage_SetAndGetState(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}

	if err := s.SetState(context.Background(), key, "waiting_name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state, err := s.GetState(context.Background(), key)
	if err != nil || state != "waiting_name" {
		t.Errorf("GetState = (%q, %v), want (waiting_name, nil)", state, err)
	}
}

func TestMemoryStorage_DataIsScopedPerKey(t *testing.T) {
	s := NewMemoryStorage()
	keyA := StorageKey{ChatID: 1, UserID: 1}
	keyB := StorageKey{ChatID: 1, UserID: 2}

	s.SetData(context.Background(), keyA, map[string]any{"name": "Alice"})
	s.SetData(context.Background(), keyB, map[string]any{"name": "Bob"})

	dataA, _ := s.GetData(context.Background(), keyA)
	dataB, _ := s.GetData(context.Background(), keyB)
	if dataA["name"] != "Alice" || dataB["name"] != "Bob" {
		t.Errorf("data leaked across keys: A=%v B=%v", dataA, dataB)
	}
}

func TestMemoryStorage_UpdateDataMerges(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}

	s.SetData(context.Background(), key, map[string]any{"name": "Alice"})
	merged, err := s.UpdateData(context.Background(), key, map[string]any{"age": 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MemoryStorage deep-copies via a JSON round-trip (same as any
	// persistent FSMStorage backend would), so a stored int comes back as
	// float64 — read numeric values through FSMGet[T] in real code.
	if merged["name"] != "Alice" || merged["age"] != float64(30) {
		t.Errorf("UpdateData did not merge correctly, got %v", merged)
	}

	stored, _ := s.GetData(context.Background(), key)
	if stored["name"] != "Alice" || stored["age"] != float64(30) {
		t.Errorf("merge wasn't persisted, got %v", stored)
	}
}

func TestMemoryStorage_GetDataReturnsACopy(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}
	s.SetData(context.Background(), key, map[string]any{"name": "Alice"})

	data, _ := s.GetData(context.Background(), key)
	data["name"] = "Mutated"

	fresh, _ := s.GetData(context.Background(), key)
	if fresh["name"] != "Alice" {
		t.Errorf("mutating the returned map affected internal storage: %v", fresh)
	}
}

func TestMemoryStorage_ClearResetsStateAndData(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}

	s.SetState(context.Background(), key, "waiting_name")
	s.SetData(context.Background(), key, map[string]any{"name": "Alice"})

	if err := s.Clear(context.Background(), key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, _ := s.GetState(context.Background(), key)
	data, _ := s.GetData(context.Background(), key)
	if state != NoState {
		t.Errorf("state after Clear = %q, want NoState", state)
	}
	if len(data) != 0 {
		t.Errorf("data after Clear = %v, want empty", data)
	}
}

func TestFSMContext_DelegatesToStorage(t *testing.T) {
	s := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}
	ctx := &FSMContext{storage: s, key: key}

	if err := ctx.SetState("waiting_age"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state, err := ctx.State(); err != nil || state != "waiting_age" {
		t.Errorf("State() = (%q, %v), want (waiting_age, nil)", state, err)
	}

	if _, err := ctx.UpdateData(map[string]any{"age": 25}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	age, ok, err := FSMGet[int](ctx, "age")
	if err != nil || !ok || age != 25 {
		t.Errorf("FSMGet[int](age) = (%v, %v, %v), want (25, true, nil)", age, ok, err)
	}

	if err := ctx.Clear(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state, _ := ctx.State(); state != NoState {
		t.Errorf("State() after Clear = %q, want NoState", state)
	}
}

// fsmCtx builds a Ctx over a message update with the given FSM storage —
// StateIs resolves state through c.FSM(), the same way dispatch does.
func fsmCtx(storage FSMStorage, m *Message) *Ctx {
	return &Ctx{Update: &Update{Message: m}, fsm: storage}
}

func TestStateIs_MatchesExactState(t *testing.T) {
	storage := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}
	storage.SetState(context.Background(), key, "waiting_name")

	c := fsmCtx(storage, &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}})

	if !StateIs("waiting_name")(c) {
		t.Error("expected StateIs to match the current state")
	}
	if StateIs("waiting_age")(c) {
		t.Error("expected StateIs to reject a different state")
	}
}

func TestStateIs_AnyStateMatchesAnyNonEmptyState(t *testing.T) {
	storage := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}
	c := fsmCtx(storage, &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}})

	if StateIs(AnyState)(c) {
		t.Error("AnyState should not match when no state is set")
	}

	storage.SetState(context.Background(), key, "anything")
	if !StateIs(AnyState)(c) {
		t.Error("AnyState should match once a state is set")
	}
}

func TestStateIs_NoStateMatchesWhenUnset(t *testing.T) {
	storage := NewMemoryStorage()
	c := fsmCtx(storage, &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}})

	if !StateIs(NoState)(c) {
		t.Error("expected NoState to match when no state has been set")
	}
}

func TestAnd_AllFiltersMustMatch(t *testing.T) {
	storage := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}
	storage.SetState(context.Background(), key, "waiting_name")

	c := fsmCtx(storage, &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}, Text: "Alice"})

	combined := And(StateIs("waiting_name"), FilterText("Alice"))
	if !combined(c) {
		t.Error("expected And to match when both filters pass")
	}

	combinedFail := And(StateIs("waiting_age"), FilterText("Alice"))
	if combinedFail(c) {
		t.Error("expected And to fail when one filter doesn't match")
	}
}

func TestStateGroup(t *testing.T) {
	reg := StateGroup("registration")
	name := reg.New("waiting_name")

	if name != State("registration:waiting_name") {
		t.Errorf("New = %q, want registration:waiting_name", name)
	}
	if !reg.Contains(name) {
		t.Error("Contains should match a state derived from the group")
	}
	if reg.Contains(StateGroup("order").New("waiting_name")) {
		t.Error("Contains should not match another group's state")
	}
	if reg.Contains(NoState) {
		t.Error("Contains should not match NoState")
	}
	// A group whose name is a prefix of another group must not claim its states.
	if StateGroup("reg").Contains(name) {
		t.Error("group \"reg\" should not contain \"registration:*\" states")
	}
}

func TestFSMGet(t *testing.T) {
	s := NewMemoryStorage()
	f := &FSMContext{storage: s, key: StorageKey{ChatID: 1, UserID: 1}}

	type profile struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	f.SetData(map[string]any{
		"count":   42,                                           // exact type, direct assertion path
		"age":     float64(30),                                  // the JSON image of an int (persistent backends)
		"profile": map[string]any{"name": "Alice", "age": 30.0}, // the JSON image of a struct
		"word":    "hello",
	})

	if v, ok, err := FSMGet[int](f, "count"); err != nil || !ok || v != 42 {
		t.Errorf("FSMGet[int](count) = (%v, %v, %v), want (42, true, nil)", v, ok, err)
	}
	if v, ok, err := FSMGet[int](f, "age"); err != nil || !ok || v != 30 {
		t.Errorf("FSMGet[int](age) = (%v, %v, %v), want (30, true, nil) via JSON round-trip", v, ok, err)
	}
	if v, ok, err := FSMGet[profile](f, "profile"); err != nil || !ok || v.Name != "Alice" || v.Age != 30 {
		t.Errorf("FSMGet[profile](profile) = (%+v, %v, %v), want ({Alice 30}, true, nil)", v, ok, err)
	}
	if v, ok, err := FSMGet[int](f, "missing"); err != nil || ok || v != 0 {
		t.Errorf("FSMGet[int](missing) = (%v, %v, %v), want (0, false, nil)", v, ok, err)
	}
	if _, ok, err := FSMGet[int](f, "word"); err == nil || !ok {
		t.Errorf("FSMGet[int](word) should report present=true with a conversion error, got (ok=%v, err=%v)", ok, err)
	}
}

func TestFSMSet(t *testing.T) {
	s := NewMemoryStorage()
	f := &FSMContext{storage: s, key: StorageKey{ChatID: 1, UserID: 1}}

	f.SetData(map[string]any{"name": "Alice"})
	if err := FSMSet(f, "age", 30); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := f.Data()
	age, ok, err := FSMGet[int](f, "age")
	if data["name"] != "Alice" || err != nil || !ok || age != 30 {
		t.Errorf("data after FSMSet = %v (age=%v,%v,%v), want name=Alice age=30", data, age, ok, err)
	}
}

func TestFSMKeyStrategy_Apply(t *testing.T) {
	cases := []struct {
		name     string
		strategy FSMKeyStrategy
		want     StorageKey
	}{
		{"chat+user (default)", FSMKeyChatUser, StorageKey{ChatID: 10, UserID: 20}},
		{"per chat", FSMKeyChat, StorageKey{ChatID: 10}},
		{"global user", FSMKeyGlobalUser, StorageKey{UserID: 20}},
		{"user in topic", FSMKeyUserInTopic, StorageKey{ChatID: 10, UserID: 20, ThreadID: 7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.strategy.apply(10, 20, 7); got != c.want {
				t.Errorf("apply(10, 20, 7) = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestCtxStorageKey_Strategies(t *testing.T) {
	topicMsg := &Message{
		Chat:            &Chat{ID: 10},
		From:            &User{ID: 20},
		IsTopicMessage:  true,
		MessageThreadID: 7,
	}

	cases := []struct {
		name     string
		strategy FSMKeyStrategy
		want     StorageKey
	}{
		{"default ignores the topic", FSMKeyChatUser, StorageKey{ChatID: 10, UserID: 20}},
		{"per chat", FSMKeyChat, StorageKey{ChatID: 10}},
		{"global user", FSMKeyGlobalUser, StorageKey{UserID: 20}},
		{"user in topic keeps the thread", FSMKeyUserInTopic, StorageKey{ChatID: 10, UserID: 20, ThreadID: 7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := &Ctx{
				Update: &Update{Message: topicMsg},
				bot:    &TelegramBot{fsmStrategy: c.strategy},
			}
			if got := ctx.storageKey(); got != c.want {
				t.Errorf("storageKey() = %+v, want %+v", got, c.want)
			}
		})
	}

	// A non-topic message never contributes a ThreadID, even under
	// FSMKeyUserInTopic — a plain group chat behaves exactly like the default.
	plain := &Ctx{
		Update: &Update{Message: &Message{Chat: &Chat{ID: 10}, From: &User{ID: 20}, MessageThreadID: 7}},
		bot:    &TelegramBot{fsmStrategy: FSMKeyUserInTopic},
	}
	if got := plain.storageKey(); got != (StorageKey{ChatID: 10, UserID: 20}) {
		t.Errorf("non-topic storageKey() = %+v, want thread-less key", got)
	}
}

// TestCtxStorageKey_SenderIdentity_ResolvesAnonymousAdminByChat pins the
// bug WithSenderIdentity exists to fix: without it, an anonymous group
// admin's message carries Telegram's shared dummy user as From, so two
// different anonymous admins posting in the *same* chat collide onto one
// FSM key (dummy user ID is identical for both). With WithSenderIdentity,
// the user component resolves through Sender().ID() instead — the chat's
// own ID for an anonymous admin, since that's the only identity Telegram
// gives one — so each chat's anonymous-admin conversation is at least
// correctly separated from every other chat's.
func TestCtxStorageKey_SenderIdentity_ResolvesAnonymousAdminByChat(t *testing.T) {
	dummy := &User{ID: 1087968824, FirstName: "Group", Username: "GroupAnonymousBot"}
	anonAdminMsg := func(chatID int64) *Message {
		chat := &Chat{ID: chatID, Type: "supergroup"}
		return &Message{Chat: chat, From: dummy, SenderChat: chat}
	}

	withoutOpt := &Ctx{Update: &Update{Message: anonAdminMsg(10)}, bot: &TelegramBot{}}
	if got := withoutOpt.storageKey(); got != (StorageKey{ChatID: 10, UserID: dummy.ID}) {
		t.Errorf("without WithSenderIdentity: storageKey() = %+v, want the raw dummy-user key", got)
	}

	chatA := &Ctx{Update: &Update{Message: anonAdminMsg(10)}, bot: &TelegramBot{senderIdentity: true}}
	chatB := &Ctx{Update: &Update{Message: anonAdminMsg(20)}, bot: &TelegramBot{senderIdentity: true}}

	wantA := StorageKey{ChatID: 10, UserID: 10}
	wantB := StorageKey{ChatID: 20, UserID: 20}
	if got := chatA.storageKey(); got != wantA {
		t.Errorf("chat A: storageKey() = %+v, want %+v", got, wantA)
	}
	if got := chatB.storageKey(); got != wantB {
		t.Errorf("chat B: storageKey() = %+v, want %+v", got, wantB)
	}
	if chatA.storageKey() == withoutOpt.storageKey() {
		t.Error("expected WithSenderIdentity to change the key away from the dummy-user collision")
	}
}

// TestCtxStorageKey_SenderIdentity_NonAnonymousUpdatesUnaffected checks
// WithSenderIdentity is a no-op for update kinds with no anonymous-sender
// concept — the whole point is to fix the anonymous-admin/channel-post
// collision, not to change keying for ordinary users.
func TestCtxStorageKey_SenderIdentity_NonAnonymousUpdatesUnaffected(t *testing.T) {
	msg := &Message{Chat: &Chat{ID: 10}, From: &User{ID: 20}}
	without := &Ctx{Update: &Update{Message: msg}, bot: &TelegramBot{}}
	with := &Ctx{Update: &Update{Message: msg}, bot: &TelegramBot{senderIdentity: true}}
	if without.storageKey() != with.storageKey() {
		t.Errorf("WithSenderIdentity changed the key for an ordinary user: %+v vs %+v", without.storageKey(), with.storageKey())
	}
}

func TestMessageAndCallbackFSM_UseStrategy(t *testing.T) {
	s := NewMemoryStorage()

	m := &Message{Chat: &Chat{ID: 10}, From: &User{ID: 20}}
	m.fsm = s
	m.fsmStrategy = FSMKeyGlobalUser
	if got := m.FSM().Key(); got != (StorageKey{UserID: 20}) {
		t.Errorf("Message.FSM key = %+v, want global-user key", got)
	}

	cq := &CallbackQuery{
		From: &User{ID: 20},
		Message: &Message{
			Chat:            &Chat{ID: 10},
			IsTopicMessage:  true,
			MessageThreadID: 7,
		},
	}
	cq.fsm = s
	cq.fsmStrategy = FSMKeyUserInTopic
	if got := cq.FSM().Key(); got != (StorageKey{ChatID: 10, UserID: 20, ThreadID: 7}) {
		t.Errorf("CallbackQuery.FSM key = %+v, want topic-scoped key", got)
	}
}

// ctxRecordingStorage wraps MemoryStorage to capture the context FSMContext
// passes down — the contract a persistent backend relies on for cancellation.
type ctxRecordingStorage struct {
	*MemoryStorage
	lastCtx context.Context
}

func (s *ctxRecordingStorage) GetState(ctx context.Context, key StorageKey) (State, error) {
	s.lastCtx = ctx
	return s.MemoryStorage.GetState(ctx, key)
}

func TestFSMContext_PassesBoundContext(t *testing.T) {
	rec := &ctxRecordingStorage{MemoryStorage: NewMemoryStorage()}

	type ctxKey struct{}
	bound := context.WithValue(context.Background(), ctxKey{}, "marker")

	f := &FSMContext{ctx: bound, storage: rec, key: StorageKey{ChatID: 1, UserID: 1}}
	f.State()
	if rec.lastCtx != bound {
		t.Error("FSMContext should pass its bound context to the storage")
	}

	unbound := &FSMContext{storage: rec, key: StorageKey{ChatID: 1, UserID: 1}}
	unbound.State()
	if rec.lastCtx == nil {
		t.Error("an unbound FSMContext must fall back to a non-nil context")
	}
}
