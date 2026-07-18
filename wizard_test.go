package golagram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWizard_Enter_SetsStateAndClearsStaleDataAndRunsOnEnter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	var enteredWith string
	w := NewWizard("reg", WithOnEnter(func(c *Ctx) error {
		enteredWith, _, _ = FSMGet[string](c.FSM(), "stale")
		return nil
	}))
	w.Step(func(wc *WizardCtx) error { return wc.Next() })

	r := NewRouter()
	r.Include(w.Router())
	r.Message(FilterCommand("register")).Handle(func(c *Ctx) error {
		FSMSet(c.FSM(), "stale", "leftover") // simulate a prior abandoned attempt
		return w.Enter(c)
	})
	bot.Dispatch(r)

	// First entry leaves stale data in the store...
	msg1 := bindMessage(&Message{Text: "/register", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg1})

	// ...second entry must not see it, because Enter clears before setting state 0.
	msg2 := bindMessage(&Message{Text: "/register", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg2})

	if enteredWith != "" {
		t.Errorf("OnEnter saw stale data %q, want empty (Enter should Clear first)", enteredWith)
	}

	key := StorageKey{ChatID: 1, UserID: 1}
	state, err := bot.fsmStorage.GetState(context.Background(), key)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != w.State(0) {
		t.Errorf("state = %q, want step 0 (%q)", state, w.State(0))
	}
}

func TestWizard_Next_WalksThroughStepsAndExitsAfterLast(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	exited := false
	w := NewWizard("reg", WithOnExit(func(c *Ctx) error {
		exited = true
		return nil
	}))
	w.Step(func(wc *WizardCtx) error {
		FSMSet(wc.FSM(), "name", wc.Text())
		return wc.Next()
	})
	w.Step(func(wc *WizardCtx) error {
		FSMSet(wc.FSM(), "age", wc.Text())
		return wc.Next()
	})

	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	msg1 := bindMessage(&Message{Text: "Alice", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg1})

	state, _ := bot.fsmStorage.GetState(context.Background(), key)
	if state != w.State(1) {
		t.Fatalf("after step 0's Next(), state = %q, want step 1 (%q)", state, w.State(1))
	}
	if exited {
		t.Fatal("OnExit ran too early, after only one of two steps")
	}

	msg2 := bindMessage(&Message{Text: "30", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)
	runWorkerFor(t, bot, &Update{Message: msg2})

	state, _ = bot.fsmStorage.GetState(context.Background(), key)
	if state != NoState {
		t.Errorf("after the last step's Next(), state = %q, want NoState (wizard should have exited)", state)
	}
	if !exited {
		t.Error("expected OnExit to run after Next() past the last step")
	}

	name, _, _ := FSMGet[string](NewFSMContext(context.Background(), bot.fsmStorage, key), "name")
	age, _, _ := FSMGet[string](NewFSMContext(context.Background(), bot.fsmStorage, key), "age")
	if name != "" || age != "" {
		t.Errorf("expected Exit to clear data, got name=%q age=%q", name, age)
	}
}

func TestWizard_Back_ReturnsToPreviousStep_NoopOnFirstStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { return wc.Back() }) // step 0: Back should no-op
	w.Step(func(wc *WizardCtx) error { return wc.Back() }) // step 1: Back should return to step 0

	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}

	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "x", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != w.State(0) {
		t.Errorf("Back() on step 0 = %q, want no-op (still step 0, %q)", state, w.State(0))
	}

	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(1)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "x", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != w.State(0) {
		t.Errorf("Back() on step 1 = %q, want step 0 (%q)", state, w.State(0))
	}
}

func TestWizard_GoTo_JumpsArbitrarily_PanicsOutOfRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { return wc.GoTo(2) })
	w.Step(func(wc *WizardCtx) error { return nil })
	w.Step(func(wc *WizardCtx) error { return wc.GoTo(99) }) // out of range, from step 2

	var capturedErr error
	bot.OnError(func(err error, c *Ctx) { capturedErr = err })
	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "x", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != w.State(2) {
		t.Fatalf("GoTo(2) = %q, want step 2 (%q)", state, w.State(2))
	}

	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "x", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})
	if capturedErr == nil || !strings.Contains(capturedErr.Error(), "out of range") {
		t.Errorf("expected the out-of-range GoTo panic to be recovered into OnError, got %v", capturedErr)
	}
}

func TestWizard_CancelCommand_ClearsStateAndRunsOnCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	var canceled, exited bool
	w := NewWizard("reg",
		WithOnExit(func(c *Ctx) error { exited = true; return nil }),
		WithOnCancel(func(c *Ctx) error { canceled = true; return nil }),
	)
	w.Step(func(wc *WizardCtx) error { return nil })

	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "/cancel", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if !canceled {
		t.Error("expected the default /cancel command to run OnCancel")
	}
	if exited {
		t.Error("OnCancel is set, so OnExit should not also run")
	}
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != NoState {
		t.Errorf("state after cancel = %q, want NoState", state)
	}
}

