package golagram

import (
	"context"
	"errors"
	"testing"
)

func ctxFor(u *Update) *Ctx {
	return newCtx(context.Background(), u, nil, nil, NewMemoryStorage(), "")
}

func TestRouter_Message_DispatchesToMatchingHandler(t *testing.T) {
	r := NewRouter()

	called := false
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error {
		called = true
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "/start"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || !called {
		t.Errorf("matched=%v called=%v, want both true", matched, called)
	}
}

func TestRouter_FirstMatchWins(t *testing.T) {
	r := NewRouter()

	var order []string
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "any")
		return nil
	})
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error {
		order = append(order, "start")
		return nil
	})

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "/start"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 1 || order[0] != "any" {
		t.Errorf("expected only the first registered match to run, got %v", order)
	}
}

// Regression test: a callback-query update must not run message handlers
// and vice versa (the original emptyEvent() bug this whole redesign traces
// back to).
func TestRouter_CallbackQueryUpdate_DoesNotRunMessageHandlers(t *testing.T) {
	r := NewRouter()

	messageRuns, callbackRuns := 0, 0
	r.Message().Handle(func(c *Ctx) error {
		messageRuns++
		return nil
	})
	r.CallbackQuery().Handle(func(c *Ctx) error {
		callbackRuns++
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{CallbackQuery: &CallbackQuery{Data: "x"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || callbackRuns != 1 || messageRuns != 0 {
		t.Errorf("matched=%v messageRuns=%d callbackRuns=%d, want true/0/1", matched, messageRuns, callbackRuns)
	}
}

func TestRouter_NoMatch_ReturnsFalse(t *testing.T) {
	r := NewRouter()
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error { return nil })

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "/other"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("expected no match for an unregistered command")
	}
}

func TestRouter_EditedMessage_DispatchesSeparatelyFromMessage(t *testing.T) {
	r := NewRouter()

	var editedText string
	messageRuns := 0
	r.Message().Handle(func(c *Ctx) error {
		messageRuns++
		return nil
	})
	r.EditedMessage().Handle(func(c *Ctx) error {
		editedText = c.EditedMessage.Text
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{EditedMessage: &Message{Text: "fixed typo"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || editedText != "fixed typo" || messageRuns != 0 {
		t.Errorf("matched=%v editedText=%q messageRuns=%d", matched, editedText, messageRuns)
	}
}

// updateKindCase pairs one Update kind with a way to build a matching
// *Update and to register a handler for that kind on a Router.
type updateKindCase struct {
	kind     string
	build    func() *Update
	register func(r *Router) *registration
}

// allUpdateKindCases enumerates every update kind Router dispatches on,
// mirroring Update.Kind()'s switch, so TestRouter_AllUpdateKinds_DispatchIsolation
// stays honest against the full kind list without hand-maintained duplication.
func allUpdateKindCases() []updateKindCase {
	return []updateKindCase{
		{"message", func() *Update { return &Update{Message: &Message{}} }, func(r *Router) *registration { return r.Message() }},
		{"edited_message", func() *Update { return &Update{EditedMessage: &Message{}} }, func(r *Router) *registration { return r.EditedMessage() }},
		{"channel_post", func() *Update { return &Update{ChannelPost: &Message{}} }, func(r *Router) *registration { return r.ChannelPost() }},
		{"edited_channel_post", func() *Update { return &Update{EditedChannelPost: &Message{}} }, func(r *Router) *registration { return r.EditedChannelPost() }},
		{"business_connection", func() *Update { return &Update{BusinessConnection: &BusinessConnection{}} }, func(r *Router) *registration { return r.BusinessConnection() }},
		{"business_message", func() *Update { return &Update{BusinessMessage: &Message{}} }, func(r *Router) *registration { return r.BusinessMessage() }},
		{"edited_business_message", func() *Update { return &Update{EditedBusinessMessage: &Message{}} }, func(r *Router) *registration { return r.EditedBusinessMessage() }},
		{"deleted_business_messages", func() *Update { return &Update{DeletedBusinessMessages: &BusinessMessagesDeleted{}} }, func(r *Router) *registration { return r.DeletedBusinessMessages() }},
		{"guest_message", func() *Update { return &Update{GuestMessage: &Message{}} }, func(r *Router) *registration { return r.GuestMessage() }},
		{"message_reaction", func() *Update { return &Update{MessageReaction: &MessageReactionUpdated{}} }, func(r *Router) *registration { return r.MessageReaction() }},
		{"message_reaction_count", func() *Update { return &Update{MessageReactionCount: &MessageReactionCountUpdated{}} }, func(r *Router) *registration { return r.MessageReactionCount() }},
		{"inline_query", func() *Update { return &Update{InlineQuery: &InlineQuery{ID: "1", Query: "cats"}} }, func(r *Router) *registration { return r.InlineQuery() }},
		{"chosen_inline_result", func() *Update { return &Update{ChosenInlineResult: &ChosenInlineResult{}} }, func(r *Router) *registration { return r.ChosenInlineResult() }},
		{"callback_query", func() *Update { return &Update{CallbackQuery: &CallbackQuery{Data: "x"}} }, func(r *Router) *registration { return r.CallbackQuery() }},
		{"shipping_query", func() *Update { return &Update{ShippingQuery: &ShippingQuery{}} }, func(r *Router) *registration { return r.ShippingQuery() }},
		{"pre_checkout_query", func() *Update { return &Update{PreCheckoutQuery: &PreCheckoutQuery{}} }, func(r *Router) *registration { return r.PreCheckoutQuery() }},
		{"purchased_paid_media", func() *Update { return &Update{PurchasedPaidMedia: &PaidMediaPurchased{}} }, func(r *Router) *registration { return r.PurchasedPaidMedia() }},
		{"poll", func() *Update { return &Update{Poll: &Poll{}} }, func(r *Router) *registration { return r.Poll() }},
		{"poll_answer", func() *Update { return &Update{PollAnswer: &PollAnswer{}} }, func(r *Router) *registration { return r.PollAnswer() }},
		{"my_chat_member", func() *Update { return &Update{MyChatMember: &ChatMemberUpdated{}} }, func(r *Router) *registration { return r.MyChatMember() }},
		{"chat_member", func() *Update { return &Update{ChatMember: &ChatMemberUpdated{}} }, func(r *Router) *registration { return r.ChatMember() }},
		{"chat_join_request", func() *Update { return &Update{ChatJoinRequest: &ChatJoinRequest{}} }, func(r *Router) *registration { return r.ChatJoinRequest() }},
		{"chat_boost", func() *Update { return &Update{ChatBoost: &ChatBoostUpdated{}} }, func(r *Router) *registration { return r.ChatBoost() }},
		{"removed_chat_boost", func() *Update { return &Update{RemovedChatBoost: &ChatBoostRemoved{}} }, func(r *Router) *registration { return r.RemovedChatBoost() }},
		{"managed_bot", func() *Update { return &Update{ManagedBot: &ManagedBotUpdated{}} }, func(r *Router) *registration { return r.ManagedBot() }},
	}
}

// TestRouter_AllUpdateKinds_DispatchIsolation is the broad-coverage
// regression test: for every one of Router's per-kind registration methods,
// register a handler for every kind on a single router, then dispatch one
// update per kind and assert exactly the matching kind's handler ran.
// Catches the "kind A's update also runs kind B's handler" class of bug
// across the full kind list, not just the two (message, callback_query)
// that had dedicated tests plus a two-kind smoke test for the other 22.
func TestRouter_AllUpdateKinds_DispatchIsolation(t *testing.T) {
	cases := allUpdateKindCases()

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			r := NewRouter()
			ran := map[string]int{}
			for _, reg := range cases {
				kind := reg.kind
				reg.register(r).Handle(func(c *Ctx) error {
					ran[kind]++
					return nil
				})
			}

			matched, err := r.dispatch(ctxFor(tc.build()))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !matched {
				t.Fatalf("expected a handler to match kind %q", tc.kind)
			}
			if ran[tc.kind] != 1 {
				t.Errorf("expected kind %q's own handler to run exactly once, ran=%d", tc.kind, ran[tc.kind])
			}
			for k, n := range ran {
				if k != tc.kind && n != 0 {
					t.Errorf("kind %q's update also ran handler for kind %q (%d times)", tc.kind, k, n)
				}
			}
		})
	}
}

func TestRouter_Use_WrapsMatchedHandlersInRegistrationOrder(t *testing.T) {
	r := NewRouter()

	var order []string
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			order = append(order, "first")
			return next(c)
		}
	})
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			order = append(order, "second")
			return next(c)
		}
	})
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "handler")
		return nil
	})

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"first", "second", "handler"}
	if len(order) != len(want) {
		t.Fatalf("got order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got order %v, want %v", order, want)
		}
	}
}

