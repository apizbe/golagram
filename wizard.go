package golagram

import "fmt"

// WizardStepFunc is one step's handler in a [Wizard].
type WizardStepFunc func(wc *WizardCtx) error

// WizardCtx is the [*Ctx] handed to a Wizard step, plus step-advance
// sugar. Embeds *Ctx, so all of Ctx's own sugar (Answer, Reply, FSM,
// Text, ...) is available unchanged.
type WizardCtx struct {
	*Ctx
	wizard    *Wizard
	stepIndex int
}

// Next advances to the step after the current one. If the current step is
// the last one, Next behaves like [Wizard.Exit] instead.
func (wc *WizardCtx) Next() error {
	next := wc.stepIndex + 1
	if next >= len(wc.wizard.states) {
		return wc.wizard.Exit(wc.Ctx)
	}
	return wc.Ctx.FSM().SetState(wc.wizard.states[next])
}

// Back returns to the step before the current one. A no-op on the first
// step — there's nowhere to go back to.
func (wc *WizardCtx) Back() error {
	if wc.stepIndex == 0 {
		return nil
	}
	return wc.Ctx.FSM().SetState(wc.wizard.states[wc.stepIndex-1])
}

// GoTo jumps directly to step i (0-based, in [Wizard.Step] registration
// order) — an escape hatch for non-linear flows, e.g. an "edit an earlier
// answer" menu. Panics if i is out of range: that's a caller-side bug to
// fix, not a runtime condition to handle gracefully.
func (wc *WizardCtx) GoTo(i int) error {
	if i < 0 || i >= len(wc.wizard.states) {
		panic(fmt.Sprintf("golagram: Wizard %q: GoTo(%d) out of range [0,%d)", wc.wizard.name, i, len(wc.wizard.states)))
	}
	return wc.Ctx.FSM().SetState(wc.wizard.states[i])
}

// StepIndex reports which step (0-based) is currently active.
func (wc *WizardCtx) StepIndex() int { return wc.stepIndex }

// Exit leaves the wizard normally — see [Wizard.Exit].
func (wc *WizardCtx) Exit() error { return wc.wizard.Exit(wc.Ctx) }

// Cancel leaves the wizard as a cancellation — see [Wizard.Cancel].
func (wc *WizardCtx) Cancel() error { return wc.wizard.Cancel(wc.Ctx) }

type stepConfig struct {
	name          string
	ignore        []Filter
	allowCommands bool
}

// StepOption configures a [Wizard.Step] registration.
type StepOption func(*stepConfig)

// WithStepName overrides the step's underlying [State] name (default
// "step<N>") — useful for readable state values when inspecting storage
// directly or in logs.
func WithStepName(name string) StepOption {
	return func(c *stepConfig) { c.name = name }
}

// WithStepIgnore excludes updates matching any of filters from this step —
// e.g. a persistent reply-keyboard button that should fall through to its
// own handler instead of being swallowed as this step's answer.
func WithStepIgnore(filters ...Filter) StepOption {
	return func(c *stepConfig) { c.ignore = append(c.ignore, filters...) }
}

// WithStepAllowCommands lets this step's handler see text starting with
// "/". By default, any command falls through past the wizard instead of
// being misread as step input — so a stray command mid-wizard reaches its
// own handler (or nothing) instead of being swallowed.
func WithStepAllowCommands() StepOption {
	return func(c *stepConfig) { c.allowCommands = true }
}

// Wizard is a linear, stateful conversation builder on top of the
// existing FSM primitives (see [StateGroup], [StateIs], [FSMContext]):
// each [Wizard.Step] becomes its own [State] under one [StateGroup],
// auto-advanced by [WizardCtx.Next]/[WizardCtx.Back]/[WizardCtx.GoTo],
// with a configurable cancel command and enter/exit/cancel hooks.
//
//	w := gg.NewWizard("register")
//	w.Step(func(wc *gg.WizardCtx) error {
//		gg.FSMSet(wc.FSM(), "name", wc.Text())
//		if _, err := wc.Answer("And your age?"); err != nil {
//			return err
//		}
//		return wc.Next()
//	})
//	// ... more steps ...
//	r.Include(w.Router())
//
//	r.Message(gg.FilterCommand("register")).Handle(func(c *gg.Ctx) error {
//		if err := w.Enter(c); err != nil {
//			return err
//		}
//		_, err := c.Answer("What's your name?")
//		return err
//	})
type Wizard struct {
	name   string
	group  StateGroup
	router *Router
	states []State

	cancelCmd string
	onEnter   func(c *Ctx) error
	onExit    func(c *Ctx) error
	onCancel  func(c *Ctx) error
}

// WizardOption configures a [Wizard] built by [NewWizard].
type WizardOption func(*Wizard)

// WithCancelCommand sets the command (without "/") that cancels the
// wizard from any step (default "cancel"). Pass "" to disable the
// built-in cancel route entirely.
func WithCancelCommand(command string) WizardOption {
	return func(w *Wizard) { w.cancelCmd = command }
}

// WithOnEnter registers a hook run by [Wizard.Enter] right after the
// wizard's state is set to its first step.
func WithOnEnter(fn func(c *Ctx) error) WizardOption {
	return func(w *Wizard) { w.onEnter = fn }
}

