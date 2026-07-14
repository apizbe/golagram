package golagram

import (
	"context"
	"fmt"
	"sync"

	"github.com/apizbe/golagram/internal/api"
)

// Ctx is what handlers receive, regardless of update kind: it carries the
// context.Context for cancellation, the raw [Update] (embedded, so
// c.Message, c.CallbackQuery, c.InlineQuery, ... all resolve directly), and
// sugar for the common actions ([Ctx.Answer], [Ctx.AnswerCallback],
// [Ctx.FSM]). [Message], [CallbackQuery], and friends stay pure data types;
// this is where the ergonomics live.
type Ctx struct {
	context.Context
	*Update

	bot         *TelegramBot
	api         *api.Client
	fsm         FSMStorage
	botUsername string

	// routeFlags is set by Router.dispatch right before running the
	// matched route's middleware+handler chain — see registration.WithFlags.
	routeFlags map[string]any

	// replySink is non-nil only when TelegramBot.dispatchSync (bot.go) is
	// dispatching this update for a registration that opted into
	// AllowWebhookReply — see tryEmbedWebhookReply.
	replySink *webhookReplySink

	mu     sync.Mutex
	values map[string]any
}

func newCtx(parent context.Context, u *Update, bot *TelegramBot, client *api.Client, fsm FSMStorage, botUsername string) *Ctx {
	return &Ctx{
		Context:     parent,
		Update:      u,
		bot:         bot,
		api:         client,
		fsm:         fsm,
		botUsername: botUsername,
	}
}

// attachWebhookReplySink equips c to capture a Reply(...) return value
// instead of it becoming a real API call — see tryEmbedWebhookReply.
func (c *Ctx) attachWebhookReplySink() *webhookReplySink {
	sink := &webhookReplySink{}
	c.replySink = sink
	return sink
}

// tryEmbedWebhookReply captures wr into c's sink so the webhook HTTP
// response can embed it, if c has one attached and wr is eligible (no local
// file upload — multipart can't ride in a JSON response body). Reports
// whether it succeeded; on false, the caller (resolveWebhookReply in
// bot.go) falls back to a real API call instead.
func (c *Ctx) tryEmbedWebhookReply(wr *WebhookReply) bool {
	if c.replySink == nil || api.HasUpload(wr.params) {
		return false
	}
	c.replySink.reply = wr
	return true
}

// Bot returns the bot that received this update, giving handlers access to
// the full generated Bot API surface ([TelegramBot.SendPhoto],
// [TelegramBot.BanChatMember], ...) — not just the [Ctx.Answer] /
// [Ctx.AnswerCallback] sugar available directly on Ctx.
func (c *Ctx) Bot() *TelegramBot {
	return c.bot
}

// Set stores a value on the Ctx, visible to every middleware/handler further
// down the chain for this update.
func (c *Ctx) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = make(map[string]any)
	}
	c.values[key] = value
}

// Get retrieves a value stored with Set.
func (c *Ctx) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[key]
	return v, ok
}

// Flags returns the metadata attached to the specific registration that
// matched this update via WithFlags, or nil if it wasn't used. Unlike
// [Ctx.Set] / [Ctx.Get] (a general value bag any handler/middleware can
// write to), flags are read-only from a handler's perspective — they're
// fixed at registration time — and scoped to exactly the one route that
// matched, not shared across the whole dispatch chain.
func (c *Ctx) Flags() map[string]any {
	return c.routeFlags
}

// anyMessage returns whichever message-shaped payload this update carries
// (message, edited_message, channel_post, ..., or a callback's attached
// message), or nil if none — the common case handlers reach for.
func (c *Ctx) anyMessage() *Message {
	switch {
	case c.Message != nil:
		return c.Message
	case c.EditedMessage != nil:
		return c.EditedMessage
	case c.ChannelPost != nil:
		return c.ChannelPost
	case c.EditedChannelPost != nil:
		return c.EditedChannelPost
	case c.BusinessMessage != nil:
		return c.BusinessMessage
	case c.EditedBusinessMessage != nil:
		return c.EditedBusinessMessage
	case c.GuestMessage != nil:
		return c.GuestMessage
	case c.CallbackQuery != nil:
		return c.CallbackQuery.Message
	default:
		return nil
	}
}

