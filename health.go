package golagram

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthStatus is a raw-numbers health report plus a coarse runtime state.
// Status is one of starting, running, stopping, stopped, or error; callers
// still decide what counter thresholds should page them.
type HealthStatus struct {
	Status            string    `json:"status"` // starting, running, stopping, stopped, or error
	Uptime            string    `json:"uptime"`
	UptimeSeconds     int64     `json:"uptime_seconds"`
	StartTime         time.Time `json:"start_time"`
	UpdatesDispatched int64     `json:"updates_dispatched"` // updates that entered dispatch
	HandlersMatched   int64     `json:"handlers_matched"`   // updates a handler matched
	UpdatesUnmatched  int64     `json:"updates_unmatched"`  // updates no handler matched
	ErrorsCount       int64     `json:"errors_count"`       // handler errors
	LastError         string    `json:"last_error"`
	LastErrorTime     string    `json:"last_error_time"`

	// DispatchedByKind/MatchedByKind/UnmatchedByKind break the three
	// aggregate counters above down by update kind (e.g. "message",
	// "callback_query") — e.g. spotting that inline_query updates are
	// arriving but never matching, when the aggregate counters alone
	// wouldn't show which kind that is.
	DispatchedByKind map[string]int64 `json:"dispatched_by_kind,omitempty"`
	MatchedByKind    map[string]int64 `json:"matched_by_kind,omitempty"`
	UnmatchedByKind  map[string]int64 `json:"unmatched_by_kind,omitempty"`
}

// HealthMonitor tracks the bot's dispatch and error counters.
type HealthMonitor struct {
	status            string
	startTime         time.Time
	updatesDispatched int64
	handlersMatched   int64
	updatesUnmatched  int64
	errorsCount       int64
	lastError         string
	lastErrorTime     time.Time
	dispatchedByKind  map[string]int64
	matchedByKind     map[string]int64
	unmatchedByKind   map[string]int64
	mu                sync.RWMutex
}

// NewHealthMonitor creates a health monitor with all counters at zero and
// its start time set to now.
func NewHealthMonitor() *HealthMonitor {
	return &HealthMonitor{
		status:           "starting",
		startTime:        time.Now(),
		dispatchedByKind: map[string]int64{},
		matchedByKind:    map[string]int64{},
		unmatchedByKind:  map[string]int64{},
	}
}

// SetStatus records the bot lifecycle state reported by health checks.
// Health counters remain available in every state; this field is only a
// coarse readiness signal, not an application-specific health judgment.
func (hm *HealthMonitor) SetStatus(status string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.status = status
}

// IncrementDispatched counts an update entering dispatch, whether or not
// any handler ends up matching it, broken down by kind (e.g. "message",
// "callback_query" — see [Update.Kind]).
func (hm *HealthMonitor) IncrementDispatched(kind string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.updatesDispatched++
	hm.dispatchedByKind[kind]++
}

// IncrementMatched counts an update a handler matched, broken down by kind.
func (hm *HealthMonitor) IncrementMatched(kind string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.handlersMatched++
	hm.matchedByKind[kind]++
}

// IncrementUnmatched counts an update that no registered handler matched,
// broken down by kind.
func (hm *HealthMonitor) IncrementUnmatched(kind string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.updatesUnmatched++
	hm.unmatchedByKind[kind]++
}

// RecordError records a handler error.
func (hm *HealthMonitor) RecordError(err error) {
	if err == nil {
		return
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.errorsCount++
	hm.lastError = err.Error()
	hm.lastErrorTime = time.Now()
}

// GetStatus returns the current counters.
func (hm *HealthMonitor) GetStatus() HealthStatus {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	uptime := time.Since(hm.startTime)

	lastErrorTime := ""
	if !hm.lastErrorTime.IsZero() {
		lastErrorTime = hm.lastErrorTime.Format(time.RFC3339)
	}

	return HealthStatus{
		Status:            hm.status,
		Uptime:            uptime.String(),
		UptimeSeconds:     int64(uptime.Seconds()),
		StartTime:         hm.startTime,
		UpdatesDispatched: hm.updatesDispatched,
		HandlersMatched:   hm.handlersMatched,
		UpdatesUnmatched:  hm.updatesUnmatched,
		ErrorsCount:       hm.errorsCount,
		LastError:         hm.lastError,
		LastErrorTime:     lastErrorTime,
		DispatchedByKind:  copyKindCounts(hm.dispatchedByKind),
		MatchedByKind:     copyKindCounts(hm.matchedByKind),
		UnmatchedByKind:   copyKindCounts(hm.unmatchedByKind),
	}
}

// copyKindCounts returns a shallow copy so callers of
// [HealthMonitor.GetStatus] can't mutate HealthMonitor's internal maps
// through the returned HealthStatus.
func copyKindCounts(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// HealthGate reports whether an incoming /health or /healthz request should
// be served. Return false to reject it — [HealthMonitor.StartHealthServer]
// responds 403 instead of serving the status. Passing none (the default)
// serves every request unauthenticated.
type HealthGate func(*http.Request) bool

// HealthCheckHandler returns an HTTP handler for liveness/diagnostic checks, open to any
// request that can reach it — no auth of its own. LastError frequently
// contains chat IDs, user IDs, or upstream error text (whatever a handler
// returned), so bind this to localhost, put auth in front, or pass a
// [HealthGate] to [HealthMonitor.StartHealthServer] before exposing it on a
// public address.
func (hm *HealthMonitor) HealthCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hm.writeStatus(w, http.StatusOK)
	}
}

// ReadinessHandler returns the same status payload as HealthCheckHandler,
// but responds with 503 until the bot is running. It is suitable for a
// load-balancer or Kubernetes readiness probe; /health remains a liveness
// and diagnostic endpoint that reports counters with HTTP 200.
func (hm *HealthMonitor) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := hm.GetStatus()
		code := http.StatusServiceUnavailable
		if status.Status == "running" {
			code = http.StatusOK
		}
		hm.writeStatus(w, code)
	}
}

func (hm *HealthMonitor) writeStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(hm.GetStatus())
}

// gatedHealthHandler wraps handler so a request failing gate gets 403
// instead of the health status — split out from StartHealthServer so the
// gating logic itself is testable without spinning up a real listener.
func gatedHealthHandler(handler http.HandlerFunc, gate HealthGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gate(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		handler(w, r)
	}
}

// StartHealthServer serves /health (liveness) and /healthz (readiness) on
// addr until ctx is canceled, then shuts down gracefully. It blocks; run it in a goroutine
// ([TelegramBot.StartHealthServer] does).
//
// gate, if given, is checked on every request; a false return responds 403
// instead of serving the status — a lightweight alternative to a reverse
// proxy or auth middleware when addr is reachable from outside a trusted
// network. Without one, /health and /healthz serve unauthenticated to
// anyone who can reach addr — see [HealthCheckHandler]'s doc for what that
// exposes.
func (hm *HealthMonitor) StartHealthServer(ctx context.Context, addr string, gate ...HealthGate) error {
	healthHandler := hm.HealthCheckHandler()
	readyHandler := hm.ReadinessHandler()
	if len(gate) > 0 && gate[0] != nil {
		healthHandler = gatedHealthHandler(healthHandler, gate[0])
		readyHandler = gatedHealthHandler(readyHandler, gate[0])
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/healthz", readyHandler) // Kubernetes-style readiness endpoint

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
