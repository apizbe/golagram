package golagram

import (
	"regexp"
	"strings"
)

// Filters are plain predicates over *Ctx (see [Filter], defined in
// router.go). Message-content filters read whichever message-shaped
// payload the update carries (c.anyMessage()), so the same filter works
// for [Router.Message], [Router.EditedMessage], [Router.ChannelPost], ...
// — and a filter that needs FSM state, the resolved chat/user, or the bot
// itself can reach all of them through the Ctx.

// filterMessage adapts a message predicate into a Filter, returning false
// when the update carries no message-shaped payload at all.
func filterMessage(pred func(m *Message) bool) Filter {
	return func(c *Ctx) bool {
		m := c.anyMessage()
		return m != nil && pred(m)
	}
}

// FilterCommand matches a bot command the way Telegram clients actually send
// them: "/start", "/start args...", and — in groups — "/start@my_bot". A
// mention addressed to a different bot does not match, so two bots in one
// group don't answer each other's commands. The command name itself is
// matched case-insensitively (clients happily send "/Start"); read
// arguments in the handler via c.Command().Args.
//
// Reads text-or-caption, same as [Ctx.Command] — a photo captioned
// "/report spam" matches FilterCommand("report") exactly like a plain text
// "/report spam" message, which is how media-heavy bots (channel reposts,
// user reports on a photo) actually receive commands.
func FilterCommand(command string) Filter {
	return func(c *Ctx) bool {
		m := c.anyMessage()
		if m == nil {
			return false
		}
		cmd, ok := ParseCommand(m.textOrCaption())
		if !ok || !strings.EqualFold(cmd.Command, command) {
			return false
		}
		if cmd.Mention != "" && !strings.EqualFold(cmd.Mention, c.botUsername) {
			return false
		}
		return true
	}
}

// FilterText matches a message whose text (or caption, for media messages
// without text) equals text exactly.
func FilterText(text string) Filter {
	return filterMessage(func(m *Message) bool { return m.textOrCaption() == text })
}

// FilterTextEqualFold matches a message whose text (or caption) equals
// text, ignoring case. For prefix/suffix/contains, case-insensitivity is
// rare enough in practice that callers can fold both sides themselves:
// FilterRegexp(regexp.MustCompile(`(?i)^hi\b`)).
func FilterTextEqualFold(text string) Filter {
	return filterMessage(func(m *Message) bool { return strings.EqualFold(m.textOrCaption(), text) })
}

// FilterTextPrefix matches a message whose text (or caption) starts with
// prefix.
func FilterTextPrefix(prefix string) Filter {
	return filterMessage(func(m *Message) bool { return strings.HasPrefix(m.textOrCaption(), prefix) })
}

// FilterTextSuffix matches a message whose text (or caption) ends with
// suffix.
func FilterTextSuffix(suffix string) Filter {
	return filterMessage(func(m *Message) bool { return strings.HasSuffix(m.textOrCaption(), suffix) })
}

// FilterTextContains matches a message whose text (or caption) contains
// substr.
func FilterTextContains(substr string) Filter {
	return filterMessage(func(m *Message) bool { return strings.Contains(m.textOrCaption(), substr) })
}

// FilterRegexp matches a message whose text (or caption, for media
// messages without text) matches re. Compile the pattern once at
// registration time — regexp.MustCompile(`^\d+$`) — not inside a handler.
func FilterRegexp(re *regexp.Regexp) Filter {
	return filterMessage(func(m *Message) bool { return re.MatchString(m.textOrCaption()) })
}

// FilterCallbackData matches a callback query whose data equals data exactly.
func FilterCallbackData(data string) Filter {
	return func(c *Ctx) bool {
		return c.CallbackQuery != nil && c.CallbackQuery.Data == data
	}
}

