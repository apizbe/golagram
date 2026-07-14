// Command media shows golagram's InputFile constructors: /photo sends an
// image by URL (no local file needed to run this example anywhere), and an
// incoming photo gets echoed back by file_id — no re-upload. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/media
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
	r.Message(gg.FilterCommand("photo")).Handle(func(c *gg.Ctx) error {
		_, err := c.Bot().SendPhoto(c, &gg.SendPhotoRequest{
			ChatID:  gg.ChatIDFromInt(c.Chat().ID),
			Photo:   gg.InputFileURL("https://picsum.photos/500"),
			Caption: "Fetched straight from a URL — Telegram downloads it, golagram never touches the bytes.",
		})
		return err
	})
	r.Message(gg.FilterPhoto()).Handle(func(c *gg.Ctx) error {
		sizes := c.Message.Photo
		largest := sizes[len(sizes)-1] // Telegram lists photo sizes smallest to largest
		_, err := c.Bot().SendPhoto(c, &gg.SendPhotoRequest{
			ChatID:  gg.ChatIDFromInt(c.Chat().ID),
			Photo:   gg.InputFileID(largest.FileID),
			Caption: "Same photo, sent back by file_id — no re-upload.",
		})
		return err
	})
	r.Message(gg.FilterCommand("start")).Handle(func(c *gg.Ctx) error {
		_, err := c.Answer("Try /photo, or send me a photo and I'll echo it back.")
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("media bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
