package golagram

import (
	"encoding/json"
	"errors"
	"fmt"
)

// webhookMethodNamer is implemented by every generated *...Request type in
// methods.gen.go, recovering the Bot API method name it belongs to —
// "sendMessage", not "SendMessageRequest" — without a hand-maintained
// type->name table. Unexported so only golagram's own generated request
// types can satisfy it.
type webhookMethodNamer interface {
	webhookMethod() string
}

// WebhookReply pairs a generated request with the Bot API method name it
// belongs to, built by [Reply]. Its JSON encoding is exactly the shape
// Telegram's reply-in-webhook-response optimization expects: the request's
// own fields, plus a top-level "method" field naming which Bot API method
// to invoke with them.
type WebhookReply struct {
	method string
	params any
}

// Method returns the Bot API method name this reply calls (e.g.
// "sendMessage") — mainly for a custom dispatch loop that drives [Router]
// directly (see [AsWebhookReply]) and needs to perform the call itself.
func (wr *WebhookReply) Method() string { return wr.method }

// Params returns the request this reply calls Method with — the same
// value passed to [Reply].
func (wr *WebhookReply) Params() any { return wr.params }

// MarshalJSON renders wr as Telegram's webhook-response-body shape: params'
// own JSON fields with "method" added alongside them.
func (wr *WebhookReply) MarshalJSON() ([]byte, error) {
	paramsJSON, err := json.Marshal(wr.params)
	if err != nil {
		return nil, fmt.Errorf("golagram: encoding webhook reply for %s: %w", wr.method, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(paramsJSON, &fields); err != nil {
		return nil, fmt.Errorf("golagram: webhook reply for %s did not encode as a JSON object: %w", wr.method, err)
	}
	methodJSON, err := json.Marshal(wr.method)
	if err != nil {
		return nil, err
	}
	fields["method"] = methodJSON
	return json.Marshal(fields)
}

// Reply wraps req (e.g. &gg.SendMessageRequest{...}) so a handler can
// return it from [HandlerFunc] instead of calling
// [TelegramBot.SendMessage] /etc. directly. What happens to it depends on
// how this update was dispatched:
//
//   - A registration that opted into AllowWebhookReply, which
//     [TelegramBot.RunWebhook]'s [TelegramBot.Handler] dispatches
//     synchronously in the HTTP request goroutine because it matched this
//     update: req is embedded directly in the webhook HTTP response body
//     — Telegram's documented reply-in-response optimization, no
//     follow-up HTTPS call.
//   - Anything else — polling, a registration that didn't opt in, or req
//     itself carrying a local file upload (multipart can't ride in a JSON
//     response body): req is sent as a normal, fire-and-forget API call
//     instead. Either way, the call gets made.
//
// In both cases the call's result is discarded — Reply trades the typed
// response value (the sent Message, its ID, ...) for behaving identically
// regardless of dispatch mode. If you need that value back, call
// [TelegramBot.SendMessage] /etc. directly instead of using Reply.
func Reply(req webhookMethodNamer) error {
	return &webhookReplyError{&WebhookReply{method: req.webhookMethod(), params: req}}
}

// webhookReplyError is what Reply returns: not a real failure, just a
// vehicle for a handler to hand a *WebhookReply back to the dispatch layer
// through HandlerFunc's existing `error` return, without adding a second
// return value or changing HandlerFunc's signature.
type webhookReplyError struct {
	reply *WebhookReply
}

// Error implements the error interface.
func (e *webhookReplyError) Error() string {
	return fmt.Sprintf("golagram: Reply(%s) wasn't resolved — if you're driving Router without TelegramBot's dispatch, use AsWebhookReply(err) to recover it and perform the call yourself", e.reply.method)
}

// AsWebhookReply extracts the *WebhookReply from err, if it is (or wraps)
// one returned by Reply. TelegramBot's own dispatch (bot.go) always
// resolves one before it could reach a caller; this is for code that
// drives Router directly instead of going through TelegramBot.
func AsWebhookReply(err error) (*WebhookReply, bool) {
	var re *webhookReplyError
	if errors.As(err, &re) {
		return re.reply, true
	}
	return nil, false
}

// webhookReplySink is attached to a Ctx only by TelegramBot.dispatchSync
// (bot.go), for a registration that opted into AllowWebhookReply and
// matched while RunWebhook's Handler was deciding how to dispatch this
// update. Its presence is what lets Ctx.tryEmbedWebhookReply succeed
// instead of the reply falling back to a real API call.
type webhookReplySink struct {
	reply *WebhookReply
}
