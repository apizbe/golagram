package golagram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// msgCtx and cbCtx build minimal Ctx values for filter tests — filters take
// *Ctx, so tests wrap the payload in an Update the same way dispatch does.
func msgCtx(m *Message) *Ctx {
	return &Ctx{Update: &Update{Message: m}}
}

func cbCtx(cq *CallbackQuery) *Ctx {
	return &Ctx{Update: &Update{CallbackQuery: cq}}
}

func TestFilterCommand(t *testing.T) {
	filter := FilterCommand("start")

	cases := []struct {
		name        string
		text        string
		botUsername string
		want        bool
	}{
		{"bare command", "/start", "my_bot", true},
		{"command with args (deep link)", "/start ref_12345", "my_bot", true},
		{"command mentioning this bot", "/start@my_bot", "my_bot", true},
		{"mention is case-insensitive", "/start@My_Bot", "my_bot", true},
		{"command name is case-insensitive", "/Start", "my_bot", true},
		{"mention with args", "/start@my_bot ref_1", "my_bot", true},
		{"command mentioning another bot", "/start@other_bot", "my_bot", false},
		{"different command", "/stop", "my_bot", false},
		{"not a command", "start", "my_bot", false},
		{"empty text", "", "my_bot", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := msgCtx(&Message{Text: c.text})
			ctx.botUsername = c.botUsername
			got := filter(ctx)
			if got != c.want {
				t.Errorf("FilterCommand(%q) on %q = %v, want %v", "start", c.text, got, c.want)
			}
		})
	}
}

// erroringFSMStorage fails every call — stands in for a backend outage
// (Redis down, etc.) in TestStateIs_StorageError_LogsAndMatchesFalse.
type erroringFSMStorage struct{}

func (erroringFSMStorage) SetState(context.Context, StorageKey, State) error { return errFSMDown }
func (erroringFSMStorage) GetState(context.Context, StorageKey) (State, error) {
	return "", errFSMDown
}
func (erroringFSMStorage) SetData(context.Context, StorageKey, map[string]any) error {
	return errFSMDown
}
func (erroringFSMStorage) GetData(context.Context, StorageKey) (map[string]any, error) {
	return nil, errFSMDown
}
func (erroringFSMStorage) UpdateData(context.Context, StorageKey, map[string]any) (map[string]any, error) {
	return nil, errFSMDown
}
func (erroringFSMStorage) Clear(context.Context, StorageKey) error { return errFSMDown }

var errFSMDown = errors.New("fsm storage down")

func TestStateIs_StorageError_LogsAndMatchesFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	bot := newTestBot(server)
	var buf bytes.Buffer
	bot.logger = slog.New(slog.NewTextHandler(&buf, nil))

	c := newCtx(context.Background(), &Update{Message: &Message{
		Chat: &Chat{ID: 1, Type: "private"},
		From: &User{ID: 2},
	}}, bot, nil, erroringFSMStorage{}, "test_bot")

	if StateIs(AnyState)(c) {
		t.Error("expected StateIs to match false on a storage error")
	}
	if StateIn(StateGroup("x"))(c) {
		t.Error("expected StateIn to match false on a storage error")
	}
	if !strings.Contains(buf.String(), "fsm storage down") {
		t.Errorf("expected the storage error to be logged, got: %s", buf.String())
	}
}

func TestCtxTextFallsBackToCaption(t *testing.T) {
	if got := msgCtx(&Message{Text: "hi"}).Text(); got != "hi" {
		t.Errorf("Ctx.Text() with Text set = %q, want %q", got, "hi")
	}
	if got := msgCtx(&Message{Caption: "hi"}).Text(); got != "hi" {
		t.Errorf("Ctx.Text() with only Caption set = %q, want %q", got, "hi")
	}
	if got := (&Ctx{Update: &Update{}}).Text(); got != "" {
		t.Errorf("Ctx.Text() with no message = %q, want empty", got)
	}
}

func TestFilterCommandMatchesCaption(t *testing.T) {
	filter := FilterCommand("report")
	ctx := msgCtx(&Message{Caption: "/report spam"})
	ctx.botUsername = "my_bot"
	if !filter(ctx) {
		t.Error("expected FilterCommand to match a command sent as a photo caption")
	}
	if cmd := ctx.Message.Command(); cmd == nil || cmd.Args != "spam" {
		t.Errorf("Message.Command() on a captioned command = %+v, want Args %q", cmd, "spam")
	}
}

