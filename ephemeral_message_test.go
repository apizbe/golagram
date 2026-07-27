package golagram

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ephemeralSendOK is a fake sendMessage response shaped like Telegram's real
// reply to an ephemeral send: ReceiverUser/EphemeralMessageID populated, so
// the returned *Message carries what Edit*/Delete need without the caller
// tracking those IDs separately.
const ephemeralSendOK = `{"ok":true,"result":{
	"message_id":900,"chat":{"id":555,"type":"supergroup"},"text":"sent",
	"receiver_user":{"id":42,"is_bot":false,"first_name":"U"},
	"ephemeral_message_id":77
}}`

func TestCallbackQuery_SendEphemeral_SetsReceiverAndCallbackQueryID(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(ephemeralSendOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{
		ID:   "cq1",
		From: &User{ID: 42},
		Message: &Message{
			MessageID: 10,
			Chat:      &Chat{ID: 555},
		},
	}, bot)

	sent, err := cq.SendEphemeral("private info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["receiver_user_id"].(float64); int64(got) != 42 {
		t.Errorf("receiver_user_id = %v, want 42", body["receiver_user_id"])
	}
	if got, _ := body["callback_query_id"].(string); got != "cq1" {
		t.Errorf("callback_query_id = %q, want %q", got, "cq1")
	}
	if sent.ReceiverUser == nil || sent.ReceiverUser.ID != 42 || sent.EphemeralMessageID != 77 {
		t.Errorf("sent message didn't rebind ReceiverUser/EphemeralMessageID: %+v", sent)
	}
}

func TestMessage_AnswerEphemeral_SetsReceiverNoCallbackQueryID(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(ephemeralSendOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 555}, From: &User{ID: 42}}, bot)

	if _, err := msg.AnswerEphemeral("only you can see this"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["receiver_user_id"].(float64); int64(got) != 42 {
		t.Errorf("receiver_user_id = %v, want 42", body["receiver_user_id"])
	}
	if _, ok := body["callback_query_id"]; ok {
		t.Errorf("callback_query_id should be omitted outside a callback query, got %v", body["callback_query_id"])
	}
}

func TestCtx_AnswerEphemeral_PrefersCallbackQueryOverMessage(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(ephemeralSendOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{
		ID:      "cq1",
		From:    &User{ID: 42},
		Message: &Message{MessageID: 10, Chat: &Chat{ID: 555}},
	}, bot)
	c := ctxForBot(bot, &Update{CallbackQuery: cq})

	if _, err := c.AnswerEphemeral("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["callback_query_id"].(string); got != "cq1" {
		t.Errorf("expected Ctx.AnswerEphemeral to route through the callback query, callback_query_id = %q", got)
	}
}

func TestCtx_AnswerEphemeral_FallsBackToMessage(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(ephemeralSendOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 555}, From: &User{ID: 42}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if _, err := c.AnswerEphemeral("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["receiver_user_id"].(float64); int64(got) != 42 {
		t.Errorf("receiver_user_id = %v, want 42", body["receiver_user_id"])
	}
	if _, ok := body["callback_query_id"]; ok {
		t.Errorf("callback_query_id should be omitted when routed through a Message, got %v", body["callback_query_id"])
	}
}

func TestCtx_AnswerEphemeral_NoMessageOrCallback_Errors(t *testing.T) {
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})
	if _, err := c.AnswerEphemeral("hi"); err == nil {
		t.Error("expected an error for an update with no message or callback query")
	}
}

// sentEphemeral builds a *Message the way AnswerEphemeral/SendEphemeral
// would leave it — ReceiverUser/EphemeralMessageID populated and bound to
// bot — as the starting point for the Edit*/Delete tests below.
func sentEphemeral(bot *TelegramBot) *Message {
	return bindMessage(&Message{
		Chat:               &Chat{ID: 555},
		ReceiverUser:       &User{ID: 42},
		EphemeralMessageID: 77,
	}, bot)
}

func TestMessage_EditEphemeralText_SendsReceiverAndEphemeralID(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := sentEphemeral(bot)

	if err := msg.EditEphemeralText("updated"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["receiver_user_id"].(float64); int64(got) != 42 {
		t.Errorf("receiver_user_id = %v, want 42", body["receiver_user_id"])
	}
	if got, _ := body["ephemeral_message_id"].(float64); int64(got) != 77 {
		t.Errorf("ephemeral_message_id = %v, want 77", body["ephemeral_message_id"])
	}
	if got, _ := body["text"].(string); got != "updated" {
		t.Errorf("text = %q, want %q", got, "updated")
	}
}

func TestMessage_EditEphemeralCaption(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := sentEphemeral(bot)

	if err := msg.EditEphemeralCaption("new caption"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["caption"].(string); got != "new caption" {
		t.Errorf("caption = %q, want %q", got, "new caption")
	}
}

func TestMessage_EditEphemeralReplyMarkup(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := sentEphemeral(bot)

	kb := NewInlineKeyboard().Row(NewInlineButton("OK", "ok")).Build()
	if err := msg.EditEphemeralReplyMarkup(kb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["reply_markup"] == nil {
		t.Error("expected a reply_markup field in the request")
	}
}

func TestMessage_DeleteEphemeral(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := sentEphemeral(bot)

	if err := msg.DeleteEphemeral(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := body["receiver_user_id"].(float64); int64(got) != 42 {
		t.Errorf("receiver_user_id = %v, want 42", body["receiver_user_id"])
	}
	if got, _ := body["ephemeral_message_id"].(float64); int64(got) != 77 {
		t.Errorf("ephemeral_message_id = %v, want 77", body["ephemeral_message_id"])
	}
}

func TestFilterIsEphemeral(t *testing.T) {
	yes := msgCtx(&Message{EphemeralMessageID: 77})
	no := msgCtx(&Message{})
	if !FilterIsEphemeral()(yes) {
		t.Error("expected FilterIsEphemeral to match a message with EphemeralMessageID set")
	}
	if FilterIsEphemeral()(no) {
		t.Error("expected FilterIsEphemeral to reject a message without EphemeralMessageID")
	}
}
