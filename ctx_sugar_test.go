package golagram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCtx_Reply_SetsReplyParameters(t *testing.T) {
	var gotReplyTo float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		if rp, ok := body["reply_parameters"].(map[string]any); ok {
			gotReplyTo, _ = rp["message_id"].(float64)
		}
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 42, Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if _, err := c.Reply("a reply"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int64(gotReplyTo) != 42 {
		t.Errorf("reply_parameters.message_id = %v, want 42", gotReplyTo)
	}
}

func TestCtx_Reply_NoMessage_Errors(t *testing.T) {
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})
	if _, err := c.Reply("hi"); err == nil {
		t.Error("expected an error replying to an update with no message")
	}
}

func TestCtx_EditText_UsesCurrentMessageID(t *testing.T) {
	var gotMessageID float64
	var gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotMessageID, _ = body["message_id"].(float64)
		gotText, _ = body["text"].(string)
		w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":1,"type":"private"},"text":"edited"}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	edited, err := c.EditText("edited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edited.Text != "edited" {
		t.Errorf("edited.Text = %q, want %q", edited.Text, "edited")
	}
	if int64(gotMessageID) != 7 {
		t.Errorf("message_id sent = %v, want 7", gotMessageID)
	}
	if gotText != "edited" {
		t.Errorf("text sent = %q, want %q", gotText, "edited")
	}
}

func TestCtx_EditReplyMarkup(t *testing.T) {
	var gotReplyMarkup map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotReplyMarkup, _ = body["reply_markup"].(map[string]any)
		w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	kb := NewInlineKeyboard().Row(NewInlineButton("OK", "ok")).Build()
	if _, err := c.EditReplyMarkup(kb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReplyMarkup == nil {
		t.Fatal("expected a reply_markup field in the request")
	}
}

func TestCtx_EditCaption(t *testing.T) {
	var gotCaption string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotCaption, _ = body["caption"].(string)
		w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":1,"type":"private"},"caption":"new caption"}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	edited, err := c.EditCaption("new caption")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCaption != "new caption" {
		t.Errorf("caption sent = %q, want %q", gotCaption, "new caption")
	}
	if edited.Caption != "new caption" {
		t.Errorf("edited.Caption = %q, want %q", edited.Caption, "new caption")
	}
}

func TestCtx_Delete(t *testing.T) {
	var gotPath string
	var gotMessageID float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotMessageID, _ = body["message_id"].(float64)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if err := c.Delete(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/deleteMessage") {
		t.Errorf("path = %q, want deleteMessage", gotPath)
	}
	if int64(gotMessageID) != 7 {
		t.Errorf("message_id = %v, want 7", gotMessageID)
	}
}

func TestCtx_SendChatAction(t *testing.T) {
	var gotAction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotAction, _ = body["action"].(string)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if err := c.SendChatAction("typing"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAction != "typing" {
		t.Errorf("action = %q, want typing", gotAction)
	}
}

func TestCtx_AnswerInline(t *testing.T) {
	var gotResults []any
	var gotQueryID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotQueryID, _ = body["inline_query_id"].(string)
		gotResults, _ = body["results"].([]any)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	c := ctxForBot(bot, &Update{InlineQuery: &InlineQuery{ID: "q1"}})

	err := c.AnswerInline([]InlineQueryResult{&InlineQueryResultArticle{Type: "article", ID: "1", Title: "Hi"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQueryID != "q1" {
		t.Errorf("inline_query_id = %q, want q1", gotQueryID)
	}
	if len(gotResults) != 1 {
		t.Errorf("expected 1 result, got %d", len(gotResults))
	}
}

func TestCtx_AnswerInline_NotAnInlineQuery_Errors(t *testing.T) {
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})
	if err := c.AnswerInline(nil); err == nil {
		t.Error("expected an error answering inline on a non-inline-query update")
	}
}

func TestCtx_MessageActionShortcuts_NoMessage_Errors(t *testing.T) {
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})

	if _, err := c.EditText("x"); err == nil {
		t.Error("expected EditText to error with no message")
	}
	if _, err := c.EditReplyMarkup(nil); err == nil {
		t.Error("expected EditReplyMarkup to error with no message")
	}
	if _, err := c.EditCaption("x"); err == nil {
		t.Error("expected EditCaption to error with no message")
	}
	if err := c.Delete(); err == nil {
		t.Error("expected Delete to error with no message")
	}
	if err := c.SendChatAction("typing"); err == nil {
		t.Error("expected SendChatAction to error with no message")
	}
}

// ctxForBot builds a Ctx hydrated with bot's real api client/fsm/username,
// the way dispatch() does — needed so Ctx-level sugar (Reply, EditText,
// ...) actually reaches the fake server, unlike ctxFor's nil client.
func ctxForBot(b *TelegramBot, u *Update) *Ctx {
	return newCtx(context.Background(), u, b, b.api, b.fsmStorage, b.botUsername())
}

func decodeJSONBody(t *testing.T, r *http.Request, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}
}

// Regression for the 2026-07-04 re-audit finding #7: sugar used to call
// api.Call(context.Background(), ...) — Ctx embedded a context that nothing
// respected, so shutdown couldn't cancel in-flight sends. Hydration now
// binds the run context into every payload; canceling it must abort a
// sugar call.
func TestMessageSugar_RespectsRunContextCancellation(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // hold the request open until the test finishes
	}))
	defer server.Close()
	defer close(block)

	bot := newTestBot(server)
	runCtx, cancel := context.WithCancel(context.Background())
	bot.runContext = runCtx

	msg := bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)

	done := make(chan error, 1)
	go func() {
		_, err := msg.Answer("hi")
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Answer to fail once the run context is canceled")
		}
		if !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("expected a context cancellation error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Answer did not return after run-context cancellation — sugar is ignoring the bound context")
	}
}