func TestRouter_UseOuter_RunsEvenWhenNothingMatches(t *testing.T) {
	r := NewRouter()

	outerRuns := 0
	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			outerRuns++
			return next(c)
		}
	})
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error { return nil })

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "not a command"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("expected no match")
	}
	if outerRuns != 1 {
		t.Errorf("expected outer middleware to run once even on a miss, ran %d times", outerRuns)
	}
}

func TestRouter_UseOuter_RunsBeforeFiltersCanReadWhatItSets(t *testing.T) {
	r := NewRouter()

	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			c.Set("role", "admin")
			return next(c)
		}
	})
	isAdmin := func(c *Ctx) bool {
		role, _ := c.Get("role")
		return role == "admin"
	}

	var ran bool
	r.Message(isAdmin).Handle(func(c *Ctx) error {
		ran = true
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || !ran {
		t.Errorf("expected the filter to see outer middleware's ctx data and match, matched=%v ran=%v", matched, ran)
	}
}

func TestRouter_UseOuter_ScopedToOwnRouter_NotInheritedByIncludedSubRouter(t *testing.T) {
	r := NewRouter()
	sub := NewRouter()

	outerRuns := 0
	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			outerRuns++
			return next(c)
		}
	})
	sub.Message(FilterCommand("ping")).Handle(func(c *Ctx) error { return nil })
	r.Include(sub)

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "/ping"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outerRuns != 1 {
		t.Errorf("expected the parent's outer middleware to run once for the update reaching sub, ran %d times", outerRuns)
	}

	// sub itself has no outer middleware of its own — confirm nothing about
	// this test relies on sub secretly inheriting r's.
	if len(sub.outerMiddlewares) != 0 {
		t.Error("sub-router should not have inherited the parent's outer middleware")
	}
}