// FilterCallbackPrefix matches callback data by prefix — the standard
// pattern for parameterized buttons like "buy:42". For typed payloads see
// [NewCallbackData].
func FilterCallbackPrefix(prefix string) Filter {
	return func(c *Ctx) bool {
		return c.CallbackQuery != nil && strings.HasPrefix(c.CallbackQuery.Data, prefix)
	}
}

// Chat/user identity filters.

// FilterChatType matches updates whose resolved chat (c.Chat()) is one of
// the given types: "private", "group", "supergroup", "channel". The first
// thing every group bot needs.
func FilterChatType(types ...string) Filter {
	return func(c *Ctx) bool {
		chat := c.Chat()
		if chat == nil {
			return false
		}
		for _, t := range types {
			if chat.Type == t {
				return true
			}
		}
		return false
	}
}

// FilterFromUser matches updates from any of the given user IDs — admin
// commands, owner-only handlers.
func FilterFromUser(ids ...int64) Filter {
	return func(c *Ctx) bool {
		from := c.From()
		if from == nil {
			return false
		}
		for _, id := range ids {
			if from.ID == id {
				return true
			}
		}
		return false
	}
}

// FilterFromSender matches updates whose [Ctx.Sender] identity is any of
// the given IDs. Unlike FilterFromUser (which reads c.From() — Telegram's
// shared dummy user for an anonymous group admin or channel post, so no
// real ID ever matches one), this resolves through Sender() first: an
// anonymous admin matches by their group's chat ID and a channel post
// matches by the channel's ID, the only identity Telegram gives either of
// them. Use it to allow-list "posts from channel X" or "any anonymous
// admin of chat Y" the way FilterFromUser allow-lists real users — it is
// not a substitute for FilterFromUser when you specifically need a real
// person (an anonymous admin's actual account is never recoverable).
func FilterFromSender(ids ...int64) Filter {
	return func(c *Ctx) bool {
		s := c.Sender()
		if s == nil {
			return false
		}
		id := s.ID()
		if id == 0 {
			return false
		}
		for _, want := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
}

// FilterFromUsername matches updates from any of the given @usernames — a
// leading "@" is accepted and ignored either side, and the comparison is
// case-insensitive, matching how Telegram itself treats usernames. Prefer
// FilterFromUser by numeric ID where possible: a username can be changed or
// dropped, a user ID never changes.
func FilterFromUsername(usernames ...string) Filter {
	return func(c *Ctx) bool {
		from := c.From()
		if from == nil || from.Username == "" {
			return false
		}
		for _, u := range usernames {
			if strings.EqualFold(strings.TrimPrefix(u, "@"), from.Username) {
				return true
			}
		}
		return false
	}
}

// Message-shape filters.

// FilterIsForwarded matches a message forwarded from somewhere else
// (forward_origin is set).
func FilterIsForwarded() Filter {
	return filterMessage(func(m *Message) bool { return m.ForwardOrigin != nil })
}

// FilterIsReply matches a message that replies to another message.
func FilterIsReply() Filter {
	return filterMessage(func(m *Message) bool { return m.ReplyToMessage != nil })
}

// FilterIsTopicMessage matches a message sent inside a forum topic.
func FilterIsTopicMessage() Filter {
	return filterMessage(func(m *Message) bool { return m.IsTopicMessage })
}

// FilterViaBot matches a message sent via an inline bot (via_bot is set).
func FilterViaBot() Filter {
	return filterMessage(func(m *Message) bool { return m.ViaBot != nil })
}

// FilterMediaGroup matches a message that is part of an album (media group).
func FilterMediaGroup() Filter {
	return filterMessage(func(m *Message) bool { return m.MediaGroupID != "" })
}

// Content-type filters — match based on which kind of content a message
// carries, regardless of its text/caption.

// FilterPhoto matches a message carrying at least one photo size.
func FilterPhoto() Filter {
	return filterMessage(func(m *Message) bool { return len(m.Photo) > 0 })
}

// FilterDocument matches a message carrying a document (a generic file).
func FilterDocument() Filter {
	return filterMessage(func(m *Message) bool { return m.Document != nil })
}

// FilterSticker matches a message carrying a sticker.
func FilterSticker() Filter {
	return filterMessage(func(m *Message) bool { return m.Sticker != nil })
}

// FilterVoice matches a message carrying a voice note.
func FilterVoice() Filter {
	return filterMessage(func(m *Message) bool { return m.Voice != nil })
}

// FilterVideo matches a message carrying a video.
func FilterVideo() Filter {
	return filterMessage(func(m *Message) bool { return m.Video != nil })
}

// FilterVideoNote matches a message carrying a round video note.
func FilterVideoNote() Filter {
	return filterMessage(func(m *Message) bool { return m.VideoNote != nil })
}

// FilterAudio matches a message carrying an audio file.
func FilterAudio() Filter {
	return filterMessage(func(m *Message) bool { return m.Audio != nil })
}

// FilterAnimation matches a message carrying an animation (a GIF, or an
// H.264/MPEG-4 AVC video without sound).
func FilterAnimation() Filter {
	return filterMessage(func(m *Message) bool { return m.Animation != nil })
}

// FilterContact matches a message sharing a contact.
func FilterContact() Filter {
	return filterMessage(func(m *Message) bool { return m.Contact != nil })
}

// FilterLocation matches a message sharing a location.
func FilterLocation() Filter {
	return filterMessage(func(m *Message) bool { return m.Location != nil })
}

// FilterVenue matches a message sharing a venue.
func FilterVenue() Filter {
	return filterMessage(func(m *Message) bool { return m.Venue != nil })
}

// FilterPoll matches a message carrying a poll (the message announcing it,
// not a poll_answer or poll update — see [Router.Poll]).
func FilterPoll() Filter {
	return filterMessage(func(m *Message) bool { return m.Poll != nil })
}

// FilterDice matches a message carrying a dice-style roll (dice, dart,
// basketball, ...) — see [FilterDiceValue] to match a specific outcome.
func FilterDice() Filter {
	return filterMessage(func(m *Message) bool { return m.Dice != nil })
}

// FilterDiceValue matches a dice roll landing on any of the given values —
// e.g. FilterDiceValue(6) for a 🎲/🎯/🎳 six, or FilterDiceValue(1) for a
// "you lose" 🎰. Value ranges depend on the emoji (1-6 for dice/darts/bowling,
// 1-5 for basketball/football, 1-64 for the slot machine) — see
// [Dice.Emoji] to distinguish them if a handler cares which.
func FilterDiceValue(values ...int64) Filter {
	return filterMessage(func(m *Message) bool {
		if m.Dice == nil {
			return false
		}
		for _, v := range values {
			if m.Dice.Value == v {
				return true
			}
		}
		return false
	})
}

// FilterSuccessfulPayment matches the service message confirming a
// completed payment — the message a payments bot fulfills orders from.
func FilterSuccessfulPayment() Filter {
	return filterMessage(func(m *Message) bool { return m.SuccessfulPayment != nil })
}

// FilterWebAppData matches the service message carrying data sent from a
// Web App back to the bot.
func FilterWebAppData() Filter {
	return filterMessage(func(m *Message) bool { return m.WebAppData != nil })
}

// FilterNewChatMembers matches the service message announcing users added
// to a group.
func FilterNewChatMembers() Filter {
	return filterMessage(func(m *Message) bool { return len(m.NewChatMembers) > 0 })
}

// FilterLeftChatMember matches the service message announcing a user
// removed from (or leaving) a group.
func FilterLeftChatMember() Filter {
	return filterMessage(func(m *Message) bool { return m.LeftChatMember != nil })
}

// FilterHasEntity matches a message (or the caption of a media message)
// carrying at least one entity of any of the given types — e.g.
// FilterHasEntity(gg.EntityURL) for "this message contains a link". The
// Entity* constants are in consts.gen.go; formatting-only ones
// ([EntityBold], [EntityItalic], ...) are about rendering, not content, and
// are what the [Node] formatting builder produces instead.
func FilterHasEntity(types ...string) Filter {
	hasAny := func(entities []Entity) bool {
		for _, e := range entities {
			for _, t := range types {
				if e.Type == t {
					return true
				}
			}
		}
		return false
	}
	return filterMessage(func(m *Message) bool {
		return hasAny(m.Entities) || hasAny(m.CaptionEntities)
	})
}

// Chat-member transition filters — for r.ChatMember / r.MyChatMember,
// reading the old→new membership change as named predicates.

// memberUpdate returns whichever membership-change payload this update
// carries (chat_member or my_chat_member), or nil.
func memberUpdate(c *Ctx) *ChatMemberUpdated {
	if c.ChatMember != nil {
		return c.ChatMember
	}
	return c.MyChatMember
}

// ChatMemberStatus returns a [ChatMember] union value's status string
// ("creator", "administrator", "member", "restricted", "left", "kicked"),
// or "" for nil. [ChatMember.GetStatus] is generated (types.gen.go) from
// the fields every ChatMember member has in common, so this no longer
// hand-maintains a status string per member — it just can't drift from the
// spec the way a hand-written switch could.
func ChatMemberStatus(m ChatMember) string {
	if m == nil {
		return ""
	}
	return m.GetStatus()
}

// ChatMemberIsMember reports whether a ChatMember union value represents
// someone currently in the chat (owner/admin/member, or restricted with
// is_member true).
func ChatMemberIsMember(m ChatMember) bool {
	switch v := m.(type) {
	case *ChatMemberOwner, *ChatMemberAdministrator, *ChatMemberMember:
		return true
	case *ChatMemberRestricted:
		return v.IsMember
	default:
		return false
	}
}

// chatMemberIsAdmin reports whether a ChatMember union value represents an
// owner or administrator — the boundary FilterPromotedToAdmin checks for.
func chatMemberIsAdmin(m ChatMember) bool {
	switch m.(type) {
	case *ChatMemberOwner, *ChatMemberAdministrator:
		return true
	default:
		return false
	}
}

// FilterJoined matches a chat_member/my_chat_member update where the user
// went from not-in-the-chat to in-the-chat — the "welcome new member"
// trigger.
func FilterJoined() Filter {
	return func(c *Ctx) bool {
		u := memberUpdate(c)
		if u == nil || u.OldChatMember == nil || u.NewChatMember == nil {
			return false
		}
		return !ChatMemberIsMember(u.OldChatMember) && ChatMemberIsMember(u.NewChatMember)
	}
}

// FilterLeft matches a chat_member/my_chat_member update where the user
// went from in-the-chat to not-in-the-chat (left or was banned).
func FilterLeft() Filter {
	return func(c *Ctx) bool {
		u := memberUpdate(c)
		if u == nil || u.OldChatMember == nil || u.NewChatMember == nil {
			return false
		}
		return ChatMemberIsMember(u.OldChatMember) && !ChatMemberIsMember(u.NewChatMember)
	}
}

// FilterPromotedToAdmin matches a chat_member/my_chat_member update where
// the user gained admin rights (member/restricted → administrator/creator).
func FilterPromotedToAdmin() Filter {
	return func(c *Ctx) bool {
		u := memberUpdate(c)
		if u == nil || u.OldChatMember == nil || u.NewChatMember == nil {
			return false
		}
		return !chatMemberIsAdmin(u.OldChatMember) && chatMemberIsAdmin(u.NewChatMember)
	}
}

// FilterBotBlocked matches a my_chat_member update in a private chat where
// the user blocked the bot — the signal to stop messaging them.
func FilterBotBlocked() Filter {
	return func(c *Ctx) bool {
		u := c.MyChatMember
		if u == nil || u.Chat == nil || u.Chat.Type != "private" || u.NewChatMember == nil {
			return false
		}
		_, banned := u.NewChatMember.(*ChatMemberBanned)
		return banned
	}
}

// FilterBotUnblocked matches a my_chat_member update in a private chat
// where the user unblocked the bot.
func FilterBotUnblocked() Filter {
	return func(c *Ctx) bool {
		u := c.MyChatMember
		if u == nil || u.Chat == nil || u.Chat.Type != "private" || u.OldChatMember == nil || u.NewChatMember == nil {
			return false
		}
		_, wasBanned := u.OldChatMember.(*ChatMemberBanned)
		return wasBanned && ChatMemberIsMember(u.NewChatMember)
	}
}

// FSM state filters.

// NoState matches a user who has no FSM state set, i.e. isn't in a conversation.
const NoState State = ""

// StateIs matches if the FSM state of whoever this update is from is one of
// the given states — on any update kind, so a conversational flow can route
// its callback-query steps by state exactly like its message steps. Pass
// [AnyState] to match any state other than [NoState].
//
// A storage error (the FSM backend is down) also matches false — an
// operator would otherwise see "bot ignores users mid-wizard" with no clue
// why, so the error is logged (via the bot's [WithLogger], falling back to
// the standard logger) before returning false.
func StateIs(states ...State) Filter {
	return func(c *Ctx) bool {
		current, err := c.FSM().State()
		if err != nil {
			logFSMFilterError(c, err)
			return false
		}
		for _, s := range states {
			if s == AnyState && current != NoState {
				return true
			}
			if s == current {
				return true
			}
		}
		return false
	}
}

// StateIn matches if the current FSM state belongs to the given
// [StateGroup] — the whole conversation at once, without listing every
// step.
//
// A storage error also matches false, logged the same way [StateIs] logs
// one.
func StateIn(group StateGroup) Filter {
	return func(c *Ctx) bool {
		current, err := c.FSM().State()
		if err != nil {
			logFSMFilterError(c, err)
			return false
		}
		return group.Contains(current)
	}
}

// logFSMFilterError logs an FSM storage error encountered while evaluating
// a state filter, through the bot's logger if one is reachable from c —
// nil only in tests that construct a Ctx without a bot.
func logFSMFilterError(c *Ctx, err error) {
	if b := c.Bot(); b != nil {
		b.logErrorf("golagram: FSM state lookup failed during filter evaluation: %v", err)
	}
}

// FilterCommandStart matches the /start command — including deep links,
// where t.me/your_bot?start=ref_12345 arrives as "/start ref_12345". Read
// the payload in the handler via c.Command().Args:
//
//	r.Message(gg.FilterCommandStart()).Handle(func(c *gg.Ctx) error {
//		payload := c.Command().Args // "ref_12345", or "" for a plain /start
//		...
//	})
func FilterCommandStart() Filter {
	return FilterCommand("start")
}

// FilterCommandStartDeepLink matches only a /start carrying a deep-link
// payload (t.me/your_bot?start=...), letting referral/deep-link flows route
// separately from a plain /start.
func FilterCommandStartDeepLink() Filter {
	return func(c *Ctx) bool {
		if !FilterCommand("start")(c) {
			return false
		}
		cmd := c.Command()
		return cmd != nil && cmd.Args != ""
	}
}

// Combinators.

// And combines filters so all of them must match. Rarely needed directly —
// multiple filters passed to a registration already AND — but useful inside
// Or.
func And(filters ...Filter) Filter {
	return func(c *Ctx) bool {
		for _, f := range filters {
			if !f(c) {
				return false
			}
		}
		return true
	}
}

// Or combines filters so at least one must match.
func Or(filters ...Filter) Filter {
	return func(c *Ctx) bool {
		for _, f := range filters {
			if f(c) {
				return true
			}
		}
		return false
	}
}

// Not inverts a filter.
func Not(f Filter) Filter {
	return func(c *Ctx) bool {
		return !f(c)
	}
}
