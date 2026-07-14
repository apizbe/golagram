// Command formatting shows the two questions every bot library eventually
// gets asked: /bio <text> proves untrusted input can't break HTML parsing
// (try "/bio <b>hi" — a naive bot would crash the parse_mode with a 400),
// and /lipsum proves a message far past Telegram's 4096-char limit still
// sends, split at word boundaries. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/formatting
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
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
	r.Message(gg.FilterCommand("start")).Handle(func(c *gg.Ctx) error {
		_, err := c.Answer("Try /bio <anything, even <b>tags</b> or under_scores> or /lipsum.")
		return err
	})
	r.Message(gg.FilterCommand("bio")).Handle(func(c *gg.Ctx) error {
		text := c.Command().Args
		if text == "" {
			_, err := c.Answer("Usage: /bio <text>")
			return err
		}
		safe := "<b>Bio:</b> " + gg.EscapeHTML(text)
		_, err := c.Answer(safe, &gg.SendMessageOptions{ParseMode: "HTML"})
		return err
	})
	r.Message(gg.FilterCommand("lipsum")).Handle(func(c *gg.Ctx) error {
		long := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 100)
		for _, chunk := range gg.SplitText(long) {
			if _, err := c.Answer(chunk); err != nil {
				return err
			}
		}
		return nil
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("formatting bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