func TestFilterText(t *testing.T) {
	filter := FilterText("hello")

	if !filter(msgCtx(&Message{Text: "hello"})) {
		t.Error("expected exact text match to pass")
	}
	if filter(msgCtx(&Message{Text: "Hello"})) {
		t.Error("expected case-sensitive mismatch to fail")
	}
	if filter(msgCtx(&Message{Text: "hello world"})) {
		t.Error("expected substring to fail (FilterText is exact match)")
	}
	if filter(&Ctx{Update: &Update{}}) {
		t.Error("expected an update with no message to fail")
	}
	if !filter(msgCtx(&Message{Caption: "hello"})) {
		t.Error("expected FilterText to match a caption when the message has no text")
	}
}

func TestFilterTextPrefixAndContains(t *testing.T) {
	if !FilterTextPrefix("/set")(msgCtx(&Message{Text: "/settings now"})) {
		t.Error("FilterTextPrefix should match")
	}
	if FilterTextPrefix("/set")(msgCtx(&Message{Text: "go /set"})) {
		t.Error("FilterTextPrefix should not match mid-string")
	}
	if !FilterTextContains("world")(msgCtx(&Message{Text: "hello world"})) {
		t.Error("FilterTextContains should match")
	}
}

func TestFilterTextSuffix(t *testing.T) {
	if !FilterTextSuffix("!")(msgCtx(&Message{Text: "hello world!"})) {
		t.Error("FilterTextSuffix should match")
	}
	if FilterTextSuffix("!")(msgCtx(&Message{Text: "hello! world"})) {
		t.Error("FilterTextSuffix should not match mid-string")
	}
}

func TestFilterTextEqualFold(t *testing.T) {
	filter := FilterTextEqualFold("hello")
	if !filter(msgCtx(&Message{Text: "hello"})) {
		t.Error("expected exact match to pass")
	}
	if !filter(msgCtx(&Message{Text: "HELLO"})) {
		t.Error("expected case-insensitive match to pass")
	}
	if filter(msgCtx(&Message{Text: "hello world"})) {
		t.Error("expected substring to fail")
	}
}

func TestFilterRegexp(t *testing.T) {
	digits := FilterRegexp(regexp.MustCompile(`^\d+$`))
	if !digits(msgCtx(&Message{Text: "12345"})) {
		t.Error("expected digits to match")
	}
	if digits(msgCtx(&Message{Text: "12a45"})) {
		t.Error("expected non-digits to fail")
	}
	// Falls back to the caption for media messages without text.
	if !digits(msgCtx(&Message{Caption: "42"})) {
		t.Error("expected caption fallback to match")
	}
}

func TestFilterCallbackData(t *testing.T) {
	filter := FilterCallbackData("buy")

	if !filter(cbCtx(&CallbackQuery{Data: "buy"})) {
		t.Error("expected exact callback data match to pass")
	}
	if filter(cbCtx(&CallbackQuery{Data: "sell"})) {
		t.Error("expected mismatched callback data to fail")
	}
	if filter(msgCtx(&Message{Text: "buy"})) {
		t.Error("expected a message update to fail a callback filter")
	}
}

func TestFilterCallbackPrefix(t *testing.T) {
	filter := FilterCallbackPrefix("buy:")

	if !filter(cbCtx(&CallbackQuery{Data: "buy:42"})) {
		t.Error("expected prefixed callback data to pass")
	}
	if filter(cbCtx(&CallbackQuery{Data: "sell:42"})) {
		t.Error("expected different prefix to fail")
	}
}

func TestFilterChatType(t *testing.T) {
	group := FilterChatType("group", "supergroup")

	if !group(msgCtx(&Message{Chat: &Chat{ID: 1, Type: "supergroup"}})) {
		t.Error("expected supergroup to match")
	}
	if group(msgCtx(&Message{Chat: &Chat{ID: 1, Type: "private"}})) {
		t.Error("expected private to fail")
	}
	// Resolves the chat for non-message kinds too.
	if !group(&Ctx{Update: &Update{ChatJoinRequest: &ChatJoinRequest{Chat: &Chat{ID: 2, Type: "group"}}}}) {
		t.Error("expected chat_join_request's chat to resolve")
	}
}

func TestFilterFromUser(t *testing.T) {
	admins := FilterFromUser(10, 20)

	if !admins(msgCtx(&Message{From: &User{ID: 20}})) {
		t.Error("expected listed user to match")
	}
	if admins(msgCtx(&Message{From: &User{ID: 30}})) {
		t.Error("expected unlisted user to fail")
	}
	if !admins(cbCtx(&CallbackQuery{From: &User{ID: 10}})) {
		t.Error("expected callback clicker to resolve via c.From()")
	}
}

