package golagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apizbe/golagram/internal/api"
)

// testToken is shaped like a real bot token (<bot_id>:<35-char secret>) so
// it passes ValidateToken — NewTelegramBot checks that before its first
// request now, and a fake-but-malformed token would fail there instead of
// exercising what these tests actually mean to test.
const testToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// newTestBot builds a TelegramBot wired to a fake Telegram server, without
// going through NewTelegramBot (which makes a real getMe call on construction).
func newTestBot(server *httptest.Server) *TelegramBot {
	client := api.NewClientWithBaseURL(testToken, server.URL+"/bot")
	return &TelegramBot{
		api:             client,
		healthMonitor:   NewHealthMonitor(),
		fsmStorage:      NewMemoryStorage(),
		dispatchLocks:   newKeyedMutex(),
		updateChan:      make(chan *Update, 10),
		me:              &User{ID: 1000, IsBot: true, Username: "test_bot"},
		numWorkers:      defaultWorkers,
		updateBuffer:    defaultUpdateBuffer,
		pollTimeoutSecs: defaultPollTimeoutSecs,
	}
}

// bindMessage wires a Message to the bot's API client the same way Run()
// hydrates real updates coming off getUpdates.
func bindMessage(m *Message, b *TelegramBot) *Message {
	b.hydrateMessage(m)
	return m
}

func bindCallback(c *CallbackQuery, b *TelegramBot) *CallbackQuery {
	c.api = b.api
	c.fsm = b.fsmStorage
	if c.Message != nil {
		bindMessage(c.Message, b)
	}
	return c
}

// sendMessageOK is a fake-server response shaped like a real sendMessage
// result, so Answer/Reply can decode the sent message.
const sendMessageOK = `{"ok":true,"result":{"message_id":900,"chat":{"id":555,"type":"private"},"text":"sent"}}`

// runWorkerFor feeds updates through a single worker via a private,
// pre-closed channel — matching how StopWorkers really shuts a worker down
// (worker() only returns once its channel is closed and drained, never on
// ctx cancellation, so shutdown can't drop buffered updates) — and blocks
// until every update has been synchronously dispatched. b.updateChan is
// swapped back afterward so it stays reusable across multiple calls
// against the same bot within one test.
func runWorkerFor(t *testing.T, b *TelegramBot, updates ...*Update) {
	t.Helper()

	ch := make(chan *Update, len(updates))
	for _, u := range updates {
		ch <- u
	}
	close(ch)

	orig := b.updateChan
	b.updateChan = ch
	defer func() { b.updateChan = orig }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.worker(context.Background())
	}()
	wg.Wait()
}

func TestBot_DispatchesMessageToMatchingHandlerAndCallsSendMessage(t *testing.T) {
	var receivedBody map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		mu.Unlock()
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	handlerCalled := false
	r := NewRouter()
	r.Message(FilterCommand("ping")).Handle(func(c *Ctx) error {
		handlerCalled = true
		sent, err := c.Answer("pong")
		if err != nil {
			return err
		}
		if sent == nil || sent.MessageID != 900 {
			t.Errorf("expected Answer to return the sent message, got %+v", sent)
		}
		return nil
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{
		Text: "/ping",
		Chat: &Chat{ID: 555},
		From: &User{ID: 1},
	}, bot)

	runWorkerFor(t, bot, &Update{Message: msg})

	if !handlerCalled {
		t.Fatal("expected the /ping handler to run")
	}

	mu.Lock()
	defer mu.Unlock()
	if receivedBody["text"] != "pong" || receivedBody["chat_id"].(float64) != 555 {
		t.Errorf("server received unexpected sendMessage body: %+v", receivedBody)
	}

	status := bot.healthMonitor.GetStatus()
	if status.UpdatesDispatched != 1 || status.HandlersMatched != 1 {
		t.Errorf("expected 1 dispatched / 1 matched, got %+v", status)
	}
}

// Regression test for REVIEW-WORST #1: a callback-query update must trigger
// zero message handlers. The old emptyEvent() pre-allocation made every
// update look like all three kinds at once and misrouted every single one.
func TestBot_CallbackQueryUpdate_TriggersZeroMessageHandlers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	messageHandlerRuns := 0
	callbackHandlerRuns := 0
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		messageHandlerRuns++
		return nil
	})
	r.CallbackQuery().Handle(func(c *Ctx) error {
		callbackHandlerRuns++
		return nil
	})
	bot.Dispatch(r)

	cb := bindCallback(&CallbackQuery{
		ID:      "cbq1",
		Data:    "anything",
		From:    &User{ID: 9},
		Message: &Message{MessageID: 1, Chat: &Chat{ID: 5}},
	}, bot)

	runWorkerFor(t, bot, &Update{UpdateID: 1, CallbackQuery: cb})

	if messageHandlerRuns != 0 {
		t.Errorf("a callback update ran %d message handlers, want 0", messageHandlerRuns)
	}
	if callbackHandlerRuns != 1 {
		t.Errorf("callback handler ran %d times, want 1", callbackHandlerRuns)
	}
}

