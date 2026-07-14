package golagram

import "errors"

// HandlerFunc is what every handler looks like, regardless of update kind.
// [Ctx] exposes whichever payload this update carries (c.Message,
// c.CallbackQuery, c.InlineQuery, ...) plus sugar ([Ctx.Answer],
// [Ctx.FSM], ...).
type HandlerFunc func(c *Ctx) error

// MiddlewareFunc wraps a [HandlerFunc] — the single middleware shape for
// every update kind, since handlers themselves are now unified on [Ctx].
// Registered via [Router.Use], this is "inner" middleware: it only runs
// once a specific registration's filters have already matched, wrapping
// that one handler — see [OuterMiddlewareFunc] for the outer/inner
// middleware distinction.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// OuterMiddlewareFunc wraps a router's entire dispatch attempt — every
// update that reaches this router, whether or not any of its registrations
// end up matching — unlike [MiddlewareFunc], which only wraps the handler
// once a specific registration has already matched. This is the
// "outer" (pre-filter) vs "inner" (post-filter) distinction: register outer
// middleware via [Router.UseOuter] for anything that needs to run before
// filters can evaluate — most commonly loading data (a user record, a
// permission set) into Ctx via [Ctx.Set] so a later [Filter] or inner
// middleware can read it without a duplicate lookup — or for logging/metrics
// that should count every update this router saw, not just the ones it
// matched.
//
// next's own (matched, err) return already reflects this router's own
// OnError handling for the matched-handler case, so outer middleware sees
// the settled outcome, not a raw handler error — the same as if it wrapped
// dispatch from the outside. An outer middleware that decides not to call
// next at all can return (true, err) to signal "I handled this myself" or
// (false, nil) to decline, letting a sibling route (or an ancestor router,
// if this one was reached via Include) keep trying.
type OuterMiddlewareFunc func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error)

// Filter is the single filter shape for every update kind: a predicate over
// the full [Ctx], so filters can read the payload (c.Message,
// c.CallbackQuery, ...), the resolved chat/user ([Ctx.Chat], [Ctx.From]),
// and — crucially — FSM state ([StateIs] works on callback queries, not
// just messages). A filter is only invoked after the registration's own
// kind check passed, so a filter passed to [Router.Message] can rely on
// c.Message being non-nil.
//
// Naming rule: payload predicates carry the Filter prefix ([FilterPhoto],
// [FilterCommand], ...) — the bare nouns belong to the generated Bot API
// types and always will. Combinators ([And], [Or], [Not]) and FSM
// predicates ([StateIs]) are not payload checks and stay unprefixed.
//
// Keep filters cheap. A filter runs inside [TelegramBot]'s per-{chat,user}
// dispatch lock (the same one that keeps two updates from the same user
// from racing each other's FSM state), so a slow filter — one doing I/O,
// most commonly — stalls every other queued update for that user until it
// returns. [AdminCache.FilterIsAdmin] does exactly this on a cache miss;
// its own doc says so.
type Filter func(c *Ctx) bool

// ErrSkipHandler lets a handler decline after its filters already matched,
// so dispatch falls through to the next matching handler for this update
// instead of stopping. Return it from a handler that discovers mid-flight
// it isn't the right one after all.
var ErrSkipHandler = errors.New("golagram: skip to next handler")

// Router collects handler registrations, one per update kind, tried in
// registration order (first match wins). Compose routers with
// [Router.Include]: a router included partway through registration is
// tried at that point in the overall order, with its own middleware
// applied only to its own handlers.
//
// A kind's "default" handler is just a registration with no filters,
// placed last for that kind — since registration order is priority, it
// only runs when nothing more specific matched.
type Router struct {
	middlewares      []MiddlewareFunc
	outerMiddlewares []OuterMiddlewareFunc
	routes           []route
	errorHandler     ErrorHandlerFunc
}

type route struct {
	kind      string // Telegram's update field name, e.g. "message"; "" when sub != nil
	condition func(*Ctx) bool
	handler   HandlerFunc
	flags     map[string]any
	sub       *Router

	// allowWebhookReply is set by registration.AllowWebhookReply — see its
	// doc and webhook_reply.go.
	allowWebhookReply bool
}

// NewRouter creates an empty router.
func NewRouter() *Router {
	return &Router{}
}

// Use registers middleware that wraps every handler matched by this router
// (not included sub-routers' own middleware, which is theirs alone).
func (r *Router) Use(mw MiddlewareFunc) {
	r.middlewares = append(r.middlewares, mw)
}