func TestFilterFromSender(t *testing.T) {
	allowed := FilterFromSender(10, -100999)

	if !allowed(msgCtx(&Message{From: &User{ID: 10}, Chat: &Chat{ID: 1}})) {
		t.Error("expected an ordinary listed user to match")
	}
	if allowed(msgCtx(&Message{From: &User{ID: 999}, Chat: &Chat{ID: 1}})) {
		t.Error("expected an ordinary unlisted user to fail")
	}

	anonAdminChat := &Chat{ID: -100999, Type: "supergroup"}
	anonAdminMsg := &Message{
		Chat:       anonAdminChat,
		From:       &User{ID: 1087968824, Username: "GroupAnonymousBot"},
		SenderChat: anonAdminChat,
	}
	if !allowed(msgCtx(anonAdminMsg)) {
		t.Error("expected an anonymous admin of the listed chat to match by Sender().ID()")
	}
	if FilterFromSender(-100111)(msgCtx(anonAdminMsg)) {
		t.Error("expected an anonymous admin of a different, unlisted chat to fail")
	}

	// FilterFromUser("the chat's ID") can never match this update — it only
	// ever sees Telegram's shared dummy user, the same for every anonymous
	// admin everywhere. That's exactly the gap FilterFromSender covers.
	if FilterFromUser(-100999)(msgCtx(anonAdminMsg)) {
		t.Error("FilterFromUser matching a chat ID would be a coincidence, not real behavior")
	}
}

func TestFilterFromUsername(t *testing.T) {
	admins := FilterFromUsername("@Alice", "bob")

	if !admins(msgCtx(&Message{From: &User{Username: "alice"}})) {
		t.Error("expected case-insensitive, @-stripped match against 'alice'")
	}
	if !admins(msgCtx(&Message{From: &User{Username: "Bob"}})) {
		t.Error("expected case-insensitive match against 'bob'")
	}
	if admins(msgCtx(&Message{From: &User{Username: "carol"}})) {
		t.Error("expected unlisted username to fail")
	}
	if admins(msgCtx(&Message{From: &User{}})) {
		t.Error("expected a user with no username to fail")
	}
	if admins(msgCtx(&Message{})) {
		t.Error("expected a message with no sender to fail")
	}
}

func TestFilterMessageShape(t *testing.T) {
	if !FilterIsReply()(msgCtx(&Message{ReplyToMessage: &Message{MessageID: 1}})) {
		t.Error("FilterIsReply should match")
	}
	if !FilterIsForwarded()(msgCtx(&Message{ForwardOrigin: &MessageOriginUser{Type: "user"}})) {
		t.Error("FilterIsForwarded should match")
	}
	if !FilterIsTopicMessage()(msgCtx(&Message{IsTopicMessage: true, MessageThreadID: 7})) {
		t.Error("FilterIsTopicMessage should match")
	}
	if !FilterMediaGroup()(msgCtx(&Message{MediaGroupID: "g1"})) {
		t.Error("FilterMediaGroup should match")
	}
	if !FilterSuccessfulPayment()(msgCtx(&Message{SuccessfulPayment: &SuccessfulPayment{Currency: "XTR"}})) {
		t.Error("FilterSuccessfulPayment should match")
	}
	if !FilterWebAppData()(msgCtx(&Message{WebAppData: &WebAppData{Data: "x"}})) {
		t.Error("FilterWebAppData should match")
	}
	if !FilterNewChatMembers()(msgCtx(&Message{NewChatMembers: []User{{ID: 1}}})) {
		t.Error("FilterNewChatMembers should match")
	}
	if FilterIsReply()(msgCtx(&Message{Text: "plain"})) {
		t.Error("FilterIsReply should not match a plain message")
	}
}

func memberCtx(kind string, old, new ChatMember, chatType string) *Ctx {
	upd := &ChatMemberUpdated{Chat: &Chat{ID: 1, Type: chatType}, OldChatMember: old, NewChatMember: new}
	u := &Update{}
	if kind == "my_chat_member" {
		u.MyChatMember = upd
	} else {
		u.ChatMember = upd
	}
	return &Ctx{Update: u}
}