// Every other test drives callback queries through bindCallback+runWorkerFor,
// which wires cq.api/cq.fsm by hand and never actually exercises hydrate's
// own CallbackQuery branch. HandleUpdate is the one path that calls hydrate
// for real, so this is the only test proving that branch wires the callback
// query's FSM storage — without it, a handler's FSM reads/writes on a
// callback query would panic on a nil storage.
func TestBot_HandleUpdate_HydratesCallbackQueryFSM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	r := NewRouter()
	r.CallbackQuery().Handle(func(c *Ctx) error {
		return FSMSet(c.FSM(), "clicked", true)
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), &Update{
		CallbackQuery: &CallbackQuery{
			ID:      "1",
			From:    &User{ID: 1},
			Data:    "x",
			Message: &Message{Chat: &Chat{ID: 1}},
		},
	})

	key := StorageKey{ChatID: 1, UserID: 1}
	clicked, ok, err := FSMGet[bool](NewFSMContext(context.Background(), bot.fsmStorage, key), "clicked")
	if err != nil || !ok || !clicked {
		t.Errorf("expected hydrate to wire the callback query's FSM storage so the handler's FSMSet persisted; got clicked=%v ok=%v err=%v", clicked, ok, err)
	}
}

// Regression test for REVIEW-WORST #4: edited_message used to be parsed and
// hydrated but never dispatched.
func TestBot_EditedMessageUpdate_DispatchesToEditedHandlers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var editedTexts []string
	messageHandlerRuns := 0
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		messageHandlerRuns++
		return nil
	})
	r.EditedMessage().Handle(func(c *Ctx) error {
		editedTexts = append(editedTexts, c.EditedMessage.Text)
		return nil
	})
	bot.Dispatch(r)

	edited := bindMessage(&Message{
		Text:     "fixed typo",
		EditDate: 1234,
		Chat:     &Chat{ID: 5},
		From:     &User{ID: 9},
	}, bot)

	runWorkerFor(t, bot, &Update{UpdateID: 2, EditedMessage: edited})

	if len(editedTexts) != 1 || editedTexts[0] != "fixed typo" {
		t.Errorf("expected the edited-message handler to run once, got %v", editedTexts)
	}
	if messageHandlerRuns != 0 {
		t.Errorf("an edited_message update ran %d plain message handlers, want 0", messageHandlerRuns)
	}
}

// Zero-value decoding is what makes routing correct: absent JSON fields must
// stay nil (the emptyEvent() bug pre-allocated all of them to non-nil).
func TestUpdate_ZeroValueDecoding_OnlyPresentFieldsAreNonNil(t *testing.T) {
	raw := []byte(`{"update_id":77,"callback_query":{"id":"q1","data":"buy:1","from":{"id":3},"message":{"message_id":2,"chat":{"id":4,"type":"private"}}}}`)

	var u Update
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if u.UpdateID != 77 {
		t.Errorf("UpdateID = %d, want 77", u.UpdateID)
	}
	if u.Message != nil {
		t.Error("Message must be nil for a callback-query update")
	}
	if u.EditedMessage != nil {
		t.Error("EditedMessage must be nil for a callback-query update")
	}
	if u.InlineQuery != nil || u.ChatMember != nil || u.PollAnswer != nil {
		t.Error("unrelated update-kind fields must stay nil")
	}
	if u.CallbackQuery == nil || u.CallbackQuery.Data != "buy:1" {
		t.Errorf("CallbackQuery not decoded: %+v", u.CallbackQuery)
	}
}

func TestBot_FirstMatchWins_SecondHandlerNeverRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var order []string
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "catch-all")
		return nil
	})
	r.Message(FilterCommand("ping")).Handle(func(c *Ctx) error {
		order = append(order, "ping")
		return nil
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "/ping", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	if len(order) != 1 || order[0] != "catch-all" {
		t.Errorf("expected only the first registered match to run, got %v", order)
	}
}

func TestBot_MiddlewareRunsBeforeHandlerAndCanBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	handlerCalled := false
	r := NewRouter()
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			if c.Message.FromID() != 42 {
				return nil // blocks: never calls next
			}
			return next(c)
		}
	})
	r.Message().Handle(func(c *Ctx) error {
		handlerCalled = true
		return nil
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	if handlerCalled {
		t.Error("expected middleware to block the handler for unauthorized user")
	}
}

func TestBot_HandlerErrorInvokesErrorHandlerAndRecordsHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var capturedErr error
	bot.OnError(func(err error, c *Ctx) {
		capturedErr = err
	})
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		return errTestHandler
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	if capturedErr != errTestHandler {
		t.Errorf("expected OnError to be called with the handler's error, got %v", capturedErr)
	}
	if got := bot.healthMonitor.GetStatus().ErrorsCount; got != 1 {
		t.Errorf("ErrorsCount = %d, want 1", got)
	}
}

// Reply(...) dispatched through the async worker pool (polling, or a
// webhook registration that didn't opt into AllowWebhookReply) has no HTTP
// response to embed into, so it falls back to a normal, fire-and-forget
// API call — see resolveWebhookReply.
func TestBot_Reply_AsyncDispatch_MakesRealAPICall(t *testing.T) {
	var receivedBody map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		mu.Unlock()
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		return Reply(&SendMessageRequest{ChatID: ChatIDFromInt(555), Text: "async pong"})
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "ping", Chat: &Chat{ID: 555}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	mu.Lock()
	defer mu.Unlock()
	if receivedBody == nil {
		t.Fatal("expected Reply(...) to trigger a real sendMessage call under async dispatch")
	}
	if receivedBody["text"] != "async pong" || receivedBody["chat_id"].(float64) != 555 {
		t.Errorf("server received unexpected sendMessage body: %+v", receivedBody)
	}
}

