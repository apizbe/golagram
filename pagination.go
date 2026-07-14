package golagram

import (
	"fmt"
	"strconv"
	"strings"
)

// Pagination builds the standard « ‹ 3/10 › » inline-keyboard navigation
// row and routes its taps, so a paged list is one keyboard row and one
// handler:
//
//	var pages = gg.NewPagination("results")
//
//	// sending page 1:
//	kb := gg.NewInlineKeyboardMarkup([][]gg.InlineKeyboardButton{pages.Row(1, total)})
//	c.Answer(renderPage(1), &gg.SendMessageOptions{ReplyMarkup: kb})
//
//	// handling navigation taps:
//	r.CallbackQuery(pages.Filter()).Handle(func(c *gg.Ctx) error {
//		page, _ := pages.Page(c)
//		kb := gg.NewInlineKeyboardMarkup([][]gg.InlineKeyboardButton{pages.Row(page, total)})
//		if _, err := c.EditText(renderPage(page), &gg.EditMessageOptions{ReplyMarkup: kb}); err != nil {
//			return err
//		}
//		return c.AnswerCallback("")
//	})
//
// The prefix namespaces the callback data, so several independent
// paginations coexist in one bot (and even one message).
type Pagination struct {
	prefix string
}

// NewPagination creates a pagination helper whose buttons carry callback
// data under the given prefix ("<prefix>:pg:<page>").
func NewPagination(prefix string) *Pagination {
	return &Pagination{prefix: prefix}
}

// Row returns the navigation row for the given 1-based page: first («) and
// previous (‹) on the left when there's anywhere to go back to, the
// "page/total" indicator in the middle, next (›) and last (») on the right.
// Buttons pointing nowhere are omitted, and jump buttons («/») only appear
// once they'd differ from the step buttons. Returns nil when total < 2 —
// a single page needs no navigation.
func (p *Pagination) Row(page, total int) []InlineKeyboardButton {
	if total < 2 {
		return nil
	}
	page = min(max(page, 1), total)

	var row []InlineKeyboardButton
	if page > 2 {
		row = append(row, NewInlineButton("« 1", p.data(1)))
	}
	if page > 1 {
		row = append(row, NewInlineButton("‹", p.data(page-1)))
	}
	row = append(row, InlineKeyboardButton{
		Text:         fmt.Sprintf("%d/%d", page, total),
		CallbackData: p.prefix + ":pgcur",
	})
	if page < total {
		row = append(row, NewInlineButton("›", p.data(page+1)))
	}
	if page < total-1 {
		row = append(row, NewInlineButton(fmt.Sprintf("%d »", total), p.data(total)))
	}
	return row
}

func (p *Pagination) data(page int) string {
	return fmt.Sprintf("%s:pg:%d", p.prefix, page)
}

// Filter matches callback queries from this pagination's navigation
// buttons — not the page indicator, which points nowhere. Combine it with
// other filters like any [Filter].
func (p *Pagination) Filter() Filter {
	return func(c *Ctx) bool {
		_, ok := p.Page(c)
		return ok
	}
}

// Page extracts the tapped target page from a navigation callback. The
// bool is false when the update isn't one of this pagination's navigation
// taps (wrong prefix, the indicator button, not a callback query at all).
func (p *Pagination) Page(c *Ctx) (int, bool) {
	if c.CallbackQuery == nil {
		return 0, false
	}
	rest, found := strings.CutPrefix(c.CallbackQuery.Data, p.prefix+":pg:")
	if !found {
		return 0, false
	}
	page, err := strconv.Atoi(rest)
	if err != nil || page < 1 {
		return 0, false
	}
	return page, true
}
