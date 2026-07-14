package golagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookHandler_RejectsWrongSecretToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	handler := bot.Handler(WebhookConfig{SecretToken: "correct-token"})

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"update_id":1}`)))
	req.Header.Set(secretTokenHeader, "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for wrong secret token", rec.Code)
	}
}

func TestWebhookHandler_AcceptsCorrectSecretTokenAndQueuesUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	handler := bot.Handler(WebhookConfig{SecretToken: "correct-token"})

	body, _ := json.Marshal(&Update{UpdateID: 42, Message: &Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(secretTokenHeader, "correct-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	select {
	case u := <-bot.updateChan:
		if u.UpdateID != 42 {
			t.Errorf("UpdateID = %d, want 42", u.UpdateID)
		}
		if u.Message == nil || u.Message.api == nil {
			t.Error("expected the queued update's message to be hydrated (api client wired)")
		}
	case <-time.After(time.Second):
		t.Fatal("update was never queued onto updateChan")
	}
}

func TestWebhookHandler_NoSecretConfigured_SkipsCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	handler := bot.Handler(WebhookConfig{}) // no SecretToken set

	body, _ := json.Marshal(&Update{UpdateID: 1})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when no secret token is configured", rec.Code)
	}
}

func TestWebhookHandler_RejectsNonPOST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	handler := bot.Handler(WebhookConfig{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for a GET request", rec.Code)
	}
}

func TestWebhookHandler_RejectsMalformedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	handler := bot.Handler(WebhookConfig{})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed JSON", rec.Code)
	}
}

// A webhook URL is reachable by anyone who discovers it (SecretToken is
// optional); without a body size cap, a bare POST of an arbitrarily large
// payload would force the process to buffer all of it in memory. Real
// Telegram updates are well under 1 MB.
func TestWebhookHandler_RejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	handler := bot.Handler(WebhookConfig{})
	oversized := bytes.Repeat([]byte("a"), maxWebhookBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(oversized))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 for a body over maxWebhookBodyBytes", rec.Code)
	}
}

// TestWebhookHandler_AllowWebhookReply_EmbedsReplyInResponse is the core
// reply-in-webhook-response test: a registration that opted into
// AllowWebhookReply and returns Reply(...) should have that method embedded
// directly in the webhook HTTP response body, with no follow-up HTTPS call
// to Telegram at all.
func TestWebhookHandler_AllowWebhookReply_EmbedsReplyInResponse(t *testing.T) {
	var realAPICalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		realAPICalls++
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	r := NewRouter()
	r.Message().AllowWebhookReply().Handle(func(c *Ctx) error {
		return Reply(&SendMessageRequest{ChatID: ChatIDFromInt(555), Text: "pong"})
	})
	bot.Dispatch(r)

	handler := bot.Handler(WebhookConfig{})
	body, _ := json.Marshal(&Update{UpdateID: 1, Message: &Message{Text: "ping", Chat: &Chat{ID: 555}, From: &User{ID: 1}}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body wasn't valid JSON: %v (%s)", err, rec.Body.String())
	}
	if got["method"] != "sendMessage" {
		t.Errorf(`response method = %v, want "sendMessage"`, got["method"])
	}
	if got["text"] != "pong" {
		t.Errorf(`response text = %v, want "pong"`, got["text"])
	}
	if got["chat_id"].(float64) != 555 {
		t.Errorf("response chat_id = %v, want 555", got["chat_id"])
	}

	if realAPICalls != 0 {
		t.Errorf("expected zero follow-up HTTPS calls to Telegram, got %d", realAPICalls)
	}
}

// A handler on an AllowWebhookReply registration that doesn't return
// Reply(...) just gets a plain 200 — the optimization is opt-in per call,
// not forced.
func TestWebhookHandler_AllowWebhookReply_NoReplyReturned_RespondsPlain200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	r := NewRouter()
	r.Message().AllowWebhookReply().Handle(func(c *Ctx) error {
		return nil
	})
	bot.Dispatch(r)

	handler := bot.Handler(WebhookConfig{})
	body, _ := json.Marshal(&Update{UpdateID: 1, Message: &Message{Text: "ping", Chat: &Chat{ID: 555}, From: &User{ID: 1}}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected an empty body when the handler returned no reply, got %q", rec.Body.String())
	}
}

// A Reply(...) whose request carries a local file upload can't ride in a
// JSON webhook response body — Handler falls back to a normal follow-up
// API call instead, even though the registration opted into
// AllowWebhookReply.
func TestWebhookHandler_AllowWebhookReply_UploadFallsBackToRealAPICall(t *testing.T) {
	var realAPICalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		realAPICalls++
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	r := NewRouter()
	r.Message().AllowWebhookReply().Handle(func(c *Ctx) error {
		return Reply(&SendPhotoRequest{
			ChatID: ChatIDFromInt(555),
			Photo:  InputFileUpload("photo.jpg", strings.NewReader("fake-bytes")),
		})
	})
	bot.Dispatch(r)

	handler := bot.Handler(WebhookConfig{})
	body, _ := json.Marshal(&Update{UpdateID: 1, Message: &Message{Text: "ping", Chat: &Chat{ID: 555}, From: &User{ID: 1}}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "application/json" {
		t.Errorf("expected a plain 200 (real call made instead), got an embedded JSON reply: %s", rec.Body.String())
	}
	if realAPICalls != 1 {
		t.Errorf("expected exactly 1 follow-up HTTPS call for the upload, got %d", realAPICalls)
	}
}

// A TelegramBot is one-shot (see the doc on [TelegramBot]): RunWebhook
// called after a previous Run/RunWebhook/StartWorkers must fail fast with
// errAlreadyRan instead of attempting setWebhook against a bot whose
// updateChan is already closed.
func TestRunWebhook_AlreadyRan_ReturnsErrWithoutCallingSetWebhook(t *testing.T) {
	var setWebhookCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "setWebhook") {
			setWebhookCalled = true
		}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.markRan() // simulate a previous Run/RunWebhook/StartWorkers call

	err := bot.RunWebhook(context.Background(), WebhookConfig{PublicURL: "https://example.com/webhook"})
	if !errors.Is(err, errAlreadyRan) {
		t.Errorf("RunWebhook() error = %v, want errAlreadyRan", err)
	}
	if setWebhookCalled {
		t.Error("expected RunWebhook to bail out before calling setWebhook")
	}
}

// End-to-end: RunWebhook actually calls setWebhook, binds a real listener,
// serves a real HTTP request through to a registered handler, and shuts
// down cleanly when ctx is canceled.
func TestRunWebhook_EndToEnd(t *testing.T) {
	var setWebhookURL string
	var handlerRan bool

	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bot"+testToken+"/setWebhook" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			setWebhookURL, _ = body["url"].(string)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		w.Write([]byte(sendMessageOK))
	}))
	defer telegram.Close()

	bot := newTestBot(telegram)

	done := make(chan struct{})
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		handlerRan = true
		close(done)
		return nil
	})
	bot.Dispatch(r)

	// Bind our own listener on an ephemeral port so we know its address
	// without guessing a fixed one; hand it to RunWebhook via its Server field.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind listener: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // RunWebhook binds it again itself via ListenAndServe on the same addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- bot.RunWebhook(ctx, WebhookConfig{
			Addr:      addr,
			Path:      "/hook",
			PublicURL: "https://example.com/hook",
		})
	}()

	// Wait for the listener to actually come up before hitting it.
	var resp *http.Response
	body, _ := json.Marshal(&Update{UpdateID: 1, Message: &Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var postErr error
		resp, postErr = http.Post("http://"+addr+"/hook", "application/json", bytes.NewReader(body))
		if postErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("webhook server never came up")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("webhook POST status = %d, want 200", resp.StatusCode)
	}

	if setWebhookURL != "https://example.com/hook" {
		t.Errorf("setWebhook was called with url=%q, want https://example.com/hook", setWebhookURL)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
	if !handlerRan {
		t.Error("expected the registered handler to run for the webhook-delivered update")
	}

	cancel()
	select {
	case err := <-runErrCh:
		if err != nil {
			t.Errorf("RunWebhook returned an error on shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWebhook did not return after context cancellation")
	}
}
