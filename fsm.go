package golagram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// State represents a single step in a conversation. It's a plain string
// under the hood — group related states with a [StateGroup], e.g.:
//
//	var Reg = StateGroup("registration")
//	var (
//		RegWaitingName = Reg.New("waiting_name") // "registration:waiting_name"
//		RegWaitingAge  = Reg.New("waiting_age")  // "registration:waiting_age"
//	)
type State string

// AnyState matches in [StateIs] when the user has any state set (i.e. is
// somewhere in a conversation), regardless of which one.
const AnyState State = "*"

// StateGroup names a family of related states. It exists so a whole
// conversation can be matched at once ([StateIn]) without listing every
// step, and so state names can't collide across flows.
type StateGroup string

// New derives a State belonging to this group: StateGroup("reg").New("name")
// is the State "reg:name".
func (g StateGroup) New(name string) State {
	return State(string(g) + ":" + name)
}

// Contains reports whether s belongs to this group.
func (g StateGroup) Contains(s State) bool {
	return strings.HasPrefix(string(s), string(g)+":")
}

// FSMKeyStrategy controls how conversation state is scoped — which updates
// share one FSM key. The default, [FSMKeyChatUser], gives each user
// independent state in each chat.
type FSMKeyStrategy int

const (
	// FSMKeyChatUser scopes state per {chat, user}: the same user talking
	// to the bot in two different groups is in two independent
	// conversations. The default.
	FSMKeyChatUser FSMKeyStrategy = iota

	// FSMKeyChat scopes state per chat: everyone in a group shares one
	// conversation state (collaborative flows, group games).
	FSMKeyChat

	// FSMKeyGlobalUser scopes state per user across all chats: a user
	// mid-conversation in private continues that same conversation if they
	// message the bot from a group.
	FSMKeyGlobalUser

	// FSMKeyUserInTopic scopes state per {chat, user, forum topic}: in a
	// forum supergroup, the same user gets independent state per topic.
	// Outside forum topics it behaves exactly like FSMKeyChatUser.
	FSMKeyUserInTopic
)

// apply folds the raw identifiers of an update into the StorageKey this
// strategy scopes state by. Unused halves are zeroed so keys that must be
// shared compare equal.
func (s FSMKeyStrategy) apply(chatID, userID, threadID int64) StorageKey {
	switch s {
	case FSMKeyChat:
		return StorageKey{ChatID: chatID}
	case FSMKeyGlobalUser:
		return StorageKey{UserID: userID}
	case FSMKeyUserInTopic:
		return StorageKey{ChatID: chatID, UserID: userID, ThreadID: threadID}
	default: // FSMKeyChatUser
		return StorageKey{ChatID: chatID, UserID: userID}
	}
}

// StorageKey identifies one conversation in an FSMStorage. Which fields are
// populated depends on the bot's FSMKeyStrategy — the default fills ChatID
// and UserID; ThreadID is only used under FSMKeyUserInTopic.
type StorageKey struct {
	ChatID   int64
	UserID   int64
	ThreadID int64
}

// FSMStorage persists conversation state and data per [StorageKey].
// [MemoryStorage] is the only built-in implementation — implement this
// interface for a persistent backend, and verify it with fsmtest.Run.
//
// The ctx is the handler's context (or the bot's run context for sugar
// calls) — a backend doing real I/O must respect its cancellation.
//
// JSON round-trip contract: every value put into the data map must be
// marshalable with encoding/json, and a storage is allowed to persist the
// map as JSON. That means a value read back is only guaranteed to be the
// *JSON image* of what was stored: numbers may come back as float64,
// structs as map[string]any. Read through [FSMGet] — it converts the JSON
// image back to T — instead of type-asserting raw Data() values, and the
// same handler code works on every backend.
type FSMStorage interface {
	SetState(ctx context.Context, key StorageKey, state State) error
	GetState(ctx context.Context, key StorageKey) (State, error)
	SetData(ctx context.Context, key StorageKey, data map[string]any) error
	GetData(ctx context.Context, key StorageKey) (map[string]any, error)
	UpdateData(ctx context.Context, key StorageKey, partial map[string]any) (map[string]any, error)
	Clear(ctx context.Context, key StorageKey) error
}

