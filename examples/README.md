# Examples

Thirteen small, single-file bots, each demonstrating one golagram feature,
plus one full-featured multi-file bot showing how they compose. No per-example
`go.mod` — they import `github.com/apizbe/golagram` directly, so
any of them runs immediately after cloning:

```bash
export BOT_TOKEN=your-token-from-botfather   # get one from @BotFather
go run ./examples/echo
```

Start with `echo`, then jump to whichever feature you came for.

| Example | What it shows | Run |
|---|---|---|
| [`echo`](echo/main.go) | The smallest real bot: `/start` + text echo | `go run ./examples/echo` |
| [`keyboard`](keyboard/main.go) | Inline keyboards, `Adjust`, answer-then-edit | `go run ./examples/keyboard` |
| [`pagination`](pagination/main.go) | `NewPagination` + typed `CallbackData[T]` catalog paging | `go run ./examples/pagination` |
| [`fsm`](fsm/main.go) | Conversations, the low-level way: `StateGroup`, `FSMGet`/`FSMSet` | `go run ./examples/fsm` |
| [`wizard`](wizard/main.go) | The same conversation via `gg.Wizard` — diff it against `fsm` | `go run ./examples/wizard` |
| [`media`](media/main.go) | `InputFile`: send by URL, echo back by `file_id` | `go run ./examples/media` |
| [`middleware`](middleware/main.go) | `Router.Use`/`Include`, built-in middleware, `ErrSkipHandler` | `go run ./examples/middleware` |
| [`formatting`](formatting/main.go) | `EscapeHTML` and `SplitText` for long messages | `go run ./examples/formatting` |
| [`payments`](payments/main.go) | Telegram Stars: invoice, pre-checkout, refund | `go run ./examples/payments` |
| [`i18n`](i18n/main.go) | Locale-aware `c.T`/`c.TN` with real CLDR plural rules | `go run ./examples/i18n` |
| [`deeplink`](deeplink/main.go) | Referral deep links + a live "typing…" indicator | `go run ./examples/deeplink` |
| [`broadcast`](broadcast/main.go) | `gg.Broadcast`: paced bulk sends with live progress | `go run ./examples/broadcast` |
| [`richmessage`](richmessage/main.go) | Bot API 10.1 Rich Messages: `gg.RichParagraph`/`gg.RichTable`/... + `gg.RenderRichMessage` to build and send, streaming drafts, and `RichMessage.PlainText()` to read one back | `go run ./examples/richmessage` |
| [`todo`](todo/) | A full per-user task-list bot, split into a route table | `go run ./examples/todo` |

Each single-file example opens with a short header comment explaining what
it shows and exactly how to run it — read top to bottom, they're all under
a minute.

## `todo` — how a real bot is organized

The other eleven are one file each, on purpose — easy to read top to bottom.
`todo/` is the exception: a small but complete task-list bot, split the way
a real project would be:

- **`routes.go`** — the whole route table in one place: `r.Message(gg.FilterCommand("add")).Handle(h.Add)` reads as "on this trigger, call this handler."
- **`handlers.go`** — one method per route on a `*Handlers` struct (its dependencies — here, just a `*Store` — are what a handler needs, not globals).
- **`store.go`** — the data layer, in memory; swap it for a database without touching a handler.
- **`main.go`** — wiring only: build the store, build the handlers, build the router, run.

Commands: `/add <task>`, `/list` (checkbox to toggle done, 🗑 to delete,
paginated past 5 tasks), `/clear` (removes completed tasks), `/help`.
