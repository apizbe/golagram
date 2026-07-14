// Command middleware shows composability: global logging + auto-answered
// callbacks on the root router, and an admin-only sub-router mounted with
// Router.Include that non-admins fall straight through (ErrSkipHandler).
// Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	export ADMIN_ID=your-telegram-user-id
//	go run ./examples/middleware
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	gg "github.com/apizbe/golagram"
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("set BOT_TOKEN to a token from @BotFather")
	}
	adminID, _ := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)
	if adminID == 0 {
		log.Println("warning: ADMIN_ID not set — /broadcast will be unreachable")
	}

	bot, err := gg.NewTelegramBot(token)
	if err != nil {
		log.Fatal(err)
	}

	r := gg.NewRouter()
	r.Use(gg.LoggingMiddleware(slog.Default())) // one structured line per update, on every route
	r.Use(gg.CallbackAnswerMiddleware())        // dismisses the tap spinner even if a handler forgets to

	r.Message(gg.FilterCommand("start")).Handle(func(c *gg.Ctx) error {
		kb := gg.NewInlineKeyboard().Add(gg.NewInlineButton("Ping", "ping")).Build()
		_, err := c.Answer("Tap the button — no explicit AnswerCallback needed, the middleware handles it.",
			&gg.SendMessageOptions{ReplyMarkup: kb})
		return err
	})
	r.CallbackQuery(gg.FilterCallbackData("ping")).Handle(func(c *gg.Ctx) error {
		_, err := c.EditText("Pong!")
		return err // no c.AnswerCallback here — CallbackAnswerMiddleware covers it
	})

	admin := gg.NewRouter()
	admin.Use(adminOnly(adminID))
	admin.Message(gg.FilterCommand("broadcast")).Handle(func(c *gg.Ctx) error {
		_, err := c.Answer("(pretend this reached every subscriber)")
		return err
	})
	r.Include(admin) // non-admins fall through past admin entirely, see adminOnly below

	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("middleware bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

// adminOnly gates every handler on admin's router: a non-admin sender makes
// the whole router act as if it never matched (ErrSkipHandler), instead of
// silently answering "no" — so a non-admin's /broadcast can still fall
// through to whatever r itself registers after Include.
func adminOnly(adminID int64) gg.MiddlewareFunc {
	return func(next gg.HandlerFunc) gg.HandlerFunc {
		return func(c *gg.Ctx) error {
			if u := c.From(); u == nil || u.ID != adminID {
				return gg.ErrSkipHandler
			}
			return next(c)
		}
	}
}
