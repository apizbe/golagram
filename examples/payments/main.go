// Command payments shows a complete Telegram Stars purchase flow — no
// external payment provider to configure, since Stars are native to
// Telegram: /buy sends an invoice, a pre_checkout_query is approved
// automatically, a successful payment is acknowledged and its charge ID
// remembered, and /refund gives the Stars back. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/payments
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	gg "github.com/apizbe/golagram"
)

// lastCharge remembers each buyer's most recent payment, so /refund knows
// which charge to reverse — a real bot would persist this alongside the
// order it paid for.
var (
	mu         sync.Mutex
	lastCharge = map[int64]string{}
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
		_, err := c.Answer("/buy to purchase a Pro Badge for 1 Stars, /refund to get them back.")
		return err
	})
	r.Message(gg.FilterCommand("buy")).Handle(func(c *gg.Ctx) error {
		_, err := c.SendStarsInvoice("Pro Badge", "Unlock a shiny profile badge", "pro-badge", 1)
		return err
	})
	r.PreCheckoutQuery().Handle(func(c *gg.Ctx) error {
		return c.AnswerPreCheckout()
	})
	r.Message(gg.FilterSuccessfulPayment()).Handle(func(c *gg.Ctx) error {
		sp := c.Message.SuccessfulPayment
		mu.Lock()
		lastCharge[c.From().ID] = sp.TelegramPaymentChargeID
		mu.Unlock()
		_, err := c.Answer(fmt.Sprintf("Thanks! Paid %d Stars ⭐ — /refund if you change your mind.", sp.TotalAmount))
		return err
	})
	r.Message(gg.FilterCommand("refund")).Handle(func(c *gg.Ctx) error {
		mu.Lock()
		chargeID, ok := lastCharge[c.From().ID]
		mu.Unlock()
		if !ok {
			_, err := c.Answer("No payment on file to refund.")
			return err
		}
		if err := c.Bot().RefundStars(c, c.From().ID, chargeID); err != nil {
			return err
		}
		_, err := c.Answer("Refunded.")
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("payments bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