// The fallback real API call in resolveWebhookReply can itself fail (network
// blip, rate limit, ...) — that failure must reach OnError wrapped as
// "reply(<method>): ...", the same as any other handler-side error, instead
// of vanishing silently since the handler itself already returned via the
// Reply(...) sentinel rather than a normal error.
func TestBot_Reply_AsyncDispatch_APIFailureReachesErrorHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	bot := newTestBot(server)
	var capturedErr error
	bot.OnError(func(err error, c *Ctx) { capturedErr = err })

	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		return Reply(&SendMessageRequest{ChatID: ChatIDFromInt(555), Text: "async pong"})
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "ping", Chat: &Chat{ID: 555}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	if capturedErr == nil || !strings.Contains(capturedErr.Error(), "reply(sendMessage)") {
		t.Errorf("expected a wrapped reply(sendMessage) error to reach OnError, got %v", capturedErr)
	}
}

func TestBot_CallbackQueryReplyUsesMessageChatID(t *testing.T) {
	var receivedBody map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		mu.Unlock()
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	r := NewRouter()
	r.CallbackQuery(FilterCallbackData("confirm")).Handle(func(c *Ctx) error {
		_, err := c.CallbackQuery.Reply("got it")
		return err
	})
	bot.Dispatch(r)

	// The callback's clicking user (From.ID) deliberately differs from the
	// chat the original message lives in (Message.Chat.ID), as happens for
	// callbacks fired in a group chat. Reply must target the chat, not the user.
	cb := bindCallback(&CallbackQuery{
		Data: "confirm",
		From: &User{ID: 999},
		Message: &Message{
			MessageID: 321,
			Chat:      &Chat{ID: -100555},
		},
	}, bot)

	runWorkerFor(t, bot, &Update{CallbackQuery: cb})

	mu.Lock()
	defer mu.Unlock()
	if receivedBody["chat_id"].(float64) != -100555 {
		t.Errorf("expected reply to target the message's chat (-100555), got %v", receivedBody["chat_id"])
	}
	rp, ok := receivedBody["reply_parameters"].(map[string]any)
	if !ok || rp["message_id"].(float64) != 321 {
		t.Errorf("expected reply_parameters.message_id 321, got %v", receivedBody["reply_parameters"])
	}
}

func TestCtx_AnswerCallback_CallsAnswerCallbackQuery(t *testing.T) {
	var gotPath string
	var receivedBody map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&receivedBody)
		mu.Unlock()
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var answerErr error
	r := NewRouter()
	r.CallbackQuery(FilterCallbackData("confirm")).Handle(func(c *Ctx) error {
		answerErr = c.AnswerCallback("Done!", &AnswerCallbackOptions{ShowAlert: true})
		return nil
	})
	bot.Dispatch(r)

	cb := bindCallback(&CallbackQuery{
		ID:      "cbq42",
		Data:    "confirm",
		From:    &User{ID: 9},
		Message: &Message{MessageID: 1, Chat: &Chat{ID: 5}},
	}, bot)

	runWorkerFor(t, bot, &Update{CallbackQuery: cb})

	if answerErr != nil {
		t.Fatalf("unexpected error: %v", answerErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/bot"+testToken+"/answerCallbackQuery" {
		t.Errorf("path = %q, want answerCallbackQuery", gotPath)
	}
	if receivedBody["callback_query_id"] != "cbq42" || receivedBody["text"] != "Done!" ||
		receivedBody["show_alert"] != true {
		t.Errorf("unexpected answerCallbackQuery body: %+v", receivedBody)
	}
}

var errTestHandler = &testError{"handler failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// runWorkersFor starts numWorkers worker goroutines sharing a private,
// pre-closed updateChan (swapped in for b.updateChan and restored after) and
// blocks until every update has been dispatched and every worker has
// returned. worker() now only returns once its channel is closed and
// drained — never on ctx cancellation, so real shutdown can't drop buffered
// updates — so shutting these down means closing the channel, same as
// StopWorkers. Used to exercise real cross-worker concurrency.
func runWorkersFor(t *testing.T, b *TelegramBot, numWorkers int, updates ...*Update) {
	t.Helper()

	ch := make(chan *Update, len(updates))
	for _, u := range updates {
		ch <- u
	}
	close(ch)

	orig := b.updateChan
	b.updateChan = ch
	defer func() { b.updateChan = orig }()

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.worker(context.Background())
		}()
	}
	wg.Wait()
}

const testStateWaitingName State = "test:waiting_name"

