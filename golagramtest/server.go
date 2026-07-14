package golagramtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	gg "github.com/apizbe/golagram"
)

// Call is one recorded request against a [Server].
type Call struct {
	Method string
	Body   json.RawMessage
}

// Decode unmarshals the call's raw request body into v, e.g.
// call.Decode(&map[string]any{}). Decoding straight into a generated
// request type such as [gg.SendMessageRequest] fails for any request
// carrying a [gg.ChatID] field — it has no UnmarshalJSON — so prefer a
// map or a narrower ad hoc struct over the request type itself.
func (c Call) Decode(v any) error {
	return json.Unmarshal(c.Body, v)
}

type responder struct {
	result any
	isErr  bool
	code   int
	desc   string
}

// Server is a fake Telegram Bot API backed by an [httptest.Server]. Point a
// bot at it with [gg.WithBaseURL](server.URL()) — [NewBot] does this for
// you — then inspect what the bot sent via [Server.Calls]/[Server.CallsTo]/
// [Server.LastCall], or script its replies via [Server.Respond]/
// [Server.Fail].
//
// Every unconfigured method defaults to returning a synthetic [gg.Message]
// (not a bare true): the hand-written Answer/Reply sugar strictly decodes
// its result as a Message, unlike generated methods, which tolerate either
// shape — defaulting to Message keeps the common case (asserting on a
// reply) working without per-test setup. If you're testing a method whose
// return value actually matters and isn't a Message — DeleteMessage,
// SetChatTitle, and other bool-returning calls — configure it explicitly:
//
//	server.Respond("deleteMessage", true)
type Server struct {
	httpSrv *httptest.Server

	mu        sync.Mutex
	calls     []Call
	overrides map[string]responder
	botUser   *gg.User

	msgIDSeq int64
}

// ServerOption configures a [Server] built by [NewServer].
type ServerOption func(*Server)

// WithBotUser overrides the [gg.User] returned for getMe (default: a
// generic test bot). NewTelegramBot calls getMe once at construction, so
// this is what a bot built via [NewBot] sees as its own identity.
func WithBotUser(u *gg.User) ServerOption {
	return func(s *Server) { s.botUser = u }
}

// NewServer starts a fake Bot API server. Call [Server.Close] when done.
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		overrides: make(map[string]responder),
		botUser:   &gg.User{ID: 10000000, IsBot: true, FirstName: "Test Bot", Username: "test_bot"},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.httpSrv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// URL returns the base URL to pass to [gg.WithBaseURL]. [NewBot] does this
// automatically.
func (s *Server) URL() string {
	return s.httpSrv.URL + "/bot"
}

// Close shuts down the underlying HTTP server.
func (s *Server) Close() {
	s.httpSrv.Close()
}

// Calls returns every recorded call, in request order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Call, len(s.calls))
	copy(out, s.calls)
	return out
}

// CallsTo returns the recorded calls to method, in request order.
func (s *Server) CallsTo(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// LastCall returns the most recently recorded call, or false if none has
// happened yet.
func (s *Server) LastCall() (Call, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return Call{}, false
	}
	return s.calls[len(s.calls)-1], true
}

// Respond configures method to return result as a successful API response.
// The override is sticky — it applies to every future call to method until
// changed by another Respond/Fail or cleared by [Server.Reset].
func (s *Server) Respond(method string, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[method] = responder{result: result}
}

// Fail configures method to return a Telegram-shaped API error
// ({"ok":false,"error_code":code,"description":description}). Sticky, like
// [Server.Respond].
func (s *Server) Fail(method string, code int, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[method] = responder{isErr: true, code: code, desc: description}
}

// Reset clears every recorded call and configured override, back to a
// freshly-built [Server]'s defaults (getMe still pre-wired).
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
	s.overrides = make(map[string]responder)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	method := methodFromPath(r.URL.Path)
	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: method, Body: json.RawMessage(body)})
	override, hasOverride := s.overrides[method]
	s.msgIDSeq++
	seq := s.msgIDSeq
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	switch {
	case method == "getMe":
		writeResult(w, s.botUser)
	case hasOverride && override.isErr:
		writeError(w, override.code, override.desc)
	case hasOverride:
		writeResult(w, override.result)
	default:
		writeResult(w, defaultMessage(seq))
	}
}

func defaultMessage(seq int64) *gg.Message {
	return &gg.Message{
		MessageID: seq,
		Date:      time.Now().Unix(),
		Chat:      &gg.Chat{ID: 1, Type: gg.ChatTypePrivate},
	}
}

func methodFromPath(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func writeResult(w http.ResponseWriter, v any) {
	result, err := json.Marshal(v)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	body, _ := json.Marshal(struct {
		Ok     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}{true, result})
	// A local httptest connection; a write failure here means the test
	// already tore the server down, which the test itself will surface.
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, code int, description string) {
	body, _ := json.Marshal(struct {
		Ok          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}{false, code, description})
	_, _ = w.Write(body)
}
