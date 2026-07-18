# golagram

[![CI](https://github.com/apizbe/golagram/actions/workflows/ci.yml/badge.svg)](https://github.com/apizbe/golagram/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/apizbe/golagram.svg)](https://pkg.go.dev/github.com/apizbe/golagram)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-blue.svg)](https://golang.org/doc/devel/release.html)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

Telegram bots in Go, with the type system on your side: one `Router`/`Ctx`
model for all ~25 update kinds, conversations as a first-class citizen, and
the entire Bot API generated from Telegram's spec.

## Quickstart

**Requirements:** Go 1.22+, and a bot token from
[@BotFather](https://t.me/BotFather) (`/newbot`, then copy the token it
gives you).

**1. Install.**

```bash
go get github.com/apizbe/golagram
```

The root module is stdlib-only — this adds zero third-party dependencies to
your `go.sum`.

**2. Write a bot.** Save as `main.go`:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	gg "github.com/apizbe/golagram"
)

func main() {
	bot, err := gg.NewTelegramBot(os.Getenv("BOT_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	r := gg.NewRouter()
	r.Message(gg.FilterCommand("start")).Handle(handleStart)
	r.Message().Handle(handleEcho)
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil { // long-polling; see "Going to production" below for webhooks
		log.Fatal(err)
	}
}

func handleStart(c *gg.Ctx) error {
	_, err := c.Answer("Hi! Send me anything and I'll echo it back.")
	return err
}

func handleEcho(c *gg.Ctx) error {
	_, err := c.Answer(c.Text())
	return err
}
```

**3. Run it.**

```bash
export BOT_TOKEN=your-token-from-botfather
go run .
```

Open a chat with your bot on Telegram and send `/start`, then anything
else — it echoes back what you sent. `Ctrl+C` shuts it down cleanly (the
`signal.NotifyContext` cancels `ctx`, which `Run` treats as a request to
finish in-flight handlers and return, not a crash).

That's the whole shape of a golagram program: build a `*Router`, register
handlers with `Handle`, `Dispatch` it onto a `*TelegramBot`, `Run`. Every
other feature — keyboards, conversations, media, payments — plugs into
this same `Router`/`Ctx` pair. [`examples/echo`](examples/echo/main.go) is
this exact program; [`examples/`](examples/) has thirteen more, one
feature each.

## Why another Telegram library?

- **The full API, typed.** All 180 methods and every type are generated
  from Telegram's spec — including polymorphic unions, so `GetChatMember`
  returns a concrete `*ChatMemberAdministrator` and `chat_member` updates
  carry their old/new membership. No `map[string]any`, no type assertions
  on your side.
- **Conversations are first-class.** FSM state is scoped correctly for
  *every* update kind — `StateIs(...)` routes callback queries mid-flow,
  not just messages. `Wizard` builds linear flows declaratively, and
  [`storage/redis`](storage/redis/) swaps in with one option when state
  must survive restarts.
- **The hard parts are already done.** UTF-16-correct text splitting that
  never cuts an emoji or formatting entity in half; `attach://` hoisting
  so a local upload nested inside a media group just works; per-{chat,user}
  dispatch locking so two updates can't race each other's state; optional
  transparent flood-control retry.
- **Testable without Telegram.** [`golagramtest`](golagramtest/) fakes the
  Bot API server and `bot.HandleUpdate` drives your real router
  synchronously — assert on the exact API calls your handler made, no
  network, no hand-rolled mocks.
- **A utility belt you'd otherwise write yourself.** Keyboard
  layout/pagination builders, HTML/MarkdownV2 escaping, deep links,
  Telegram Stars payments, WebApp/Login Widget validation (HMAC and
  Ed25519), i18n with real CLDR plural rules, paced `Broadcast`, a
  `/health` endpoint, and Bot API 10.1 Rich Messages.

## Learn it

| | |
|---|---|
| 💡 [`examples/`](examples/) | Fourteen runnable bots, one feature each — `export BOT_TOKEN=... && go run ./examples/echo` |
| 📖 [Guides](https://github.com/apizbe/golagram-web/tree/main/content/en/docs/guides) | Routing, conversations, media, webhooks, testing — each in depth |
| 📦 [pkg.go.dev](https://pkg.go.dev/github.com/apizbe/golagram) | Every exported symbol, straight from the godoc |

Start with [`examples/echo`](examples/echo/main.go), then jump to whichever
feature you came for — [`examples/README.md`](examples/README.md) has the
map. [`examples/todo`](examples/todo/) is the one multi-file example,
showing how a real project is laid out.

## Testing your bot

Handlers are plain functions of `*gg.Ctx`, so they're unit-testable, but
[`golagramtest`](golagramtest/) goes further: it fakes the Bot API over
HTTP and drives your *real* router synchronously, so you assert on the
exact calls a handler made instead of hand-rolling a mock:

```go
func TestStart(t *testing.T) {
	server := golagramtest.NewServer()
	defer server.Close()

	bot := golagramtest.NewBot(t, server)
	bot.Dispatch(routes()) // the same *gg.Router your program runs

	bot.HandleUpdate(context.Background(), golagramtest.CommandMessage(1, 2, "start"))

	calls := server.CallsTo("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}
```

No network, no live bot token, no flaky Telegram calls in CI.

## Going to production: webhooks

`Run` (long-polling) is the right default for local development — it needs
no public URL. In production, `RunWebhook` swaps in without touching a
single handler:

```go
err := bot.RunWebhook(ctx, gg.WebhookConfig{
	Addr:        ":8443",
	Path:        "/telegram/webhook",
	PublicURL:   "https://bot.example.com/telegram/webhook",
	SecretToken: os.Getenv("WEBHOOK_SECRET"), // verified on every request — see below
})
```

`SecretToken` is webhook mode's entire security model: without it, anyone
who discovers `PublicURL` can feed your bot forged updates, so treat it as
a credential (long, random, out of source control). If you're embedding
into an existing `net/http` server or router (chi, echo, stdlib `mux`)
instead of letting golagram own the listener, `bot.Handler(cfg)` returns
just the `http.Handler` — call `bot.StartWorkers`/`StopWorkers` yourself
around it. See [`examples/`](examples/) and the
[webhooks guide](https://github.com/apizbe/golagram-web/tree/main/content/en/docs/guides)
for TLS termination behind a reverse proxy and self-hosted Bot API servers.

## Configuration

Everything tunes through options on the constructor:

```go
bot, err := gg.NewTelegramBot(token,
	gg.WithWorkers(16),                  // concurrent update handlers
	gg.WithFSMStorage(redisStorage),     // persistent conversation state
	gg.WithAutoRetry(2*time.Minute),     // sleep retry_after and retry on 429s
	gg.WithBaseURL("http://localhost:8081"), // self-hosted Bot API server
	gg.WithLogger(slog.Default()),       // structured logs for golagram's own lines
)
```

The full option list is on
[pkg.go.dev](https://pkg.go.dev/github.com/apizbe/golagram#Option).

## FAQ

**Polling or webhooks?** Polling (`bot.Run`) for local development and
small/simple deployments — no public URL, no TLS to manage. Webhooks
(`bot.RunWebhook`) for anything latency-sensitive or running at scale:
Telegram pushes updates instead of you polling for them. Handlers don't
change either way.

**How do conversations survive a restart?** FSM state is in-memory by
default (`fsm_memory.go`) — fine for a single process, gone on restart.
Pass `gg.WithFSMStorage(redisStorage)` from [`storage/redis`](storage/redis/)
to persist it, or implement the small `FSMStorage` interface yourself for
another backend.

**Does it cover the whole Bot API?** `types.gen.go`, `methods.gen.go`, and
`consts.gen.go` are generated straight from Telegram's published spec —
all methods, all types, including polymorphic unions. They're regenerated
with `go generate ./...` when Telegram ships new API surface; see
[CONTRIBUTING.md](CONTRIBUTING.md).

**Can I point it at a self-hosted Bot API server?** Yes —
`gg.WithBaseURL("http://localhost:8081")`.

**Is it safe to use in production today?** Yes — v1.0.0 has shipped (see
Versioning below): the public API is stable and 1.x stays backward
compatible.

## Versioning

golagram follows [SemVer](https://semver.org/) and has shipped **v1.0.0** —
the public API is stable, and 1.x stays backward compatible from here. New
Bot API releases (additive by nature) land as minor bumps; breaking changes
wait for v2. A version exists once a pushed git tag matches `version.go` —
[CI](.github/workflows/release.yml) refuses to release when they disagree.

## Getting help

- **Questions and ideas:** open a
  [GitHub issue](https://github.com/apizbe/golagram/issues/new/choose)
  — there are templates for bug reports and feature requests.
- **Something not working as documented:** check
  [pkg.go.dev](https://pkg.go.dev/github.com/apizbe/golagram) for
  the exact contract first, then open an issue if the docs and behavior
  disagree.
- **Security vulnerabilities:** do not open a public issue — see
  [SECURITY.md](SECURITY.md) for the private reporting channel.

## Contributing

Contributions are welcome — [CONTRIBUTING.md](CONTRIBUTING.md) covers the
build/test/codegen workflow, and [SECURITY.md](SECURITY.md) is the private
channel for vulnerabilities.

## License

[MIT](LICENSE)