func TestBot_FSMConversation_TwoSteps(t *testing.T) {
	var replies []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		replies = append(replies, body["text"].(string))
		mu.Unlock()
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	r := NewRouter()
	r.Message(FilterCommand("register")).Handle(func(c *Ctx) error {
		if err := c.FSM().SetState(testStateWaitingName); err != nil {
			return err
		}
		_, err := c.Answer("What's your name?")
		return err
	})
	r.Message(StateIs(testStateWaitingName)).Handle(func(c *Ctx) error {
		name := c.Message.Text
		if _, err := c.FSM().UpdateData(map[string]any{"name": name}); err != nil {
			return err
		}
		if err := c.FSM().Clear(); err != nil {
			return err
		}
		_, err := c.Answer("Registered " + name + "!")
		return err
	})
	bot.Dispatch(r)

	chat := &Chat{ID: 1}
	user := &User{ID: 1}

	runWorkerFor(t, bot,
		&Update{Message: bindMessage(&Message{Text: "/register", Chat: chat, From: user}, bot)},
	)
	runWorkerFor(t, bot,
		&Update{Message: bindMessage(&Message{Text: "Alice", Chat: chat, From: user}, bot)},
	)

	mu.Lock()
	defer mu.Unlock()
	if len(replies) != 2 || replies[0] != "What's your name?" || replies[1] != "Registered Alice!" {
		t.Fatalf("unexpected conversation replies: %v", replies)
	}

	state, _ := bot.fsmStorage.GetState(context.Background(), StorageKey{ChatID: 1, UserID: 1})
	if state != NoState {
		t.Errorf("expected state to be cleared after registration, got %q", state)
	}
}

// StateIs reads FSM state via Message.FSM(), scoped by the message's own
// {chat, user} — since c.Message here has the same fsm/chat/user as the
// dispatch-level Ctx.FSM(), the two agree, which is what this test proves is
// wired correctly end to end (Router filters operate on the typed payload,
// not Ctx, so they must see the same storage the handler's c.FSM() does).
func TestRouter_MessageFilterSeesSameFSMStateAsCtx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.fsmStorage.SetState(context.Background(), StorageKey{ChatID: 1, UserID: 1}, "waiting_name")

	matched := false
	r := NewRouter()
	r.Message(StateIs("waiting_name")).Handle(func(c *Ctx) error {
		matched = true
		return nil
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "Alice", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	if !matched {
		t.Error("expected StateIs filter to see the state set directly on the bot's fsmStorage")
	}
}

// This test proves the per-key dispatch lock in dispatch() actually prevents
// the race it's meant to: without it, concurrent updates from the same user
// reading-then-writing FSM data would lose updates.
func TestBot_ConcurrentUpdatesFromSameUser_DoNotRaceFSMState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		count, _, err := FSMGet[int](c.FSM(), "count")
		if err != nil {
			return err
		}
		time.Sleep(2 * time.Millisecond) // widen the race window
		count++
		return c.FSM().SetData(map[string]any{"count": count})
	})
	bot.Dispatch(r)

	chat := &Chat{ID: 1}
	user := &User{ID: 1}

	const numUpdates = 20
	updates := make([]*Update, numUpdates)
	for i := range updates {
		updates[i] = &Update{Message: bindMessage(&Message{Text: "ping", Chat: chat, From: user}, bot)}
	}

	runWorkersFor(t, bot, 5, updates...)

	data, err := bot.fsmStorage.GetData(context.Background(), StorageKey{ChatID: 1, UserID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// MemoryStorage deep-copies via a JSON round-trip, so the stored int
	// comes back as float64 here — see FSMGet's doc.
	if count, _ := data["count"].(float64); count != numUpdates {
		t.Errorf("count = %v, want %d (updates were lost to a race)", data["count"], numUpdates)
	}
}

// Regression test: shutdown must not drop updates still buffered in
// updateChan. worker() used to select on ctx.Done() and exit immediately on
// cancellation, abandoning anything still queued — silent data loss on
// every deploy/restart, since Run's offset advancement already tells
// Telegram those updates were handled. StopWorkers must block until every
// buffered update is actually dispatched, regardless of ctx state.
func TestBot_StopWorkers_DrainsBufferedUpdatesBeforeReturning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var mu sync.Mutex
	processed := 0
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		time.Sleep(time.Millisecond) // widen the window for a premature exit
		mu.Lock()
		processed++
		mu.Unlock()
		return nil
	})
	bot.Dispatch(r)

	ctx, cancel := context.WithCancel(context.Background())
	bot.StartWorkers(ctx)

	const numUpdates = 20
	for range numUpdates {
		bot.updateChan <- &Update{Message: bindMessage(&Message{Text: "ping", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)}
	}

	// Cancel before draining finishes — a worker that exits on ctx.Done()
	// would abandon whatever's still buffered here.
	cancel()
	bot.StopWorkers()

	mu.Lock()
	defer mu.Unlock()
	if processed != numUpdates {
		t.Errorf("processed = %d, want %d (buffered updates were dropped on shutdown)", processed, numUpdates)
	}
}

// worker's `for update := range b.updateChan` never sends a nil itself, but
// the guard exists for whatever reason a nil ends up queued anyway — it must
// be skipped rather than passed to dispatch, which would panic building a
// Ctx from a nil *Update.
func TestBot_Worker_SkipsNilUpdatesWithoutPanicking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	handlerRan := false
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error { handlerRan = true; return nil })
	bot.Dispatch(r)

	runWorkerFor(t, bot, nil, &Update{Message: bindMessage(&Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if !handlerRan {
		t.Error("expected the worker to skip the nil update and still dispatch the real one that followed it")
	}
}

// A handler panic must not crash the worker: it should be recovered and
// routed through OnError like any other handler error, and later updates
// must still dispatch normally on the same (now-recovered) worker.
func TestBot_HandlerPanic_IsRecoveredAndRoutedToErrorHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var capturedErr error
	bot.OnError(func(err error, c *Ctx) {
		capturedErr = err
	})

	secondHandlerRan := false
	r := NewRouter()
	r.Message(FilterCommand("boom")).Handle(func(c *Ctx) error {
		panic("kaboom")
	})
	r.Message().Handle(func(c *Ctx) error {
		secondHandlerRan = true
		return nil
	})
	bot.Dispatch(r)

	panicMsg := bindMessage(&Message{Text: "/boom", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: panicMsg})

	if capturedErr == nil || !strings.Contains(capturedErr.Error(), "kaboom") {
		t.Errorf("expected the recovered panic to reach OnError, got %v", capturedErr)
	}

	// The worker goroutine must still be usable afterward.
	okMsg := bindMessage(&Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: okMsg})
	if !secondHandlerRan {
		t.Error("expected the worker to keep processing updates after recovering a panic")
	}
}