// Text returns the text of whichever message-shaped payload this update
// carries, falling back to its caption for a captioned media message ("" if
// there's neither — e.g. a poll_answer or chat_member update). Agrees with
// [FilterText] and friends, so a handler matched on a caption still gets it
// back here instead of "".
func (c *Ctx) Text() string {
	if m := c.anyMessage(); m != nil {
		return m.textOrCaption()
	}
	return ""
}

// Command returns the parsed command of whichever message-shaped payload
// this update carries, or nil if it isn't a command (or there's no message).
func (c *Ctx) Command() *CommandObject {
	if m := c.anyMessage(); m != nil {
		return m.Command()
	}
	return nil
}

// Chat returns the chat this update relates to, resolved per update kind
// (a message's chat, a callback's message's chat, a chat_member update's
// chat, ...), or nil for kinds with no chat (inline_query, poll, ...).
func (c *Ctx) Chat() *Chat {
	if m := c.anyMessage(); m != nil {
		return m.Chat
	}
	switch {
	case c.DeletedBusinessMessages != nil:
		return c.DeletedBusinessMessages.Chat
	case c.MessageReaction != nil:
		return c.MessageReaction.Chat
	case c.MessageReactionCount != nil:
		return c.MessageReactionCount.Chat
	case c.PollAnswer != nil:
		return c.PollAnswer.VoterChat
	case c.MyChatMember != nil:
		return c.MyChatMember.Chat
	case c.ChatMember != nil:
		return c.ChatMember.Chat
	case c.ChatJoinRequest != nil:
		return c.ChatJoinRequest.Chat
	case c.ChatBoost != nil:
		return c.ChatBoost.Chat
	case c.RemovedChatBoost != nil:
		return c.RemovedChatBoost.Chat
	default:
		return nil
	}
}

// From returns the user this update is "from" — the message sender, the
// callback clicker, the inline querier, the poll voter, ... — or nil for
// kinds with no user (poll, message_reaction_count, channel posts).
func (c *Ctx) From() *User {
	switch {
	case c.CallbackQuery != nil:
		return c.CallbackQuery.From
	case c.BusinessConnection != nil:
		return c.BusinessConnection.User
	case c.MessageReaction != nil:
		return c.MessageReaction.User
	case c.InlineQuery != nil:
		return c.InlineQuery.From
	case c.ChosenInlineResult != nil:
		return c.ChosenInlineResult.From
	case c.ShippingQuery != nil:
		return c.ShippingQuery.From
	case c.PreCheckoutQuery != nil:
		return c.PreCheckoutQuery.From
	case c.PurchasedPaidMedia != nil:
		return c.PurchasedPaidMedia.From
	case c.PollAnswer != nil:
		return c.PollAnswer.User
	case c.MyChatMember != nil:
		return c.MyChatMember.From
	case c.ChatMember != nil:
		return c.ChatMember.From
	case c.ChatJoinRequest != nil:
		return c.ChatJoinRequest.From
	case c.ManagedBot != nil:
		return c.ManagedBot.User
	default:
		if m := c.anyMessage(); m != nil {
			return m.From
		}
		return nil
	}
}

// Answer sends a message into whichever chat this update relates to —
// whichever message-shaped payload is present, or the chat a callback
// query's message lives in — propagating the source message's business
// connection and forum topic like [Message.Answer] does. Returns an error
// if the update carries neither (e.g. inline_query, poll_answer).
func (c *Ctx) Answer(text string, options ...*SendMessageOptions) (*Message, error) {
	if m := c.anyMessage(); m != nil {
		return m.Answer(text, options...)
	}
	return nil, fmt.Errorf("Ctx.Answer: this update has no message to answer into")
}

