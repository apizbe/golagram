// Command i18n shows locale-aware translations with real CLDR plural
// rules: /lang switches locale, /hello greets in it, and /apples <n>
// against Russian shows the one/few/many split (not just one/other) firing
// correctly for 1, 2, 5, 21. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/i18n
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	gg "github.com/apizbe/golagram"
)

func translator() *gg.I18n {
	return gg.NewI18n("en").
		Add("en", map[string]string{
			"greeting":     "Hello! I speak your language.",
			"apples.one":   "You have %d apple.",
			"apples.other": "You have %d apples.",
		}).
		Add("ru", map[string]string{
			"greeting":     "Привет! Я говорю на твоём языке.",
			"apples.one":   "У вас %d яблоко.",
			"apples.few":   "У вас %d яблока.",
			"apples.many":  "У вас %d яблок.",
			"apples.other": "У вас %d яблок.",
		}).
		Add("uz", map[string]string{
			"greeting":     "Salom! Men sizning tilingizda gapiraman.",
			"apples.other": "Sizda %d ta olma bor.",
		})
}

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
	r.Use(gg.I18nMiddleware(translator()))

	r.Message(gg.FilterCommand("start")).Handle(func(c *gg.Ctx) error {
		_, err := c.Answer(c.T("greeting") + "\n\nTry /lang, /hello, or /apples <n>.")
		return err
	})
	r.Message(gg.FilterCommand("lang")).Handle(func(c *gg.Ctx) error {
		kb := gg.NewInlineKeyboard().Row(
			gg.NewInlineButton("English", "lang:en"),
			gg.NewInlineButton("Русский", "lang:ru"),
			gg.NewInlineButton("Oʻzbekcha", "lang:uz"),
		).Build()
		_, err := c.Answer("Choose your language:", &gg.SendMessageOptions{ReplyMarkup: kb})
		return err
	})
	r.CallbackQuery(gg.FilterCallbackPrefix("lang:")).Handle(func(c *gg.Ctx) error {
		locale := c.CallbackQuery.Data[len("lang:"):]
		if err := c.SetLocale(locale); err != nil {
			return err
		}
		if err := c.AnswerCallback(""); err != nil {
			return err
		}
		_, err := c.EditText(c.T("greeting"))
		return err
	})
	r.Message(gg.FilterCommand("hello")).Handle(func(c *gg.Ctx) error {
		_, err := c.Answer(c.T("greeting"))
		return err
	})
	r.Message(gg.FilterCommand("apples")).Handle(func(c *gg.Ctx) error {
		n, err := strconv.Atoi(c.Command().Args)
		if err != nil {
			_, err := c.Answer("Usage: /apples <n>")
			return err
		}
		_, err = c.Answer(c.TN("apples", n, n))
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("i18n bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