func TestRouter_UseOuter_CanShortCircuitWithoutCallingNext(t *testing.T) {
	r := NewRouter()

	var handlerRan bool
	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			return true, errors.New("rejected before routing")
		}
	})
	r.Message().Handle(func(c *Ctx) error {
		handlerRan = true
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if !matched || err == nil || err.Error() != "rejected before routing" {
		t.Errorf("expected the short-circuit result to pass through unchanged, got matched=%v err=%v", matched, err)
	}
	if handlerRan {
		t.Error("handler should never have run — outer middleware never called next")
	}
}

func TestRouter_UseOuter_ErrorGoesThroughOwnOnError(t *testing.T) {
	r := NewRouter()

	var handledErr error
	r.OnError(func(err error, c *Ctx) { handledErr = err })
	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			return true, errors.New("boom")
		}
	})
	r.Message().Handle(func(c *Ctx) error { return nil })

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if err != nil {
		t.Errorf("expected OnError to absorb the error (return nil), got %v", err)
	}
	if !matched {
		t.Error("expected matched=true to pass through even though OnError absorbed the error")
	}
	if handledErr == nil || handledErr.Error() != "boom" {
		t.Errorf("expected this router's OnError to see the outer middleware's error, got %v", handledErr)
	}
}

func TestRouter_UseOuter_MultipleRunInRegistrationOrder(t *testing.T) {
	r := NewRouter()

	var order []string
	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			order = append(order, "first")
			return next(c)
		}
	})
	r.UseOuter(func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			order = append(order, "second")
			return next(c)
		}
	})
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "handler")
		return nil
	})

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"first", "second", "handler"}
	if len(order) != len(want) {
		t.Fatalf("got order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got order %v, want %v", order, want)
		}
	}
}

