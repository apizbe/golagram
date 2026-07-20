package golagram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apizbe/golagram/internal/api"
)

const (
	defaultWorkers         = 5
	defaultUpdateBuffer    = 100
	defaultPollTimeoutSecs = 30

	// pollBackoffMin/Max bound the delay between getUpdates retries after a
	// failure — doubling from Min, capped at Max, reset to Min on the next
	// successful call. Replaces the old flat 1s sleep, which hammered
	// Telegram (or whatever's actually down) at a fixed rate regardless of
	// how long the failure has been going on.
	pollBackoffMin = 1 * time.Second
	pollBackoffMax = 30 * time.Second
)

// TelegramBot is a running bot: one Bot API client, one [Router], one FSM
// storage backend, and the worker pool that both [TelegramBot.Run] (polling)
// and [TelegramBot.RunWebhook] dispatch through. Construct one with
// [NewTelegramBot], wire a router with [TelegramBot.Dispatch], then call Run
// or RunWebhook.
//
// A TelegramBot runs once: Run/RunWebhook/StartWorkers close updateChan on
// shutdown ([TelegramBot.StopWorkers]) and never reopen it, so a second call
// after the first has returned fails fast instead of panicking on a
// send-to-closed-channel later. Construct a new TelegramBot to run again.
type TelegramBot struct {
	api            *api.Client
	router         *Router
	errorHandler   ErrorHandlerFunc
	healthMonitor  *HealthMonitor
	fsmStorage     FSMStorage
	fsmStrategy    FSMKeyStrategy
	senderIdentity bool
	dispatchLocks  *keyedMutex
	updateChan     chan *Update
	wg             sync.WaitGroup
	stopOnce       sync.Once
	ran            atomic.Bool
	me             *User

	// runContext is the context Run/RunWebhook (or StartWorkers) was given —
	// bound into every hydrated payload so sugar calls (m.Answer, cq.Answer,
	// ...) are canceled when the bot shuts down.
	runContext context.Context

	// configuration, set via Option before the bot starts
	numWorkers         int
	updateBuffer       int
	pollTimeoutSecs    int
	baseURL            string
	httpClient         *http.Client
	allowedUpdates     []string
	autoRetryMaxWait   time.Duration
	dropPendingUpdates bool
	logger             *slog.Logger
	skipGetMe          bool

	startupHooks  []LifecycleFunc
	shutdownHooks []LifecycleFunc
}

// LifecycleFunc is a startup or shutdown hook registered via
// [TelegramBot.OnStartup] or [TelegramBot.OnShutdown].
type LifecycleFunc func(ctx context.Context) error

// Option configures a TelegramBot at construction time.
type Option func(*TelegramBot)

// WithWorkers sets the number of concurrent update-processing workers.
func WithWorkers(n int) Option {
	return func(b *TelegramBot) {
		if n > 0 {
			b.numWorkers = n
		}
	}
}

// WithUpdateBuffer sets the size of the internal update queue that absorbs
// traffic spikes between polling and the workers.
func WithUpdateBuffer(n int) Option {
	return func(b *TelegramBot) {
		if n > 0 {
			b.updateBuffer = n
		}
	}
}

// WithPollTimeout sets the getUpdates long-poll timeout in seconds.
func WithPollTimeout(seconds int) Option {
	return func(b *TelegramBot) {
		if seconds > 0 {
			b.pollTimeoutSecs = seconds
		}
	}
}

// WithHTTPClient replaces the HTTP client used for all Telegram API calls
// (proxies, custom transports, tighter timeouts).
func WithHTTPClient(hc *http.Client) Option {
	return func(b *TelegramBot) { b.httpClient = hc }
}

// WithBaseURL points the bot at a custom Bot API server, e.g. a self-hosted
// telegram-bot-api instance or a fake server in tests.
func WithBaseURL(url string) Option {
	return func(b *TelegramBot) { b.baseURL = url }
}

