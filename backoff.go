package golagram

import "time"

// Backoff is a stateful exponential backoff with a cap: the same "double,
// cap, reset on success" policy golagram's own polling runtime uses between
// failed getUpdates calls, exported so a user's own retry loop (a custom
// API poller, a health check, anything hitting an external service that
// wants to back off on failure) doesn't have to reimplement it. Not
// goroutine-safe — one Backoff per loop, like a time.Timer.
type Backoff struct {
	min, max time.Duration
	current  time.Duration
}

// NewBackoff creates a Backoff starting at min, doubling on every
// [Backoff.Next] call up to max.
func NewBackoff(min, max time.Duration) *Backoff {
	return &Backoff{min: min, max: max, current: min}
}

// Next returns the current delay and doubles it (capped at max) for the
// following call — call it, sleep (or select on a timer) for the
// returned duration, then retry.
func (b *Backoff) Next() time.Duration {
	d := b.current
	b.current = min(b.current*2, b.max)
	return d
}

// Reset returns the backoff to its minimum delay. Call it after a
// successful operation so the next failure starts backing off from
// scratch instead of wherever the previous failure streak left off.
func (b *Backoff) Reset() {
	b.current = b.min
}
