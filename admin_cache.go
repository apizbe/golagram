package golagram

import (
	"sync"
	"time"
)

// adminCacheKey identifies one {chat, user} pair.
type adminCacheKey struct {
	chatID int64
	userID int64
}

type adminCacheEntry struct {
	isAdmin   bool
	expiresAt time.Time
}

// AdminCache caches [TelegramBot.GetChatMember] lookups per {chat, user}
// for [AdminCache.FilterIsAdmin], so a busy chat's every single update
// doesn't round-trip the Bot API just to check who's an admin. Unlike
// [RateLimiter] / [MemoryStorage]'s sliding TTL, an entry's expiry is
// fixed at write time — an admin promotion/demotion should become visible
// within one ttl window of continuous activity, not be pushed back
// indefinitely by it.
type AdminCache struct {
	mu      sync.Mutex
	entries map[adminCacheKey]adminCacheEntry
	ttl     time.Duration

	stopCleanup chan struct{}
	closeOnce   sync.Once
}

// NewAdminCache creates a cache that re-checks admin status via
// [TelegramBot.GetChatMember] at most once per ttl for a given {chat,
// user}. Call [AdminCache.Close] when done to stop the background sweep
// goroutine.
func NewAdminCache(ttl time.Duration) *AdminCache {
	a := &AdminCache{
		entries:     make(map[adminCacheKey]adminCacheEntry),
		ttl:         ttl,
		stopCleanup: make(chan struct{}),
	}
	go a.cleanupLoop()
	return a
}

// Close stops the background eviction goroutine. Safe to call more than
// once.
func (a *AdminCache) Close() {
	a.closeOnce.Do(func() { close(a.stopCleanup) })
}

func (a *AdminCache) cleanupLoop() {
	interval := a.ttl
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.stopCleanup:
			return
		case <-ticker.C:
			a.evictExpired()
		}
	}
}

func (a *AdminCache) evictExpired() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for key, e := range a.entries {
		if now.After(e.expiresAt) {
			delete(a.entries, key)
		}
	}
}

func (a *AdminCache) get(key adminCacheKey) (isAdmin, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, found := a.entries[key]
	if !found || time.Now().After(e.expiresAt) {
		return false, false
	}
	return e.isAdmin, true
}

func (a *AdminCache) set(key adminCacheKey, isAdmin bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries[key] = adminCacheEntry{isAdmin: isAdmin, expiresAt: time.Now().Add(a.ttl)}
}

// FilterIsAdmin matches updates from a chat administrator or owner, checked
// via [TelegramBot.GetChatMember] and cached in a. A lookup failure
// (network error, or the bot itself lacking admin rights — GetChatMember
// only guarantees full detail on other members when the bot is one)
// matches false rather than erroring: an admin-only handler should fail
// closed, not open.
//
// On a cache miss this makes a real getChatMember call — synchronously,
// inside the per-{chat,user} dispatch lock (see [Filter]'s doc) — so a
// slow or hanging Telegram response here stalls every other queued update
// from the same user until it returns. Usually invisible (misses are rare
// once the cache is warm); worth knowing about under flood plus a cold
// cache. a.ttl controls how often a given {chat,user} pays for it again.
func (a *AdminCache) FilterIsAdmin() Filter {
	return func(c *Ctx) bool {
		chat := c.Chat()
		from := c.From()
		if chat == nil || from == nil {
			return false
		}

		key := adminCacheKey{chatID: chat.ID, userID: from.ID}
		if isAdmin, ok := a.get(key); ok {
			return isAdmin
		}

		bot := c.Bot()
		if bot == nil {
			return false
		}
		member, err := bot.GetChatMember(c, &GetChatMemberRequest{
			ChatID: ChatIDFromInt(chat.ID),
			UserID: from.ID,
		})
		if err != nil {
			return false
		}
		isAdmin := chatMemberIsAdmin(member)
		a.set(key, isAdmin)
		return isAdmin
	}
}
