package golagram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/apizbe/golagram/internal/api"
)

// Message's struct definition is generated (types.gen.go) with every field
// the Bot API spec lists, plus unexported bindings the dispatcher sets
// (bot.go: bindMessage). This file holds the hand-written sugar on top: the
// methods that let an incoming message respond to itself.

// ChatID returns the ID of the chat the message was sent in (0 if unknown).
func (e *Message) ChatID() int64 {
	if e.Chat == nil {
		return 0
	}
	return e.Chat.ID
}

// FromID returns the sender's user ID (0 for channel posts and other
// messages without a sender).
func (e *Message) FromID() int64 {
	if e.From == nil {
		return 0
	}
	return e.From.ID
}

// IsInaccessible reports whether this is Telegram's InaccessibleMessage
// placeholder — a message the bot can no longer read (too old, or from
// before the bot joined), carrying only Chat, MessageID, and Date == 0.
// golagram flattens the spec's MaybeInaccessibleMessage union into Message
// (see types.gen.go), so this is the check that distinguishes the two —
// most relevant on CallbackQuery.Message.
func (e *Message) IsInaccessible() bool {
	return e.Date == 0
}

// ctx returns the context bound at hydration — the bot's run context, so
// sugar calls (Answer, Delete, ...) are canceled when the bot shuts down —
// falling back to context.Background for an unbound Message.
func (e *Message) ctx() context.Context {
	if e.boundCtx != nil {
		return e.boundCtx
	}
	return context.Background()
}

// applyDefaults propagates this message's own context into an outgoing
// request: the business connection it arrived through, and — for forum
// supergroups — the topic it lives in, so answering a message in a topic
// lands in that topic instead of General. An explicitly set value always
// wins.
func (e *Message) applyDefaults(req *SendMessageRequest) {
	if req.BusinessConnectionID == "" {
		req.BusinessConnectionID = e.BusinessConnectionID
	}
	if req.MessageThreadID == 0 && e.IsTopicMessage {
		req.MessageThreadID = e.MessageThreadID
	}
}

// sendMessage is the one code path every outgoing sendMessage call takes:
// pre-flight validation, the API call, and decoding + re-binding the sent
// message so callers can keep working with it (edit it, delete it, ...).
func sendMessage(ctx context.Context, client *api.Client, fsm FSMStorage, strategy FSMKeyStrategy, botUsername string, logf func(string, ...any), req *SendMessageRequest) (*Message, error) {
	if err := validateOutgoingText(req.Text); err != nil {
		return nil, err
	}
	if err := validateReplyMarkup(req.ReplyMarkup); err != nil {
		return nil, err
	}

	raw, err := client.Call(ctx, "sendMessage", req)
	if err != nil {
		return nil, err
	}

	var sent Message
	if err := json.Unmarshal(raw, &sent); err != nil {
		return nil, fmt.Errorf("failed to decode sent message: %w", err)
	}
	sent.api = client
	sent.fsm = fsm
	sent.fsmStrategy = strategy
	sent.botUsername = botUsername
	sent.boundCtx = ctx
	sent.logf = logf
	return &sent, nil
}

// Answer sends a message to the same chat and returns the sent message,
// enabling the send-then-edit progress flow:
//
//	m, err := message.Answer("⏳ working...")
//	// ... do work ...
//	m.EditText("✅ done")
//
// Optional parameters ride in via SendMessageOptions:
//
//	message.Answer("text", &SendMessageOptions{ParseMode: "HTML"})
//	message.Answer("text", &SendMessageOptions{ReplyMarkup: keyboard})
func (e *Message) Answer(text string, options ...*SendMessageOptions) (*Message, error) {
	req := &SendMessageRequest{ChatID: ChatIDFromInt(e.ChatID()), Text: text}
	if len(options) > 0 {
		options[0].applyTo(req)
	}
	e.applyDefaults(req)
	return sendMessage(e.ctx(), e.api, e.fsm, e.fsmStrategy, e.botUsername, e.logf, req)
}

// Reply sends a reply to this message and returns the sent message. The
// reply target rides in reply_parameters; pass your own
// SendMessageOptions.ReplyParameters to override (e.g. to quote a specific
// part of the message).
func (e *Message) Reply(text string, options ...*SendMessageOptions) (*Message, error) {
	req := &SendMessageRequest{ChatID: ChatIDFromInt(e.ChatID()), Text: text}
	if len(options) > 0 {
		options[0].applyTo(req)
	}
	if req.ReplyParameters == nil {
		req.ReplyParameters = &ReplyParameters{MessageID: e.MessageID}
	}
	e.applyDefaults(req)
	return sendMessage(e.ctx(), e.api, e.fsm, e.fsmStrategy, e.botUsername, e.logf, req)
}

// EditText edits this message's text (and optionally its inline keyboard)
// and returns the edited message. To edit a different message in the
// chat, use [TelegramBot.EditMessageText] with an explicit message_id.
func (e *Message) EditText(text string, options ...*EditMessageOptions) (*Message, error) {
	if err := validateOutgoingText(text); err != nil {
		return nil, err
	}

	req := &EditMessageTextRequest{
		ChatID:               ChatIDFromInt(e.ChatID()),
		MessageID:            e.MessageID,
		Text:                 text,
		BusinessConnectionID: e.BusinessConnectionID,
	}
	if len(options) > 0 && options[0] != nil {
		o := options[0]
		req.ParseMode = o.ParseMode
		req.Entities = o.Entities
		req.LinkPreviewOptions = o.LinkPreviewOptions
		req.ReplyMarkup = o.ReplyMarkup
		if o.BusinessConnectionID != "" {
			req.BusinessConnectionID = o.BusinessConnectionID
		}
	}

	raw, err := e.api.Call(e.ctx(), "editMessageText", req)
	if err != nil {
		return nil, err
	}
	return decodeEditedMessage(e.ctx(), raw, e.api, e.fsm, e.fsmStrategy, e.botUsername, e.logf)
}

