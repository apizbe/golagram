package golagram

// Update mirrors one Telegram update. Updates are decoded into a zero
// value, so exactly one payload pointer is non-nil per update (see
// [Update.Kind]) — nil-checks are meaningful and routing keys off which
// field is present.
type Update struct {
	// UpdateID is Telegram's own sequential identifier for this update —
	// the value to resume getUpdates from (see [WithDropPendingUpdates]
	// and the offset parameter it drives).
	UpdateID int64 `json:"update_id"`

	// Message is set for a new incoming message of any kind — text,
	// photo, sticker, etc. See [Router.Message].
	Message *Message `json:"message,omitempty"`
	// EditedMessage is set when a known message was edited.
	// See [Router.EditedMessage].
	EditedMessage *Message `json:"edited_message,omitempty"`
	// ChannelPost is set for a new post in a channel the bot can read.
	// See [Router.ChannelPost].
	ChannelPost *Message `json:"channel_post,omitempty"`
	// EditedChannelPost is set when a channel post was edited.
	// See [Router.EditedChannelPost].
	EditedChannelPost *Message `json:"edited_channel_post,omitempty"`
	// BusinessConnection is set when the bot was connected to or
	// disconnected from a Telegram Business account, or the connection
	// was edited. See [Router.BusinessConnection].
	BusinessConnection *BusinessConnection `json:"business_connection,omitempty"`
	// BusinessMessage is set for a new message received through a
	// connected business account. See [Router.BusinessMessage].
	BusinessMessage *Message `json:"business_message,omitempty"`
	// EditedBusinessMessage is set when a business-account message was
	// edited. See [Router.EditedBusinessMessage].
	EditedBusinessMessage *Message `json:"edited_business_message,omitempty"`
	// DeletedBusinessMessages is set when messages were deleted from a
	// connected business account. See [Router.DeletedBusinessMessages].
	DeletedBusinessMessages *BusinessMessagesDeleted `json:"deleted_business_messages,omitempty"`
	// GuestMessage is set for a new guest message; reply via
	// Message.guest_query_id and answerGuestQuery. See [Router.GuestMessage].
	GuestMessage *Message `json:"guest_message,omitempty"`
	// MessageReaction is set when a user's reaction to a message changed
	// — requires the bot to be a chat admin and message_reaction to be in
	// allowed_updates; never fires for reactions set by other bots. See
	// [Router.MessageReaction].
	MessageReaction *MessageReactionUpdated `json:"message_reaction,omitempty"`
	// MessageReactionCount is set when anonymous reactions to a message
	// changed — same admin/allowed_updates requirement as
	// MessageReaction, but grouped and delivered with up to a few
	// minutes' delay. See [Router.MessageReactionCount].
	MessageReactionCount *MessageReactionCountUpdated `json:"message_reaction_count,omitempty"`
	// InlineQuery is set for a new incoming inline query. See
	// [Router.InlineQuery].
	InlineQuery *InlineQuery `json:"inline_query,omitempty"`
	// ChosenInlineResult is set when a user picked one of the bot's
	// inline query results — only delivered if inline feedback collection
	// is enabled for the bot. See [Router.ChosenInlineResult].
	ChosenInlineResult *ChosenInlineResult `json:"chosen_inline_result,omitempty"`
	// CallbackQuery is set for a new incoming callback query (an inline
	// keyboard button press). See [Router.CallbackQuery].
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
	// ShippingQuery is set for a new shipping query — only for invoices
	// with a flexible price. See [Router.ShippingQuery].
	ShippingQuery *ShippingQuery `json:"shipping_query,omitempty"`
	// PreCheckoutQuery is set for a new pre-checkout query, carrying full
	// checkout details; must be answered within 10 seconds (see
	// [Ctx.AnswerPreCheckout]). See [Router.PreCheckoutQuery].
	PreCheckoutQuery *PreCheckoutQuery `json:"pre_checkout_query,omitempty"`
	// PurchasedPaidMedia is set when a user purchased paid media with a
	// non-empty payload the bot sent in a non-channel chat. See
	// [Router.PurchasedPaidMedia].
	PurchasedPaidMedia *PaidMediaPurchased `json:"purchased_paid_media,omitempty"`
	// Poll is set for a poll's public state changing — only for polls the
	// bot sent, or that were manually stopped; this is not one voter's
	// answer (see PollAnswer). See [Router.Poll].
	Poll *Poll `json:"poll,omitempty"`
	// PollAnswer is set when a user (re)voted in a non-anonymous poll the
	// bot sent. See [Router.PollAnswer].
	PollAnswer *PollAnswer `json:"poll_answer,omitempty"`
	// MyChatMember is set when the bot's own membership status changed in
	// a chat — for private chats, only on block/unblock. See
	// [Router.MyChatMember].
	MyChatMember *ChatMemberUpdated `json:"my_chat_member,omitempty"`
	// ChatMember is set when some other member's status changed —
	// requires the bot to be a chat admin and chat_member to be in
	// allowed_updates. See [Router.ChatMember].
	ChatMember *ChatMemberUpdated `json:"chat_member,omitempty"`
	// ChatJoinRequest is set for a new join request — requires the bot to
	// have the can_invite_users admin right. See [Router.ChatJoinRequest].
	ChatJoinRequest *ChatJoinRequest `json:"chat_join_request,omitempty"`
	// ChatBoost is set when a chat boost was added or changed — requires
	// the bot to be a chat admin. See [Router.ChatBoost].
	ChatBoost *ChatBoostUpdated `json:"chat_boost,omitempty"`
	// RemovedChatBoost is set when a boost was removed from a chat —
	// requires the bot to be a chat admin. See [Router.RemovedChatBoost].
	RemovedChatBoost *ChatBoostRemoved `json:"removed_chat_boost,omitempty"`
	// ManagedBot is set when a bot managed by this bot was created, or a
	// managed bot's token or owner changed. See [Router.ManagedBot].
	ManagedBot *ManagedBotUpdated `json:"managed_bot,omitempty"`
}

