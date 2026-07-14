package golagram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	h := LoggingMiddleware(logger)(func(c *Ctx) error { return nil })
	c := msgCtx(&Message{Chat: &Chat{ID: 555}, From: &User{ID: 42}})
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"dispatched", "kind=message", "chat_id=555", "user_id=42"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q; got: %s", want, out)
		}
	}
}

func TestLoggingMiddleware_LogsHandlerError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	wantErr := errors.New("boom")
	h := LoggingMiddleware(logger)(func(c *Ctx) error { return wantErr })
	c := msgCtx(&Message{Chat: &Chat{ID: 1}})
	if err := h(c); err != wantErr {
		t.Fatalf("expected error to pass through, got %v", err)
	}
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "boom") {
		t.Errorf("expected an ERROR line mentioning the error; got: %s", buf.String())
	}
}

func TestCallbackAnswerMiddleware_AnswersUnansweredCallback(t *testing.T) {
	var gotAnswer bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "answerCallbackQuery") {
			gotAnswer = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{ID: "1", From: &User{ID: 1}}, bot)
	c := cbCtx(cq)

	h := CallbackAnswerMiddleware()(func(c *Ctx) error { return nil })
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotAnswer {
		t.Error("expected the middleware to auto-answer the callback query")
	}
	if !cq.Answered() {
		t.Error("expected Answered() to report true after auto-answer")
	}
}

func TestCallbackAnswerMiddleware_LeavesAlreadyAnsweredCallback(t *testing.T) {
	var answerCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "answerCallbackQuery") {
			answerCount++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{ID: "1", From: &User{ID: 1}}, bot)
	c := cbCtx(cq)

	h := CallbackAnswerMiddleware()(func(c *Ctx) error {
		return c.CallbackQuery.Answer("already handled")
	})
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answerCount != 1 {
		t.Errorf("expected exactly one answerCallbackQuery call, got %d", answerCount)
	}
}

func TestCallbackAnswerMiddleware_IgnoresNonCallbackUpdates(t *testing.T) {
	h := CallbackAnswerMiddleware()(func(c *Ctx) error { return nil })
	c := msgCtx(&Message{Text: "hi"})
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatActionMiddleware_NoFlagIsNoOp(t *testing.T) {
	var ran bool
	h := ChatActionMiddleware()(func(c *Ctx) error {
		ran = true
		return nil
	})
	c := msgCtx(&Message{Chat: &Chat{ID: 1}})
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("expected the handler to run")
	}
}

func TestChatActionMiddleware_KeepsActionAliveWithFlag(t *testing.T) {
	// KeepChatAction's own periodic-refresh behavior is covered by
	// chat_action_test.go; this only needs to prove the middleware wires
	// the FlagChatAction flag to it — the immediate send on start is
	// enough evidence of that, without racing chatActionRefreshInterval
	// (a package var KeepChatAction's background goroutine reads
	// unsynchronized with any test-side restore).
	var actionsSent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sendChatAction") {
			actionsSent.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	m := bindMessage(&Message{Chat: &Chat{ID: 1}}, bot)
	c := newCtx(context.Background(), &Update{Message: m}, bot, bot.api, bot.fsmStorage, "")
	c.routeFlags = map[string]any{FlagChatAction: ChatActionTyping}

	h := ChatActionMiddleware()(func(c *Ctx) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actionsSent.Load() < 1 {
		t.Error("expected at least the immediate sendChatAction to have fired")
	}
}

func TestRateLimiter_AllowsUpToBurstThenBlocks(t *testing.T) {
	rl := NewRateLimiter(1, 3, time.Minute)
	defer rl.Close()

	for i := 0; i < 3; i++ {
		if !rl.Allow(1) {
			t.Fatalf("expected burst request %d to be allowed", i)
		}
	}
	if rl.Allow(1) {
		t.Error("expected the 4th immediate request to be denied")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	rl := NewRateLimiter(1000, 1, time.Minute) // 1000/s refill — fast enough to test without sleeping long
	defer rl.Close()

	if !rl.Allow(1) {
		t.Fatal("expected first request to be allowed")
	}
	if rl.Allow(1) {
		t.Error("expected immediate second request to be denied")
	}
	time.Sleep(5 * time.Millisecond)
	if !rl.Allow(1) {
		t.Error("expected a request after refill time to be allowed")
	}
}

func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	defer rl.Close()

	if !rl.Allow(1) {
		t.Fatal("expected user 1's first request to be allowed")
	}
	if !rl.Allow(2) {
		t.Error("expected user 2's request to be allowed independently of user 1")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	defer rl.Close()

	var ran int
	h := RateLimitMiddleware(rl, nil)(func(c *Ctx) error {
		ran++
		return nil
	})

	c := msgCtx(&Message{From: &User{ID: 1}})
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran != 1 {
		t.Errorf("expected the handler to run once (second call throttled), got %d", ran)
	}
}

func TestRateLimitMiddleware_OnLimitedCallback(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	defer rl.Close()

	var limited bool
	h := RateLimitMiddleware(rl, func(c *Ctx) error {
		limited = true
		return nil
	})(func(c *Ctx) error { return nil })

	c := msgCtx(&Message{From: &User{ID: 1}})
	_ = h(c)
	_ = h(c)
	if !limited {
		t.Error("expected onLimited to run for the throttled request")
	}
}

func TestRateLimitMiddleware_NoSenderPassesThrough(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	defer rl.Close()

	var ran bool
	h := RateLimitMiddleware(rl, nil)(func(c *Ctx) error {
		ran = true
		return nil
	})
	c := msgCtx(&Message{Chat: &Chat{ID: 1}}) // no From
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("expected an update with no identifiable sender to pass through")
	}
}

func TestRateLimitMiddlewareBySender_ThrottlesPerChatNotGlobally(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	defer rl.Close()

	var ran int
	h := RateLimitMiddlewareBySender(rl, nil)(func(c *Ctx) error {
		ran++
		return nil
	})

	// RateLimitMiddleware would let both of these straight through
	// unthrottled (From is nil for a channel post — no identifiable
	// sender). RateLimitMiddlewareBySender buckets by Sender().ID()
	// instead, which for a channel post is the channel's own ID — so the
	// first post from a given channel is allowed and a same-second second
	// post from that *same* channel is throttled...
	channelA := &Chat{ID: -100111, Type: "channel"}
	if err := h(msgCtx(&Message{Chat: channelA, SenderChat: channelA})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h(msgCtx(&Message{Chat: channelA, SenderChat: channelA})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran != 1 {
		t.Errorf("expected channel A's second post in the window to be throttled, ran=%d", ran)
	}

	// ...but a *different* channel gets its own independent bucket, not
	// lumped in with channel A.
	channelB := &Chat{ID: -100222, Type: "channel"}
	if err := h(msgCtx(&Message{Chat: channelB, SenderChat: channelB})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran != 2 {
		t.Errorf("expected channel B's first post to run independently of channel A's bucket, ran=%d", ran)
	}
}

func TestRateLimitMiddlewareBySender_NoSenderPassesThrough(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	defer rl.Close()

	var ran bool
	h := RateLimitMiddlewareBySender(rl, nil)(func(c *Ctx) error {
		ran = true
		return nil
	})
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}}) // no sender concept at all
	if err := h(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("expected an update with no Sender() to pass through")
	}
}