// WithFSMStorage sets the conversation-state backend (default: in-memory).
func WithFSMStorage(storage FSMStorage) Option {
	return func(b *TelegramBot) {
		if storage != nil {
			b.fsmStorage = storage
		}
	}
}

// WithFSMKeyStrategy sets how conversation state is scoped (default:
// [FSMKeyChatUser] — independent state per user per chat). See
// [FSMKeyStrategy] for the alternatives. The strategy also scopes the
// per-key dispatch lock, so updates sharing FSM state are processed
// serially and never race on it.
func WithFSMKeyStrategy(s FSMKeyStrategy) Option {
	return func(b *TelegramBot) { b.fsmStrategy = s }
}

// WithSenderIdentity makes FSM state keying resolve identity through
// [Ctx.Sender] instead of the update's raw c.From(). Without this,
// [FSMKeyStrategy] folds in c.From().ID() — which for an anonymous group
// admin or a channel post is Telegram's fixed dummy user (the same ID for
// every anonymous admin in every chat this bot runs in), so under
// [FSMKeyChatUser] two different anonymous admins in the same group
// collide onto one conversation, and under [FSMKeyGlobalUser] *every*
// anonymous admin across every chat collides onto one. WithSenderIdentity
// resolves that component from Sender().ID() instead — a chat's ID for its
// anonymous admins, a channel's ID for its posts — the only identity
// Telegram gives either, so state is at least scoped correctly per
// chat/channel instead of colliding across unrelated senders.
//
// This only affects FSM keying. [FilterFromSender] and
// [RateLimitMiddlewareBySender] are separate, explicit opt-ins for the
// same Sender()-vs-From() distinction elsewhere.
func WithSenderIdentity() Option {
	return func(b *TelegramBot) { b.senderIdentity = true }
}

// WithAllowedUpdates overrides which update kinds Telegram delivers
// (getUpdates'/setWebhook's allowed_updates). Several kinds — including
// message_reaction, message_reaction_count, and chat_member — are opt-in
// and simply won't arrive without this: a handler registered for one of
// them will compile and never fire, with no error. Without this option,
// golagram computes it automatically from whichever registration methods
// on [Router] were actually called (see [Router.UsedUpdateKinds]) once
// [TelegramBot.Dispatch] is set, so most bots never need to touch this
// directly.
func WithAllowedUpdates(kinds ...string) Option {
	return func(b *TelegramBot) { b.allowedUpdates = kinds }
}

// WithAutoRetry makes every API call transparently sleep and retry when
// Telegram returns a 429 flood-control error, instead of returning it
// straight to the caller — as many times as fit within maxWait's total
// budget (summed across all attempts for one call, using the server's own
// retry_after each time), then giving up and returning the [*APIError] like
// normal. Off by default (maxWait <= 0): flood control still surfaces as a
// normal error, exactly as before this existed — use [IsFlood] and
// [AsAPIError] to handle it yourself if you don't want automatic retry.
//
// This is retry, not pacing: each call still blocks its own goroutine until
// Telegram lets it through, so a broadcast loop that fires many sends
// concurrently will still hit floods repeatedly instead of being throttled
// ahead of time. There's no proactive outbound rate limiter today —
// [RateLimitMiddleware] paces inbound updates per user, not outbound sends.
func WithAutoRetry(maxWait time.Duration) Option {
	return func(b *TelegramBot) { b.autoRetryMaxWait = maxWait }
}

// WithDropPendingUpdates discards any updates that queued up while the bot
// wasn't running (or was running as a webhook) before [TelegramBot.Run]
// starts polling — useful after a redeploy when you don't want to process
// a backlog of stale updates. Applied once, at the start of Run;
// [TelegramBot.RunWebhook] has its own equivalent via
// [WebhookConfig.DropPendingUpdates] (a real setWebhook parameter —
// polling's getUpdates has no such parameter, so this approximates it by
// fast-forwarding past everything queued so far).
func WithDropPendingUpdates() Option {
	return func(b *TelegramBot) { b.dropPendingUpdates = true }
}

