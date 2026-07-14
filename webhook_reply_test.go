package golagram

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestReply_AsWebhookReply_RoundTrips(t *testing.T) {
	err := Reply(&SendMessageRequest{ChatID: ChatIDFromInt(1), Text: "hi"})

	wr, ok := AsWebhookReply(err)
	if !ok {
		t.Fatal("expected AsWebhookReply to recover a *WebhookReply from Reply's return value")
	}
	if wr.Method() != "sendMessage" {
		t.Errorf("Method() = %q, want %q", wr.Method(), "sendMessage")
	}
	if req, ok := wr.Params().(*SendMessageRequest); !ok || req.Text != "hi" {
		t.Errorf("Params() = %+v, want the original *SendMessageRequest", wr.Params())
	}
}

func TestAsWebhookReply_FalseForOrdinaryError(t *testing.T) {
	if _, ok := AsWebhookReply(errors.New("boom")); ok {
		t.Error("expected AsWebhookReply to report false for an unrelated error")
	}
	if _, ok := AsWebhookReply(nil); ok {
		t.Error("expected AsWebhookReply to report false for a nil error")
	}
}

func TestWebhookReply_MarshalJSON_MergesMethodIntoParams(t *testing.T) {
	err := Reply(&SendMessageRequest{ChatID: ChatIDFromInt(555), Text: "pong"})
	wr, _ := AsWebhookReply(err)

	data, err := json.Marshal(wr)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("output wasn't valid JSON: %v", err)
	}
	if got["method"] != "sendMessage" {
		t.Errorf(`method = %v, want "sendMessage"`, got["method"])
	}
	if got["text"] != "pong" {
		t.Errorf(`text = %v, want "pong"`, got["text"])
	}
	if got["chat_id"].(float64) != 555 {
		t.Errorf("chat_id = %v, want 555", got["chat_id"])
	}
}

func TestRouter_MatchingAllowsWebhookReply(t *testing.T) {
	r := NewRouter()
	r.Message(FilterCommand("plain")).Handle(func(c *Ctx) error { return nil })
	r.Message(FilterCommand("fast")).AllowWebhookReply().Handle(func(c *Ctx) error { return nil })

	if r.matchingAllowsWebhookReply(ctxFor(&Update{Message: &Message{Text: "/plain"}})) {
		t.Error("expected a registration without AllowWebhookReply to report false")
	}
	if !r.matchingAllowsWebhookReply(ctxFor(&Update{Message: &Message{Text: "/fast"}})) {
		t.Error("expected the AllowWebhookReply registration to report true")
	}
	if r.matchingAllowsWebhookReply(ctxFor(&Update{Message: &Message{Text: "/nomatch"}})) {
		t.Error("expected no match to report false")
	}
}

// AllowWebhookReply set on a registration inside an Include'd sub-router
// must still be found by matchingAllowsWebhookReply, since Handler
// (webhook.go) needs to know before dispatch which registration — however
// deep in the router tree — will actually run.
func TestRouter_MatchingAllowsWebhookReply_ThroughInclude(t *testing.T) {
	r := NewRouter()
	sub := NewRouter()
	sub.Message(FilterCommand("fast")).AllowWebhookReply().Handle(func(c *Ctx) error { return nil })
	r.Include(sub)

	if !r.matchingAllowsWebhookReply(ctxFor(&Update{Message: &Message{Text: "/fast"}})) {
		t.Error("expected AllowWebhookReply on a sub-router's registration to be visible from the parent")
	}
}