// UseOuter registers outer middleware — see OuterMiddlewareFunc — that wraps
// this router's entire dispatch attempt, including updates none of its own
// registrations end up matching. Scoped the same way Use is: an included
// sub-router's own UseOuter middleware is its own, not inherited from (or
// shared with) whichever router included it — register on every router in
// the tree that needs it, same guidance as Use's doc.
func (r *Router) UseOuter(mw OuterMiddlewareFunc) {
	r.outerMiddlewares = append(r.outerMiddlewares, mw)
}

// OnError sets this router's error handler, intercepting any error a
// handler matched within its own subtree returns (including from included
// sub-routers) before it bubbles further out. If a sub-router that matched
// has its own OnError, that one runs instead — this router only sees the
// error if the sub-router that actually handled the update didn't already
// handle it. An error that reaches the root router with no OnError set
// anywhere in the chain falls through to [TelegramBot.OnError], exactly as
// before per-router error handlers existed.
func (r *Router) OnError(h ErrorHandlerFunc) {
	r.errorHandler = h
}

// handleOrBubble applies r's OnError to err, if both are set, treating it
// as handled (returns nil so it stops here). Otherwise returns err
// unchanged, to bubble up to the parent router (or, at the root, to
// [TelegramBot.OnError]).
func (r *Router) handleOrBubble(err error, ctx *Ctx) error {
	if err == nil || r.errorHandler == nil {
		return err
	}
	r.errorHandler(err, ctx)
	return nil
}

// Include appends a sub-router, tried at this point in the parent's
// registration order. Panics if including sub would create a cycle (sub
// is r itself, or already — directly or transitively — includes r):
// dispatch recurses through included routers, so an actual cycle would
// infinite-loop the first update that reaches it, a far worse failure
// mode than a panic at wiring time, before [TelegramBot.Run] /
// [TelegramBot.RunWebhook] ever starts.
//
// Middleware does NOT flow from r into sub through Include — see
// [Router.Use]'s doc. This differs from the "outer wraps inner" middleware
// semantics some router libraries default to; it's a deliberate choice
// here, not an oversight. If r.Use(mw) must apply to sub's handlers too,
// call sub.Use(mw) directly (or register mw on every router in the tree
// that needs it) rather than relying on Include to propagate it.
func (r *Router) Include(sub *Router) {
	if sub.reaches(r) {
		panic("golagram: Router.Include would create a cycle")
	}
	r.routes = append(r.routes, route{sub: sub})
}

// reaches reports whether target is r itself, or reachable by following
// included sub-routers from r.
func (r *Router) reaches(target *Router) bool {
	if r == target {
		return true
	}
	for _, rt := range r.routes {
		if rt.sub != nil && rt.sub.reaches(target) {
			return true
		}
	}
	return false
}

// updateCatchAllKind is the sentinel route.kind for Router.Update(...)
// registrations, which match every kind. Its presence anywhere in the tree
// means UsedUpdateKinds() imposes no restriction at all — a catch-all
// observer wants to see everything Telegram can send, same as having no
// router set.
const updateCatchAllKind = "*"

// UsedUpdateKinds returns the Telegram update field names (e.g. "message",
// "chat_member") that have at least one registered handler anywhere in this
// router or its included sub-routers, deduplicated. Used to compute
// getUpdates'/setWebhook's allowed_updates automatically — see
// [WithAllowedUpdates] and [TelegramBot.Run]. Returns nil (no restriction)
// if any [Router.Update] catch-all is registered anywhere in the tree.
func (r *Router) UsedUpdateKinds() []string {
	seen := map[string]bool{}
	r.collectUsedUpdateKinds(seen)
	if seen[updateCatchAllKind] {
		return nil
	}
	kinds := make([]string, 0, len(seen))
	for k := range seen {
		kinds = append(kinds, k)
	}
	return kinds
}

func (r *Router) collectUsedUpdateKinds(seen map[string]bool) {
	for _, rt := range r.routes {
		if rt.sub != nil {
			rt.sub.collectUsedUpdateKinds(seen)
			continue
		}
		seen[rt.kind] = true
	}
}