// WithoutGetMe skips the getMe network round-trip [NewTelegramBot] otherwise
// makes before returning — the only network I/O construction does, and
// otherwise an unconditional one with no caller-supplied context (a cold
// serverless start, a unit test with no fake Bot API server, CI with no
// network all pay for it or work around it).
//
// [TelegramBot.Me] still works: the bot ID is the numeric prefix of the
// token itself (already shape-validated by the time this option runs), so
// it's derived locally instead of asked for. Username stays "" — Telegram
// never sends it back except from getMe — which means /cmd@bot mention
// matching (see [FilterCommand]) and username-based deep links don't work
// until it's populated. Call [TelegramBot.LoadMe] once, after construction,
// for whichever of those this bot actually needs.
func WithoutGetMe() Option {
	return func(b *TelegramBot) { b.skipGetMe = true }
}

// WithLogger switches golagram's internal logging (dispatcher lifecycle,
// getUpdates/webhook errors, unrecovered handler errors that fall through
// to no [TelegramBot.OnError]) from plain log.Printf/log.Println to a
// structured [*slog.Logger] — every message still says the same thing, but
// now goes through your handler (JSON output, a level filter,
// request-scoped attributes via a wrapped logger, whatever you've
// configured slog for) instead of the standard library's bare text logger.
// Without this, golagram's log output is unchanged from before *slog.Logger
// existed here.
func WithLogger(logger *slog.Logger) Option {
	return func(b *TelegramBot) {
		if logger != nil {
			b.logger = logger
		}
	}
}

// logInfo logs an informational message (dispatcher lifecycle events —
// nothing went wrong) through b.logger if configured, else plain log.Println.
func (b *TelegramBot) logInfo(msg string) {
	if b.logger != nil {
		b.logger.Info(msg)
		return
	}
	log.Println(msg)
}

// logErrorf logs a formatted error/warning message through b.logger (as a
// single slog message, not broken into structured attributes — a
// deliberately simple translation, not full structured logging) if
// configured, else plain log.Printf.
func (b *TelegramBot) logErrorf(format string, args ...any) {
	if b.logger != nil {
		b.logger.Error(fmt.Sprintf(format, args...))
		return
	}
	log.Printf(format, args...)
}

// NewTelegramBot creates a bot for token, applies opts, and validates the
// token with a real getMe call before returning — so a bad token fails here,
// at construction, rather than on the first update. The returned bot has no
// router yet; call [TelegramBot.Dispatch] before [TelegramBot.Run] or
// [TelegramBot.RunWebhook]. Pass [WithoutGetMe] to skip that call (and the
// network round-trip, and its uncancelable context) entirely.
func NewTelegramBot(token string, opts ...Option) (*TelegramBot, error) {
	if err := ValidateToken(token); err != nil {
		return nil, err
	}

	b := &TelegramBot{
		healthMonitor:   NewHealthMonitor(),
		fsmStorage:      NewMemoryStorage(),
		dispatchLocks:   newKeyedMutex(),
		numWorkers:      defaultWorkers,
		updateBuffer:    defaultUpdateBuffer,
		pollTimeoutSecs: defaultPollTimeoutSecs,
	}
	for _, opt := range opts {
		opt(b)
	}

	if b.baseURL != "" {
		b.api = api.NewClientWithBaseURL(token, b.baseURL)
	} else {
		b.api = api.NewClient(token)
	}
	if b.httpClient != nil {
		b.api.SetHTTPClient(b.httpClient)
	}
	if b.autoRetryMaxWait > 0 {
		b.api.SetAutoRetry(b.autoRetryMaxWait)
	}
	b.updateChan = make(chan *Update, b.updateBuffer)

	if b.skipGetMe {
		id, err := botIDFromToken(token)
		if err != nil {
			return nil, err
		}
		b.me = &User{ID: id, IsBot: true}
		return b, nil
	}

	// getMe validates the token and keeps the bot's own identity, which
	// command matching needs to recognize /cmd@my_bot in groups.
	me, err := b.GetMe(context.Background())
	if err != nil {
		return nil, fmt.Errorf("getMe failed: %w", err)
	}
	b.me = me

	return b, nil
}