func TestWizard_CancelCommand_FallsBackToOnExit_WhenOnCancelUnset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	var exited bool
	w := NewWizard("reg", WithOnExit(func(c *Ctx) error { exited = true; return nil }))
	w.Step(func(wc *WizardCtx) error { return nil })

	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "/cancel", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if !exited {
		t.Error("expected /cancel to fall back to OnExit's hook when OnCancel is unset")
	}
}

func TestWizard_WithCancelCommand_Empty_DisablesBuiltinRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg", WithCancelCommand(""))
	w.Step(func(wc *WizardCtx) error { return nil })

	fallthroughRan := false
	r := NewRouter()
	r.Include(w.Router())
	r.Message(FilterCommand("cancel")).Handle(func(c *Ctx) error {
		fallthroughRan = true
		return nil
	})
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "/cancel", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if !fallthroughRan {
		t.Error("expected /cancel to fall through to the app's own handler when WithCancelCommand(\"\") disables the built-in route")
	}
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != w.State(0) {
		t.Errorf("state = %q, want unchanged step 0 (%q) — disabled cancel route shouldn't touch FSM state", state, w.State(0))
	}
}

func TestWizard_Step_ExcludesCommandsByDefault_WithStepAllowCommandsOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	stepRan := false
	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { stepRan = true; return nil })

	otherCommandRan := false
	r := NewRouter()
	r.Include(w.Router())
	r.Message(FilterCommand("order")).Handle(func(c *Ctx) error {
		otherCommandRan = true
		return nil
	})
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "/order", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if stepRan {
		t.Error("a stray /order mid-wizard should not be swallowed as step input")
	}
	if !otherCommandRan {
		t.Error("expected /order to fall through to its own handler")
	}
}

func TestWizard_Step_WithStepAllowCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	var receivedText string
	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error {
		receivedText = wc.Text()
		return nil
	}, WithStepAllowCommands())

	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "/looks-like-a-command", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if receivedText != "/looks-like-a-command" {
		t.Errorf("expected WithStepAllowCommands to let command-shaped text reach the step, got %q", receivedText)
	}
}

func TestWizard_WithStepIgnore_FallsThrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	isMenuButton := func(c *Ctx) bool { return c.Text() == "🏠 Menu" }

	stepRan := false
	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { stepRan = true; return nil }, WithStepIgnore(isMenuButton))

	menuRan := false
	r := NewRouter()
	r.Include(w.Router())
	r.Message(isMenuButton).Handle(func(c *Ctx) error {
		menuRan = true
		return nil
	})
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "🏠 Menu", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if stepRan {
		t.Error("a WithStepIgnore-matched update should not reach the step")
	}
	if !menuRan {
		t.Error("expected the ignored update to fall through to the menu handler")
	}
}

func TestWizard_CancelAndExit_WorkFromPlainHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	var canceled bool
	w := NewWizard("reg", WithOnCancel(func(c *Ctx) error { canceled = true; return nil }))
	w.Step(func(wc *WizardCtx) error { return nil })

	// A callback-query handler outside any wizard step, calling
	// Wizard.Cancel directly on a plain *Ctx — e.g. an inline "Cancel"
	// button shown alongside a wizard step's own prompt.
	r := NewRouter()
	r.Include(w.Router())
	r.CallbackQuery(FilterCallbackData("cancel_btn")).Handle(func(c *Ctx) error {
		return w.Cancel(c)
	})
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	cq := bindCallback(&CallbackQuery{ID: "1", From: &User{ID: 1}, Data: "cancel_btn",
		Message: &Message{Chat: &Chat{ID: 1}}}, bot)
	runWorkerFor(t, bot, &Update{CallbackQuery: cq})

	if !canceled {
		t.Error("expected Wizard.Cancel called from a plain (non-step) handler to run OnCancel")
	}
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != NoState {
		t.Errorf("state = %q, want NoState after Cancel", state)
	}
}

// setStateFailsStorage lets Clear/GetState/GetData/etc. succeed normally
// (delegated to a real MemoryStorage) while SetState always fails — isolates
// Enter's SetState-error return from its separate Clear-error return.
type setStateFailsStorage struct{ *MemoryStorage }

func (setStateFailsStorage) SetState(context.Context, StorageKey, State) error {
	return errFSMDown
}

func TestWizard_State_PanicsOutOfRange(t *testing.T) {
	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { return nil })

	for _, i := range []int{-1, 1, 99} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("State(%d): expected a panic, got none", i)
					return
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, "out of range") {
					t.Errorf("State(%d) panic = %v, want it to mention 'out of range'", i, r)
				}
			}()
			w.State(i)
		}()
	}
}

