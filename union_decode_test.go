package golagram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The 2026-07-04 re-audit proved GetChatMember could never succeed: it
// decoded into a bare union interface, which encoding/json always rejects.
// These tests pin the fix — polymorphic decode through the generated
// discriminator-switching unmarshalers — at every level it operates.

func TestUnmarshalChatMember_AllStatuses(t *testing.T) {
	cases := []struct {
		payload string
		want    string
	}{
		{`{"status":"creator","user":{"id":1,"is_bot":false,"first_name":"A"},"is_anonymous":false}`, "creator"},
		{`{"status":"administrator","user":{"id":1,"is_bot":false,"first_name":"A"}}`, "administrator"},
		{`{"status":"member","user":{"id":1,"is_bot":false,"first_name":"A"}}`, "member"},
		{`{"status":"restricted","user":{"id":1,"is_bot":false,"first_name":"A"},"is_member":true}`, "restricted"},
		{`{"status":"left","user":{"id":1,"is_bot":false,"first_name":"A"}}`, "left"},
		{`{"status":"kicked","user":{"id":1,"is_bot":false,"first_name":"A"},"until_date":0}`, "kicked"},
	}
	for _, c := range cases {
		m, err := unmarshalChatMember([]byte(c.payload))
		if err != nil {
			t.Fatalf("unmarshalChatMember(%s): %v", c.want, err)
		}
		if got := ChatMemberStatus(m); got != c.want {
			t.Errorf("ChatMemberStatus = %q, want %q", got, c.want)
		}
	}

	if _, err := unmarshalChatMember([]byte(`{"status":"time_traveler"}`)); err == nil {
		t.Error("expected an unknown status to error loudly, not return nil")
	}
}

func TestGetChatMember_DecodesUnionResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{"status":"administrator","user":{"id":42,"is_bot":false,"first_name":"Admin"},"can_manage_chat":true}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	member, err := bot.GetChatMember(context.Background(), &GetChatMemberRequest{ChatID: ChatIDFromInt(1), UserID: 42})
	if err != nil {
		t.Fatalf("GetChatMember: %v", err)
	}
	admin, ok := member.(*ChatMemberAdministrator)
	if !ok {
		t.Fatalf("expected *ChatMemberAdministrator, got %T", member)
	}
	if admin.User == nil || admin.User.ID != 42 || !admin.CanManageChat {
		t.Errorf("unexpected admin payload: %+v", admin)
	}

	// Status and User are common to every ChatMember member, so the
	// generator promotes them onto the interface — no type switch needed to
	// read them generically, only for member-specific fields like
	// CanManageChat above.
	if member.GetStatus() != "administrator" {
		t.Errorf("member.GetStatus() = %q, want administrator", member.GetStatus())
	}
	if member.GetUser() == nil || member.GetUser().ID != 42 {
		t.Errorf("member.GetUser() = %+v, want ID 42", member.GetUser())
	}
}

func TestGetChatAdministrators_DecodesUnionArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":[
			{"status":"creator","user":{"id":1,"is_bot":false,"first_name":"Owner"},"is_anonymous":false},
			{"status":"administrator","user":{"id":2,"is_bot":false,"first_name":"Mod"}}
		]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	admins, err := bot.GetChatAdministrators(context.Background(), &GetChatAdministratorsRequest{ChatID: ChatIDFromInt(1)})
	if err != nil {
		t.Fatalf("GetChatAdministrators: %v", err)
	}
	if len(admins) != 2 {
		t.Fatalf("expected 2 admins, got %d", len(admins))
	}
	if _, ok := admins[0].(*ChatMemberOwner); !ok {
		t.Errorf("admins[0]: expected *ChatMemberOwner, got %T", admins[0])
	}
	if _, ok := admins[1].(*ChatMemberAdministrator); !ok {
		t.Errorf("admins[1]: expected *ChatMemberAdministrator, got %T", admins[1])
	}
}

func TestGetChatMenuButton_DecodesUnion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{"type":"web_app","text":"Open","web_app":{"url":"https://example.com"}}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	btn, err := bot.GetChatMenuButton(context.Background(), &GetChatMenuButtonRequest{})
	if err != nil {
		t.Fatalf("GetChatMenuButton: %v", err)
	}
	webApp, ok := btn.(*MenuButtonWebApp)
	if !ok {
		t.Fatalf("expected *MenuButtonWebApp, got %T", btn)
	}
	if webApp.Text != "Open" || webApp.WebApp == nil || webApp.WebApp.URL != "https://example.com" {
		t.Errorf("unexpected menu button: %+v", webApp)
	}
}