// EditReplyMarkup edits only this message's inline keyboard and returns the
// edited message. Pass an empty (non-nil) *InlineKeyboardMarkup to remove
// the keyboard entirely — editMessageText's caveat applies here too:
// omitting ReplyMarkup leaves an existing keyboard untouched, it doesn't
// clear it.
func (e *Message) EditReplyMarkup(markup *InlineKeyboardMarkup) (*Message, error) {
	req := &EditMessageReplyMarkupRequest{
		ChatID:               ChatIDFromInt(e.ChatID()),
		MessageID:            e.MessageID,
		ReplyMarkup:          markup,
		BusinessConnectionID: e.BusinessConnectionID,
	}
	raw, err := e.api.Call(e.ctx(), "editMessageReplyMarkup", req)
	if err != nil {
		return nil, err
	}
	return decodeEditedMessage(e.ctx(), raw, e.api, e.fsm, e.fsmStrategy, e.botUsername, e.logf)
}

// EditCaption edits this media message's caption and returns the edited
// message.
func (e *Message) EditCaption(caption string, options ...*EditCaptionOptions) (*Message, error) {
	req := &EditMessageCaptionRequest{
		ChatID:               ChatIDFromInt(e.ChatID()),
		MessageID:            e.MessageID,
		Caption:              caption,
		BusinessConnectionID: e.BusinessConnectionID,
	}
	if len(options) > 0 && options[0] != nil {
		o := options[0]
		req.ParseMode = o.ParseMode
		req.CaptionEntities = o.CaptionEntities
		req.ShowCaptionAboveMedia = o.ShowCaptionAboveMedia
		req.ReplyMarkup = o.ReplyMarkup
		if o.BusinessConnectionID != "" {
			req.BusinessConnectionID = o.BusinessConnectionID
		}
	}
	raw, err := e.api.Call(e.ctx(), "editMessageCaption", req)
	if err != nil {
		return nil, err
	}
	return decodeEditedMessage(e.ctx(), raw, e.api, e.fsm, e.fsmStrategy, e.botUsername, e.logf)
}

// decodeEditedMessage handles every edit*'s dual return shape: Telegram
// returns the edited Message normally, but the literal `true` instead when
// acting on an inline message (inline_message_id, no chat_id/message_id) —
// there's nothing to decode in that case.
func decodeEditedMessage(ctx context.Context, raw []byte, client *api.Client, fsm FSMStorage, strategy FSMKeyStrategy, botUsername string, logf func(string, ...any)) (*Message, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
		return nil, nil
	}
	var edited Message
	if err := json.Unmarshal(raw, &edited); err != nil {
		return nil, fmt.Errorf("failed to decode edited message: %w", err)
	}
	edited.api = client
	edited.fsm = fsm
	edited.fsmStrategy = strategy
	edited.botUsername = botUsername
	edited.boundCtx = ctx
	edited.logf = logf
	return &edited, nil
}

// Delete deletes this message.
func (e *Message) Delete() error {
	req := &DeleteMessageRequest{ChatID: ChatIDFromInt(e.ChatID()), MessageID: e.MessageID}
	_, err := e.api.Call(e.ctx(), "deleteMessage", req)
	return err
}

// DeleteAfter schedules this message's deletion d from now — the
// self-destructing confirmation toast:
//
//	m, _ := c.Answer("Saved ✅")
//	m.DeleteAfter(5 * time.Second)
//
// The delete is best-effort: it runs on a timer after the handler has
// returned, so a failure is logged (via [WithLogger] if configured) rather
// than returned, and bot shutdown cancels it silently. Stop the returned
// timer to call it off.
func (e *Message) DeleteAfter(d time.Duration) *time.Timer {
	ctx := e.ctx()
	return time.AfterFunc(d, func() {
		if ctx.Err() != nil {
			return
		}
		if err := e.Delete(); err != nil {
			e.logErrorf("DeleteAfter: deleting message %d in chat %d: %v", e.MessageID, e.ChatID(), err)
		}
	})
}

// logErrorf routes sugar-path errors to the owning bot's logger (bound at
// hydration/send), falling back to the standard library logger for a
// Message that was never bound to a bot.
func (e *Message) logErrorf(format string, args ...any) {
	if e.logf != nil {
		e.logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// SendChatAction broadcasts a chat action (e.g. "typing", "upload_photo")
// into this message's chat — useful right before a slow operation so the
// user sees something happening in the meantime.
func (e *Message) SendChatAction(action string) error {
	req := &SendChatActionRequest{
		ChatID:               ChatIDFromInt(e.ChatID()),
		Action:               action,
		BusinessConnectionID: e.BusinessConnectionID,
	}
	_, err := e.api.Call(e.ctx(), "sendChatAction", req)
	return err
}

// threadID returns the forum topic this message lives in, or 0 when it's
// not a topic message — the identity [FSMKeyUserInTopic] scopes state by.
func (e *Message) threadID() int64 {
	if e.IsTopicMessage {
		return e.MessageThreadID
	}
	return 0
}

// FSM returns the conversation state context for the user who sent this
// message, scoped per the bot's [FSMKeyStrategy] (by default: this user in
// this chat).
func (e *Message) FSM() *FSMContext {
	return &FSMContext{
		ctx:     e.ctx(),
		storage: e.fsm,
		key:     e.fsmStrategy.apply(e.ChatID(), e.FromID(), e.threadID()),
	}
}