// dispatch is dispatchRoutes wrapped in this router's outer middleware
// (see [OuterMiddlewareFunc] / [Router.UseOuter]) — the entry point every
// caller (bot.go, Include recursion) actually uses. The extra
// r.handleOrBubble here is a no-op for the already-settled outcomes
// dispatchRoutes returns (its own handleOrBubble calls already turned a
// handled error into nil, and handleOrBubble is a no-op on nil) — it only
// does new work for an error an outer middleware raises directly without
// calling next, giving this router's OnError a first chance at it exactly
// like a handler error gets.
func (r *Router) dispatch(ctx *Ctx) (matched bool, err error) {
	next := r.dispatchRoutes
	for i := len(r.outerMiddlewares) - 1; i >= 0; i-- {
		next = r.outerMiddlewares[i](next)
	}
	matched, err = next(ctx)
	return matched, r.handleOrBubble(err, ctx)
}

// dispatchRoutes tries this router's routes (and included sub-routers) in
// registration order against ctx, running the first handler whose
// filters match. matched reports whether any handler ran (even if it then
// returned an error) — the caller uses it to decide between "handled" and
// "nothing matched". An error is passed through r's own OnError (if set)
// before being returned, so it either stops here or keeps bubbling up
// unchanged to whichever ancestor router (or bot.OnError) handles it.
func (r *Router) dispatchRoutes(ctx *Ctx) (matched bool, err error) {
	for _, rt := range r.routes {
		if rt.sub != nil {
			if m, subErr := rt.sub.dispatch(ctx); m {
				return true, r.handleOrBubble(subErr, ctx)
			}
			continue
		}
		if !rt.condition(ctx) {
			continue
		}

		ctx.routeFlags = rt.flags
		h := rt.handler
		for i := len(r.middlewares) - 1; i >= 0; i-- {
			h = r.middlewares[i](h)
		}
		hErr := h(ctx)
		if errors.Is(hErr, ErrSkipHandler) {
			continue
		}
		return true, r.handleOrBubble(hErr, ctx)
	}
	return false, nil
}

// firstMatchingRoute returns the leaf route that dispatch would run for
// ctx — same registration order, same Include recursion — without running
// any middleware or handler. Used by matchingAllowsWebhookReply so
// [TelegramBot.Handler] (RunWebhook's http.Handler) can decide whether to
// dispatch an update synchronously before either middleware or the handler
// runs.
func (r *Router) firstMatchingRoute(ctx *Ctx) *route {
	for i := range r.routes {
		rt := &r.routes[i]
		if rt.sub != nil {
			if m := rt.sub.firstMatchingRoute(ctx); m != nil {
				return m
			}
			continue
		}
		if rt.condition(ctx) {
			return rt
		}
	}
	return nil
}

// matchingAllowsWebhookReply reports whether the registration that would
// handle ctx opted into AllowWebhookReply. Computed before any handler
// runs — see the ErrSkipHandler interaction documented on
// [registration.AllowWebhookReply] for what that can't account for.
func (r *Router) matchingAllowsWebhookReply(ctx *Ctx) bool {
	rt := r.firstMatchingRoute(ctx)
	return rt != nil && rt.allowWebhookReply
}

// registration is the intermediate value returned by a per-kind method
// (r.Message(...), r.CallbackQuery(...), ...); call Handle to finish
// registering it.
type registration struct {
	r                 *Router
	kind              string
	match             func(*Ctx) bool
	flags             map[string]any
	allowWebhookReply bool
}

// WithFlags attaches metadata to this registration, readable via
// [Ctx.Flags] while (and only while) this specific handler is running —
// for middleware that behaves differently per handler rather than per
// router. Example: a logging/chat-action middleware registered once via
// [Router.Use] can check c.Flags()["chat_action"] and act on it, instead
// of needing a separate middleware stack per handler that wants the
// behavior.
//
//	r.Message(gg.FilterCommand("photo")).WithFlags(map[string]any{"chat_action": "upload_photo"}).Handle(sendPhotoHandler)
func (reg *registration) WithFlags(flags map[string]any) *registration {
	reg.flags = flags
	return reg
}

