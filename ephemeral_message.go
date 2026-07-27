package golagram

import "fmt"

// Ephemeral Messages (Bot API 10.2): a message visible only to one specific
// user in a group or supergroup, invisible to everyone else in the chat —
// sent, edited, and deleted through their own dedicated endpoints
// (sendMessage with receiver_user_id, editEphemeralMessage*,
// deleteEphemeralMessage), correlated by {chat, receiver user, ephemeral
// message ID} rather than the usual {chat, message ID}. Send one with
// [CallbackQuery.SendEphemeral], [Message.AnswerEphemeral], or
// [Ctx.AnswerEphemeral]; the returned *Message carries ReceiverUser and
// EphemeralMessageID, which the Edit*/Delete methods below read back off
// it — no need to track those IDs separately.

// SendEphemeral sends a message visible only to the user who pressed this
// button, correlated to the press via CallbackQueryID — the standard
// "private reply to a button" pattern, deliverable within Telegram's
// documented 15-second window of the press. To message a chat member
// outside that window (or without a button at all), the bot must be a
// chat administrator — see [Message.AnswerEphemeral].
func (e *CallbackQuery) SendEphemeral(text string, options ...*SendMessageOptions) (*Message, error) {
	req := &SendMessageRequest{
		ChatID:          ChatIDFromInt(e.ChatID()),
		Text:            text,
		ReceiverUserID:  e.FromID(),
		CallbackQueryID: e.ID,
	}
	if len(options) > 0 {
		options[0].applyTo(req)
	}
	if e.Message != nil {
		e.Message.applyDefaults(req)
	}
	return sendMessage(e.ctx(), e.api, e.fsm, e.fsmStrategy, e.botUsername(), e.logf, req)
}

// AnswerEphemeral sends a message visible only to this message's sender,
// into the same chat — the ephemeral counterpart to [Message.Answer]. With
// no button press to correlate it to, Telegram only accepts this when the
// bot is a chat administrator; from a button press, use
// [CallbackQuery.SendEphemeral] instead, which works for any bot within 15
// seconds of the press.
func (e *Message) AnswerEphemeral(text string, options ...*SendMessageOptions) (*Message, error) {
	req := &SendMessageRequest{
		ChatID:         ChatIDFromInt(e.ChatID()),
		Text:           text,
		ReceiverUserID: e.FromID(),
	}
	if len(options) > 0 {
		options[0].applyTo(req)
	}
	e.applyDefaults(req)
	return sendMessage(e.ctx(), e.api, e.fsm, e.fsmStrategy, e.botUsername, e.logf, req)
}

// AnswerEphemeral sends a message visible only to whichever user this
// update concerns: [CallbackQuery.SendEphemeral] (correlated to the button
// press) when the update is a callback query, [Message.AnswerEphemeral]
// (sent to the message's sender) otherwise. Returns an error if the update
// carries neither, same as [Ctx.Answer].
func (c *Ctx) AnswerEphemeral(text string, options ...*SendMessageOptions) (*Message, error) {
	if c.CallbackQuery != nil {
		return c.CallbackQuery.SendEphemeral(text, options...)
	}
	if m := c.anyMessage(); m != nil {
		return m.AnswerEphemeral(text, options...)
	}
	return nil, fmt.Errorf("Ctx.AnswerEphemeral: this update has no message or callback query to answer into")
}

// receiverUserID reads back the recipient Telegram assigned this ephemeral
// message when it was sent (via AnswerEphemeral/SendEphemeral) — 0 for a
// Message that was never sent as ephemeral, which the Edit*/Delete calls
// below let Telegram itself reject rather than golagram guessing.
func (e *Message) receiverUserID() int64 {
	if e.ReceiverUser == nil {
		return 0
	}
	return e.ReceiverUser.ID
}

// EditEphemeralText edits this ephemeral message's text — the ephemeral
// counterpart to [Message.EditText]. Unlike editMessageText,
// editEphemeralMessageText has no "return the edited Message" success
// shape, only an always-true bool, so this reports error only — same as
// [Message.Delete] does for deleteMessage's identical always-true reply.
func (e *Message) EditEphemeralText(text string, options ...*EditMessageOptions) error {
	if err := validateOutgoingText(text); err != nil {
		return err
	}
	req := &EditEphemeralMessageTextRequest{
		ChatID:             ChatIDFromInt(e.ChatID()),
		ReceiverUserID:     e.receiverUserID(),
		EphemeralMessageID: e.EphemeralMessageID,
		Text:               text,
	}
	if len(options) > 0 && options[0] != nil {
		o := options[0]
		req.ParseMode = o.ParseMode
		req.Entities = o.Entities
		req.LinkPreviewOptions = o.LinkPreviewOptions
		req.ReplyMarkup = o.ReplyMarkup
	}
	_, err := e.api.Call(e.ctx(), "editEphemeralMessageText", req)
	return err
}

// EditEphemeralCaption edits this ephemeral media message's caption — the
// ephemeral counterpart to [Message.EditCaption].
func (e *Message) EditEphemeralCaption(caption string, options ...*EditCaptionOptions) error {
	req := &EditEphemeralMessageCaptionRequest{
		ChatID:             ChatIDFromInt(e.ChatID()),
		ReceiverUserID:     e.receiverUserID(),
		EphemeralMessageID: e.EphemeralMessageID,
		Caption:            caption,
	}
	if len(options) > 0 && options[0] != nil {
		o := options[0]
		req.ParseMode = o.ParseMode
		req.CaptionEntities = o.CaptionEntities
		req.ReplyMarkup = o.ReplyMarkup
	}
	_, err := e.api.Call(e.ctx(), "editEphemeralMessageCaption", req)
	return err
}

// EditEphemeralReplyMarkup edits only this ephemeral message's inline
// keyboard — the ephemeral counterpart to [Message.EditReplyMarkup]. Pass
// an empty (non-nil) *InlineKeyboardMarkup to remove the keyboard
// entirely, same omitted-vs-empty caveat as EditReplyMarkup.
func (e *Message) EditEphemeralReplyMarkup(markup *InlineKeyboardMarkup) error {
	req := &EditEphemeralMessageReplyMarkupRequest{
		ChatID:             ChatIDFromInt(e.ChatID()),
		ReceiverUserID:     e.receiverUserID(),
		EphemeralMessageID: e.EphemeralMessageID,
		ReplyMarkup:        markup,
	}
	_, err := e.api.Call(e.ctx(), "editEphemeralMessageReplyMarkup", req)
	return err
}

// DeleteEphemeral deletes this ephemeral message — the ephemeral
// counterpart to [Message.Delete].
func (e *Message) DeleteEphemeral() error {
	req := &DeleteEphemeralMessageRequest{
		ChatID:             ChatIDFromInt(e.ChatID()),
		ReceiverUserID:     e.receiverUserID(),
		EphemeralMessageID: e.EphemeralMessageID,
	}
	_, err := e.api.Call(e.ctx(), "deleteEphemeralMessage", req)
	return err
}
