// Package golagram is a Go framework for building Telegram bots: a
// Router/Ctx dispatch model routes each of Telegram's ~25 update kinds to
// composable filters and handlers, an FSM tracks per-chat/user conversation
// state across any update kind (including callback queries), and generated
// code covers the full Bot API surface (all methods, types, and
// polymorphic union decoding) straight from Telegram's published spec.
//
// Both polling (Run) and webhook (RunWebhook) runtimes share one dispatcher
// and worker pool, so a bot's handlers don't change when switching between
// them. See the README for a quick start and a tour of the utility belt
// (keyboards, formatting, deep links, payments, i18n, WebApp validation);
// types.gen.go and methods.gen.go are generated from scripts/api.json — see
// internal/gen and `go generate ./...` to regenerate after a Bot API
// update.
package golagram
