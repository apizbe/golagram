// Command deeplink shows a referral flow: plain /start hands back a
// shareable https://t.me/<bot>?start=<payload> link (CreateStartLink), and
// /start <payload> — the receiving side, opened via that link — reads the
// payload back while a live "typing..." indicator (KeepChatAction) covers
// a pretend lookup. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/deeplink
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	gg "github.com/apizbe/golagram"
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("set BOT_TOKEN to a token from @BotFather")
	}

	bot, err := gg.NewTelegramBot(token)
	if err != nil {
		log.Fatal(err)
	}

	r := gg.NewRouter()
	r.Message(gg.FilterCommandStartDeepLink()).Handle(func(c *gg.Ctx) error {
		payload := c.Command().Args

		stop := gg.Typing(c)
		time.Sleep(time.Second) // pretend to look the referral code up
		stop()

		_, err := c.Answer(fmt.Sprintf("Welcome! You came from referral code %q.", payload))
		return err
	})
	r.Message(gg.FilterCommandStart()).Handle(func(c *gg.Ctx) error {
		payload := fmt.Sprintf("user%d", c.From().ID)
		link, err := c.Bot().CreateStartLink(payload, false)
		if err != nil {
			return err
		}
		_, err = c.Answer("Share this link to invite friends:\n" + link)
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("deeplink bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