// The chat_member update kind used to arrive with old_chat_member and
// new_chat_member omitted entirely — dispatchable but useless. Decode a
// realistic payload end-to-end through Update.
func TestChatMemberUpdated_DecodesOldAndNewMember(t *testing.T) {
	raw := `{
		"update_id": 1,
		"chat_member": {
			"chat": {"id": -100, "type": "supergroup", "title": "Group"},
			"from": {"id": 7, "is_bot": false, "first_name": "Admin"},
			"date": 1700000000,
			"old_chat_member": {"status": "left", "user": {"id": 9, "is_bot": false, "first_name": "New"}},
			"new_chat_member": {"status": "member", "user": {"id": 9, "is_bot": false, "first_name": "New"}}
		}
	}`
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	cm := u.ChatMember
	if cm == nil || cm.OldChatMember == nil || cm.NewChatMember == nil {
		t.Fatalf("chat_member payload incomplete: %+v", cm)
	}
	if ChatMemberStatus(cm.OldChatMember) != "left" || ChatMemberStatus(cm.NewChatMember) != "member" {
		t.Errorf("statuses = %q → %q, want left → member",
			ChatMemberStatus(cm.OldChatMember), ChatMemberStatus(cm.NewChatMember))
	}
	// And the transition filter built on it fires.
	if !FilterJoined()(&Ctx{Update: &u}) {
		t.Error("FilterJoined should match a left→member transition")
	}
}

// message_reaction updates used to arrive without the reactions themselves.
func TestMessageReactionUpdated_DecodesReactions(t *testing.T) {
	raw := `{
		"chat": {"id": 5, "type": "private"},
		"message_id": 3,
		"user": {"id": 9, "is_bot": false, "first_name": "R"},
		"date": 1700000000,
		"old_reaction": [],
		"new_reaction": [
			{"type": "emoji", "emoji": "👍"},
			{"type": "custom_emoji", "custom_emoji_id": "c1"}
		]
	}`
	var mr MessageReactionUpdated
	if err := json.Unmarshal([]byte(raw), &mr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(mr.NewReaction) != 2 {
		t.Fatalf("expected 2 new reactions, got %d", len(mr.NewReaction))
	}
	emoji, ok := mr.NewReaction[0].(*ReactionTypeEmoji)
	if !ok || emoji.Emoji != "👍" {
		t.Errorf("NewReaction[0]: got %T %+v", mr.NewReaction[0], mr.NewReaction[0])
	}
	custom, ok := mr.NewReaction[1].(*ReactionTypeCustomEmoji)
	if !ok || custom.CustomEmojiID != "c1" {
		t.Errorf("NewReaction[1]: got %T %+v", mr.NewReaction[1], mr.NewReaction[1])
	}
}

// Message.forward_origin is a union field on the (generated) Message type.
func TestMessage_ForwardOrigin_Decodes(t *testing.T) {
	raw := `{
		"message_id": 1, "date": 1700000000,
		"chat": {"id": 1, "type": "private"},
		"forward_origin": {"type": "channel", "chat": {"id": -1001, "type": "channel", "title": "News"}, "message_id": 77, "date": 1690000000}
	}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ch, ok := m.ForwardOrigin.(*MessageOriginChannel)
	if !ok {
		t.Fatalf("expected *MessageOriginChannel, got %T", m.ForwardOrigin)
	}
	if ch.Chat == nil || ch.Chat.ID != -1001 || ch.MessageID != 77 {
		t.Errorf("unexpected origin: %+v", ch)
	}
	if !FilterIsForwarded()(msgCtx(&m)) {
		t.Error("FilterIsForwarded should match")
	}
}

// The re-audit's other headline: InlineQueryResultArticle had no
// input_message_content field at all (required by the spec), because the
// omission rule swept input-only unions in with decode-risk ones. Pin the
// full inline answer encoding.
func TestInlineQueryResultArticle_MarshalsInputMessageContent(t *testing.T) {
	article := &InlineQueryResultArticle{
		Type:  "article",
		ID:    "1",
		Title: "Hello",
		InputMessageContent: &InputTextMessageContent{
			MessageText: "hello from inline",
			ParseMode:   "HTML",
		},
	}
	raw, err := json.Marshal(article)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var flat map[string]any
	json.Unmarshal(raw, &flat)
	imc, ok := flat["input_message_content"].(map[string]any)
	if !ok {
		t.Fatalf("input_message_content missing from marshaled article: %s", raw)
	}
	if imc["message_text"] != "hello from inline" || imc["parse_mode"] != "HTML" {
		t.Errorf("unexpected input_message_content: %v", imc)
	}
}

// CallbackQuery now carries the full spec field set — a callback from an
// inline-mode message arrives with inline_message_id instead of message.
func TestCallbackQuery_DecodesInlineMessageID(t *testing.T) {
	raw := `{"id":"q1","from":{"id":9,"is_bot":false,"first_name":"U"},"inline_message_id":"im42","chat_instance":"ci1","data":"pick:2"}`
	var cq CallbackQuery
	if err := json.Unmarshal([]byte(raw), &cq); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cq.InlineMessageID != "im42" || cq.ChatInstance != "ci1" || cq.Data != "pick:2" {
		t.Errorf("unexpected callback query: %+v", cq)
	}
}

// MessageEntity (aliased as Entity) now carries url/user/etc. — a text_link
// entity used to silently lose its URL.
func TestMessageEntity_DecodesURLAndUser(t *testing.T) {
	raw := `{
		"message_id": 1, "date": 1700000000, "chat": {"id": 1, "type": "private"},
		"text": "click here or ping bob",
		"entities": [
			{"type": "text_link", "offset": 6, "length": 4, "url": "https://example.com"},
			{"type": "text_mention", "offset": 19, "length": 3, "user": {"id": 77, "is_bot": false, "first_name": "Bob"}}
		]
	}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(m.Entities))
	}
	if m.Entities[0].URL != "https://example.com" {
		t.Errorf("text_link URL = %q, want the link", m.Entities[0].URL)
	}
	if m.Entities[1].User == nil || m.Entities[1].User.ID != 77 {
		t.Errorf("text_mention user lost: %+v", m.Entities[1])
	}
}

