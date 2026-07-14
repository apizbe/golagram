package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testToken = "TESTTOKEN"

func newTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Telegram's real layout is https://api.telegram.org/bot<token>/<method>,
	// so the base URL stops right before the token.
	return NewClientWithBaseURL(testToken, server.URL+"/bot")
}

func TestClient_Call_HitsMethodPathWithJSONBody(t *testing.T) {
	var gotPath, gotMethod, gotContentType string
	var gotBody map[string]any

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	})

	raw, err := client.Call(context.Background(), "sendMessage",
		map[string]any{"chat_id": 42, "text": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/bot"+testToken+"/sendMessage" {
		t.Errorf("path = %q, want /bot%s/sendMessage", gotPath, testToken)
	}
	if gotMethod != http.MethodPost || gotContentType != "application/json" {
		t.Errorf("expected POST with JSON content type, got %s %s", gotMethod, gotContentType)
	}
	if gotBody["chat_id"].(float64) != 42 || gotBody["text"] != "hello" {
		t.Errorf("server received unexpected body: %+v", gotBody)
	}

	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.MessageID != 7 {
		t.Errorf("expected raw result with message_id 7, got %s (err %v)", raw, err)
	}
}

func TestClient_Call_NilParamsSendsEmptyBody(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body for nil params, got %q", body)
		}
		w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"test_bot"}}`))
	})

	raw, err := client.Call(context.Background(), "getMe", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &me); err != nil || me.Username != "test_bot" {
		t.Errorf("unexpected getMe result: %s", raw)
	}
}

func TestClient_Call_APIErrorIsTyped(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	})

	_, err := client.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})
	if err == nil {
		t.Fatal("expected an error when Telegram reports ok:false")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *api.Error, got %T: %v", err, err)
	}
	if apiErr.Code != 400 || apiErr.Description != "Bad Request: chat not found" {
		t.Errorf("unexpected parsed error: %+v", apiErr)
	}
}

func TestClient_Call_FloodErrorCarriesRetryAfter(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 17","parameters":{"retry_after":17}}`))
	})

	_, err := client.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *api.Error, got %T: %v", err, err)
	}
	if apiErr.Code != 429 || apiErr.RetryAfter == nil || *apiErr.RetryAfter != 17 {
		t.Errorf("expected 429 with RetryAfter=17, got %+v", apiErr)
	}
}

func TestClient_Call_MigrationErrorCarriesNewChatID(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-100123456789}}`))
	})

	_, err := client.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *api.Error, got %T: %v", err, err)
	}
	if apiErr.MigrateToChatID == nil || *apiErr.MigrateToChatID != -100123456789 {
		t.Errorf("expected MigrateToChatID=-100123456789, got %+v", apiErr)
	}
}

func TestClient_Call_ContextCancellationStopsRequest(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client gives up
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Call(ctx, "getUpdates", map[string]any{"offset": 0})
	if err == nil {
		t.Fatal("expected an error for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in error chain, got: %v", err)
	}
}

// A transport-level failure (DNS, connection refused, TLS, a typo'd
// WithBaseURL, ...) puts the full request URL — baseURL+token+method,
// since that's how Telegram authenticates a call — inside Go's *url.Error,
// one %w-unwrap away from a caller's log.Printf/slog call. Verified against
// a live repro before the fix: the raw token appeared in the returned
// error's message.
func TestClient_Call_TransportErrorDoesNotLeakToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // closed immediately: any request now hits connection refused

	client := NewClientWithBaseURL(testToken, server.URL+"/bot")

	_, err := client.Call(context.Background(), "getMe", nil)
	if err == nil {
		t.Fatal("expected an error against a closed server")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("token leaked into error message: %v", err)
	}
	if !strings.Contains(err.Error(), "<TOKEN>") {
		t.Errorf("expected the redacted <TOKEN> placeholder in the error message, got: %v", err)
	}

	// The redaction mutates *url.Error.URL in place rather than flattening
	// the error, so the chain — and errors.As on the underlying network
	// error — still works for callers who need it.
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatal("expected *url.Error in the error chain to survive sanitization")
	}
}
