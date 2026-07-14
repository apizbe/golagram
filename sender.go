package golagram

// Sender resolves who is really behind an update, in the cases where
// Telegram reports the sender "as a chat" rather than as a user:
// anonymous group admins, channel posts, a channel's linked-discussion
// auto-forwards, and anonymous poll voters/reactors. In all of these,
// the update's User field is either absent or a fixed dummy value
// (Telegram's own docs call it a "fake sender user" for backward
// compatibility) — Chat carries the real identity instead.
//
// Only Message- and reaction-derived Senders populate the internal chatID
// used by the Is* predicates; a Sender derived from a poll answer leaves it
// unset (Telegram gives poll answers no separate "landing chat" to compare
// against), so [Sender.IsAnonymousAdmin] / [Sender.IsChannelPost] /
// [Sender.IsAnonymousChannel] / [Sender.IsLinkedChannel] always report
// false for one. Use [Sender.ID] / [Sender.Name] / [Sender.Username] for
// poll-answer senders instead.
type Sender struct {
	// User is set when the sender is a real user (including the dummy
	// user Telegram substitutes for backward compatibility — check Chat
	// first, since Chat being non-nil means User should be ignored).
	User *User
	// Chat is set when the update was sent "as" a chat: an anonymous
	// admin (as the group), a channel post or its linked-channel
	// auto-forward (as the channel), or an anonymous poll voter/reactor
	// (as whichever chat voted/reacted anonymously).
	Chat *Chat
	// IsAutomaticForward mirrors [Message.IsAutomaticForward]: true when
	// this is a channel post that was auto-forwarded into its linked
	// discussion group, as opposed to the channel posting to itself.
	IsAutomaticForward bool
	// AuthorSignature is the anonymous admin's custom title, or a
	// channel post's author signature, when Telegram provides one.
	AuthorSignature string

	chatID int64
}

// ID returns the sender's identifying ID: Chat.ID when sending as a chat,
// otherwise User.ID, otherwise 0.
func (s *Sender) ID() int64 {
	if s.Chat != nil {
		return s.Chat.ID
	}
	if s.User != nil {
		return s.User.ID
	}
	return 0
}

// Name returns the chat's title, or the user's first name, whichever
// applies.
func (s *Sender) Name() string {
	if s.Chat != nil {
		return s.Chat.Title
	}
	if s.User != nil {
		return s.User.FirstName
	}
	return ""
}

// Username returns the chat's or user's @username, whichever applies.
func (s *Sender) Username() string {
	if s.Chat != nil {
		return s.Chat.Username
	}
	if s.User != nil {
		return s.User.Username
	}
	return ""
}

// IsAnonymousAdmin reports whether this is an anonymous group/supergroup
// admin, sending as the group itself.
func (s *Sender) IsAnonymousAdmin() bool {
	return s.Chat != nil && s.Chat.ID == s.chatID && s.Chat.Type != "channel"
}

// IsChannelPost reports whether this is a channel posting to itself.
func (s *Sender) IsChannelPost() bool {
	return s.Chat != nil && s.Chat.ID == s.chatID && s.Chat.Type == "channel"
}

// IsAnonymousChannel reports whether this is a channel sending anonymously
// into a different chat (not an auto-forwarded linked-channel post).
func (s *Sender) IsAnonymousChannel() bool {
	return s.Chat != nil && s.Chat.ID != s.chatID && !s.IsAutomaticForward && s.Chat.Type == "channel"
}

// IsLinkedChannel reports whether this is a channel post that was
// automatically forwarded into its linked discussion group.
func (s *Sender) IsLinkedChannel() bool {
	return s.Chat != nil && s.Chat.ID != s.chatID && s.IsAutomaticForward
}

// Sender resolves the real identity behind this update — a message's
// sender, a reaction's actor, or a poll answer's voter — following through
// to the anonymous chat identity where Telegram provides one, instead of
// stopping at its dummy user. Returns nil for update kinds with no sender
// concept at all.
func (c *Ctx) Sender() *Sender {
	switch {
	case c.CallbackQuery != nil:
		// Must be checked before the default branch's c.anyMessage() call:
		// anyMessage() maps a callback query to its *attached* message (for
		// FilterText/Command-style content reads), but that message's From
		// is whoever/whatever sent it — often this bot itself — not the
		// user who clicked the button. CallbackQuery carries no
		// SenderChat/anonymous-chat concept of its own (Telegram's schema
		// has none), so From is the whole story here.
		return &Sender{User: c.CallbackQuery.From}
	case c.MessageReaction != nil:
		chatID := int64(0)
		if chat := c.MessageReaction.Chat; chat != nil {
			chatID = chat.ID
		}
		return &Sender{User: c.MessageReaction.User, Chat: c.MessageReaction.ActorChat, chatID: chatID}
	case c.PollAnswer != nil:
		return &Sender{User: c.PollAnswer.User, Chat: c.PollAnswer.VoterChat}
	default:
		if m := c.anyMessage(); m != nil {
			chatID := int64(0)
			if m.Chat != nil {
				chatID = m.Chat.ID
			}
			return &Sender{
				User:               m.From,
				Chat:               m.SenderChat,
				IsAutomaticForward: m.IsAutomaticForward,
				AuthorSignature:    m.AuthorSignature,
				chatID:             chatID,
			}
		}
		if u := c.From(); u != nil {
			return &Sender{User: u}
		}
		return nil
	}
}