// AllowWebhookReply opts this registration into RunWebhook's
// reply-in-response optimization: when it matches an update,
// [TelegramBot.Handler] dispatches that update synchronously in the HTTP
// request goroutine instead of handing it to the async worker pool, so a
// handler that returns Reply(req) gets req embedded directly in the
// webhook HTTP response instead of golagram making a separate HTTPS call
// for it.
//
// Trade-off: unlike every other registration (hand off, respond 200
// immediately, run async — see [TelegramBot.Handler]'s doc), the webhook
// HTTP response for this one stays open for as long as the handler takes
// to run. Opt in only for handlers you've confirmed are fast; everything
// else keeps the default async behavior untouched.
//
// Sync-vs-async is decided once, from [Router.firstMatchingRoute], before
// any middleware or handler runs — [TelegramBot.Handler] has to commit to
// holding the HTTP response open (or not) before it can dispatch anything.
// A handler earlier in registration order that returns [ErrSkipHandler] can
// still shift actual handling to a later registration for the same update;
// if that later one has a *different* AllowWebhookReply setting than the
// one this decision was made from, the update runs under the wrong choice
// — a Reply(...) meant to embed goes out as a separate API call instead (or
// vice versa). Both outcomes still work, just not the intended optimization.
// Avoid the mismatch by giving every registration that can be reached via
// an ErrSkipHandler fallthrough from one another (same router, same update
// kind, overlapping filters) the same AllowWebhookReply setting.
func (reg *registration) AllowWebhookReply() *registration {
	reg.allowWebhookReply = true
	return reg
}

// Handle finishes a registration, attaching the handler that runs when
// match returned true.
func (reg *registration) Handle(h HandlerFunc) {
	reg.r.routes = append(reg.r.routes, route{
		kind:              reg.kind,
		condition:         reg.match,
		handler:           h,
		flags:             reg.flags,
		allowWebhookReply: reg.allowWebhookReply,
	})
}

// register builds the registration for one update kind: present gates on
// the kind's payload being set, then the filters run in order (AND).
func (r *Router) register(kind string, present func(*Ctx) bool, filters []Filter) *registration {
	return &registration{r: r, kind: kind, match: func(c *Ctx) bool {
		if !present(c) {
			return false
		}
		for _, f := range filters {
			if !f(c) {
				return false
			}
		}
		return true
	}}
}

// Message registers a handler for the message update kind. Filters combine
// with AND; zero filters matches every message.
func (r *Router) Message(filters ...Filter) *registration {
	return r.register("message", func(c *Ctx) bool { return c.Message != nil }, filters)
}

// EditedMessage registers a handler for the edited_message update kind.
func (r *Router) EditedMessage(filters ...Filter) *registration {
	return r.register("edited_message", func(c *Ctx) bool { return c.EditedMessage != nil }, filters)
}

// ChannelPost registers a handler for the channel_post update kind.
func (r *Router) ChannelPost(filters ...Filter) *registration {
	return r.register("channel_post", func(c *Ctx) bool { return c.ChannelPost != nil }, filters)
}

// EditedChannelPost registers a handler for the edited_channel_post update kind.
func (r *Router) EditedChannelPost(filters ...Filter) *registration {
	return r.register("edited_channel_post", func(c *Ctx) bool { return c.EditedChannelPost != nil }, filters)
}

// BusinessMessage registers a handler for the business_message update kind.
func (r *Router) BusinessMessage(filters ...Filter) *registration {
	return r.register("business_message", func(c *Ctx) bool { return c.BusinessMessage != nil }, filters)
}

// EditedBusinessMessage registers a handler for the edited_business_message update kind.
func (r *Router) EditedBusinessMessage(filters ...Filter) *registration {
	return r.register("edited_business_message", func(c *Ctx) bool { return c.EditedBusinessMessage != nil }, filters)
}

// GuestMessage registers a handler for the guest_message update kind.
func (r *Router) GuestMessage(filters ...Filter) *registration {
	return r.register("guest_message", func(c *Ctx) bool { return c.GuestMessage != nil }, filters)
}

// CallbackQuery registers a handler for the callback_query update kind.
func (r *Router) CallbackQuery(filters ...Filter) *registration {
	return r.register("callback_query", func(c *Ctx) bool { return c.CallbackQuery != nil }, filters)
}

// InlineQuery registers a handler for the inline_query update kind.
func (r *Router) InlineQuery(filters ...Filter) *registration {
	return r.register("inline_query", func(c *Ctx) bool { return c.InlineQuery != nil }, filters)
}

// ChosenInlineResult registers a handler for the chosen_inline_result update kind.
func (r *Router) ChosenInlineResult(filters ...Filter) *registration {
	return r.register("chosen_inline_result", func(c *Ctx) bool { return c.ChosenInlineResult != nil }, filters)
}

// ShippingQuery registers a handler for the shipping_query update kind.
func (r *Router) ShippingQuery(filters ...Filter) *registration {
	return r.register("shipping_query", func(c *Ctx) bool { return c.ShippingQuery != nil }, filters)
}

