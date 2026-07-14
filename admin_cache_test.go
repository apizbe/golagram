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

func adminLookupServer(status string, calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getChatMember") {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"` + status + `","user":{"id":1,"is_bot":false}}}`))
	}))
}

func ctxForAdminTest(bot *TelegramBot, chatID, userID int64) *Ctx {
	m := &Message{Chat: &Chat{ID: chatID, Type: "supergroup"}, From: &User{ID: userID}}
	return newCtx(context.Background(), &Update{Message: m}, bot, bot.api, bot.fsmStorage, "")
}

func TestFilterIsAdmin_MatchesAdministrator(t *testing.T) {
	var calls atomic.Int32
	server := adminLookupServer("administrator", &calls)
	defer server.Close()

	bot := newTestBot(server)
	cache := NewAdminCache(time.Minute)
	defer cache.Close()

	if !cache.FilterIsAdmin()(ctxForAdminTest(bot, 1, 1)) {
		t.Error("expected an administrator to match")
	}
}

func TestFilterIsAdmin_RejectsPlainMember(t *testing.T) {
	var calls atomic.Int32
	server := adminLookupServer("member", &calls)
	defer server.Close()

	bot := newTestBot(server)
	cache := NewAdminCache(time.Minute)
	defer cache.Close()

	if cache.FilterIsAdmin()(ctxForAdminTest(bot, 1, 1)) {
		t.Error("expected a plain member not to match")
	}
}

func TestFilterIsAdmin_CachesLookups(t *testing.T) {
	var calls atomic.Int32
	server := adminLookupServer("creator", &calls)
	defer server.Close()

	bot := newTestBot(server)
	cache := NewAdminCache(time.Minute)
	defer cache.Close()

	filter := cache.FilterIsAdmin()
	for i := 0; i < 5; i++ {
		if !filter(ctxForAdminTest(bot, 1, 1)) {
			t.Fatal("expected creator to match")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected exactly one getChatMember call across 5 lookups, got %d", got)
	}
}

func TestFilterIsAdmin_CacheIsPerChatAndUser(t *testing.T) {
	var calls atomic.Int32
	server := adminLookupServer("member", &calls)
	defer server.Close()

	bot := newTestBot(server)
	cache := NewAdminCache(time.Minute)
	defer cache.Close()

	filter := cache.FilterIsAdmin()
	filter(ctxForAdminTest(bot, 1, 1))
	filter(ctxForAdminTest(bot, 1, 2)) // different user, same chat
	filter(ctxForAdminTest(bot, 2, 1)) // same user, different chat
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 distinct lookups for 3 distinct {chat,user} keys, got %d", got)
	}
}

func TestFilterIsAdmin_ExpiresAfterTTL(t *testing.T) {
	var calls atomic.Int32
	server := adminLookupServer("creator", &calls)
	defer server.Close()

	bot := newTestBot(server)
	cache := NewAdminCache(10 * time.Millisecond)
	defer cache.Close()

	filter := cache.FilterIsAdmin()
	filter(ctxForAdminTest(bot, 1, 1))
	time.Sleep(20 * time.Millisecond)
	filter(ctxForAdminTest(bot, 1, 1))
	if got := calls.Load(); got != 2 {
		t.Errorf("expected a second lookup after the entry expired, got %d calls", got)
	}
}

func TestFilterIsAdmin_NoChatOrSenderFailsClosed(t *testing.T) {
	cache := NewAdminCache(time.Minute)
	defer cache.Close()

	c := newCtx(context.Background(), &Update{}, nil, nil, nil, "")
	if cache.FilterIsAdmin()(c) {
		t.Error("expected an update with no chat/sender to fail closed")
	}
}