// botIDFromToken extracts the numeric bot ID BotFather assigns from the
// token's "<bot_id>:<secret>" shape, which [ValidateToken] has already
// confirmed the token matches by the time this runs.
func botIDFromToken(token string) (int64, error) {
	idPart, _, _ := strings.Cut(token, ":")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("golagram: could not parse bot ID from token: %w", err)
	}
	return id, nil
}

// Me returns the bot's own user info: the full result of getMe, or — if
// this bot was constructed with [WithoutGetMe] and [TelegramBot.LoadMe]
// hasn't been called since — just the ID derived from the token, with
// Username empty.
func (b *TelegramBot) Me() *User {
	return b.me
}

// LoadMe calls getMe and replaces [TelegramBot.Me] with the result,
// including Username. Only needed for a bot constructed with
// [WithoutGetMe] that turns out to need /cmd@bot mention matching (see
// [FilterCommand]) or username-based deep links after all — call it once,
// any time before those features are exercised.
func (b *TelegramBot) LoadMe(ctx context.Context) error {
	me, err := b.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe failed: %w", err)
	}
	b.me = me
	return nil
}

// SetFSMStorage swaps the conversation-state backend. TelegramBot starts
// with an in-process MemoryStorage by default; call this before Run to use
// a persistent FSMStorage instead.
func (b *TelegramBot) SetFSMStorage(storage FSMStorage) {
	b.fsmStorage = storage
}

// Dispatch sets the router that handles every incoming update. Call it
// before Run.
func (b *TelegramBot) Dispatch(r *Router) {
	b.router = r
}

// OnStartup registers a hook to run once, in registration order, before
// Run or RunWebhook starts dispatching updates — e.g. connecting to a
// database, warming a cache. If any hook returns an error, Run/RunWebhook
// aborts immediately (no workers started, no polling/serving) and returns
// that error wrapped.
func (b *TelegramBot) OnStartup(f LifecycleFunc) {
	b.startupHooks = append(b.startupHooks, f)
}

// OnShutdown registers a hook to run once, in registration order, after
// Run/RunWebhook has stopped accepting new updates and every in-flight
// handler has finished — e.g. closing a database connection. A hook's
// error is logged, not fatal: by the time these run the bot is already
// shutting down, so there's nothing left to abort, and one hook failing
// shouldn't skip the others' cleanup.
func (b *TelegramBot) OnShutdown(f LifecycleFunc) {
	b.shutdownHooks = append(b.shutdownHooks, f)
}