func TestRouter_Use_CanShortCircuit(t *testing.T) {
	r := NewRouter()
	blockErr := errors.New("blocked")

	handlerCalled := false
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			return blockErr // never calls next
		}
	})
	r.Message().Handle(func(c *Ctx) error {
		handlerCalled = true
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if !matched {
		t.Error("expected matched=true even though middleware short-circuited (the handler was found and run)")
	}
	if !errors.Is(err, blockErr) {
		t.Fatalf("expected blockErr, got %v", err)
	}
	if handlerCalled {
		t.Error("handler should not run when middleware short-circuits")
	}
}

func TestRouter_Include_TriesSubRouterAtRegistrationPoint(t *testing.T) {
	r := NewRouter()
	admin := NewRouter()

	var order []string
	adminMWRan := false
	admin.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			adminMWRan = true
			return next(c)
		}
	})
	admin.Message(FilterCommand("ban")).Handle(func(c *Ctx) error {
		order = append(order, "admin:ban")
		return nil
	})

	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error {
		order = append(order, "start")
		return nil
	})
	r.Include(admin)
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "fallback")
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "/ban"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || len(order) != 1 || order[0] != "admin:ban" || !adminMWRan {
		t.Errorf("expected included router's handler to run with its own middleware, got order=%v adminMWRan=%v", order, adminMWRan)
	}

	// Included router's middleware must not leak onto the parent's own handlers.
	order = nil
	adminMWRan = false
	matched, err = r.dispatch(ctxFor(&Update{Message: &Message{Text: "/start"}}))
	if err != nil || !matched || len(order) != 1 || order[0] != "start" || adminMWRan {
		t.Errorf("parent handler should run without the included router's middleware, got order=%v adminMWRan=%v", order, adminMWRan)
	}

	// Falls through to the parent's own catch-all when nothing in the
	// included router matches.
	order = nil
	matched, err = r.dispatch(ctxFor(&Update{Message: &Message{Text: "anything else"}}))
	if err != nil || !matched || len(order) != 1 || order[0] != "fallback" {
		t.Errorf("expected fallback to run, got order=%v", order)
	}
}

func TestErrSkipHandler_FallsThroughToNextMatch(t *testing.T) {
	r := NewRouter()

	var order []string
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "first")
		return ErrSkipHandler
	})
	r.Message().Handle(func(c *Ctx) error {
		order = append(order, "second")
		return nil
	})

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("expected both handlers to run in order via ErrSkipHandler fallthrough, got %v", order)
	}
}

func TestRouter_DefaultHandler_OnlyRunsWhenNothingMoreSpecificMatched(t *testing.T) {
	r := NewRouter()

	var ran string
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error {
		ran = "start"
		return nil
	})
	r.Message().Handle(func(c *Ctx) error { // default: registered last, no filters
		ran = "default"
		return nil
	})

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "/start"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran != "start" {
		t.Errorf("expected the specific handler to win, got %q", ran)
	}

	ran = ""
	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "anything"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran != "default" {
		t.Errorf("expected the default handler to catch it, got %q", ran)
	}
}

func TestRouter_UsedUpdateKinds_CollectsAcrossIncludedRouters(t *testing.T) {
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error { return nil })
	r.CallbackQuery().Handle(func(c *Ctx) error { return nil })

	sub := NewRouter()
	sub.ChatMember().Handle(func(c *Ctx) error { return nil })
	r.Include(sub)

	// Registering a second handler for a kind already used shouldn't
	// duplicate it in the result.
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error { return nil })

	got := map[string]bool{}
	for _, k := range r.UsedUpdateKinds() {
		got[k] = true
	}

	want := []string{"message", "callback_query", "chat_member"}
	if len(got) != len(want) {
		t.Fatalf("UsedUpdateKinds() = %v, want exactly %v", r.UsedUpdateKinds(), want)
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("expected %q in UsedUpdateKinds(), got %v", k, r.UsedUpdateKinds())
		}
	}
}

func TestRouter_UsedUpdateKinds_EmptyRouterReturnsEmpty(t *testing.T) {
	r := NewRouter()
	if kinds := r.UsedUpdateKinds(); len(kinds) != 0 {
		t.Errorf("expected no used kinds for an empty router, got %v", kinds)
	}
}

