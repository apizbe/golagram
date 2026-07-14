package golagram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthMonitor_StartsWithZeroedCounters(t *testing.T) {
	hm := NewHealthMonitor()
	status := hm.GetStatus()

	if status.Status != "ok" {
		t.Errorf("Status = %q, want ok", status.Status)
	}
	if status.UpdatesDispatched != 0 || status.HandlersMatched != 0 ||
		status.UpdatesUnmatched != 0 || status.ErrorsCount != 0 {
		t.Errorf("expected zeroed counters, got %+v", status)
	}
}

func TestHealthMonitor_CountsDispatchedMatchedAndUnmatchedSeparately(t *testing.T) {
	hm := NewHealthMonitor()
	hm.IncrementDispatched("message")
	hm.IncrementMatched("message")
	hm.IncrementDispatched("callback_query")
	hm.IncrementUnmatched("callback_query")

	status := hm.GetStatus()
	if status.UpdatesDispatched != 2 {
		t.Errorf("UpdatesDispatched = %d, want 2", status.UpdatesDispatched)
	}
	if status.HandlersMatched != 1 {
		t.Errorf("HandlersMatched = %d, want 1", status.HandlersMatched)
	}
	if status.UpdatesUnmatched != 1 {
		t.Errorf("UpdatesUnmatched = %d, want 1", status.UpdatesUnmatched)
	}
}

func TestHealthMonitor_TracksCountersPerUpdateKind(t *testing.T) {
	hm := NewHealthMonitor()
	hm.IncrementDispatched("message")
	hm.IncrementDispatched("message")
	hm.IncrementDispatched("callback_query")
	hm.IncrementMatched("message")
	hm.IncrementUnmatched("callback_query")
	hm.IncrementUnmatched("inline_query")

	status := hm.GetStatus()
	if status.DispatchedByKind["message"] != 2 {
		t.Errorf("DispatchedByKind[message] = %d, want 2", status.DispatchedByKind["message"])
	}
	if status.DispatchedByKind["callback_query"] != 1 {
		t.Errorf("DispatchedByKind[callback_query] = %d, want 1", status.DispatchedByKind["callback_query"])
	}
	if status.MatchedByKind["message"] != 1 {
		t.Errorf("MatchedByKind[message] = %d, want 1", status.MatchedByKind["message"])
	}
	if status.UnmatchedByKind["callback_query"] != 1 || status.UnmatchedByKind["inline_query"] != 1 {
		t.Errorf("UnmatchedByKind = %v, want callback_query=1 inline_query=1", status.UnmatchedByKind)
	}
}

func TestHealthMonitor_GetStatus_KindMapsAreIndependentCopies(t *testing.T) {
	hm := NewHealthMonitor()
	hm.IncrementDispatched("message")

	status := hm.GetStatus()
	status.DispatchedByKind["message"] = 999 // mutate the returned copy

	fresh := hm.GetStatus()
	if fresh.DispatchedByKind["message"] != 1 {
		t.Errorf("mutating a returned HealthStatus leaked into the monitor: got %d, want 1", fresh.DispatchedByKind["message"])
	}
}

func TestHealthMonitor_RecordError(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RecordError(errors.New("boom"))

	status := hm.GetStatus()
	if status.ErrorsCount != 1 {
		t.Errorf("ErrorsCount = %d, want 1", status.ErrorsCount)
	}
	if status.LastError != "boom" {
		t.Errorf("LastError = %q, want boom", status.LastError)
	}
	if status.LastErrorTime == "" {
		t.Error("expected LastErrorTime to be set")
	}
}

func TestHealthMonitor_RecordError_NilIsNoop(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RecordError(nil)

	if status := hm.GetStatus(); status.ErrorsCount != 0 {
		t.Errorf("ErrorsCount = %d, want 0 after recording nil error", status.ErrorsCount)
	}
}

func TestHealthMonitor_HealthCheckHandler_ReportsRawNumbers(t *testing.T) {
	hm := NewHealthMonitor()
	hm.IncrementDispatched("message")
	hm.RecordError(errors.New("fail"))

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	hm.HealthCheckHandler()(rec, req)

	// Errors are reported, not judged: the process serves, so HTTP 200 with
	// the raw counters for operators to alert on.
	if rec.Code != 200 {
		t.Errorf("status code = %d, want 200", rec.Code)
	}
	var status HealthStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if status.ErrorsCount != 1 || status.UpdatesDispatched != 1 {
		t.Errorf("unexpected reported counters: %+v", status)
	}
}

func TestGatedHealthHandler_RejectsWhenGateReturnsFalse(t *testing.T) {
	hm := NewHealthMonitor()
	handler := gatedHealthHandler(hm.HealthCheckHandler(), func(r *http.Request) bool { return false })

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status code = %d, want 403 when the gate rejects the request", rec.Code)
	}
}

func TestGatedHealthHandler_ServesWhenGateReturnsTrue(t *testing.T) {
	hm := NewHealthMonitor()
	handler := gatedHealthHandler(hm.HealthCheckHandler(), func(r *http.Request) bool { return true })

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want 200 when the gate allows the request", rec.Code)
	}
}

func TestHealthMonitor_StartHealthServer_ShutsDownWithContext(t *testing.T) {
	hm := NewHealthMonitor()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- hm.StartHealthServer(ctx, "127.0.0.1:0")
	}()

	time.Sleep(50 * time.Millisecond) // let the server start listening
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected clean shutdown, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("health server did not shut down after context cancellation")
	}
}