func TestFilterMemberTransitions(t *testing.T) {
	joined := memberCtx("chat_member", &ChatMemberLeft{}, &ChatMemberMember{}, "supergroup")
	left := memberCtx("chat_member", &ChatMemberMember{}, &ChatMemberBanned{}, "supergroup")
	promoted := memberCtx("chat_member", &ChatMemberMember{}, &ChatMemberAdministrator{}, "supergroup")
	restrictedIn := memberCtx("chat_member", &ChatMemberLeft{}, &ChatMemberRestricted{IsMember: true}, "supergroup")

	if !FilterJoined()(joined) {
		t.Error("FilterJoined should match left→member")
	}
	if !FilterJoined()(restrictedIn) {
		t.Error("FilterJoined should match left→restricted(is_member)")
	}
	if FilterJoined()(left) {
		t.Error("FilterJoined should not match member→kicked")
	}
	if !FilterLeft()(left) {
		t.Error("FilterLeft should match member→kicked")
	}
	if !FilterPromotedToAdmin()(promoted) {
		t.Error("FilterPromotedToAdmin should match member→administrator")
	}
	if FilterPromotedToAdmin()(joined) {
		t.Error("FilterPromotedToAdmin should not match a plain join")
	}

	blocked := memberCtx("my_chat_member", &ChatMemberMember{}, &ChatMemberBanned{}, "private")
	unblocked := memberCtx("my_chat_member", &ChatMemberBanned{}, &ChatMemberMember{}, "private")
	if !FilterBotBlocked()(blocked) {
		t.Error("FilterBotBlocked should match")
	}
	if !FilterBotUnblocked()(unblocked) {
		t.Error("FilterBotUnblocked should match")
	}
	if FilterBotBlocked()(memberCtx("my_chat_member", &ChatMemberMember{}, &ChatMemberBanned{}, "supergroup")) {
		t.Error("FilterBotBlocked should only match private chats")
	}
}

// StateIs must work on every update kind — the whole point of filters
// taking *Ctx. A callback query from a user in a state routes by it.
func TestStateIs_WorksOnCallbackQueries(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	const asking State = "reg:asking_age"
	key := StorageKey{ChatID: 5, UserID: 9}
	if err := storage.SetState(context.Background(), key, asking); err != nil {
		t.Fatal(err)
	}

	cq := &CallbackQuery{
		From:    &User{ID: 9},
		Message: &Message{Chat: &Chat{ID: 5}},
		Data:    "age:18-25",
	}
	c := cbCtx(cq)
	c.fsm = storage
	cq.fsm = storage

	if !StateIs(asking)(c) {
		t.Error("StateIs should match the callback clicker's state")
	}
	if StateIs("other")(c) {
		t.Error("StateIs should not match a different state")
	}
	if !StateIs(AnyState)(c) {
		t.Error("AnyState should match any non-empty state")
	}
}

func TestCombinators(t *testing.T) {
	yes := Filter(func(*Ctx) bool { return true })
	no := Filter(func(*Ctx) bool { return false })
	c := msgCtx(&Message{Text: "x"})

	if !And(yes, yes)(c) || And(yes, no)(c) {
		t.Error("And misbehaved")
	}
	if !Or(no, yes)(c) || Or(no, no)(c) {
		t.Error("Or misbehaved")
	}
	if Not(yes)(c) || !Not(no)(c) {
		t.Error("Not misbehaved")
	}
}

func TestStateIn(t *testing.T) {
	reg := StateGroup("registration")
	storage := NewMemoryStorage()
	key := StorageKey{ChatID: 1, UserID: 1}

	c := fsmCtx(storage, &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}})

	if StateIn(reg)(c) {
		t.Error("StateIn should not match with no state set")
	}

	storage.SetState(context.Background(), key, reg.New("waiting_name"))
	if !StateIn(reg)(c) {
		t.Error("StateIn should match a state belonging to the group")
	}

	storage.SetState(context.Background(), key, StateGroup("order").New("waiting_name"))
	if StateIn(reg)(c) {
		t.Error("StateIn should not match another group's state")
	}
}

func TestFilterCommandStart(t *testing.T) {
	plain := msgCtx(&Message{Text: "/start"})
	deepLink := msgCtx(&Message{Text: "/start ref_12345"})
	other := msgCtx(&Message{Text: "/help"})

	if !FilterCommandStart()(plain) || !FilterCommandStart()(deepLink) {
		t.Error("FilterCommandStart should match /start with or without a payload")
	}
	if FilterCommandStart()(other) {
		t.Error("FilterCommandStart should not match other commands")
	}

	if FilterCommandStartDeepLink()(plain) {
		t.Error("FilterCommandStartDeepLink should not match a bare /start")
	}
	if !FilterCommandStartDeepLink()(deepLink) {
		t.Error("FilterCommandStartDeepLink should match /start with a payload")
	}
	if cmd := deepLink.Command(); cmd.Args != "ref_12345" {
		t.Errorf("deep-link payload = %q, want ref_12345", cmd.Args)
	}
}