func TestRouter_Include_DirectSelfCyclePanics(t *testing.T) {
	r := NewRouter()
	defer func() {
		if recover() == nil {
			t.Error("expected Include(r) on itself to panic")
		}
	}()
	r.Include(r)
}

func TestRouter_Include_IndirectCyclePanics(t *testing.T) {
	a := NewRouter()
	b := NewRouter()
	c := NewRouter()
	a.Include(b)
	b.Include(c)

	defer func() {
		if recover() == nil {
			t.Error("expected c.Include(a) to panic — it would close a -> b -> c -> a")
		}
	}()
	c.Include(a)
}

func TestRouter_Include_NonCyclicSharedSubRouterIsFine(t *testing.T) {
	// Including the same sub-router under two different parents is not a
	// cycle (no path leads back to either parent) and must not panic.
	shared := NewRouter()
	shared.Message().Handle(func(c *Ctx) error { return nil })

	a := NewRouter()
	b := NewRouter()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic for a non-cyclic shared sub-router: %v", r)
		}
	}()
	a.Include(shared)
	b.Include(shared)
}

func TestRouter_Update_CatchAll_MatchesEveryKind(t *testing.T) {
	r := NewRouter()
	var seenKinds []string
	r.Update().Handle(func(c *Ctx) error {
		switch {
		case c.Message != nil:
			seenKinds = append(seenKinds, "message")
		case c.CallbackQuery != nil:
			seenKinds = append(seenKinds, "callback_query")
		case c.Poll != nil:
			seenKinds = append(seenKinds, "poll")
		}
		return nil
	})

	updates := []*Update{
		{Message: &Message{Text: "hi"}},
		{CallbackQuery: &CallbackQuery{Data: "x"}},
		{Poll: &Poll{ID: "p1"}},
	}
	for _, u := range updates {
		if matched, err := r.dispatch(ctxFor(u)); !matched || err != nil {
			t.Errorf("dispatch(%+v) = matched=%v err=%v, want matched=true err=nil", u, matched, err)
		}
	}

	want := []string{"message", "callback_query", "poll"}
	if len(seenKinds) != len(want) {
		t.Fatalf("seenKinds = %v, want %v", seenKinds, want)
	}
	for i, k := range want {
		if seenKinds[i] != k {
			t.Errorf("seenKinds[%d] = %q, want %q", i, seenKinds[i], k)
		}
	}
}

func TestRouter_Update_CatchAll_DisablesAllowedUpdatesRestriction(t *testing.T) {
	r := NewRouter()
	r.Message().Handle(func(c *Ctx) error { return nil })
	r.Update().Handle(func(c *Ctx) error { return nil })

	if kinds := r.UsedUpdateKinds(); kinds != nil {
		t.Errorf("UsedUpdateKinds() = %v, want nil (no restriction) when a catch-all is registered", kinds)
	}
}

func TestRouter_Update_WithFilters(t *testing.T) {
	r := NewRouter()
	var ran bool
	onlyMessages := func(c *Ctx) bool { return c.Message != nil }
	r.Update(onlyMessages).Handle(func(c *Ctx) error {
		ran = true
		return nil
	})

	if matched, _ := r.dispatch(ctxFor(&Update{CallbackQuery: &CallbackQuery{Data: "x"}})); matched {
		t.Error("expected the filtered catch-all not to match a callback query")
	}
	if ran {
		t.Error("handler should not have run")
	}

	if matched, _ := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}})); !matched {
		t.Error("expected the filtered catch-all to match a message")
	}
	if !ran {
		t.Error("handler should have run")
	}
}

func TestRouter_OnError_InterceptsOwnHandlerError(t *testing.T) {
	r := NewRouter()
	wantErr := errors.New("boom")
	r.Message().Handle(func(c *Ctx) error { return wantErr })

	var gotErr error
	r.OnError(func(err error, c *Ctx) { gotErr = err })

	matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if !matched {
		t.Fatal("expected the handler to match")
	}
	if err != nil {
		t.Errorf("dispatch returned err=%v, want nil (OnError should have swallowed it)", err)
	}
	if gotErr != wantErr {
		t.Errorf("OnError received %v, want %v", gotErr, wantErr)
	}
}