func (b *TelegramBot) runStartupHooks(ctx context.Context) error {
	for _, f := range b.startupHooks {
		if err := f(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (b *TelegramBot) runShutdownHooks(ctx context.Context) {
	for _, f := range b.shutdownHooks {
		if err := f(ctx); err != nil {
			b.logErrorf("shutdown hook error: %v", err)
		}
	}
}

// errAlreadyRan is returned by Run/RunWebhook when called a second time on
// the same TelegramBot — see the "runs once" note on [TelegramBot].
var errAlreadyRan = fmt.Errorf("golagram: this TelegramBot already ran once (Run, RunWebhook, or StartWorkers); construct a new TelegramBot to run again")

// markRan reports whether this is the first call across Run/RunWebhook/
// StartWorkers for this bot — false on every call after the first. Checking
// it turns the send-to-a-closed-updateChan panic a second call would
// otherwise hit (StopWorkers closes it permanently) into a clear error or
// log line instead.
func (b *TelegramBot) markRan() bool {
	return b.ran.CompareAndSwap(false, true)
}

// startWorkers launches the worker pool that both Run (polling) and
// RunWebhook share — same dispatch(), same concurrency knobs, regardless of
// how updates arrive.
func (b *TelegramBot) startWorkers(ctx context.Context) {
	b.runContext = ctx
	for range b.numWorkers {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.worker(ctx)
		}()
	}
}

// StartWorkers starts the update-processing worker pool without either
// runtime — for embedding Handler(cfg) into your own HTTP server/mux
// instead of letting RunWebhook own the listener. You're still responsible
// for calling SetWebhook yourself; call StopWorkers on shutdown to drain
// in-flight handlers. Bots using Run or RunWebhook never need this.
//
// One-shot, like Run/RunWebhook (see [TelegramBot]): calling it again after
// StopWorkers has run is a no-op, logged as an error, rather than a panic.
func (b *TelegramBot) StartWorkers(ctx context.Context) {
	if !b.markRan() {
		b.logErrorf("%v", errAlreadyRan)
		return
	}
	b.startWorkers(ctx)
}

// StopWorkers stops accepting new updates and blocks until every in-flight
// handler has finished. Idempotent; called automatically by Run/RunWebhook
// on shutdown, needed directly only by StartWorkers users.
func (b *TelegramBot) StopWorkers() {
	b.stopOnce.Do(func() {
		close(b.updateChan)
		b.wg.Wait()
	})
}

// runCtx returns the context the bot is running under, or Background before
// Run/RunWebhook/StartWorkers has been called.
func (b *TelegramBot) runCtx() context.Context {
	if b.runContext != nil {
		return b.runContext
	}
	return context.Background()
}

// bindMessage wires a Message to this bot: API client, FSM storage, the
// bot's own username (for /cmd@bot matching), and ctx for cancellation of
// the message's own sugar calls (Answer, Delete, ...). Generated methods
// that return a *Message/[]Message call this so returned messages are
// immediately usable.
func (b *TelegramBot) bindMessage(ctx context.Context, m *Message) {
	if m == nil {
		return
	}
	m.api = b.api
	m.fsm = b.fsmStorage
	m.fsmStrategy = b.fsmStrategy
	m.boundCtx = ctx
	m.logf = b.logErrorf
	if b.me != nil {
		m.botUsername = b.me.Username
	}
	// Bind the nested messages handlers commonly act on (edit the message
	// the user replied to, delete the pinned message, ...) so their sugar
	// works too. Decoded JSON is a tree — no cycles to recurse into.
	b.bindMessage(ctx, m.ReplyToMessage)
	b.bindMessage(ctx, m.PinnedMessage)
}

// Run starts long-polling getUpdates and dispatches every update through
// the router set by [TelegramBot.Dispatch], blocking until ctx is canceled
// or a startup hook (see [TelegramBot.OnStartup]) returns an error. On a
// getUpdates failure it retries with exponential backoff instead of
// returning — a canceled ctx is the only case Run returns from, and it
// returns nil for that, not ctx.Err(). Shutdown drains every in-flight
// handler (see [TelegramBot.StopWorkers]) and runs shutdown hooks (see
// [TelegramBot.OnShutdown]) before returning. One-shot, like RunWebhook and
// StartWorkers (see [TelegramBot]) — calling Run again after a previous
// Run/RunWebhook/StartWorkers has returned returns errAlreadyRan instead of
// polling.
func (b *TelegramBot) Run(ctx context.Context) error {
	if !b.markRan() {
		return errAlreadyRan
	}

	if err := b.runStartupHooks(ctx); err != nil {
		return fmt.Errorf("startup hook: %w", err)
	}

	b.startWorkers(ctx)

	defer func() {
		b.StopWorkers()
		b.runShutdownHooks(context.Background())
	}()

	allowedUpdates := b.resolveAllowedUpdates()

	var offset int64
	if b.dropPendingUpdates {
		skipTo, err := b.skipPendingUpdates(ctx, allowedUpdates)
		if err != nil {
			return fmt.Errorf("drop pending updates: %w", err)
		}
		offset = skipTo
	}

	backoff := NewBackoff(pollBackoffMin, pollBackoffMax)
	for {
		select {
		case <-ctx.Done():
			b.logInfo("Context canceled — stopping bot gracefully...")
			return nil
		default:
		}

		updates, err := b.getUpdates(ctx, offset, allowedUpdates)
		if err != nil {
			if ctx.Err() != nil {
				b.logInfo("Context canceled — stopping bot gracefully...")
				return nil
			}
			wait := backoff.Next()
			if IsConflict(err) {
				b.logErrorf("getUpdates conflict: another getUpdates (or a webhook) is already active for this bot token — retrying in %s", wait)
			} else {
				b.logErrorf("getUpdates error: %v (retrying in %s)", err, wait)
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				b.logInfo("Context canceled — stopping bot gracefully...")
				return nil
			}
			continue
		}
		backoff.Reset()

		for _, update := range updates {
			b.hydrate(update)
			select {
			case b.updateChan <- update:
			case <-ctx.Done():
				b.logInfo("Context canceled while queueing update — exiting...")
				return nil
			}
			offset = update.UpdateID + 1
		}
	}
}

// skipPendingUpdates discards any updates queued before Run starts, by
// fetching only the most recent one (offset=-1, Telegram's shorthand for
// "just the last update") and returning the offset to resume just after
// it — see WithDropPendingUpdates.
func (b *TelegramBot) skipPendingUpdates(ctx context.Context, allowedUpdates []string) (int64, error) {
	updates, err := b.getUpdates(ctx, -1, allowedUpdates)
	if err != nil {
		return 0, err
	}
	if len(updates) == 0 {
		return 0, nil
	}
	return updates[len(updates)-1].UpdateID + 1, nil
}

// resolveAllowedUpdates returns the explicit WithAllowedUpdates override if
// set, otherwise computes it from the dispatched router's registered kinds
// (nil — "all kinds" — if no router is set, matching the pre-existing
// default behavior).
func (b *TelegramBot) resolveAllowedUpdates() []string {
	if b.allowedUpdates != nil {
		return b.allowedUpdates
	}
	if b.router == nil {
		return nil
	}
	return b.router.UsedUpdateKinds()
}

// getUpdates long-polls Telegram through the same api.Client as every other
// call, decoding into zero-value Updates so absent fields stay nil.
func (b *TelegramBot) getUpdates(ctx context.Context, offset int64, allowedUpdates []string) ([]*Update, error) {
	params := struct {
		Offset         int64    `json:"offset"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates,omitempty"`
	}{Offset: offset, Timeout: b.pollTimeoutSecs, AllowedUpdates: allowedUpdates}

	// The deadline must exceed Telegram's long-poll timeout, otherwise every
	// poll gets killed client-side before the server has a chance to respond.
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(b.pollTimeoutSecs+10)*time.Second)
	defer cancel()

	raw, err := b.api.Call(callCtx, "getUpdates", params)
	if err != nil {
		return nil, err
	}

	var updates []*Update
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, fmt.Errorf("failed to decode updates: %w", err)
	}
	return updates, nil
}