// PreCheckoutQuery registers a handler for the pre_checkout_query update kind.
func (r *Router) PreCheckoutQuery(filters ...Filter) *registration {
	return r.register("pre_checkout_query", func(c *Ctx) bool { return c.PreCheckoutQuery != nil }, filters)
}

// PurchasedPaidMedia registers a handler for the purchased_paid_media update kind.
func (r *Router) PurchasedPaidMedia(filters ...Filter) *registration {
	return r.register("purchased_paid_media", func(c *Ctx) bool { return c.PurchasedPaidMedia != nil }, filters)
}

// Poll registers a handler for the poll update kind (a poll's public status
// changed — this is not poll_answer, which is one voter's answer).
func (r *Router) Poll(filters ...Filter) *registration {
	return r.register("poll", func(c *Ctx) bool { return c.Poll != nil }, filters)
}

// PollAnswer registers a handler for the poll_answer update kind.
func (r *Router) PollAnswer(filters ...Filter) *registration {
	return r.register("poll_answer", func(c *Ctx) bool { return c.PollAnswer != nil }, filters)
}

// MyChatMember registers a handler for the my_chat_member update kind (the
// bot's own membership status changed in a chat).
func (r *Router) MyChatMember(filters ...Filter) *registration {
	return r.register("my_chat_member", func(c *Ctx) bool { return c.MyChatMember != nil }, filters)
}

// ChatMember registers a handler for the chat_member update kind (some
// other member's status changed — requires the bot to be an admin).
func (r *Router) ChatMember(filters ...Filter) *registration {
	return r.register("chat_member", func(c *Ctx) bool { return c.ChatMember != nil }, filters)
}

// ChatJoinRequest registers a handler for the chat_join_request update kind.
func (r *Router) ChatJoinRequest(filters ...Filter) *registration {
	return r.register("chat_join_request", func(c *Ctx) bool { return c.ChatJoinRequest != nil }, filters)
}

// ChatBoost registers a handler for the chat_boost update kind.
func (r *Router) ChatBoost(filters ...Filter) *registration {
	return r.register("chat_boost", func(c *Ctx) bool { return c.ChatBoost != nil }, filters)
}

// RemovedChatBoost registers a handler for the removed_chat_boost update kind.
func (r *Router) RemovedChatBoost(filters ...Filter) *registration {
	return r.register("removed_chat_boost", func(c *Ctx) bool { return c.RemovedChatBoost != nil }, filters)
}

// BusinessConnection registers a handler for the business_connection update kind.
func (r *Router) BusinessConnection(filters ...Filter) *registration {
	return r.register("business_connection", func(c *Ctx) bool { return c.BusinessConnection != nil }, filters)
}

// DeletedBusinessMessages registers a handler for the deleted_business_messages update kind.
func (r *Router) DeletedBusinessMessages(filters ...Filter) *registration {
	return r.register("deleted_business_messages", func(c *Ctx) bool { return c.DeletedBusinessMessages != nil }, filters)
}

// MessageReaction registers a handler for the message_reaction update kind.
func (r *Router) MessageReaction(filters ...Filter) *registration {
	return r.register("message_reaction", func(c *Ctx) bool { return c.MessageReaction != nil }, filters)
}

// MessageReactionCount registers a handler for the message_reaction_count update kind.
func (r *Router) MessageReactionCount(filters ...Filter) *registration {
	return r.register("message_reaction_count", func(c *Ctx) bool { return c.MessageReactionCount != nil }, filters)
}

// ManagedBot registers a handler for the managed_bot update kind.
func (r *Router) ManagedBot(filters ...Filter) *registration {
	return r.register("managed_bot", func(c *Ctx) bool { return c.ManagedBot != nil }, filters)
}

// Update registers a handler matching every update kind unconditionally
// (subject only to any filters passed) — a catch-all observer for
// cross-cutting concerns that want to see every update regardless of kind
// (logging, metrics, ...), not one of the per-kind handlers above. Since
// it's a catch-all, place it last in registration order unless you
// specifically want it to preempt more specific handlers for the same
// update. Its presence anywhere in the router tree disables
// [Router.UsedUpdateKinds]'s allowed_updates restriction entirely (see its
// doc).
func (r *Router) Update(filters ...Filter) *registration {
	return r.register(updateCatchAllKind, func(c *Ctx) bool { return true }, filters)
}
