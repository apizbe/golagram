// Package golagramtest provides first-class testing support for bots built
// on golagram. The usage pattern:
//
//	server := golagramtest.NewServer()
//	defer server.Close()
//
//	bot := golagramtest.NewBot(t, server)
//	bot.Dispatch(router) // required — HandleUpdate matches nothing without it
//
//	bot.HandleUpdate(context.Background(), golagramtest.CommandMessage(1, 2, "start"))
//
//	calls := server.CallsTo("sendMessage")
//
// [Server] is a fake Bot API backed by [net/http/httptest]; [NewBot] wires
// a real [gg.TelegramBot] at it (including the startup getMe call
// NewTelegramBot always makes). [TextMessage], [CommandMessage], and
// [CallbackQueryUpdate] build synthetic updates to feed to
// [gg.TelegramBot.HandleUpdate], which dispatches synchronously — no
// StartWorkers/Run/RunWebhook needed.
package golagramtest

import (
	"testing"

	gg "github.com/apizbe/golagram"
)

// testToken is shaped like a real bot token (<bot_id>:<35-char secret>) so
// it passes [gg.ValidateToken].
const testToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// NewBot builds a [gg.TelegramBot] pointed at server, applying opts after
// [gg.WithBaseURL](server.URL()) so callers can still pass their own
// WithFSMStorage/WithLogger/etc. Fails the test via t.Fatalf if
// construction errors (e.g. server isn't answering getMe correctly).
func NewBot(t *testing.T, server *Server, opts ...gg.Option) *gg.TelegramBot {
	t.Helper()
	allOpts := append([]gg.Option{gg.WithBaseURL(server.URL())}, opts...)
	bot, err := gg.NewTelegramBot(testToken, allOpts...)
	if err != nil {
		t.Fatalf("golagramtest: NewBot: %v", err)
	}
	return bot
}