// AnswerCallback acknowledges the callback query via answerCallbackQuery —
// dismissing the client-side loading spinner. Returns an error if this
// update isn't a callback query.
func (c *Ctx) AnswerCallback(text string, options ...*AnswerCallbackOptions) error {
	if c.CallbackQuery == nil {
		return fmt.Errorf("Ctx.AnswerCallback: this update is not a callback query")
	}
	return c.CallbackQuery.Answer(text, options...)
}

// AnswerInline answers an inline query via answerInlineQuery. Returns an
// error if this update isn't an inline query.
func (c *Ctx) AnswerInline(results []InlineQueryResult, options ...*AnswerInlineOptions) error {
	if c.InlineQuery == nil {
		return fmt.Errorf("Ctx.AnswerInline: this update is not an inline query")
	}
	req := &AnswerInlineQueryRequest{InlineQueryID: c.InlineQuery.ID, Results: results}
	if len(options) > 0 && options[0] != nil {
		req.CacheTime = options[0].CacheTime
		req.IsPersonal = options[0].IsPersonal
		req.NextOffset = options[0].NextOffset
		req.Button = options[0].Button
	}
	_, err := c.api.Call(c, "answerInlineQuery", req)
	return err
}

// Reply replies to whichever message-shaped payload this update carries
// (or a callback query's attached message) and returns the sent message —
// like Answer, but sets the reply target to the original message.
// Returns an error if the update carries neither.
func (c *Ctx) Reply(text string, options ...*SendMessageOptions) (*Message, error) {
	if m := c.anyMessage(); m != nil {
		return m.Reply(text, options...)
	}
	return nil, fmt.Errorf("Ctx.Reply: this update has no message to reply to")
}

// EditText edits the text of whichever message-shaped payload this update
// carries (or a callback query's attached message) and returns the edited
// message. Returns an error if the update carries neither.
func (c *Ctx) EditText(text string, options ...*EditMessageOptions) (*Message, error) {
	if m := c.anyMessage(); m != nil {
		return m.EditText(text, options...)
	}
	return nil, fmt.Errorf("Ctx.EditText: this update has no message to edit")
}

// EditReplyMarkup edits only the inline keyboard of whichever message-shaped
// payload this update carries (or a callback query's attached message).
// Returns an error if the update carries neither.
func (c *Ctx) EditReplyMarkup(markup *InlineKeyboardMarkup) (*Message, error) {
	if m := c.anyMessage(); m != nil {
		return m.EditReplyMarkup(markup)
	}
	return nil, fmt.Errorf("Ctx.EditReplyMarkup: this update has no message to edit")
}

// EditCaption edits the caption of whichever message-shaped payload this
// update carries (or a callback query's attached message). Returns an
// error if the update carries neither.
func (c *Ctx) EditCaption(caption string, options ...*EditCaptionOptions) (*Message, error) {
	if m := c.anyMessage(); m != nil {
		return m.EditCaption(caption, options...)
	}
	return nil, fmt.Errorf("Ctx.EditCaption: this update has no message to edit")
}

// Delete deletes whichever message-shaped payload this update carries (or
// a callback query's attached message). Returns an error if the update
// carries neither.
func (c *Ctx) Delete() error {
	if m := c.anyMessage(); m != nil {
		return m.Delete()
	}
	return fmt.Errorf("Ctx.Delete: this update has no message to delete")
}

// SendChatAction broadcasts a chat action (e.g. "typing") into whichever
// chat this update relates to. Returns an error if the update carries no
// message-shaped payload to resolve a chat from.
func (c *Ctx) SendChatAction(action string) error {
	if m := c.anyMessage(); m != nil {
		return m.SendChatAction(action)
	}
	return fmt.Errorf("Ctx.SendChatAction: this update has no chat to send an action to")
}

// FSM returns the conversation state context scoped to whoever this update
// is "from" — the message sender, the callback clicker, the poll voter,
// etc. — per the bot's [FSMKeyStrategy]. Update kinds with no natural
// per-user/chat identity ([Poll]) share one global key. The Ctx itself is
// the bound context, so FSM storage calls are canceled with the handler.
func (c *Ctx) FSM() *FSMContext {
	return &FSMContext{ctx: c, storage: c.fsm, key: c.storageKey()}
}

