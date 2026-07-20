package golagram

import (
	"context"
	"encoding/json"
	"fmt"
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

// editNotFound is the fake-server response for the "edit target is gone"
// class of errors EditOrSend/EditOrReply fall back on — the exact string
// Telegram returns for a deleted message (pinned in TestIsMessageNotEditable).
const editNotFound = `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`

func TestCtx_EditOrSend_EditSucceeds(t *testing.T) {
	var sendCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sendMessage") {
			sendCalled = true
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":1,"type":"private"},"text":"edited"}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	m, err := c.EditOrSend("edited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil || m.Text != "edited" {
		t.Errorf("EditOrSend returned %+v, want the edited message", m)
	}
	if sendCalled {
		t.Error("sendMessage was called even though the edit succeeded")
	}
}

func TestCtx_EditOrSend_FallsBackToSend(t *testing.T) {
	var sendBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "editMessageText"):
			w.Write([]byte(editNotFound))
		case strings.Contains(r.URL.Path, "sendMessage"):
			decodeJSONBody(t, r, &sendBody)
			w.Write([]byte(sendMessageOK))
		default:
			t.Errorf("unexpected API call: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	kb := NewInlineKeyboard().Row(NewInlineButton("OK", "ok")).Build()
	m, err := c.EditOrSend("menu", &EditMessageOptions{ParseMode: "HTML", ReplyMarkup: kb})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil || m.MessageID != 900 {
		t.Errorf("EditOrSend returned %+v, want the freshly sent message", m)
	}
	if sendBody == nil {
		t.Fatal("sendMessage was never called — the fallback didn't run")
	}
	if got := sendBody["text"]; got != "menu" {
		t.Errorf("fallback text = %v, want %q", got, "menu")
	}
	if got := sendBody["parse_mode"]; got != "HTML" {
		t.Errorf("fallback parse_mode = %v, want HTML — edit options must carry over to the send", got)
	}
	if _, ok := sendBody["reply_markup"].(map[string]any); !ok {
		t.Error("fallback send lost the reply_markup from the edit options")
	}
	if _, ok := sendBody["reply_parameters"]; ok {
		t.Error("EditOrSend must send plain, not as a reply")
	}
}

func TestCtx_EditOrSend_NotModified_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sendMessage") {
			t.Error("sendMessage called — 'message is not modified' must not trigger the fallback")
		}
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if _, err := c.EditOrSend("hi"); err == nil {
		t.Error("expected the not-modified error to be returned")
	}
}

func TestCtx_EditOrReply_FallbackReplies(t *testing.T) {
	var sendBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "editMessageText"):
			w.Write([]byte(editNotFound))
		case strings.Contains(r.URL.Path, "sendMessage"):
			decodeJSONBody(t, r, &sendBody)
			w.Write([]byte(sendMessageOK))
		}
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 42, Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if _, err := c.EditOrReply("fresh"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rp, ok := sendBody["reply_parameters"].(map[string]any)
	if !ok {
		t.Fatal("EditOrReply's fallback must send as a reply (reply_parameters missing)")
	}
	if id, _ := rp["message_id"].(float64); int64(id) != 42 {
		t.Errorf("reply_parameters.message_id = %v, want 42", rp["message_id"])
	}
}

func TestCtx_EditOrSend_NoMessage_Errors(t *testing.T) {
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})
	if _, err := c.EditOrSend("hi"); err == nil {
		t.Error("expected an error on an update with no message")
	}
	if _, err := c.EditOrReply("hi"); err == nil {
		t.Error("expected an error on an update with no message")
	}
}

func TestMessage_DeleteAfter_Deletes(t *testing.T) {
	deleted := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "deleteMessage") {
			var body map[string]any
			decodeJSONBody(t, r, &body)
			deleted <- body
		}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 555}, From: &User{ID: 1}}, bot)

	msg.DeleteAfter(10 * time.Millisecond)

	select {
	case body := <-deleted:
		if id, _ := body["message_id"].(float64); int64(id) != 7 {
			t.Errorf("deleteMessage message_id = %v, want 7", body["message_id"])
		}
		if id, _ := body["chat_id"].(float64); int64(id) != 555 {
			t.Errorf("deleteMessage chat_id = %v, want 555", body["chat_id"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deleteMessage was never called")
	}
}

func TestMessage_DeleteAfter_StopCancels(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)

	msg.DeleteAfter(50 * time.Millisecond).Stop()

	select {
	case <-called:
		t.Error("deleteMessage was called after Stop")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestMessage_DeleteAfter_CanceledRunContextSkipsAndStaysQuiet(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	runCtx, cancel := context.WithCancel(context.Background())
	bot.runContext = runCtx
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)

	logged := make(chan string, 1)
	msg.logf = func(format string, args ...any) { logged <- format }

	cancel() // bot shut down before the timer fires
	msg.DeleteAfter(10 * time.Millisecond)

	select {
	case <-called:
		t.Error("deleteMessage was called after the run context was canceled")
	case <-logged:
		t.Error("shutdown cancellation must be silent, not logged as a failure")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestMessage_DeleteAfter_FailureIsLoggedNotFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message to delete not found"}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)

	logged := make(chan string, 1)
	msg.logf = func(format string, args ...any) { logged <- fmt.Sprintf(format, args...) }

	msg.DeleteAfter(10 * time.Millisecond)

	select {
	case line := <-logged:
		if !strings.Contains(line, "DeleteAfter") {
			t.Errorf("log line %q doesn't identify DeleteAfter as the source", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a failed best-effort delete was never logged")
	}
}

func TestCtx_DeleteAfter(t *testing.T) {
	deleted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "deleteMessage") {
			deleted <- struct{}{}
		}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 7, Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if _, err := c.DeleteAfter(10 * time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-deleted:
	case <-time.After(5 * time.Second):
		t.Fatal("deleteMessage was never called")
	}

	if _, err := ctxFor(&Update{Poll: &Poll{ID: "p1"}}).DeleteAfter(time.Second); err == nil {
		t.Error("expected an error on an update with no message")
	}
}
