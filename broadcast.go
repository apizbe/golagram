package golagram

import (
	"context"
	"sync"
	"time"
)

// BroadcastSendFunc sends to one chat. [Broadcast] paces when this is
// called; the closure itself calls whatever generated method fits
// (bot.SendMessage, bot.SendPhoto, ...).
type BroadcastSendFunc func(ctx context.Context, chatID int64) error

// BroadcastProgress is passed to a [WithBroadcastProgress] callback after
// every chat resolves.
type BroadcastProgress struct {
	Total     int
	Sent      int
	Failed    int // includes Blocked
	Blocked   int
	Remaining int
}

// BroadcastResult summarizes a finished (or canceled) [Broadcast] call.
type BroadcastResult struct {
	Sent    int
	Failed  int
	Blocked []int64         // chat IDs where [IsBlockedByUser] was true
	Errors  map[int64]error // failed, non-blocked chat IDs -> the last error seen
}

type broadcastConfig struct {
	rate        float64
	concurrency int
	progress    func(BroadcastProgress)
}

// BroadcastOption configures a [Broadcast] call.
type BroadcastOption func(*broadcastConfig)

// WithBroadcastRate caps the send rate in messages/second (default 25 — a
// safety margin under Telegram's ~30 msg/s bot-wide limit).
func WithBroadcastRate(perSecond float64) BroadcastOption {
	return func(c *broadcastConfig) { c.rate = perSecond }
}

// WithBroadcastConcurrency sets how many chats [Broadcast] sends to in
// parallel (default 10). Concurrency exists to hide per-call network
// latency, not to bypass the rate limit: every worker shares one pacer, so
// total throughput is still capped at the configured rate regardless of
// this setting. A purely serial send (concurrency 1) would top out well
// below that rate once round-trip latency is accounted for.
func WithBroadcastConcurrency(n int) BroadcastOption {
	return func(c *broadcastConfig) { c.concurrency = n }
}

// WithBroadcastProgress registers fn to run after every chat resolves
// (success or failure). Calls are serialized — fn never needs its own
// synchronization even though sends happen concurrently.
func WithBroadcastProgress(fn func(BroadcastProgress)) BroadcastOption {
	return func(c *broadcastConfig) { c.progress = fn }
}

// Broadcast sends to every chat in chatIDs via send, paced against
// Telegram's rate limits (see [WithBroadcastRate]/[WithBroadcastConcurrency]).
// An [IsBlockedByUser] failure is permanent and recorded in
// [BroadcastResult.Blocked]; a [IsFlood] (429) failure pauses the shared
// pacer for the error's RetryAfter (or 1s if Telegram didn't set one) and
// retries that one chat exactly once — a second failure on the retry is
// recorded normally, bounding the worst-case per-chat delay instead of
// retrying forever. Any other error is recorded in [BroadcastResult.Errors]
// with no retry.
//
// Scope limit: Broadcast only paces the aggregate send rate, not per-chat
// (~1 msg/s) or per-group (~20 msg/min) limits — those matter when the same
// chat receives multiple messages in quick succession, which a broadcast
// over distinct chat IDs doesn't naturally trigger. The aggregate rate is
// the binding constraint for the common case (announcing to many distinct
// subscribers).
//
// If ctx is canceled before every chat resolves, Broadcast returns
// immediately with a partial [BroadcastResult] and ctx.Err(); chats that
// hadn't started sending yet are simply absent from the result. A
// broadcast that runs to completion returns (result, nil) — per-chat
// failures never surface as the returned error, only through the result.
func Broadcast(ctx context.Context, chatIDs []int64, send BroadcastSendFunc, opts ...BroadcastOption) (*BroadcastResult, error) {
	cfg := &broadcastConfig{rate: 25, concurrency: 10}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.rate <= 0 {
		cfg.rate = 25
	}
	if cfg.concurrency <= 0 {
		cfg.concurrency = 1
	}

	pacer := newBroadcastPacer(cfg.rate)
	total := len(chatIDs)

	result := &BroadcastResult{Errors: make(map[int64]error)}
	var mu sync.Mutex // guards result and serializes progress callbacks

	resolve := func(chatID int64, err error) {
		mu.Lock()
		defer mu.Unlock()

		if err == nil {
			result.Sent++
		} else {
			result.Failed++
			result.Errors[chatID] = err
			if IsBlockedByUser(err) {
				result.Blocked = append(result.Blocked, chatID)
			}
		}
		if cfg.progress != nil {
			cfg.progress(BroadcastProgress{
				Total:     total,
				Sent:      result.Sent,
				Failed:    result.Failed,
				Blocked:   len(result.Blocked),
				Remaining: total - result.Sent - result.Failed,
			})
		}
	}

	sendOne := func(chatID int64) {
		if err := pacer.Wait(ctx); err != nil {
			resolve(chatID, err)
			return
		}

		err := send(ctx, chatID)
		if err != nil && IsFlood(err) {
			wait := time.Second
			if apiErr, ok := AsAPIError(err); ok && apiErr.RetryAfter != nil {
				wait = time.Duration(*apiErr.RetryAfter) * time.Second
			}
			pacer.pauseFor(wait)

			if werr := pacer.Wait(ctx); werr != nil {
				resolve(chatID, werr)
				return
			}
			err = send(ctx, chatID)
		}
		resolve(chatID, err)
	}

	idCh := make(chan int64)
	var wg sync.WaitGroup
	for range cfg.concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chatID := range idCh {
				sendOne(chatID)
			}
		}()
	}

feed:
	for _, id := range chatIDs {
		select {
		case idCh <- id:
		case <-ctx.Done():
			break feed
		}
	}
	close(idCh)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

// broadcastPacer is a single shared token bucket gating every worker in one
// [Broadcast] call — unlike [RateLimiter] (per-key, non-blocking, built for
// long-lived incoming-update throttling with background eviction),
// broadcastPacer is single-bucket, blocks the caller until a token is
// free, and lives only as long as one Broadcast call.
type broadcastPacer struct {
	mu          sync.Mutex
	tokens      float64
	rate        float64
	burst       float64
	lastRefill  time.Time
	pausedUntil time.Time
}

func newBroadcastPacer(rate float64) *broadcastPacer {
	return &broadcastPacer{tokens: rate, rate: rate, burst: rate, lastRefill: time.Now()}
}

// Wait blocks until a token is free (or ctx is done), consuming one.
func (p *broadcastPacer) Wait(ctx context.Context) error {
	for {
		wait, ok := p.reserve()
		if ok {
			return nil
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *broadcastPacer) reserve() (wait time.Duration, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	if now.Before(p.pausedUntil) {
		return p.pausedUntil.Sub(now), false
	}

	elapsed := now.Sub(p.lastRefill).Seconds()
	p.tokens = min(p.burst, p.tokens+elapsed*p.rate)
	p.lastRefill = now

	if p.tokens < 1 {
		deficit := 1 - p.tokens
		return time.Duration(deficit / p.rate * float64(time.Second)), false
	}
	p.tokens--
	return 0, true
}

// pauseFor holds every future Wait call for at least d — used to honor a
// 429's RetryAfter across the whole shared pacer, not just the chat that
// triggered it.
func (p *broadcastPacer) pauseFor(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(p.pausedUntil) {
		p.pausedUntil = until
	}
}