// keyStrategy returns the bot's configured [FSMKeyStrategy] (the zero
// value, [FSMKeyChatUser], for a Ctx constructed without a bot in tests).
func (c *Ctx) keyStrategy() FSMKeyStrategy {
	if c.bot == nil {
		return FSMKeyChatUser
	}
	return c.bot.fsmStrategy
}

// storageKey resolves the {chat, user, thread} identity of this update via
// [Ctx.identityTriple], substituting Sender().ID() for the user component
// when the bot was constructed with [WithSenderIdentity] (see that
// option's doc for why), then folds the result through the bot's
// [FSMKeyStrategy].
func (c *Ctx) storageKey() StorageKey {
	chat, user, thread := c.identityTriple()
	if c.bot != nil && c.bot.senderIdentity {
		user = 0
		if s := c.Sender(); s != nil {
			user = s.ID()
		}
	}
	return c.keyStrategy().apply(chat, user, thread)
}

// identityTriple resolves the raw {chat, user, thread} identity of this
// update, one case per update kind — the pre-[WithSenderIdentity] values
// [Ctx.storageKey] folds through the bot's [FSMKeyStrategy]. Kinds lacking
// a chat or a user use 0 for that part of the triple — Telegram chat/user
// IDs are never 0, so this can't collide with a real key.
func (c *Ctx) identityTriple() (chat, user, thread int64) {
	if m := c.anyMessage(); m != nil && c.CallbackQuery == nil {
		return m.ChatID(), m.FromID(), m.threadID()
	}
	switch {
	case c.CallbackQuery != nil:
		var thread int64
		if m := c.CallbackQuery.Message; m != nil {
			thread = m.threadID()
		}
		return c.CallbackQuery.ChatID(), c.CallbackQuery.FromID(), thread
	case c.BusinessConnection != nil:
		return c.BusinessConnection.UserChatID, userID(c.BusinessConnection.User), 0
	case c.DeletedBusinessMessages != nil:
		return chatID(c.DeletedBusinessMessages.Chat), 0, 0
	case c.MessageReaction != nil:
		return chatID(c.MessageReaction.Chat), userID(c.MessageReaction.User), 0
	case c.MessageReactionCount != nil:
		return chatID(c.MessageReactionCount.Chat), 0, 0
	case c.InlineQuery != nil:
		return 0, userID(c.InlineQuery.From), 0
	case c.ChosenInlineResult != nil:
		return 0, userID(c.ChosenInlineResult.From), 0
	case c.ShippingQuery != nil:
		return 0, userID(c.ShippingQuery.From), 0
	case c.PreCheckoutQuery != nil:
		return 0, userID(c.PreCheckoutQuery.From), 0
	case c.PurchasedPaidMedia != nil:
		return 0, userID(c.PurchasedPaidMedia.From), 0
	case c.PollAnswer != nil:
		return chatID(c.PollAnswer.VoterChat), userID(c.PollAnswer.User), 0
	case c.MyChatMember != nil:
		return chatID(c.MyChatMember.Chat), userID(c.MyChatMember.From), 0
	case c.ChatMember != nil:
		return chatID(c.ChatMember.Chat), userID(c.ChatMember.From), 0
	case c.ChatJoinRequest != nil:
		return chatID(c.ChatJoinRequest.Chat), userID(c.ChatJoinRequest.From), 0
	case c.ChatBoost != nil:
		return chatID(c.ChatBoost.Chat), 0, 0
	case c.RemovedChatBoost != nil:
		return chatID(c.RemovedChatBoost.Chat), 0, 0
	case c.ManagedBot != nil:
		return 0, userID(c.ManagedBot.User), 0
	default:
		return 0, 0, 0
	}
}

func chatID(c *Chat) int64 {
	if c == nil {
		return 0
	}
	return c.ID
}

func userID(u *User) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}
