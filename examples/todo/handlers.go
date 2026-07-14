package main

import (
	"fmt"
	"strings"

	gg "github.com/apizbe/golagram"
)

const perPage = 5

// Toggle flips one task's done state; Delete removes it. Both carry the
// page they were tapped from, so re-rendering lands back on the same page.
type Toggle struct {
	ID   int
	Page int
}
type Delete struct {
	ID   int
	Page int
}

var (
	toggleCB = gg.NewCallbackData[Toggle]("toggle")
	deleteCB = gg.NewCallbackData[Delete]("del")
	pages    = gg.NewPagination("tasks")
)

// Handlers holds every dependency the bot's handlers need — just a Store
// here. A bigger bot would add a DB handle, a logger, config, etc. — every
// handler stays a plain method on this one struct.
type Handlers struct {
	store *Store
}

func (h *Handlers) Start(c *gg.Ctx) error {
	_, err := c.Answer("📝 Todo bot!\n\n" +
		"/add <task> — add a task\n" +
		"/list — show your tasks\n" +
		"/clear — remove completed tasks\n" +
		"/help — show this again")
	return err
}

func (h *Handlers) Help(c *gg.Ctx) error {
	return h.Start(c)
}

func (h *Handlers) Add(c *gg.Ctx) error {
	text := strings.TrimSpace(c.Command().Args)
	if text == "" {
		_, err := c.Answer("Usage: /add <task>")
		return err
	}
	h.store.Add(c.From().ID, text)
	_, err := c.Answer("Added ✅ — /list to see everything.")
	return err
}

func (h *Handlers) List(c *gg.Ctx) error {
	text, kb := h.render(c.From().ID, 1)
	_, err := c.Answer(text, &gg.SendMessageOptions{ReplyMarkup: kb})
	return err
}

func (h *Handlers) Clear(c *gg.Ctx) error {
	n := h.store.ClearDone(c.From().ID)
	_, err := c.Answer(fmt.Sprintf("Cleared %d completed task(s).", n))
	return err
}

// Toggle handles a tap on a task's checkbox.
func (h *Handlers) Toggle(c *gg.Ctx) error {
	v, err := toggleCB.FromCtx(c)
	if err != nil {
		return err
	}
	h.store.Toggle(c.From().ID, v.ID)
	return h.refresh(c, v.Page)
}

// Delete handles a tap on a task's 🗑 button.
func (h *Handlers) Delete(c *gg.Ctx) error {
	v, err := deleteCB.FromCtx(c)
	if err != nil {
		return err
	}
	h.store.Delete(c.From().ID, v.ID)
	return h.refresh(c, v.Page)
}

// Page handles a tap on the pagination row itself.
func (h *Handlers) Page(c *gg.Ctx) error {
	page, _ := pages.Page(c)
	return h.refresh(c, page)
}

func (h *Handlers) refresh(c *gg.Ctx, page int) error {
	text, kb := h.render(c.From().ID, page)
	if _, err := c.EditText(text, &gg.EditMessageOptions{ReplyMarkup: kb}); err != nil {
		return err
	}
	return c.AnswerCallback("")
}

// render builds the task list text and keyboard for one page. The keyboard
// is always non-nil — including an empty one once the last task is
// deleted — so it always explicitly clears any stale buttons instead of
// leaving a previous page's keyboard dangling.
func (h *Handlers) render(userID int64, page int) (string, *gg.InlineKeyboardMarkup) {
	tasks := h.store.List(userID)
	if len(tasks) == 0 {
		return "You have no tasks. Add one with /add <task>.", gg.NewInlineKeyboard().Build()
	}

	total := (len(tasks) + perPage - 1) / perPage
	page = min(max(page, 1), total)

	kb := gg.NewInlineKeyboard()
	start := (page - 1) * perPage
	end := min(start+perPage, len(tasks))
	for _, t := range tasks[start:end] {
		check := "⬜"
		if t.Done {
			check = "✅"
		}
		kb.Row(
			toggleCB.Button(check+" "+t.Text, Toggle{ID: t.ID, Page: page}),
			deleteCB.Button("🗑", Delete{ID: t.ID, Page: page}),
		)
	}
	if row := pages.Row(page, total); len(row) > 0 {
		kb.Row(row...)
	}

	return fmt.Sprintf("📝 Your tasks (page %d/%d):", page, total), kb.Build()
}
