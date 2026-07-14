// Command echo is the smallest real golagram bot: /start plus a plain-text
// echo. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/echo
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
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("set BOT_TOKEN to a token from @BotFather")
	}

	bot, err := gg.NewTelegramBot(token)
	if err != nil {
		log.Fatal(err)
	}

	r := gg.NewRouter()
	r.Message(gg.FilterCommand("start")).Handle(handleStart)
	r.Message().Handle(handleEcho)
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("echo bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
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
