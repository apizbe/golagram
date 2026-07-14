package api

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_AutoRetry_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int32

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	})
	client.SetAutoRetry(5 * time.Second)

	start := time.Now()
	raw, err := client.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts (1 flood + 1 success), got %d", attempts)
	}
	if elapsed < time.Second {
		t.Errorf("expected the call to actually wait ~1s for retry_after, took %v", elapsed)
	}
	if string(raw) != `{"message_id":7}` {
		t.Errorf("unexpected result: %s", raw)
	}
}

func TestClient_AutoRetry_GivesUpWhenBudgetExceeded(t *testing.T) {
	var attempts int32

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5","parameters":{"retry_after":5}}`))
	})
	client.SetAutoRetry(2 * time.Second) // less than the first retry_after

	_, err := client.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})
	if err == nil {
		t.Fatal("expected an error once the retry budget can't fit even one more wait")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != 429 {
		t.Errorf("expected the underlying 429 *Error, got %v", err)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected exactly 1 attempt (no retry fits the budget), got %d", attempts)
	}
}

func TestClient_AutoRetry_DisabledByDefault(t *testing.T) {
	var attempts int32

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
	})
	// No SetAutoRetry call — must behave exactly as before it existed.

	_, err := client.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})
	if err == nil {
		t.Fatal("expected the 429 to surface immediately with auto-retry off")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected exactly 1 attempt with auto-retry disabled, got %d", attempts)
	}
}

func TestClient_AutoRetry_ContextCancellationStopsTheWait(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 30","parameters":{"retry_after":30}}`))
	})
	client.SetAutoRetry(time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.Call(ctx, "sendMessage", map[string]any{"chat_id": 1})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the context is canceled mid-wait")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded in the error, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("expected the call to give up quickly once ctx expired, took %v", elapsed)
	}
}