// c.Bot() must reach the same TelegramBot that dispatched the update, so
// handlers can call any of the 174 generated methods (SetBotCommands here,
// as a stand-in — any generated method works the same way), not just the
// hand-written Answer/AnswerCallback sugar on Ctx itself.
func TestCtx_Bot_ReachesGeneratedMethods(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	bot := newTestBot(server)

	var callErr error
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		if c.Bot() != bot {
			t.Error("c.Bot() did not return the dispatching bot")
		}
		callErr = c.Bot().SetBotCommands([]BotCommand{{Command: "start", Description: "Start"}})
		return nil
	})
	bot.Dispatch(r)

	msg := bindMessage(&Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg})

	if callErr != nil {
		t.Fatalf("unexpected error calling a generated method via c.Bot(): %v", callErr)
	}
	if gotPath != "/bot"+testToken+"/setMyCommands" {
		t.Errorf("path = %q, want setMyCommands", gotPath)
	}
}

func TestResolveAllowedUpdates(t *testing.T) {
	t.Run("explicit override wins", func(t *testing.T) {
		bot := &TelegramBot{allowedUpdates: []string{"message"}}
		r := NewRouter()
		r.ChatMember().Handle(func(c *Ctx) error { return nil })
		bot.Dispatch(r)

		got := bot.resolveAllowedUpdates()
		if len(got) != 1 || got[0] != "message" {
			t.Errorf("resolveAllowedUpdates() = %v, want [\"message\"] (explicit override)", got)
		}
	})

	t.Run("auto-computed from router when no override", func(t *testing.T) {
		bot := &TelegramBot{}
		r := NewRouter()
		r.Message().Handle(func(c *Ctx) error { return nil })
		r.ChatMember().Handle(func(c *Ctx) error { return nil })
		bot.Dispatch(r)

		got := map[string]bool{}
		for _, k := range bot.resolveAllowedUpdates() {
			got[k] = true
		}
		if !got["message"] || !got["chat_member"] || len(got) != 2 {
			t.Errorf("resolveAllowedUpdates() = %v, want exactly [message, chat_member]", bot.resolveAllowedUpdates())
		}
	})

	t.Run("nil when no router is set", func(t *testing.T) {
		bot := &TelegramBot{}
		if got := bot.resolveAllowedUpdates(); got != nil {
			t.Errorf("resolveAllowedUpdates() = %v, want nil with no router", got)
		}
	})
}

// WithAutoRetry must actually reach the underlying api.Client — this drives
// it through NewTelegramBot (unlike newTestBot, which builds the struct
// directly and bypasses Option processing) against a server that floods
// once on a real call, then succeeds.
func TestWithAutoRetry_WiresThroughToTheClient(t *testing.T) {
	var floodedOnce bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			w.Write([]byte(`{"ok":true,"result":{"id":1000,"is_bot":true,"username":"test_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/setMyCommands") && !floodedOnce:
			floodedOnce = true
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
		default:
			w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer server.Close()

	bot, err := NewTelegramBot(testToken,
		WithBaseURL(server.URL+"/bot"),
		WithAutoRetry(5*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error constructing bot: %v", err)
	}

	if err := bot.SetBotCommands([]BotCommand{{Command: "start", Description: "Start"}}); err != nil {
		t.Errorf("expected the flood to be retried transparently, got error: %v", err)
	}
	if !floodedOnce {
		t.Error("expected the fake server's flood branch to have been hit")
	}
}

// TestWithoutGetMe_SkipsTheNetworkCallAndLoadMeHydratesLater constructs a
// bot against a server that fails any request, proving WithoutGetMe truly
// makes zero network calls at construction (the drawback this option
// closes: a cold serverless start, or CI with no network, can't tolerate an
// unconditional getMe). Me().ID still comes back right, derived from the
// token; Username is empty until LoadMe hydrates it against a real server.
func TestWithoutGetMe_SkipsTheNetworkCallAndLoadMeHydratesLater(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected network call to %s — WithoutGetMe should make none", r.URL.Path)
	}))
	defer unreachable.Close()

	bot, err := NewTelegramBot(testToken, WithBaseURL(unreachable.URL+"/bot"), WithoutGetMe())
	if err != nil {
		t.Fatalf("unexpected error constructing bot: %v", err)
	}
	if bot.Me() == nil || bot.Me().ID != 123456789 {
		t.Fatalf("Me() = %+v, want ID 123456789 derived from the token", bot.Me())
	}
	if bot.Me().Username != "" {
		t.Errorf("Me().Username = %q, want empty before LoadMe", bot.Me().Username)
	}

	real := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{"id":123456789,"is_bot":true,"username":"test_bot"}}`))
	}))
	defer real.Close()
	bot.api = api.NewClientWithBaseURL(testToken, real.URL+"/bot")

	if err := bot.LoadMe(context.Background()); err != nil {
		t.Fatalf("LoadMe: unexpected error: %v", err)
	}
	if bot.Me().Username != "test_bot" {
		t.Errorf("Me().Username after LoadMe = %q, want %q", bot.Me().Username, "test_bot")
	}
}

