package golagram

import (
	"fmt"
	"testing"
)

// BenchmarkRouterDispatch_SingleRoute is the baseline: one registration,
// immediate match — mostly measures Ctx construction and filter/handler
// call overhead, not registration-scan cost.
func BenchmarkRouterDispatch_SingleRoute(b *testing.B) {
	r := NewRouter()
	r.Message(FilterCommand("start")).Handle(func(c *Ctx) error { return nil })

	upd := &Update{Message: &Message{Text: "/start"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.dispatch(ctxFor(upd)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRouterDispatch_ManyRoutes_LastMatch registers 20 routes where
// only the last one matches — the worst case for a router that matches
// something: every earlier filter still runs and fails first.
func BenchmarkRouterDispatch_ManyRoutes_LastMatch(b *testing.B) {
	r := NewRouter()
	for i := range 19 {
		cmd := fmt.Sprintf("cmd%d", i)
		r.Message(FilterCommand(cmd)).Handle(func(c *Ctx) error { return nil })
	}
	r.Message(FilterCommand("target")).Handle(func(c *Ctx) error { return nil })

	upd := &Update{Message: &Message{Text: "/target"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.dispatch(ctxFor(upd)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRouterDispatch_ManyRoutes_NoMatch is the worst case overall:
// 20 routes, none match, so dispatch runs every filter and still misses.
func BenchmarkRouterDispatch_ManyRoutes_NoMatch(b *testing.B) {
	r := NewRouter()
	for i := range 20 {
		cmd := fmt.Sprintf("cmd%d", i)
		r.Message(FilterCommand(cmd)).Handle(func(c *Ctx) error { return nil })
	}

	upd := &Update{Message: &Message{Text: "/nomatch"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.dispatch(ctxFor(upd)); err != nil {
			b.Fatal(err)
		}
	}
}