// WithOnExit registers a hook run on [Wizard.Exit] — a step completing
// normally via [WizardCtx.Next] past the last step, or an explicit
// [WizardCtx.Exit]/[Wizard.Exit] call. Also runs on cancellation if
// [WithOnCancel] isn't set.
func WithOnExit(fn func(c *Ctx) error) WizardOption {
	return func(w *Wizard) { w.onExit = fn }
}

// WithOnCancel registers a hook run specifically on cancellation (the
// cancel command, or an explicit [WizardCtx.Cancel]/[Wizard.Cancel] call)
// — falls back to [WithOnExit]'s hook if unset.
func WithOnCancel(fn func(c *Ctx) error) WizardOption {
	return func(w *Wizard) { w.onCancel = fn }
}

// NewWizard creates a Wizard named name — name seeds the underlying
// [StateGroup], so it must be unique among wizards sharing one
// [FSMStorage]. Registers the cancel command (default "cancel", see
// [WithCancelCommand]) on the wizard's own router immediately, before any
// [Wizard.Step] — guaranteeing it's tried before any step within the
// wizard's own router, regardless of how many steps are added later.
func NewWizard(name string, opts ...WizardOption) *Wizard {
	w := &Wizard{
		name:      name,
		group:     StateGroup(name),
		router:    NewRouter(),
		cancelCmd: "cancel",
	}
	for _, opt := range opts {
		opt(w)
	}
	if w.cancelCmd != "" {
		w.router.Message(StateIn(w.group), FilterCommand(w.cancelCmd)).Handle(func(c *Ctx) error {
			return w.Cancel(c)
		})
	}
	return w
}

// Use registers scoped middleware run before every step's handler
// (including the built-in cancel command) — forwards to the wizard's
// internal router; see [Router.Use].
func (w *Wizard) Use(mw MiddlewareFunc) {
	w.router.Use(mw)
}

// Step appends a new step, gated on its own [State]: while that state is
// active, a text message runs handler wrapped in a [WizardCtx]. Returns
// the step's 0-based index, for [WizardCtx.GoTo] or [Wizard.State].
//
// By default a step doesn't match a command (see [WithStepAllowCommands])
// or anything matched by a [WithStepIgnore] filter — both fall through to
// whatever's registered after this wizard instead of being swallowed as
// step input.
func (w *Wizard) Step(handler WizardStepFunc, opts ...StepOption) int {
	cfg := &stepConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	name := cfg.name
	if name == "" {
		name = fmt.Sprintf("step%d", len(w.states))
	}
	state := w.group.New(name)
	index := len(w.states)
	w.states = append(w.states, state)

	filters := []Filter{StateIs(state)}
	if !cfg.allowCommands {
		filters = append(filters, func(c *Ctx) bool { return c.Command() == nil })
	}
	for _, ignore := range cfg.ignore {
		filters = append(filters, Not(ignore))
	}

	w.router.Message(filters...).Handle(func(c *Ctx) error {
		return handler(&WizardCtx{Ctx: c, wizard: w, stepIndex: index})
	})
	return index
}

// State returns step i's underlying [State] — for registering additional,
// non-Message-kind handlers (e.g. a CallbackQuery step) gated on the same
// step via [StateIs], since [Wizard.Step] only wires Message updates.
// Panics if i is out of range.
func (w *Wizard) State(i int) State {
	if i < 0 || i >= len(w.states) {
		panic(fmt.Sprintf("golagram: Wizard %q: State(%d) out of range [0,%d)", w.name, i, len(w.states)))
	}
	return w.states[i]
}

// Router returns the wizard's steps (and cancel command) as a [*Router],
// ready to [Router.Include] into a parent router. Include it before any
// handler that could otherwise shadow a step or the cancel command —
// registration order is priority, same as any other router.
func (w *Wizard) Router() *Router {
	return w.router
}

// Enter starts the wizard for c's conversation: clears any prior FSM
// state and data under this wizard's group (so a re-entered wizard never
// inherits stale data from an earlier abandoned attempt), sets state to
// step 0, and runs the [WithOnEnter] hook if set. Call this from whatever
// triggers the wizard (e.g. a command handler) — it works from any
// handler, not just a step's.
func (w *Wizard) Enter(c *Ctx) error {
	if len(w.states) == 0 {
		return fmt.Errorf("golagram: Wizard %q: Enter called with no steps registered", w.name)
	}
	if err := c.FSM().Clear(); err != nil {
		return err
	}
	if err := c.FSM().SetState(w.states[0]); err != nil {
		return err
	}
	if w.onEnter != nil {
		return w.onEnter(c)
	}
	return nil
}

// Exit leaves the wizard normally: clears FSM state and data, then runs
// the [WithOnExit] hook if set. Works from any handler, not just a step's.
func (w *Wizard) Exit(c *Ctx) error {
	if err := c.FSM().Clear(); err != nil {
		return err
	}
	if w.onExit != nil {
		return w.onExit(c)
	}
	return nil
}

// Cancel leaves the wizard as a cancellation: clears FSM state and data,
// then runs the [WithOnCancel] hook (falling back to [WithOnExit]'s hook
// if unset). Works from any handler, not just a step's.
func (w *Wizard) Cancel(c *Ctx) error {
	if err := c.FSM().Clear(); err != nil {
		return err
	}
	hook := w.onCancel
	if hook == nil {
		hook = w.onExit
	}
	if hook != nil {
		return hook(c)
	}
	return nil
}
