package golagram

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallbackQuery_Answer_MarksAnsweredOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{ID: "1", From: &User{ID: 1}}, bot)

	if err := cq.Answer("ok"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cq.Answered() {
		t.Error("expected Answered() to report true after a successful Answer")
	}
}

// A failed answerCallbackQuery call (network blip, flood limit) must not
// mark the query answered — CallbackAnswerMiddleware relies on Answered()
// staying false so it can still send its own safety-net answer; otherwise
// the user's button spinner would spin until Telegram's client-side
// timeout, exactly the outcome the middleware exists to prevent.
func TestCallbackQuery_Answer_LeavesUnansweredOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"description":"flood control"}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	cq := bindCallback(&CallbackQuery{ID: "1", From: &User{ID: 1}}, bot)

	if err := cq.Answer("ok"); err == nil {
		t.Fatal("expected Answer to return an error when the API call fails")
	}
	if cq.Answered() {
		t.Error("expected Answered() to still report false after a failed Answer")
	}
}