// Kind returns which of Update's 25 payload fields is set, as Telegram's own
// JSON field name (e.g. "message", "callback_query") — the same strings
// [Router.UsedUpdateKinds] and allowed_updates use. Returns "" for a
// zero-value Update (shouldn't happen for anything Telegram actually
// sends, but kept honest rather than panicking or guessing).
func (u *Update) Kind() string {
	switch {
	case u.Message != nil:
		return "message"
	case u.EditedMessage != nil:
		return "edited_message"
	case u.ChannelPost != nil:
		return "channel_post"
	case u.EditedChannelPost != nil:
		return "edited_channel_post"
	case u.BusinessConnection != nil:
		return "business_connection"
	case u.BusinessMessage != nil:
		return "business_message"
	case u.EditedBusinessMessage != nil:
		return "edited_business_message"
	case u.DeletedBusinessMessages != nil:
		return "deleted_business_messages"
	case u.GuestMessage != nil:
		return "guest_message"
	case u.MessageReaction != nil:
		return "message_reaction"
	case u.MessageReactionCount != nil:
		return "message_reaction_count"
	case u.InlineQuery != nil:
		return "inline_query"
	case u.ChosenInlineResult != nil:
		return "chosen_inline_result"
	case u.CallbackQuery != nil:
		return "callback_query"
	case u.ShippingQuery != nil:
		return "shipping_query"
	case u.PreCheckoutQuery != nil:
		return "pre_checkout_query"
	case u.PurchasedPaidMedia != nil:
		return "purchased_paid_media"
	case u.Poll != nil:
		return "poll"
	case u.PollAnswer != nil:
		return "poll_answer"
	case u.MyChatMember != nil:
		return "my_chat_member"
	case u.ChatMember != nil:
		return "chat_member"
	case u.ChatJoinRequest != nil:
		return "chat_join_request"
	case u.ChatBoost != nil:
		return "chat_boost"
	case u.RemovedChatBoost != nil:
		return "removed_chat_boost"
	case u.ManagedBot != nil:
		return "managed_bot"
	default:
		return ""
	}
}