// hydrate wires every message-shaped payload in the update (and the
// callback query itself) to the bot's API client and FSM storage, so
// handler sugar (Answer, FSM, command matching) works regardless of which
// field on Update it came in on.
func (b *TelegramBot) hydrate(u *Update) {
	b.hydrateMessage(u.Message)
	b.hydrateMessage(u.EditedMessage)
	b.hydrateMessage(u.ChannelPost)
	b.hydrateMessage(u.EditedChannelPost)
	b.hydrateMessage(u.BusinessMessage)
	b.hydrateMessage(u.EditedBusinessMessage)
	b.hydrateMessage(u.GuestMessage)
	if cq := u.CallbackQuery; cq != nil {
		cq.api = b.api
		cq.fsm = b.fsmStorage
		cq.fsmStrategy = b.fsmStrategy
		cq.boundCtx = b.runCtx()
		cq.logf = b.logErrorf
		b.hydrateMessage(cq.Message)
	}
}

func (b *TelegramBot) hydrateMessage(m *Message) {
	b.bindMessage(b.runCtx(), m)
}

// worker drains b.updateChan until it's closed, deliberately ignoring
// ctx.Done() — exiting early on cancellation would abandon whatever is
// still buffered in the channel at shutdown, silently dropping updates
// Telegram already considers delivered (see StopWorkers/Run's offset
// advancement). Handlers still observe cancellation through the Ctx they
// receive via dispatch.
func (b *TelegramBot) worker(ctx context.Context) {
	for update := range b.updateChan {
		if update == nil {
			continue
		}
		b.dispatch(ctx, update)
	}
}

