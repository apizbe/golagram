package golagramtest_test

import (
	"context"
	"errors"
	"testing"

	gg "github.com/apizbe/golagram"
	"github.com/apizbe/golagram/golagramtest"
)

func TestNewBot_CommandHandler_RepliesAndIsRecorded(t *testing.T) {
	server := golagramtest.NewServer()
	defer server.Close()

	bot := golagramtest.NewBot(t, server)
	r := gg.NewRouter()
	r.Message(gg.FilterCommand("start")).Handle(func(c *gg.Ctx) error {
		_, err := c.Reply("welcome")
		return err
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), golagramtest.CommandMessage(1, 2, "start"))

	calls := server.CallsTo("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("CallsTo(sendMessage) = %d calls, want 1", len(calls))
	}
	var body map[string]any
	if err := calls[0].Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if body["text"] != "welcome" {
		t.Errorf("sendMessage text = %v, want %q", body["text"], "welcome")
	}
}

func TestServer_Respond_OverridesDefaultResult(t *testing.T) {
	server := golagramtest.NewServer()
	defer server.Close()
	server.Respond("deleteMessage", true)

	bot := golagramtest.NewBot(t, server)
	r := gg.NewRouter()
	r.Message().Handle(func(c *gg.Ctx) error {
		_, err := c.Bot().DeleteMessage(context.Background(), &gg.DeleteMessageRequest{
			ChatID:    gg.ChatIDFromInt(c.Chat().ID),
			MessageID: c.Message.MessageID,
		})
		return err
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), golagramtest.TextMessage(1, 2, "hi"))

	if len(server.CallsTo("deleteMessage")) != 1 {
		t.Fatalf("expected deleteMessage to be called once, got %d", len(server.CallsTo("deleteMessage")))
	}
}

func TestServer_Fail_ReturnsTelegramShapedError(t *testing.T) {
	server := golagramtest.NewServer()
	defer server.Close()
	server.Fail("sendMessage", 429, "Too Many Requests")

	bot := golagramtest.NewBot(t, server)
	var capturedErr error
	bot.OnError(func(err error, c *gg.Ctx) { capturedErr = err })
	r := gg.NewRouter()
	r.Message().Handle(func(c *gg.Ctx) error {
		_, err := c.Reply("hi")
		return err
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), golagramtest.TextMessage(1, 2, "hi"))

	if capturedErr == nil {
		t.Fatal("expected the configured failure to reach OnError")
	}
	var apiErr *gg.APIError
	if !errors.As(capturedErr, &apiErr) || apiErr.Code != 429 {
		t.Errorf("expected a 429 APIError, got %v", capturedErr)
	}
}

func TestServer_Reset_ClearsCallsAndOverrides(t *testing.T) {
	server := golagramtest.NewServer()
	defer server.Close()
	server.Respond("sendMessage", &gg.Message{MessageID: 42, Chat: &gg.Chat{ID: 1, Type: gg.ChatTypePrivate}})

	bot := golagramtest.NewBot(t, server)
	r := gg.NewRouter()
	r.Message().Handle(func(c *gg.Ctx) error {
		_, err := c.Reply(c.Message.Text)
		return err
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), golagramtest.TextMessage(1, 2, "hi"))
	if len(server.Calls()) == 0 {
		t.Fatal("expected at least one recorded call before Reset")
	}

	server.Reset()
	if _, ok := server.LastCall(); ok {
		t.Error("expected LastCall to report false after Reset")
	}

	bot.HandleUpdate(context.Background(), golagramtest.TextMessage(1, 2, "hi again"))
	call, ok := server.LastCall()
	if !ok || call.Method != "sendMessage" {
		t.Errorf("expected a fresh sendMessage call after Reset, got %+v (ok=%v)", call, ok)
	}
	var body map[string]any
	if err := call.Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if body["text"] != "hi again" {
		t.Errorf("expected the overridden result to no longer apply after Reset, sendMessage text = %v", body["text"])
	}
}

func TestCallbackQueryUpdate_RoundTrips(t *testing.T) {
	server := golagramtest.NewServer()
	defer server.Close()

	bot := golagramtest.NewBot(t, server)
	var gotData string
	r := gg.NewRouter()
	r.CallbackQuery().Handle(func(c *gg.Ctx) error {
		gotData = c.CallbackQuery.Data
		return c.AnswerCallback("")
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), golagramtest.CallbackQueryUpdate(1, 2, "buy:42"))

	if gotData != "buy:42" {
		t.Errorf("callback data = %q, want %q", gotData, "buy:42")
	}
	if len(server.CallsTo("answerCallbackQuery")) != 1 {
		t.Errorf("expected answerCallbackQuery to be called once, got %d", len(server.CallsTo("answerCallbackQuery")))
	}
}
