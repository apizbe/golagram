// Command broadcast shows gg.Broadcast: an in-memory subscriber list, a
// /subscribe command anyone can run, and an admin-only /broadcast <text>
// that pages through every subscriber at a paced rate with live progress
// logged to stdout. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	export ADMIN_ID=your-telegram-user-id
//	go run ./examples/broadcast
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	gg "github.com/apizbe/golagram"
)

// subscribers is an in-memory stand-in for a real subscriber table — swap
// for a database in a real bot, same as todo/store.go does for tasks.
type subscribers struct {
	mu  sync.Mutex
	ids map[int64]bool
}

func (s *subscribers) add(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids[chatID] = true
}

func (s *subscribers) snapshot() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.ids))
	for id := range s.ids {
		ids = append(ids, id)
	}
	return ids
}

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

	subs := &subscribers{ids: make(map[int64]bool)}

	r := gg.NewRouter()
	r.Message(gg.FilterCommand("start")).Handle(func(c *gg.Ctx) error {
		subs.add(c.Chat().ID)
		_, err := c.Answer("Subscribed! An admin can now reach you with /broadcast.")
		return err
	})

	admin := gg.NewRouter()
	admin.Use(adminOnly(adminID))
	admin.Message(gg.FilterCommand("broadcast")).Handle(func(c *gg.Ctx) error {
		text := c.Message.Command().Args
		if text == "" {
			_, err := c.Answer("Usage: /broadcast <text>")
			return err
		}
		return runBroadcast(c, subs.snapshot(), text)
	})
	r.Include(admin)

	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("broadcast bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

// runBroadcast pages text out to every chat in chatIDs, logging progress as
// it goes, and replies to the admin with a final tally.
func runBroadcast(c *gg.Ctx, chatIDs []int64, text string) error {
	if len(chatIDs) == 0 {
		_, err := c.Answer("No subscribers yet.")
		return err
	}

	bot := c.Bot()
	result, broadcastErr := gg.Broadcast(c, chatIDs, func(ctx context.Context, chatID int64) error {
		_, err := bot.SendMessage(ctx, &gg.SendMessageRequest{ChatID: gg.ChatIDFromInt(chatID), Text: text})
		return err
	}, gg.WithBroadcastProgress(func(p gg.BroadcastProgress) {
		log.Printf("broadcast progress: %d/%d sent, %d failed (%d blocked), %d remaining",
			p.Sent, p.Total, p.Failed, p.Blocked, p.Remaining)
	}))

	summary := fmt.Sprintf("Broadcast finished: %d sent, %d failed (%d blocked).",
		result.Sent, result.Failed, len(result.Blocked))
	if broadcastErr != nil {
		summary += " Interrupted: " + broadcastErr.Error()
	}

	_, err := c.Answer(summary)
	return err
}

// adminOnly gates every handler on admin's router — see examples/middleware
// for the fuller explanation of why ErrSkipHandler (not a "no" reply) is
// the right response to a non-admin sender.
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
