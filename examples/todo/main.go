// Command todo is a small but complete bot — a per-user task list — split
// by responsibility: handlers.go has one method per route, routes.go is the
// route table, store.go is the data layer, and main.go just wires them
// together. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/todo
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

	h := &Handlers{store: NewStore()}
	bot.Dispatch(newRouter(h))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("todo bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
