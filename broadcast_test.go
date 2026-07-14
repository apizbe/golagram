package golagram

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func intPtr(n int) *int { return &n }

func floodErr(retryAfterSeconds int) error {
	return &APIError{Code: 429, Description: "Too Many Requests: retry later", RetryAfter: intPtr(retryAfterSeconds)}
}

func blockedErr() error {
	return &APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"}
}

func TestBroadcast_AllSuccess_AccumulatesSent(t *testing.T) {
	chatIDs := []int64{1, 2, 3, 4, 5}
	result, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}
	if result.Sent != len(chatIDs) {
		t.Errorf("Sent = %d, want %d", result.Sent, len(chatIDs))
	}
	if result.Failed != 0 || len(result.Blocked) != 0 || len(result.Errors) != 0 {
		t.Errorf("expected no failures, got Failed=%d Blocked=%v Errors=%v", result.Failed, result.Blocked, result.Errors)
	}
}

func TestBroadcast_BlockedUser_RecordedSeparately(t *testing.T) {
	chatIDs := []int64{1, 2, 3}
	result, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		if chatID == 2 {
			return blockedErr()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}
	if result.Sent != 2 {
		t.Errorf("Sent = %d, want 2", result.Sent)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if len(result.Blocked) != 1 || result.Blocked[0] != 2 {
		t.Errorf("Blocked = %v, want [2]", result.Blocked)
	}
	if result.Errors[2] == nil || !IsBlockedByUser(result.Errors[2]) {
		t.Errorf("Errors[2] = %v, want a blocked-by-user error", result.Errors[2])
	}
}

func TestBroadcast_Flood_RetriesOnceThenSucceeds(t *testing.T) {
	var attempts int32
	chatIDs := []int64{42}
	result, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return floodErr(0) // RetryAfter=0 keeps the test fast
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected exactly 2 attempts (1 flood + 1 retry), got %d", attempts)
	}
	if result.Sent != 1 || result.Failed != 0 {
		t.Errorf("expected the retry to succeed: Sent=%d Failed=%d", result.Sent, result.Failed)
	}
}

func TestBroadcast_Flood_FailsAfterRetryExhausted(t *testing.T) {
	var attempts int32
	chatIDs := []int64{42}
	result, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		atomic.AddInt32(&attempts, 1)
		return floodErr(0)
	})
	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected exactly 2 attempts (bounded retry), got %d", attempts)
	}
	if result.Sent != 0 || result.Failed != 1 {
		t.Errorf("expected the chat to be recorded as failed: Sent=%d Failed=%d", result.Sent, result.Failed)
	}
	if !IsFlood(result.Errors[42]) {
		t.Errorf("Errors[42] = %v, want a flood error", result.Errors[42])
	}
}

func TestBroadcast_WithBroadcastRate_ActuallyPaces(t *testing.T) {
	// rate=5, burst=5 (broadcastPacer sets burst=rate): the first 5 sends
	// are free, the next 5 must wait for tokens to refill at 5/s — a
	// floor of roughly (10-5)/5 = 1s. concurrency=1 keeps the timing
	// prediction simple (no overlap to reason about).
	chatIDs := make([]int64, 10)
	for i := range chatIDs {
		chatIDs[i] = int64(i)
	}

	start := time.Now()
	_, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		return nil
	}, WithBroadcastRate(5), WithBroadcastConcurrency(1))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}
	if elapsed < 700*time.Millisecond {
		t.Errorf("elapsed = %v, want at least ~1s — the rate limit doesn't appear to be pacing sends", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, suspiciously long for a 10-chat/5-per-sec broadcast", elapsed)
	}
}

func TestBroadcast_WithBroadcastConcurrency1_SendsSerially(t *testing.T) {
	chatIDs := []int64{1, 2, 3, 4, 5}

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	result, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		n := inFlight.Add(1)
		for {
			max := maxInFlight.Load()
			if n <= max || maxInFlight.CompareAndSwap(max, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	}, WithBroadcastConcurrency(1), WithBroadcastRate(1000))

	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}
	if result.Sent != len(chatIDs) {
		t.Errorf("Sent = %d, want %d", result.Sent, len(chatIDs))
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("max concurrent sends = %d, want 1 (WithBroadcastConcurrency(1))", got)
	}
}

func TestBroadcast_ProgressCallback_FiresPerChatWithCorrectCounts(t *testing.T) {
	chatIDs := []int64{1, 2, 3, 4}

	var mu sync.Mutex
	var events []BroadcastProgress
	result, err := Broadcast(context.Background(), chatIDs, func(ctx context.Context, chatID int64) error {
		if chatID == 3 {
			return fmt.Errorf("boom")
		}
		return nil
	}, WithBroadcastProgress(func(p BroadcastProgress) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, p)
	}))
	if err != nil {
		t.Fatalf("Broadcast returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != len(chatIDs) {
		t.Fatalf("progress fired %d times, want %d", len(events), len(chatIDs))
	}
	last := events[len(events)-1]
	if last.Total != len(chatIDs) || last.Remaining != 0 {
		t.Errorf("final progress = %+v, want Total=%d Remaining=0", last, len(chatIDs))
	}
	if last.Sent != result.Sent || last.Failed != result.Failed {
		t.Errorf("final progress Sent/Failed = %d/%d, want %d/%d", last.Sent, last.Failed, result.Sent, result.Failed)
	}
	prevDone := 0
	for _, e := range events {
		done := e.Sent + e.Failed
		if done < prevDone {
			t.Errorf("progress regressed: Sent+Failed went from %d to %d", prevDone, done)
		}
		prevDone = done
	}
}

func TestBroadcast_ContextCancellation_ReturnsPartialResultAndErr(t *testing.T) {
	chatIDs := make([]int64, 50)
	for i := range chatIDs {
		chatIDs[i] = int64(i)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := Broadcast(ctx, chatIDs, func(ctx context.Context, chatID int64) error {
		return nil
	}, WithBroadcastRate(1), WithBroadcastConcurrency(1)) // slow enough that 50 chats can't finish in 50ms

	if err == nil {
		t.Fatal("expected Broadcast to return a non-nil error on context cancellation")
	}
	if result.Sent+result.Failed >= len(chatIDs) {
		t.Errorf("expected a partial result, got Sent=%d Failed=%d out of %d", result.Sent, result.Failed, len(chatIDs))
	}
}
