package golagram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureBody starts a fake server that decodes each request body into a
// map and returns it via the returned function's *last* call — good enough
// for these single-request tests.
func captureBody(t *testing.T, response string) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(response))
	}))
	return server, func() map[string]any { return got }
}

func TestMessage_Answer_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if _, err := msg.Answer("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestMessage_Answer_ExplicitOverrideWins(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	_, err := msg.Answer("hi", &SendMessageOptions{BusinessConnectionID: "explicit"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "explicit" {
		t.Errorf("business_connection_id = %v, want explicit (caller override should win)", got)
	}
}

func TestMessage_Answer_NoBusinessConnection_FieldOmitted(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)

	if _, err := msg.Answer("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := body()["business_connection_id"]; ok {
		t.Errorf("expected no business_connection_id field for a non-business message, got %v", body())
	}
}

func TestMessage_Reply_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 5, Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if _, err := msg.Reply("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestMessage_EditText_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 5, Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if _, err := msg.EditText("edited"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestMessage_EditReplyMarkup_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 5, Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if _, err := msg.EditReplyMarkup(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestMessage_EditCaption_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 5, Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if _, err := msg.EditCaption("new caption"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

// EditMessageOptions has always had a BusinessConnectionID override field;
// EditCaptionOptions lacked the sibling field, forcing a caller down to the
// raw generated request to edit a caption on behalf of a different business
// connection than the source message's own.
func TestMessage_EditCaption_ExplicitBusinessConnectionIDOverridesSource(t *testing.T) {
	server, body := captureBody(t, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 5, Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if _, err := msg.EditCaption("new caption", &EditCaptionOptions{BusinessConnectionID: "explicit"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "explicit" {
		t.Errorf("business_connection_id = %v, want explicit (caller override should win)", got)
	}
}

func TestMessage_SendChatAction_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, `{"ok":true,"result":true}`)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 5, Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)

	if err := msg.SendChatAction("typing"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestCallbackQuery_SendMessage_PropagatesFromAttachedMessage(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{
		ID:      "cq1",
		From:    &User{ID: 1},
		Message: &Message{Chat: &Chat{ID: 1}, BusinessConnectionID: "biz123"},
	}, bot)

	if _, err := cq.SendMessage("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestCallbackQuery_Reply_PropagatesFromAttachedMessage(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{
		ID:      "cq1",
		From:    &User{ID: 1},
		Message: &Message{MessageID: 9, Chat: &Chat{ID: 1}, BusinessConnectionID: "biz123"},
	}, bot)

	if _, err := cq.Reply("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123", got)
	}
}

func TestMessage_UnmarshalsBusinessConnectionIDFromJSON(t *testing.T) {
	raw := `{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"},"business_connection_id":"biz123","text":"hi"}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.BusinessConnectionID != "biz123" {
		t.Errorf("BusinessConnectionID = %q, want biz123", m.BusinessConnectionID)
	}
}

// Regression for the 2026-07-04 re-audit finding #8: Ctx.Answer used to
// build its own request and drop the business connection, while Ctx.Reply
// (delegating to Message.Reply) kept it — two sibling sugar methods
// behaving differently on the same business_message update.
func TestCtx_Answer_PropagatesBusinessConnectionID(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}, BusinessConnectionID: "biz123"}, bot)
	c := ctxForBot(bot, &Update{BusinessMessage: msg})

	if _, err := c.Answer("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["business_connection_id"]; got != "biz123" {
		t.Errorf("business_connection_id = %v, want biz123 (Ctx.Answer must propagate like Message.Answer)", got)
	}
}

// A message in a forum topic must be answered into that topic, not General:
// Answer/Reply auto-propagate message_thread_id for topic messages.
func TestMessage_Answer_PropagatesForumTopic(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{
		MessageID: 5, Chat: &Chat{ID: -100, Type: "supergroup", IsForum: true},
		From: &User{ID: 1}, IsTopicMessage: true, MessageThreadID: 77,
	}, bot)

	if _, err := msg.Answer("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body()["message_thread_id"]; got != float64(77) {
		t.Errorf("message_thread_id = %v, want 77", got)
	}
}

// A reply thread in a non-forum chat also carries message_thread_id, but
// is_topic_message is false — that thread ID must NOT be propagated (it
// isn't a topic, and sendMessage would reject or misroute it).
func TestMessage_Answer_DoesNotPropagateNonTopicThreadID(t *testing.T) {
	server, body := captureBody(t, sendMessageOK)
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{
		MessageID: 5, Chat: &Chat{ID: -100, Type: "supergroup"},
		From: &User{ID: 1}, MessageThreadID: 77, // reply thread, not a topic
	}, bot)

	if _, err := msg.Answer("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, present := body()["message_thread_id"]; present {
		t.Errorf("message_thread_id = %v, want absent for a non-topic thread", got)
	}
}