// dispatch runs the router against one update via the async worker pool —
// the path both Run and RunWebhook use by default. There's no HTTP response
// here to embed a reply into, so a handler's Reply(...) return always
// becomes a real, fire-and-forget API call; see resolveWebhookReply.
func (b *TelegramBot) dispatch(ctx context.Context, u *Update) {
	c := newCtx(ctx, u, b, b.api, b.fsmStorage, b.botUsername())
	b.dispatchCtx(ctx, c)
}

// HandleUpdate synchronously dispatches u through the configured router,
// exactly as the polling and webhook workers do internally — without
// requiring StartWorkers, Run, or RunWebhook. This is the entrypoint for
// testing a bot: build an *Update by hand, or with the golagramtest
// package's factories, and pass it here directly. Call [TelegramBot.Dispatch]
// to attach a router first — with no router attached, HandleUpdate is a
// no-op that still counts the update as unmatched.
func (b *TelegramBot) HandleUpdate(ctx context.Context, u *Update) {
	b.hydrate(u)
	b.dispatch(ctx, u)
}

// dispatchSync is dispatch's synchronous counterpart: called directly from
// RunWebhook's Handler (webhook.go), still holding the HTTP response open,
// for an update whose matching registration opted into AllowWebhookReply.
// c must already carry a webhookReplySink (attachWebhookReplySink) so a
// Reply(...) return is captured instead of triggering a real API call; the
// caller gets that capture back to embed in the webhook response body.
func (b *TelegramBot) dispatchSync(ctx context.Context, c *Ctx) *WebhookReply {
	sink := c.replySink
	b.dispatchCtx(ctx, c)
	return sink.reply
}

// dispatchCtx is dispatch and dispatchSync's shared body: locks the
// {chat, user} key so FSM state reads/writes across the filter-match-and-
// handle sequence can't race when two of their updates land on different
// workers, runs the router, and resolves the outcome (health counters,
// OnError, or — for a Reply(...) return — resolveWebhookReply).
func (b *TelegramBot) dispatchCtx(ctx context.Context, c *Ctx) {
	key := c.storageKey()
	kind := c.Kind()
	if kind == "" {
		// A future Bot API update kind this version of golagram predates
		// (no field for it on Update yet) decodes to an all-nil Update
		// rather than erroring — labeling it distinctly here, instead of
		// letting it blend into the aggregate "unmatched" count under a
		// blank kind, is what actually makes that situation visible in
		// /health: a bot upgrade is overdue.
		kind = "unknown"
	}

	b.dispatchLocks.Lock(key)
	defer b.dispatchLocks.Unlock(key)

	b.healthMonitor.IncrementDispatched(kind)

	if b.router == nil {
		b.healthMonitor.IncrementUnmatched(kind)
		return
	}

	matched, err := b.safeDispatch(c)
	if !matched {
		b.healthMonitor.IncrementUnmatched(kind)
		return
	}
	b.healthMonitor.IncrementMatched(kind)

	if wr, ok := AsWebhookReply(err); ok {
		b.resolveWebhookReply(ctx, c, wr)
		return
	}
	if err != nil {
		b.handleError(err, c)
	}
}