// FSMContext is a handle bound to one conversation. Get it via [Ctx.FSM],
// [Message.FSM], or [CallbackQuery.FSM] — handlers don't construct it
// directly. It carries the context it was created under, so storage calls
// are canceled with the handler/bot without threading a ctx through every
// call site.
type FSMContext struct {
	ctx     context.Context
	storage FSMStorage
	key     StorageKey
}

// NewFSMContext builds a handle for an arbitrary conversation. Handlers get
// theirs from [Ctx.FSM] — this is for reaching *someone else's* state (an
// admin command inspecting or resetting another user's conversation) and for
// exercising an [FSMStorage] implementation directly (fsmtest does this).
func NewFSMContext(ctx context.Context, storage FSMStorage, key StorageKey) *FSMContext {
	return &FSMContext{ctx: ctx, storage: storage, key: key}
}

// context returns the bound context, falling back to Background for an
// FSMContext constructed outside a running bot (tests, mostly).
func (f *FSMContext) context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}

// Key returns the StorageKey this handle is scoped to — useful for logging
// and for storage implementations' own diagnostics.
func (f *FSMContext) Key() StorageKey {
	return f.key
}

// State returns the current conversation state, or [NoState] if none is set.
func (f *FSMContext) State() (State, error) {
	return f.storage.GetState(f.context(), f.key)
}

// SetState sets the conversation state, advancing (or resetting) which step
// the conversation is on.
func (f *FSMContext) SetState(state State) error {
	return f.storage.SetState(f.context(), f.key, state)
}

// Data returns the conversation's stored data map, or an empty map if
// nothing has been stored yet. See [FSMGet] to read one key with a
// concrete type instead of handling the map directly.
func (f *FSMContext) Data() (map[string]any, error) {
	return f.storage.GetData(f.context(), f.key)
}

// SetData replaces the conversation's entire data map. Use [FSMContext.UpdateData]
// (or [FSMSet]) instead to change one key without discarding the rest.
func (f *FSMContext) SetData(data map[string]any) error {
	return f.storage.SetData(f.context(), f.key, data)
}

// UpdateData merges partial into the conversation's stored data (adding or
// overwriting only the given keys) and returns the resulting full map.
func (f *FSMContext) UpdateData(partial map[string]any) (map[string]any, error) {
	return f.storage.UpdateData(f.context(), f.key, partial)
}

// Clear resets state and data, ending the conversation.
func (f *FSMContext) Clear() error {
	return f.storage.Clear(f.context(), f.key)
}

// FSMGet reads one typed value from a conversation's data map. The bool
// reports whether the key was present at all; the error reports a present
// value that can't be converted to T.
//
// It first tries a direct type assertion (what [MemoryStorage] returns), then
// falls back to converting through JSON — so an int stored before a restart
// and read back from a persistent backend as float64 still comes out as an
// int, and a struct stored by value comes back as that struct rather than
// map[string]any. This is the reading half of [FSMStorage]'s JSON round-trip
// contract.
func FSMGet[T any](f *FSMContext, key string) (T, bool, error) {
	var zero T
	data, err := f.Data()
	if err != nil {
		return zero, false, err
	}
	raw, ok := data[key]
	if !ok {
		return zero, false, nil
	}
	if v, ok := raw.(T); ok {
		return v, true, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return zero, true, fmt.Errorf("FSMGet %q: stored value is not JSON-marshalable: %w", key, err)
	}
	var v T
	if err := json.Unmarshal(encoded, &v); err != nil {
		return zero, true, fmt.Errorf("FSMGet %q: stored value %T does not convert to %T: %w", key, raw, zero, err)
	}
	return v, true, nil
}

// FSMSet writes one key into the conversation's data map, leaving the rest
// untouched — shorthand for [FSMContext.UpdateData] with a single-entry map.
func FSMSet(f *FSMContext, key string, value any) error {
	_, err := f.UpdateData(map[string]any{key: value})
	return err
}
