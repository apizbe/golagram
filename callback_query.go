package golagram

import "context"

// CallbackQuery's struct definition is generated (types.gen.go) with every
// field the Bot API spec lists — including inline_message_id and
// chat_instance — plus unexported bindings the dispatcher sets. This file
// holds the hand-written sugar.
//
// Note: per the spec, Message may be an inaccessible placeholder when the
// original message is too old — check [Message.IsInaccessible] before
// relying on anything beyond Chat and MessageID.

// ctx returns the context bound at hydration (the bot's run context),
// falling back to context.Background for an unbound CallbackQuery.
func (e *CallbackQuery) ctx() context.Context {
	if e.boundCtx != nil {
		return e.boundCtx
	}
	return context.Background()
}

// ChatID returns the chat the callback's message lives in. When the original
// message is missing entirely (inline mode), it falls back to the clicking
// user's ID — the private chat with them.
func (e *CallbackQuery) ChatID() int64 {
	if e.Message != nil && e.Message.Chat != nil {
		return e.Message.Chat.ID
	}
	if e.From != nil {
		return e.From.ID
	}
	return 0
}

// FromID returns the ID of the user who pressed the button.
func (e *CallbackQuery) FromID() int64 {
	if e.From == nil {
		return 0
	}
	return e.From.ID
}

// Answer acknowledges the callback query via answerCallbackQuery — this is
// what stops the client-side loading spinner on the pressed button. Call it
// in every callback handler. Empty text just dismisses the spinner; non-empty
// text shows a toast, or an alert box with &AnswerCallbackOptions{ShowAlert: true}.
func (e *CallbackQuery) Answer(text string, options ...*AnswerCallbackOptions) error {
	req := &AnswerCallbackQueryRequest{CallbackQueryID: e.ID, Text: text}
	if len(options) > 0 && options[0] != nil {
		o := options[0]
		req.ShowAlert = o.ShowAlert
		req.URL = o.URL
		req.CacheTime = o.CacheTime
	}
	_, err := e.api.Call(e.ctx(), "answerCallbackQuery", req)
	if err == nil {
		e.answered = true
	}
	return err
}

// Answered reports whether Answer has already been called on this callback
// query — [CallbackAnswerMiddleware] reads this to avoid double-answering
// one that a handler already dismissed itself.
func (e *CallbackQuery) Answered() bool {
	return e.answered
}

// SendMessage sends a new message into the chat the callback's message came
// from and returns the sent message.
func (e *CallbackQuery) SendMessage(text string, options ...*SendMessageOptions) (*Message, error) {
	req := &SendMessageRequest{ChatID: ChatIDFromInt(e.ChatID()), Text: text}
	if len(options) > 0 {
		options[0].applyTo(req)
	}
	if e.Message != nil {
		e.Message.applyDefaults(req)
	}
	return sendMessage(e.ctx(), e.api, e.fsm, e.fsmStrategy, e.botUsername(), e.logf, req)
}

// Reply replies to the message the callback was attached to and returns the
// sent message.
func (e *CallbackQuery) Reply(text string, options ...*SendMessageOptions) (*Message, error) {
	req := &SendMessageRequest{ChatID: ChatIDFromInt(e.ChatID()), Text: text}
	if len(options) > 0 {
		options[0].applyTo(req)
	}
	if e.Message != nil {
		if req.ReplyParameters == nil {
			req.ReplyParameters = &ReplyParameters{MessageID: e.Message.MessageID}
		}
		e.Message.applyDefaults(req)
	}
	return sendMessage(e.ctx(), e.api, e.fsm, e.fsmStrategy, e.botUsername(), e.logf, req)
}

func (e *CallbackQuery) botUsername() string {
	if e.Message != nil {
		return e.Message.botUsername
	}
	return ""
}

// FSM returns the conversation state context for the user who clicked this
// button, scoped per the bot's [FSMKeyStrategy] (by default: this user in
// the chat the original message lives in).
func (e *CallbackQuery) FSM() *FSMContext {
	var thread int64
	if e.Message != nil {
		thread = e.Message.threadID()
	}
	return &FSMContext{
		ctx:     e.ctx(),
		storage: e.fsm,
		key:     e.fsmStrategy.apply(e.ChatID(), e.FromID(), thread),
	}
}
