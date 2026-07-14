// Command keyboard shows the core inline-keyboard loop: send a menu, answer
// the tap, edit the message in place. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/keyboard
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	gg "github.com/apizbe/golagram"
)

var colors = []string{"Red", "Green", "Blue", "Yellow", "Purple", "Orange"}

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
		_, err := c.Answer("Pick a color:", &gg.SendMessageOptions{ReplyMarkup: colorKeyboard()})
		return err
	})
	r.CallbackQuery(gg.FilterCallbackPrefix("color:")).Handle(func(c *gg.Ctx) error {
		color := c.CallbackQuery.Data[len("color:"):]
		if err := c.AnswerCallback("You picked " + color); err != nil {
			return err
		}
		_, err := c.EditText(fmt.Sprintf("Your favorite color is %s.", color))
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("keyboard bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func colorKeyboard() *gg.InlineKeyboardMarkup {
	kb := gg.NewInlineKeyboard()
	for _, color := range colors {
		kb.Insert(gg.NewInlineButton(color, "color:"+color))
	}
	return kb.Adjust(2).Build()
}
