package golagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingActionServer counts sendChatAction calls and records the action.
func countingActionServer(t *testing.T, count *atomic.Int64, lastAction *atomic.Value) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
		var body map[string]any
		decodeJSONBody(t, r, &body)
		if a, ok := body["action"].(string); ok {
			lastAction.Store(a)
		}
		count.Add(1)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func TestKeepChatAction_SendsAndRefreshes(t *testing.T) {
	old := chatActionRefreshInterval
	chatActionRefreshInterval = 5 * time.Millisecond
	defer func() { chatActionRefreshInterval = old }()

	var count atomic.Int64
	var lastAction atomic.Value
	server := countingActionServer(t, &count, &lastAction)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	stop := KeepChatAction(c, ChatActionUploadPhoto)
	if !waitFor(t, 2*time.Second, func() bool { return count.Load() >= 3 }) {
		t.Fatalf("expected at least 3 sends (initial + refreshes), got %d", count.Load())
	}
	stop()
	stop() // stop is idempotent

	settled := count.Load()
	time.Sleep(30 * time.Millisecond)
	// One in-flight refresh may land after stop; the loop must not continue.
	if after := count.Load(); after > settled+1 {
		t.Errorf("sends kept coming after stop: %d -> %d", settled, after)
	}
	if got := lastAction.Load(); got != ChatActionUploadPhoto {
		t.Errorf("action sent = %v, want %q", got, ChatActionUploadPhoto)
	}
}

func TestKeepChatAction_StopsOnCtxCancel(t *testing.T) {
	old := chatActionRefreshInterval
	chatActionRefreshInterval = 5 * time.Millisecond
	defer func() { chatActionRefreshInterval = old }()

	var count atomic.Int64
	var lastAction atomic.Value
	server := countingActionServer(t, &count, &lastAction)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	parent, cancel := context.WithCancel(context.Background())
	c := newCtx(parent, &Update{Message: msg}, bot, bot.api, bot.fsmStorage, bot.botUsername())

	defer Typing(c)() // also covers the Typing alias; stop runs at test end
	if !waitFor(t, 2*time.Second, func() bool { return count.Load() >= 2 }) {
		t.Fatalf("expected refreshes before cancel, got %d", count.Load())
	}
	cancel()

	time.Sleep(20 * time.Millisecond)
	settled := count.Load()
	time.Sleep(30 * time.Millisecond)
	if after := count.Load(); after > settled {
		t.Errorf("sends kept coming after ctx cancel: %d -> %d", settled, after)
	}
	if got := lastAction.Load(); got != ChatActionTyping {
		t.Errorf("action sent = %v, want %q", got, ChatActionTyping)
	}
}

func TestKeepChatAction_ChatlessUpdateIsHarmless(t *testing.T) {
	// No chat to act on: the loop sends nothing but must not panic, and
	// stop must return cleanly.
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})
	stop := KeepChatAction(c, ChatActionTyping)
	time.Sleep(5 * time.Millisecond)
	stop()
}
