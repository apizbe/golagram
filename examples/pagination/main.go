// Command pagination shows a paged catalog built from gg.NewPagination and
// gg.NewCallbackData[T] together: a full « ‹ n/total › » nav row and typed,
// 64-byte-safe callback payloads, with zero hand-rolled string parsing. Run
// it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/pagination
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

type item struct {
	Name  string
	Price int
}

var catalog = []item{
	{"Apple", 1}, {"Banana", 1}, {"Cherry", 3}, {"Date", 4},
	{"Elderberry", 5}, {"Fig", 3}, {"Grape", 2}, {"Honeydew", 4},
	{"Kiwi", 2}, {"Lemon", 1}, {"Mango", 3}, {"Nectarine", 2},
}

const perPage = 4

// ViewItem opens one catalog entry's detail view; Page remembers where to
// go back to.
type ViewItem struct {
	ID   int
	Page int
}

// Back returns from a detail view to the catalog page it came from.
type Back struct {
	Page int
}

var (
	pages  = gg.NewPagination("catalog")
	viewCB = gg.NewCallbackData[ViewItem]("view")
	backCB = gg.NewCallbackData[Back]("back")
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
	r.Message(gg.FilterCommand("catalog")).Handle(func(c *gg.Ctx) error {
		text, kb := renderPage(1)
		_, err := c.Answer(text, &gg.SendMessageOptions{ReplyMarkup: kb})
		return err
	})
	r.CallbackQuery(pages.Filter()).Handle(func(c *gg.Ctx) error {
		page, _ := pages.Page(c)
		text, kb := renderPage(page)
		if _, err := c.EditText(text, &gg.EditMessageOptions{ReplyMarkup: kb}); err != nil {
			return err
		}
		return c.AnswerCallback("")
	})
	r.CallbackQuery(viewCB.Filter()).Handle(func(c *gg.Ctx) error {
		v, err := viewCB.FromCtx(c)
		if err != nil {
			return err
		}
		it := catalog[v.ID]
		text := fmt.Sprintf("%s — $%d", it.Name, it.Price)
		kb := gg.NewInlineKeyboard().
			Add(backCB.Button("« Back to catalog", Back{Page: v.Page})).
			Build()
		if _, err := c.EditText(text, &gg.EditMessageOptions{ReplyMarkup: kb}); err != nil {
			return err
		}
		return c.AnswerCallback("")
	})
	r.CallbackQuery(backCB.Filter()).Handle(func(c *gg.Ctx) error {
		b, err := backCB.FromCtx(c)
		if err != nil {
			return err
		}
		text, kb := renderPage(b.Page)
		if _, err := c.EditText(text, &gg.EditMessageOptions{ReplyMarkup: kb}); err != nil {
			return err
		}
		return c.AnswerCallback("")
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("pagination bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func renderPage(page int) (string, *gg.InlineKeyboardMarkup) {
	total := (len(catalog) + perPage - 1) / perPage
	page = min(max(page, 1), total)

	kb := gg.NewInlineKeyboard()
	start := (page - 1) * perPage
	end := min(start+perPage, len(catalog))
	for i := start; i < end; i++ {
		kb.Add(viewCB.Button(catalog[i].Name, ViewItem{ID: i, Page: page}))
	}
	kb.Row(pages.Row(page, total)...)

	return "🍎 Catalog — tap an item to see its price:", kb.Build()
}