// getUpdatesEmptyServer answers getMe (for newTestBot-free construction if
// ever needed) and getUpdates with an always-empty result — enough for Run
// to loop harmlessly until ctx is canceled, without a real getUpdates error
// slowing the test down with its 1s retry sleep.
func getUpdatesEmptyServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
}

func TestBot_OnStartup_RunsBeforePollingStarts(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server)
	var ran bool
	bot.OnStartup(func(ctx context.Context) error {
		ran = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ran {
		t.Error("expected the startup hook to have run")
	}
}

func TestBot_OnStartup_ErrorAbortsRun(t *testing.T) {
	var getUpdatesCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getUpdatesCalled = true
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	wantErr := errors.New("db connection failed")
	bot.OnStartup(func(ctx context.Context) error {
		return wantErr
	})

	err := bot.Run(context.Background())
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("Run() error = %v, want it to wrap %v", err, wantErr)
	}
	if getUpdatesCalled {
		t.Error("expected Run to abort before ever calling getUpdates")
	}
}

// A TelegramBot is one-shot: StopWorkers permanently closes updateChan, so
// calling Run/RunWebhook/StartWorkers again after a previous one returned
// used to panic on a send to that closed channel with no clue why. It
// should instead fail fast with a clear error (or, for StartWorkers, a
// clear log line — its signature has no error return).
func TestBot_Run_SecondCallReturnsErrAlreadyRan(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("first Run() failed: %v", err)
	}

	if err := bot.Run(context.Background()); !errors.Is(err, errAlreadyRan) {
		t.Errorf("second Run() error = %v, want errAlreadyRan", err)
	}
}

func TestBot_StartWorkers_SecondCallIsNoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	var buf bytes.Buffer
	bot := newTestBot(server)
	bot.logger = slog.New(slog.NewTextHandler(&buf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	bot.StartWorkers(ctx)
	cancel()
	bot.StopWorkers()

	// A second StartWorkers must not try to send on the now-closed
	// updateChan; it should log and return instead of panicking.
	bot.StartWorkers(context.Background())

	if !strings.Contains(buf.String(), "already ran") {
		t.Errorf("expected an 'already ran' log line from the second StartWorkers call, got: %s", buf.String())
	}
}

func TestBot_OnShutdown_RunsAfterDraining(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server)
	var ran bool
	bot.OnShutdown(func(ctx context.Context) error {
		ran = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ran {
		t.Error("expected the shutdown hook to have run by the time Run returned")
	}
}

func TestBot_OnShutdown_HookErrorDoesNotSkipLaterHooks(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server)
	var secondRan bool
	bot.OnShutdown(func(ctx context.Context) error { return errors.New("first hook failed") })
	bot.OnShutdown(func(ctx context.Context) error {
		secondRan = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !secondRan {
		t.Error("expected the second shutdown hook to run despite the first one's error")
	}
}

func TestBot_Lifecycle_HooksRunInRegistrationOrder(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server)
	var order []string
	bot.OnStartup(func(ctx context.Context) error { order = append(order, "startup1"); return nil })
	bot.OnStartup(func(ctx context.Context) error { order = append(order, "startup2"); return nil })
	bot.OnShutdown(func(ctx context.Context) error { order = append(order, "shutdown1"); return nil })
	bot.OnShutdown(func(ctx context.Context) error { order = append(order, "shutdown2"); return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"startup1", "startup2", "shutdown1", "shutdown2"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i, name := range want {
		if order[i] != name {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, order[i], name, order)
		}
	}
}

// Run's poll-retry backoff delegates to Backoff (backoff_test.go covers its
// doubling/capping directly); this proves Run actually wires pollBackoffMin/
// pollBackoffMax into it end to end, retrying getUpdates after a failure
// instead of giving up.
func TestBot_Run_RetriesGetUpdatesAfterFailure(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	// pollBackoffMin is 1s, so the retry needs a window past that to
	// actually fire.
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("getUpdates was called %d times, want at least 2 (a failure followed by a retry)", calls)
	}
}

// A 409 mid-poll gets its own log message (distinct from a generic getUpdates
// error) since it usually means a second bot instance/webhook is fighting
// this one over the same token — worth telling apart from ordinary network
// flakiness. The short ctx also forces cancellation to land mid-backoff-wait
// (pollBackoffMin is 1s), covering that shutdown race in the same run.
func TestBot_Run_ConflictError_LogsDistinctMessageAndStopsOnCancelDuringBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`))
	}))
	defer server.Close()

	var buf bytes.Buffer
	bot := newTestBot(server)
	bot.logger = slog.New(slog.NewTextHandler(&buf, nil))
	bot.Dispatch(NewRouter())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "getUpdates conflict") {
		t.Errorf("expected the conflict-specific log message, got: %s", out)
	}
	if !strings.Contains(out, "Context canceled") {
		t.Errorf("expected Run to stop gracefully once ctx was canceled mid-backoff-wait, got: %s", out)
	}
}

// Run enqueues each fetched update into updateChan before advancing offset;
// if ctx is canceled while that send is still blocked (e.g. the worker pool
// is momentarily backed up), Run must exit through that specific select case
// rather than the update silently vanishing into a half-finished loop
// iteration.
func TestBot_Run_ContextCanceledWhileEnqueueingUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":[{"update_id":1,"message":{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"},"text":"hi"}}]}`))
	}))
	defer server.Close()

	var buf bytes.Buffer
	bot := newTestBot(server)
	bot.logger = slog.New(slog.NewTextHandler(&buf, nil))
	bot.numWorkers = 0                  // nothing drains updateChan
	bot.updateChan = make(chan *Update) // unbuffered: the first update blocks the send
	bot.Dispatch(NewRouter())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "Context canceled while queueing update") {
		t.Errorf("expected the queueing-specific cancellation message, got: %s", buf.String())
	}
}

func TestIsConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	_, err := bot.getUpdates(context.Background(), 0, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsConflict(err) {
		t.Errorf("expected IsConflict(%v) to be true", err)
	}
	if IsFlood(err) {
		t.Error("a 409 should not also report as flood")
	}
}

func TestBot_WithDropPendingUpdates_FastForwardsOffset(t *testing.T) {
	var gotOffsets []int64
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		gotOffsets = append(gotOffsets, int64(body["offset"].(float64)))
		mu.Unlock()

		if len(gotOffsets) == 1 {
			// The drop-pending call (offset=-1): return one stale update.
			w.Write([]byte(`{"ok":true,"result":[{"update_id":100,"message":{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"},"text":"stale"}}]}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.dropPendingUpdates = true

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotOffsets) < 2 {
		t.Fatalf("expected at least 2 getUpdates calls (drop-pending + real poll), got %d", len(gotOffsets))
	}
	if gotOffsets[0] != -1 {
		t.Errorf("first getUpdates offset = %d, want -1 (drop-pending probe)", gotOffsets[0])
	}
	if gotOffsets[1] != 101 {
		t.Errorf("second getUpdates offset = %d, want 101 (past the stale update_id 100)", gotOffsets[1])
	}
}

// A dropPendingUpdates probe that finds nothing to skip past (no stale
// updates queued) must leave Run polling from offset 0, same as if
// WithDropPendingUpdates had never been set — not from some stale/undefined
// offset left over from the empty response.
func TestBot_WithDropPendingUpdates_NoStaleUpdates_StartsFromZero(t *testing.T) {
	var gotOffsets []int64
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		gotOffsets = append(gotOffsets, int64(body["offset"].(float64)))
		mu.Unlock()
		w.Write([]byte(`{"ok":true,"result":[]}`)) // always empty: nothing stale to skip
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.dropPendingUpdates = true

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotOffsets) < 2 {
		t.Fatalf("expected at least 2 getUpdates calls (drop-pending probe + real poll), got %d", len(gotOffsets))
	}
	if gotOffsets[0] != -1 {
		t.Errorf("first getUpdates offset = %d, want -1 (drop-pending probe)", gotOffsets[0])
	}
	if gotOffsets[1] != 0 {
		t.Errorf("second getUpdates offset = %d, want 0 (no stale updates to skip past)", gotOffsets[1])
	}
}

// The drop-pending probe is itself a getUpdates call and can fail like any
// other — Run must surface that failure instead of silently falling back to
// offset 0 and polling as if nothing were configured.
func TestBot_Run_DropPendingUpdatesError_AbortsRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	bot := newTestBot(server)
	bot.dropPendingUpdates = true
	bot.Dispatch(NewRouter())

	err := bot.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "drop pending updates") {
		t.Errorf("Run() error = %v, want it to wrap the drop-pending-updates failure", err)
	}
}

func TestBot_WithoutDropPendingUpdates_StartsFromZero(t *testing.T) {
	var firstOffset int64 = -999
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		once.Do(func() { firstOffset = int64(body["offset"].(float64)) })
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if firstOffset != 0 {
		t.Errorf("first getUpdates offset = %d, want 0 (no drop-pending requested)", firstOffset)
	}
}

func TestBot_WithLogger_RoutesLifecycleMessagesThroughSlog(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	bot := newTestBot(server)
	bot.logger = logger // newTestBot bypasses Option processing; set directly

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Context canceled") {
		t.Errorf("expected the configured slog logger to have received the shutdown message, got: %s", out)
	}
	if !strings.Contains(out, "level=INFO") {
		t.Errorf("expected a structured slog record (level=INFO), got: %s", out)
	}
}

func TestBot_WithoutLogger_UnaffectedDefaultBehavior(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server) // bot.logger stays nil

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bot.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No assertion beyond "it didn't panic and behaved as before" — the
	// point is that plain log.Println/log.Printf is still the default path.
}

func TestWithLogger_Option_WiresIntoNewTelegramBot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{"id":1000,"is_bot":true,"username":"test_bot"}}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	bot, err := NewTelegramBot(testToken, WithBaseURL(server.URL+"/bot"), WithLogger(logger))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bot.logger != logger {
		t.Error("expected WithLogger to set bot.logger")
	}
}

// TestBot_UnknownUpdateKind_TrackedDistinctlyInHealth simulates a future
// Bot API update kind this version of golagram has no field for: an
// all-nil Update (every payload field absent) must still dispatch without
// crashing, and must be tracked under a distinct "unknown" kind in health
// counters rather than disappearing into a blank-string bucket.
func TestBot_UnknownUpdateKind_TrackedDistinctlyInHealth(t *testing.T) {
	server := getUpdatesEmptyServer()
	defer server.Close()

	bot := newTestBot(server)
	bot.Dispatch(NewRouter())

	runWorkerFor(t, bot, &Update{UpdateID: 1}) // no payload field set at all

	status := bot.healthMonitor.GetStatus()
	if status.DispatchedByKind["unknown"] != 1 {
		t.Errorf("DispatchedByKind[unknown] = %d, want 1, got %+v", status.DispatchedByKind["unknown"], status.DispatchedByKind)
	}
	if status.UnmatchedByKind["unknown"] != 1 {
		t.Errorf("UnmatchedByKind[unknown] = %d, want 1, got %+v", status.UnmatchedByKind["unknown"], status.UnmatchedByKind)
	}
}

// HandleUpdate must hydrate the update itself: unlike runWorkerFor's tests
// above, this passes a raw, un-bound *Message straight in — if HandleUpdate
// forgot the b.hydrate(u) step, c.Reply would panic/no-op on a nil api
// client instead of making this real call.
func TestBot_HandleUpdate_HydratesAndDispatchesWithoutWorkerPool(t *testing.T) {
	var receivedBody map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		mu.Unlock()
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		_, err := c.Reply("pong")
		return err
	})
	bot.Dispatch(r)

	// No StartWorkers, no runWorkerFor, no bindMessage — HandleUpdate is the
	// only thing touching this update.
	bot.HandleUpdate(context.Background(), &Update{
		Message: &Message{Text: "ping", Chat: &Chat{ID: 555}, From: &User{ID: 1}},
	})

	mu.Lock()
	defer mu.Unlock()
	if receivedBody == nil {
		t.Fatal("expected HandleUpdate to hydrate the message and dispatch it, triggering a real sendMessage call")
	}
	if receivedBody["text"] != "pong" || receivedBody["chat_id"].(float64) != 555 {
		t.Errorf("server received unexpected sendMessage body: %+v", receivedBody)
	}
}

func TestBot_HandleUpdate_ErrorReachesErrorHandlerAndRecordsHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	var capturedErr error
	bot.OnError(func(err error, c *Ctx) {
		capturedErr = err
	})
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error {
		return errTestHandler
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), &Update{
		Message: &Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}},
	})

	if capturedErr != errTestHandler {
		t.Errorf("expected OnError to be called with the handler's error, got %v", capturedErr)
	}
	if got := bot.healthMonitor.GetStatus().ErrorsCount; got != 1 {
		t.Errorf("ErrorsCount = %d, want 1", got)
	}
}

// Every other error-handling test sets OnError, which only exercises
// handleError's custom-handler branch. A bot that never calls OnError must
// still fall back to logging the error itself instead of swallowing it.
func TestBot_HandleError_NoErrorHandlerLogsDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	var buf bytes.Buffer
	bot := newTestBot(server)
	bot.logger = slog.New(slog.NewTextHandler(&buf, nil))
	// No bot.OnError(...) — exercises handleError's default-logging fallback.

	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error { return errTestHandler })
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), &Update{
		Message: &Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}},
	})

	if !strings.Contains(buf.String(), "Error in handler") {
		t.Errorf("expected the default error-handler fallback to log the error, got: %s", buf.String())
	}
}

func TestBot_HandleUpdate_RecoversPanicAndStaysUsable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)
	var capturedErr error
	bot.OnError(func(err error, c *Ctx) {
		capturedErr = err
	})

	secondHandlerRan := false
	r := NewRouter()
	r.Message(FilterCommand("boom")).Handle(func(c *Ctx) error {
		panic("kaboom")
	})
	r.Message().Handle(func(c *Ctx) error {
		secondHandlerRan = true
		return nil
	})
	bot.Dispatch(r)

	bot.HandleUpdate(context.Background(), &Update{
		Message: &Message{Text: "/boom", Chat: &Chat{ID: 1}, From: &User{ID: 1}},
	})
	if capturedErr == nil || !strings.Contains(capturedErr.Error(), "kaboom") {
		t.Errorf("expected the recovered panic to reach OnError, got %v", capturedErr)
	}

	bot.HandleUpdate(context.Background(), &Update{
		Message: &Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}},
	})
	if !secondHandlerRan {
		t.Error("expected the bot to keep handling updates after recovering a panic")
	}
}

// A bot that never called Dispatch has no router — HandleUpdate must count
// the update as unmatched, not panic.
func TestBot_HandleUpdate_NoRouterIsUnmatchedNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()

	bot := newTestBot(server)

	bot.HandleUpdate(context.Background(), &Update{
		Message: &Message{Text: "hi", Chat: &Chat{ID: 1}, From: &User{ID: 1}},
	})

	status := bot.healthMonitor.GetStatus()
	if status.DispatchedByKind["message"] != 1 {
		t.Errorf("DispatchedByKind[message] = %d, want 1", status.DispatchedByKind["message"])
	}
	if status.UnmatchedByKind["message"] != 1 {
		t.Errorf("UnmatchedByKind[message] = %d, want 1", status.UnmatchedByKind["message"])
	}
}
