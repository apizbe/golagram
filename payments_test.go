package golagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewStarsInvoice_Shape(t *testing.T) {
	req := NewStarsInvoice(ChatIDFromInt(42), "Pro", "Unlock pro", "pro-1m", 250)
	if req.Currency != StarsCurrency {
		t.Errorf("Currency = %q, want XTR", req.Currency)
	}
	if req.ProviderToken != "" {
		t.Errorf("ProviderToken = %q, want empty (Stars requirement)", req.ProviderToken)
	}
	if len(req.Prices) != 1 || req.Prices[0].Amount != 250 || req.Prices[0].Label != "Pro" {
		t.Errorf("Prices = %+v, want exactly one 250-Star component", req.Prices)
	}
}

func TestCtx_SendStarsInvoice(t *testing.T) {
	var gotPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		decodeJSONBody(t, r, &body)
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 77}, From: &User{ID: 5}}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	if _, err := c.SendStarsInvoice("Pro", "Unlock pro", "pro-1m", 250); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/sendInvoice") {
		t.Errorf("called %s, want sendInvoice", gotPath)
	}
	if body["currency"] != "XTR" || body["chat_id"] != float64(77) {
		t.Errorf("body = %v", body)
	}
	if _, hasProviderToken := body["provider_token"]; hasProviderToken {
		t.Error("provider_token must be omitted for Stars")
	}

	chatless := ctxForBot(bot, &Update{Poll: &Poll{ID: "p"}})
	if _, err := chatless.SendStarsInvoice("t", "d", "p", 1); err == nil {
		t.Error("expected an error for an update with no chat")
	}
}

func TestCtx_AnswerPreCheckout(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerPreCheckoutQuery") {
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	bot := newTestBot(server)

	c := ctxForBot(bot, &Update{PreCheckoutQuery: &PreCheckoutQuery{ID: "pcq1", From: &User{ID: 5}}})
	if err := c.AnswerPreCheckout(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["pre_checkout_query_id"] != "pcq1" || body["ok"] != true {
		t.Errorf("approve body = %v", body)
	}
	if _, hasMsg := body["error_message"]; hasMsg {
		t.Error("error_message must be omitted on approval")
	}

	if err := c.AnswerPreCheckoutError("sold out"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["ok"] != false || body["error_message"] != "sold out" {
		t.Errorf("decline body = %v", body)
	}

	wrongKind := ctxForBot(bot, &Update{Message: bindMessage(&Message{MessageID: 1, Chat: &Chat{ID: 1}}, bot)})
	if err := wrongKind.AnswerPreCheckout(); err == nil {
		t.Error("expected an error on a non-pre_checkout_query update")
	}
}

func TestCtx_AnswerShipping(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerShippingQuery") {
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	bot := newTestBot(server)

	c := ctxForBot(bot, &Update{ShippingQuery: &ShippingQuery{ID: "sq1", From: &User{ID: 5}}})
	opts := []ShippingOption{{ID: "std", Title: "Standard", Prices: []LabeledPrice{{Label: "Shipping", Amount: 500}}}}
	if err := c.AnswerShipping(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["shipping_query_id"] != "sq1" || body["ok"] != true {
		t.Errorf("approve body = %v", body)
	}
	if _, hasOptions := body["shipping_options"]; !hasOptions {
		t.Error("shipping_options missing on approval")
	}

	if err := c.AnswerShippingError("no delivery there"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["ok"] != false || body["error_message"] != "no delivery there" {
		t.Errorf("decline body = %v", body)
	}

	wrongKind := ctxForBot(bot, &Update{Poll: &Poll{ID: "p"}})
	if err := wrongKind.AnswerShipping(nil); err == nil {
		t.Error("expected an error on a non-shipping_query update")
	}
}

func TestRefundStars(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/refundStarPayment") {
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
		decodeJSONBody(t, r, &body)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	bot := newTestBot(server)

	if err := bot.RefundStars(context.Background(), 5, "charge-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["user_id"] != float64(5) || body["telegram_payment_charge_id"] != "charge-abc" {
		t.Errorf("body = %v", body)
	}
}