// RichText's docs prose says it "can be either a String for plain text, an
// Array of RichText, or" one of its 24 named object members — the only
// union in the spec with primitive alternatives alongside its objects.
// unmarshalRichText only handled the object branch until this test was
// written: any incoming rich message with an unstyled span of text (the
// overwhelmingly common case — <p>plain text</p> encodes as a bare JSON
// string, not an object) failed to decode at all.
func TestRichText_PlainStringLeaf_Decodes(t *testing.T) {
	raw := `{"blocks":[{"type":"paragraph","text":"just plain text, no styling"}]}`
	var rm RichMessage
	if err := json.Unmarshal([]byte(raw), &rm); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, ok := rm.Blocks[0].(*RichBlockParagraph)
	if !ok {
		t.Fatalf("block[0] = %T, want *RichBlockParagraph", rm.Blocks[0])
	}
	plain, ok := p.Text.(RichPlainText)
	if !ok || string(plain) != "just plain text, no styling" {
		t.Errorf("Text = %#v, want RichPlainText(%q)", p.Text, "just plain text, no styling")
	}
}

// The array alternative: consecutive spans (plain text around a bold word)
// arrive concatenated in one JSON array instead of one object.
func TestRichText_ArrayOfMixedSpans_Decodes(t *testing.T) {
	raw := `{"blocks":[{"type":"paragraph","text":["Order ",{"type":"bold","text":"#1234"}," shipped."]}]}`
	var rm RichMessage
	if err := json.Unmarshal([]byte(raw), &rm); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p := rm.Blocks[0].(*RichBlockParagraph)
	seq, ok := p.Text.(RichTextSequence)
	if !ok || len(seq) != 3 {
		t.Fatalf("Text = %#v, want a 3-element RichTextSequence", p.Text)
	}
	if plain, ok := seq[0].(RichPlainText); !ok || plain != "Order " {
		t.Errorf("seq[0] = %#v, want RichPlainText(%q)", seq[0], "Order ")
	}
	bold, ok := seq[1].(*RichTextBold)
	if !ok {
		t.Fatalf("seq[1] = %T, want *RichTextBold", seq[1])
	}
	if inner, ok := bold.Text.(RichPlainText); !ok || inner != "#1234" {
		t.Errorf("bold.Text = %#v, want RichPlainText(%q)", bold.Text, "#1234")
	}
	if plain, ok := seq[2].(RichPlainText); !ok || plain != " shipped." {
		t.Errorf("seq[2] = %#v, want RichPlainText(%q)", seq[2], " shipped.")
	}
}

// unmarshalRichText tries string, then array, then falls through to the
// object-discriminator switch — an unrecognized shape (not a string, not
// an array, and no "type" field) must still fail loudly, not return nil.
func TestRichText_UnrecognizedShape_ErrorsLoudly(t *testing.T) {
	if _, err := unmarshalRichText([]byte(`{"foo":"bar"}`)); err == nil {
		t.Error("expected an object with no type discriminator to error, got nil")
	}
}
