package golagram

import "sync"

// keyedMutex serializes access per StorageKey so two updates from the same
// user can't race each other's FSM state, without serializing unrelated
// users against each other. Entries are reference-counted and removed once
// nobody holds or is waiting on them, so the map doesn't grow unbounded
// over a long-running bot's lifetime.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[StorageKey]*refCountedMutex
}

type refCountedMutex struct {
	mu   sync.Mutex
	refs int
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[StorageKey]*refCountedMutex)}
}

// Lock blocks until key is uncontended, then holds it.
func (k *keyedMutex) Lock(key StorageKey) {
	k.mu.Lock()
	l, ok := k.locks[key]
	if !ok {
		l = &refCountedMutex{}
		k.locks[key] = l
	}
	l.refs++
	k.mu.Unlock()

	l.mu.Lock()
}

// Unlock releases key, removing its entry once nobody else holds or is
// waiting on it.
func (k *keyedMutex) Unlock(key StorageKey) {
	k.mu.Lock()
	l := k.locks[key]
	l.refs--
	if l.refs == 0 {
		delete(k.locks, key)
	}
	k.mu.Unlock()

	l.mu.Unlock()
}