func TestWizard_Enter_NoStepsRegistered_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg") // no Step() calls
	c := newCtx(context.Background(), &Update{Message: &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}},
		bot, nil, bot.fsmStorage, "test_bot")

	err := w.Enter(c)
	if err == nil || !strings.Contains(err.Error(), "no steps registered") {
		t.Errorf("Enter() error = %v, want it to mention no steps registered", err)
	}
}

func TestWizard_Enter_WithoutOnEnter_SetsStateAndReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg") // no WithOnEnter
	w.Step(func(wc *WizardCtx) error { return nil })

	key := StorageKey{ChatID: 1, UserID: 1}
	c := newCtx(context.Background(), &Update{Message: &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}},
		bot, nil, bot.fsmStorage, "test_bot")

	if err := w.Enter(c); err != nil {
		t.Fatalf("Enter() error = %v, want nil (no OnEnter hook set)", err)
	}
	state, _ := bot.fsmStorage.GetState(context.Background(), key)
	if state != w.State(0) {
		t.Errorf("state = %q, want step 0 (%q)", state, w.State(0))
	}
}

func TestWizard_Exit_WithoutOnExit_ReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg") // no WithOnExit
	w.Step(func(wc *WizardCtx) error { return nil })

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	c := newCtx(context.Background(), &Update{Message: &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}},
		bot, nil, bot.fsmStorage, "test_bot")

	if err := w.Exit(c); err != nil {
		t.Errorf("Exit() error = %v, want nil (no OnExit hook set)", err)
	}
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != NoState {
		t.Errorf("state after Exit = %q, want NoState", state)
	}
}

func TestWizard_Cancel_WithoutAnyHook_ReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg") // no WithOnCancel, no WithOnExit
	w.Step(func(wc *WizardCtx) error { return nil })

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	c := newCtx(context.Background(), &Update{Message: &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}},
		bot, nil, bot.fsmStorage, "test_bot")

	if err := w.Cancel(c); err != nil {
		t.Errorf("Cancel() error = %v, want nil (no hooks set)", err)
	}
	if state, _ := bot.fsmStorage.GetState(context.Background(), key); state != NoState {
		t.Errorf("state after Cancel = %q, want NoState", state)
	}
}

func TestWizard_EnterExitCancel_PropagateFSMClearError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { return nil })

	c := newCtx(context.Background(), &Update{Message: &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}},
		bot, nil, erroringFSMStorage{}, "test_bot")

	if err := w.Enter(c); !errors.Is(err, errFSMDown) {
		t.Errorf("Enter() error = %v, want it to wrap errFSMDown", err)
	}
	if err := w.Exit(c); !errors.Is(err, errFSMDown) {
		t.Errorf("Exit() error = %v, want it to wrap errFSMDown", err)
	}
	if err := w.Cancel(c); !errors.Is(err, errFSMDown) {
		t.Errorf("Cancel() error = %v, want it to wrap errFSMDown", err)
	}
}

func TestWizard_Enter_PropagatesSetStateError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	bot := newTestBot(server)

	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error { return nil })

	c := newCtx(context.Background(), &Update{Message: &Message{Chat: &Chat{ID: 1}, From: &User{ID: 1}}},
		bot, nil, setStateFailsStorage{NewMemoryStorage()}, "test_bot")

	err := w.Enter(c)
	if !errors.Is(err, errFSMDown) {
		t.Errorf("Enter() error = %v, want it to wrap errFSMDown (SetState failure)", err)
	}
}

func TestWizard_DataThreading_SetInOneStepReadableInNext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sendMessageOK))
	}))
	defer server.Close()
	bot := newTestBot(server)

	var readBack string
	w := NewWizard("reg")
	w.Step(func(wc *WizardCtx) error {
		if err := FSMSet(wc.FSM(), "name", wc.Text()); err != nil {
			return err
		}
		return wc.Next()
	})
	w.Step(func(wc *WizardCtx) error {
		readBack, _, _ = FSMGet[string](wc.FSM(), "name")
		return nil
	})

	r := NewRouter()
	r.Include(w.Router())
	bot.Dispatch(r)

	key := StorageKey{ChatID: 1, UserID: 1}
	if err := bot.fsmStorage.SetState(context.Background(), key, w.State(0)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "Alice", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})
	runWorkerFor(t, bot, &Update{Message: bindMessage(&Message{Text: "ignored", Chat: &Chat{ID: 1}, From: &User{ID: 1}}, bot)})

	if readBack != "Alice" {
		t.Errorf("readBack = %q, want %q", readBack, "Alice")
	}
}
