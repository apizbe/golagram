package main

import (
	"log/slog"

	gg "github.com/apizbe/golagram"
)

// newRouter wires every route in one place: each line reads as
// "on this trigger, call this handler method" — r.Message(filter).Handle(h.Method).
// Handlers stay named methods (handlers.go), never inline closures, so this
// file is the bot's whole route table at a glance.
func newRouter(h *Handlers) *gg.Router {
	r := gg.NewRouter()
	r.Use(gg.LoggingMiddleware(slog.Default()))
	r.Use(gg.CallbackAnswerMiddleware())

	r.Message(gg.FilterCommand("start")).Handle(h.Start)
	r.Message(gg.FilterCommand("help")).Handle(h.Help)
	r.Message(gg.FilterCommand("add")).Handle(h.Add)
	r.Message(gg.FilterCommand("list")).Handle(h.List)
	r.Message(gg.FilterCommand("clear")).Handle(h.Clear)

	r.CallbackQuery(toggleCB.Filter()).Handle(h.Toggle)
	r.CallbackQuery(deleteCB.Filter()).Handle(h.Delete)
	r.CallbackQuery(pages.Filter()).Handle(h.Page)

	return r
}