func TestRouter_OnError_BubblesToParentWhenSubHasNone(t *testing.T) {
	parent := NewRouter()
	sub := NewRouter()
	wantErr := errors.New("boom")
	sub.Message().Handle(func(c *Ctx) error { return wantErr })
	parent.Include(sub)

	var gotErr error
	var gotAtParent bool
	parent.OnError(func(err error, c *Ctx) { gotErr = err; gotAtParent = true })

	matched, err := parent.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if !matched {
		t.Fatal("expected the handler to match")
	}
	if err != nil {
		t.Errorf("dispatch returned err=%v, want nil (parent's OnError should have swallowed it)", err)
	}
	if !gotAtParent || gotErr != wantErr {
		t.Errorf("expected parent's OnError to run with %v, got ran=%v err=%v", wantErr, gotAtParent, gotErr)
	}
}

func TestRouter_OnError_SubRouterOwnHandlerWinsOverParent(t *testing.T) {
	parent := NewRouter()
	sub := NewRouter()
	wantErr := errors.New("boom")
	sub.Message().Handle(func(c *Ctx) error { return wantErr })

	var subHandled, parentHandled bool
	sub.OnError(func(err error, c *Ctx) { subHandled = true })
	parent.OnError(func(err error, c *Ctx) { parentHandled = true })
	parent.Include(sub)

	matched, err := parent.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if !matched || err != nil {
		t.Fatalf("dispatch = matched=%v err=%v, want matched=true err=nil", matched, err)
	}
	if !subHandled {
		t.Error("expected the sub-router's own OnError to handle the error")
	}
	if parentHandled {
		t.Error("expected the parent's OnError NOT to run once the sub-router already handled it")
	}
}

func TestRouter_WithFlags_ReachableFromCtxWhileHandlerRuns(t *testing.T) {
	r := NewRouter()
	var gotFlags map[string]any
	r.Message().WithFlags(map[string]any{"chat_action": "typing"}).Handle(func(c *Ctx) error {
		gotFlags = c.Flags()
		return nil
	})

	if matched, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}})); !matched || err != nil {
		t.Fatalf("dispatch = matched=%v err=%v, want matched=true err=nil", matched, err)
	}
	if gotFlags["chat_action"] != "typing" {
		t.Errorf("c.Flags() = %v, want chat_action=typing", gotFlags)
	}
}

func TestRouter_WithFlags_MiddlewareCanReadFlagsBeforeHandlerRuns(t *testing.T) {
	r := NewRouter()
	var middlewareSawFlags map[string]any
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			middlewareSawFlags = c.Flags()
			return next(c)
		}
	})
	r.Message().WithFlags(map[string]any{"admin_only": true}).Handle(func(c *Ctx) error { return nil })

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if middlewareSawFlags["admin_only"] != true {
		t.Errorf("middleware saw flags = %v, want admin_only=true", middlewareSawFlags)
	}
}

func TestRouter_NoFlags_ReturnsNil(t *testing.T) {
	r := NewRouter()
	var gotFlags map[string]any
	var flagsWereNil bool
	r.Message().Handle(func(c *Ctx) error {
		gotFlags = c.Flags()
		flagsWereNil = gotFlags == nil
		return nil
	})

	if _, err := r.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagsWereNil {
		t.Errorf("expected Flags() to be nil for a registration without WithFlags, got %v", gotFlags)
	}
}

func TestRouter_OnError_Unset_BubblesAllTheWayUp(t *testing.T) {
	// No OnError anywhere — dispatch must still return the raw error, same
	// as before per-router OnError existed, so bot.OnError sees it.
	parent := NewRouter()
	sub := NewRouter()
	wantErr := errors.New("boom")
	sub.Message().Handle(func(c *Ctx) error { return wantErr })
	parent.Include(sub)

	matched, err := parent.dispatch(ctxFor(&Update{Message: &Message{Text: "hi"}}))
	if !matched {
		t.Fatal("expected the handler to match")
	}
	if err != wantErr {
		t.Errorf("dispatch returned err=%v, want the raw %v to bubble all the way up", err, wantErr)
	}
}
