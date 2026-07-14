package golagram

import (
	"log/slog"
	"sync"
	"time"
)

// FlagChatAction is the registration flag key [ChatActionMiddleware] reads
// — pair it with WithFlags to keep a chat action alive for one specific
// handler:
//
//	r.Message(gg.FilterPhoto()).WithFlags(map[string]any{gg.FlagChatAction: gg.ChatActionUploadPhoto}).Handle(processPhoto)
const FlagChatAction = "chat_action"

// LoggingMiddleware logs one structured line per dispatched update through
// logger — kind, chat/user IDs (when the update carries them), and how long
// the handler took. Errors log at Error level, everything else at Info.
// This is per-request logging; [WithLogger]'s *slog.Logger covers the
// dispatcher's own lifecycle/error messages, a different concern.
func LoggingMiddleware(logger *slog.Logger) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			start := time.Now()
			err := next(c)

			attrs := []any{
				slog.String("kind", c.Kind()),
				slog.Duration("duration", time.Since(start)),
			}
			if u := c.From(); u != nil {
				attrs = append(attrs, slog.Int64("user_id", u.ID))
			}
			if chat := c.Chat(); chat != nil {
				attrs = append(attrs, slog.Int64("chat_id", chat.ID))
			}

			if err != nil {
				logger.Error("handler error", append(attrs, slog.String("error", err.Error()))...)
			} else {
				logger.Info("dispatched", attrs...)
			}
			return err
		}
	}
}

// CallbackAnswerMiddleware auto-answers any callback query left unanswered
// by its handler, after the handler returns — killing the class of bug
// where a handler forgets AnswerCallback and the client's loading spinner
// spins until Telegram gives up. A handler that already called
// [Ctx.AnswerCallback] (directly or via [CallbackQuery.Answer]) is left
// alone: [CallbackQuery.Answered] reports whether that happened.
func CallbackAnswerMiddleware() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			err := next(c)
			if c.CallbackQuery != nil && !c.CallbackQuery.Answered() {
				if aerr := c.CallbackQuery.Answer(""); aerr != nil && err == nil {
					err = aerr
				}
			}
			return err
		}
	}
}

// ChatActionMiddleware keeps a chat action ("typing", "upload_photo", ...)
// alive for the duration of a slow handler, reading which action from the
// [FlagChatAction] registration flag (see WithFlags). A handler with no
// flag set runs unaffected — this middleware is a no-op for it.
func ChatActionMiddleware() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			action, ok := c.Flags()[FlagChatAction].(string)
			if !ok || action == "" {
				return next(c)
			}
			stop := KeepChatAction(c, action)
			defer stop()
			return next(c)
		}
	}
}

// RateLimiter is a per-key token bucket: rate tokens/second refill, burst
// tokens capacity. Idle keys (untouched for idleTTL) are swept by a
// background goroutine so a long-running bot doesn't accumulate one bucket
// per visitor forever — the same sliding-eviction shape as
// [NewMemoryStorageWithTTL], applied to rate limiting instead of FSM state.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*tokenBucket
	rate    float64
	burst   float64
	idleTTL time.Duration

	stopCleanup chan struct{}
	closeOnce   sync.Once
}

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	lastAccess time.Time
}

// NewRateLimiter creates a limiter allowing `rate` events per second per key,
// with bursts up to `burst`. idleTTL controls how long an untouched key's
// bucket is kept before being evicted; pass 0 for a sensible default (10x
// the time to refill one token, floored at a minute). Call
// [RateLimiter.Close] when done to stop the background sweep goroutine.
func NewRateLimiter(rate float64, burst int, idleTTL time.Duration) *RateLimiter {
	if idleTTL <= 0 {
		idleTTL = time.Duration(float64(time.Second) * 10 / rate)
		if idleTTL < time.Minute {
			idleTTL = time.Minute
		}
	}
	rl := &RateLimiter{
		buckets:     make(map[int64]*tokenBucket),
		rate:        rate,
		burst:       float64(burst),
		idleTTL:     idleTTL,
		stopCleanup: make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Close stops the background eviction goroutine. Safe to call more than
// once.
func (rl *RateLimiter) Close() {
	rl.closeOnce.Do(func() { close(rl.stopCleanup) })
}

func (rl *RateLimiter) cleanupLoop() {
	interval := rl.idleTTL / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCleanup:
			return
		case <-ticker.C:
			rl.evictExpired()
		}
	}
}

func (rl *RateLimiter) evictExpired() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for key, b := range rl.buckets {
		if now.Sub(b.lastAccess) > rl.idleTTL {
			delete(rl.buckets, key)
		}
	}
}

// Allow reports whether the caller identified by key may proceed right now,
// consuming one token if so.
func (rl *RateLimiter) Allow(key int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: rl.burst - 1, lastRefill: now, lastAccess: now}
		rl.buckets[key] = b
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = min(rl.burst, b.tokens+elapsed*rl.rate)
	b.lastRefill = now
	b.lastAccess = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimitMiddleware throttles per sender (c.From().ID) via rl. An update
// with no identifiable sender (e.g. a channel post) passes through
// unthrottled — there's no per-user key to bucket it by. A throttled update
// is dropped silently: onLimited, if non-nil, runs instead of the handler
// (e.g. to reply "slow down"); pass nil to just drop.
func RateLimitMiddleware(rl *RateLimiter, onLimited func(c *Ctx) error) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			u := c.From()
			if u == nil {
				return next(c)
			}
			if !rl.Allow(u.ID) {
				if onLimited != nil {
					return onLimited(c)
				}
				return nil
			}
			return next(c)
		}
	}
}

// RateLimitMiddlewareBySender is [RateLimitMiddleware], keyed by
// [Ctx.Sender] instead of c.From().ID. RateLimitMiddleware treats an
// anonymous group admin or a channel post as unidentifiable — c.From() is
// either Telegram's shared dummy user or nil — and lets every one of them
// through unthrottled, since bucketing them all under one dummy ID would
// throttle every anonymous admin across every chat together. This buckets
// by Sender().ID() instead: an anonymous admin is throttled per chat and a
// channel post is throttled per channel (the only identity Telegram gives
// either), instead of passing all of them through unthrottled.
func RateLimitMiddlewareBySender(rl *RateLimiter, onLimited func(c *Ctx) error) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			s := c.Sender()
			if s == nil {
				return next(c)
			}
			id := s.ID()
			if id == 0 {
				return next(c)
			}
			if !rl.Allow(id) {
				if onLimited != nil {
					return onLimited(c)
				}
				return nil
			}
			return next(c)
		}
	}
}