// resolveWebhookReply embeds wr into c's webhook response if c has a sink
// attached and wr is eligible (tryEmbedWebhookReply), otherwise performs it
// as a normal fire-and-forget API call — the fallback for polling mode,
// a registration that didn't opt into AllowWebhookReply, or a request
// carrying a local file upload that can't ride in a JSON response body.
func (b *TelegramBot) resolveWebhookReply(ctx context.Context, c *Ctx, wr *WebhookReply) {
	if c.tryEmbedWebhookReply(wr) {
		return
	}
	if _, err := b.api.Call(ctx, wr.method, wr.params); err != nil {
		b.handleError(fmt.Errorf("reply(%s): %w", wr.method, err), c)
	}
}

// safeDispatch runs the router against c, recovering a handler panic instead
// of letting it crash the worker goroutine — a bad handler should fail the
// same way a returned error does, not take the whole bot down with it.
func (b *TelegramBot) safeDispatch(c *Ctx) (matched bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			matched = true
			err = fmt.Errorf("panic in handler: %v\n%s", r, debug.Stack())
		}
	}()
	return b.router.dispatch(c)
}

func (b *TelegramBot) botUsername() string {
	if b.me == nil {
		return ""
	}
	return b.me.Username
}

// SetBotCommands sets the bot's command menu for the default scope. For a
// specific scope or language, use the generated [TelegramBot.SetMyCommands]
// with [SetMyCommandsRequest.Scope] / [SetMyCommandsRequest.LanguageCode].
func (b *TelegramBot) SetBotCommands(commands []BotCommand) error {
	req := &SetMyCommandsRequest{Commands: commands}
	if _, err := b.SetMyCommands(b.runCtx(), req); err != nil {
		return fmt.Errorf("failed to set bot commands: %w", err)
	}
	return nil
}

// OnError sets the bot-wide fallback for errors a handler returns (or a
// recovered handler panic) that no router-level [Router.OnError] already
// handled. Without one set, such errors are just logged.
func (b *TelegramBot) OnError(handler ErrorHandlerFunc) {
	b.errorHandler = handler
}

func (b *TelegramBot) handleError(err error, c *Ctx) {
	if err == nil {
		return
	}

	b.healthMonitor.RecordError(err)

	if b.errorHandler != nil {
		b.errorHandler(err, c)
	} else {
		b.logErrorf("Error in handler: %v", err)
	}
}

// GetHealthMonitor returns the bot's [HealthMonitor], for reading dispatch
// counters directly instead of (or in addition to) the HTTP endpoint
// started by [TelegramBot.StartHealthServer].
func (b *TelegramBot) GetHealthMonitor() *HealthMonitor {
	return b.healthMonitor
}

// StartHealthServer starts the health check HTTP server in a goroutine.
// The server shuts down when ctx is canceled — tie it to the same context
// as Run so it dies with the bot instead of reporting health for a corpse.
//
// /health and /healthz serve unauthenticated by default — fine on a private
// network, a footgun if addr ends up reachable from the public internet
// (LastError often contains chat/user IDs or upstream error text). Bind to
// localhost, put auth in front, or pass a [HealthGate] to reject requests
// that don't pass it (responds 403 instead of serving the status).
func (b *TelegramBot) StartHealthServer(ctx context.Context, addr string, gate ...HealthGate) {
	go func() {
		b.logInfo(fmt.Sprintf("Starting health check server on %s", addr))
		if err := b.healthMonitor.StartHealthServer(ctx, addr, gate...); err != nil {
			b.logErrorf("Health server error: %v", err)
		}
	}()
}
