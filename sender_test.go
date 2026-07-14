package golagram

import "testing"

func TestCtx_Sender_RegularMessage_IsUser(t *testing.T) {
	c := ctxFor(&Update{Message: &Message{
		Chat: &Chat{ID: 1, Type: "private"},
		From: &User{ID: 100, FirstName: "Aziz"},
	}})

	s := c.Sender()
	if s == nil {
		t.Fatal("Sender() = nil, want non-nil")
	}
	if s.ID() != 100 {
		t.Errorf("ID() = %d, want 100", s.ID())
	}
	if s.Name() != "Aziz" {
		t.Errorf("Name() = %q, want Aziz", s.Name())
	}
	if s.IsAnonymousAdmin() || s.IsChannelPost() || s.IsAnonymousChannel() || s.IsLinkedChannel() {
		t.Error("a regular user message should not match any anonymous predicate")
	}
}

func TestCtx_Sender_AnonymousAdmin(t *testing.T) {
	group := &Chat{ID: -100123, Type: "supergroup", Title: "My Group"}
	c := ctxFor(&Update{Message: &Message{
		Chat:            group,
		From:            &User{ID: 1087968824, IsBot: true, Username: "GroupAnonymousBot"},
		SenderChat:      group,
		AuthorSignature: "Moderator",
	}})

	s := c.Sender()
	if !s.IsAnonymousAdmin() {
		t.Error("expected IsAnonymousAdmin() = true")
	}
	if s.IsChannelPost() || s.IsAnonymousChannel() || s.IsLinkedChannel() {
		t.Error("anonymous admin should not match the other predicates")
	}
	if got := s.ID(); got != -100123 {
		t.Errorf("ID() = %d, want the group's ID (-100123), not the dummy user's", got)
	}
	if s.Name() != "My Group" {
		t.Errorf("Name() = %q, want My Group", s.Name())
	}
	if s.AuthorSignature != "Moderator" {
		t.Errorf("AuthorSignature = %q, want Moderator", s.AuthorSignature)
	}
}

func TestCtx_Sender_ChannelPost(t *testing.T) {
	channel := &Chat{ID: -1009999, Type: "channel", Title: "My Channel"}
	c := ctxFor(&Update{ChannelPost: &Message{
		Chat:       channel,
		SenderChat: channel,
	}})

	s := c.Sender()
	if !s.IsChannelPost() {
		t.Error("expected IsChannelPost() = true")
	}
	if s.IsAnonymousAdmin() || s.IsAnonymousChannel() || s.IsLinkedChannel() {
		t.Error("channel post should not match the other predicates")
	}
}

func TestCtx_Sender_LinkedChannelAutoForward(t *testing.T) {
	discussionGroup := &Chat{ID: -100555, Type: "supergroup"}
	linkedChannel := &Chat{ID: -100777, Type: "channel", Title: "Linked Channel"}
	c := ctxFor(&Update{Message: &Message{
		Chat:               discussionGroup,
		SenderChat:         linkedChannel,
		IsAutomaticForward: true,
	}})

	s := c.Sender()
	if !s.IsLinkedChannel() {
		t.Error("expected IsLinkedChannel() = true")
	}
	if s.IsAnonymousAdmin() || s.IsChannelPost() || s.IsAnonymousChannel() {
		t.Error("linked channel auto-forward should not match the other predicates")
	}
}

func TestCtx_Sender_AnonymousChannelPostingElsewhere(t *testing.T) {
	otherGroup := &Chat{ID: -100222, Type: "supergroup"}
	someChannel := &Chat{ID: -100888, Type: "channel", Title: "Some Channel"}
	c := ctxFor(&Update{Message: &Message{
		Chat:       otherGroup,
		SenderChat: someChannel,
		// IsAutomaticForward left false: this channel isn't linked to otherGroup,
		// it just posted into it directly (e.g. via "send as channel").
	}})

	s := c.Sender()
	if !s.IsAnonymousChannel() {
		t.Error("expected IsAnonymousChannel() = true")
	}
	if s.IsAnonymousAdmin() || s.IsChannelPost() || s.IsLinkedChannel() {
		t.Error("anonymous channel post should not match the other predicates")
	}
}

func TestCtx_Sender_AnonymousPollVoter(t *testing.T) {
	voterChat := &Chat{ID: -100333, Type: "supergroup", Title: "Anonymous Voters"}
	c := ctxFor(&Update{PollAnswer: &PollAnswer{
		PollID:    "p1",
		VoterChat: voterChat,
	}})

	s := c.Sender()
	if s.ID() != -100333 {
		t.Errorf("ID() = %d, want the voter chat's ID", s.ID())
	}
	// Poll answers carry no separate "landing chat" to compare against, so
	// the anonymity-kind predicates are meaningless here and must stay false.
	if s.IsAnonymousAdmin() || s.IsChannelPost() || s.IsAnonymousChannel() || s.IsLinkedChannel() {
		t.Error("poll-answer senders should never match the chat-kind predicates")
	}
}

func TestCtx_Sender_AnonymousReactor(t *testing.T) {
	chat := &Chat{ID: -100444, Type: "supergroup"}
	actorChat := &Chat{ID: -100444, Type: "supergroup", Title: "Reactor Chat"}
	c := ctxFor(&Update{MessageReaction: &MessageReactionUpdated{
		Chat:      chat,
		ActorChat: actorChat,
	}})

	s := c.Sender()
	if !s.IsAnonymousAdmin() {
		t.Error("expected IsAnonymousAdmin() = true for an anonymous reactor in the same chat")
	}
	if s.ID() != -100444 {
		t.Errorf("ID() = %d, want the actor chat's ID", s.ID())
	}
}

func TestCtx_Sender_CallbackQuery_FallsBackToUser(t *testing.T) {
	c := ctxFor(&Update{CallbackQuery: &CallbackQuery{
		From: &User{ID: 200, FirstName: "Clicker"},
	}})

	s := c.Sender()
	if s == nil {
		t.Fatal("Sender() = nil, want a User-only Sender for callback queries")
	}
	if s.ID() != 200 {
		t.Errorf("ID() = %d, want 200", s.ID())
	}
}

// TestCtx_Sender_CallbackQuery_WithAttachedMessage_UsesClickerNotMessage
// pins the bug where Sender() fell through to c.anyMessage() (which maps a
// callback query to its *attached* message, for content reads like
// FilterText) and read that message's From — usually this bot itself, or
// whoever originally posted it — instead of who actually clicked the
// button. Almost every real callback query has an attached message, so
// this was the common case, not an edge case.
func TestCtx_Sender_CallbackQuery_WithAttachedMessage_UsesClickerNotMessage(t *testing.T) {
	c := ctxFor(&Update{CallbackQuery: &CallbackQuery{
		From: &User{ID: 200, FirstName: "Clicker"},
		Message: &Message{
			From: &User{ID: 999, FirstName: "BotItself"},
			Chat: &Chat{ID: -100555, Type: "supergroup"},
		},
	}})

	s := c.Sender()
	if s == nil {
		t.Fatal("Sender() = nil, want the clicker")
	}
	if s.ID() != 200 {
		t.Errorf("ID() = %d, want 200 (the clicker), not the attached message's From", s.ID())
	}
}

func TestCtx_Sender_Poll_NoSenderConcept_IsNil(t *testing.T) {
	c := ctxFor(&Update{Poll: &Poll{ID: "p1"}})
	if s := c.Sender(); s != nil {
		t.Errorf("Sender() = %+v, want nil for a bare poll update", s)
	}
}
