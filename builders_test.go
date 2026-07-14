package golagram

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Adjust ---

func rowSizes[T any](rows [][]T) []int {
	sizes := make([]int, len(rows))
	for i, r := range rows {
		sizes[i] = len(r)
	}
	return sizes
}

func sizesEqual(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestInlineKeyboard_Adjust(t *testing.T) {
	kb := NewInlineKeyboard()
	for i := range 7 {
		kb.Insert(NewInlineButton(string(rune('a'+i)), "d"))
	}

	markup := kb.Adjust(2).Build()
	if got := rowSizes(markup.InlineKeyboard); !sizesEqual(got, []int{2, 2, 2, 1}) {
		t.Errorf("Adjust(2) row sizes = %v, want [2 2 2 1]", got)
	}

	// Buttons stay in insertion order across the re-flow.
	if markup.InlineKeyboard[0][0].Text != "a" || markup.InlineKeyboard[3][0].Text != "g" {
		t.Errorf("Adjust reordered buttons: %+v", markup.InlineKeyboard)
	}
}

func TestInlineKeyboard_Adjust_LastSizeRepeats(t *testing.T) {
	kb := NewInlineKeyboard()
	for i := range 8 {
		kb.Insert(NewInlineButton(string(rune('a'+i)), "d"))
	}
	markup := kb.Adjust(1, 2, 3).Build()
	if got := rowSizes(markup.InlineKeyboard); !sizesEqual(got, []int{1, 2, 3, 2}) {
		t.Errorf("Adjust(1,2,3) over 8 = %v, want [1 2 3 2]", got)
	}
}

func TestInlineKeyboard_Adjust_ReplacesExistingRows(t *testing.T) {
	// Rows built via Row/Add get flattened before re-flowing.
	kb := NewInlineKeyboard().
		Row(NewInlineButton("a", "d"), NewInlineButton("b", "d"), NewInlineButton("c", "d")).
		Add(NewInlineButton("d", "d"))
	markup := kb.Adjust(2).Build()
	if got := rowSizes(markup.InlineKeyboard); !sizesEqual(got, []int{2, 2}) {
		t.Errorf("row sizes = %v, want [2 2]", got)
	}
}

func TestInlineKeyboard_Adjust_DefaultsAndDegenerates(t *testing.T) {
	kb := NewInlineKeyboard().Insert(NewInlineButton("a", "d")).Insert(NewInlineButton("b", "d"))
	if got := rowSizes(kb.Adjust().Build().InlineKeyboard); !sizesEqual(got, []int{1, 1}) {
		t.Errorf("Adjust() = %v, want one per row", got)
	}

	kb = NewInlineKeyboard().Insert(NewInlineButton("a", "d")).Insert(NewInlineButton("b", "d"))
	if got := rowSizes(kb.Adjust(0, -3).Build().InlineKeyboard); !sizesEqual(got, []int{1, 1}) {
		t.Errorf("non-positive sizes = %v, want treated as 1", got)
	}

	if got := NewInlineKeyboard().Adjust(3).Build(); len(got.InlineKeyboard) != 0 {
		t.Errorf("empty builder Adjust = %v, want no rows", got.InlineKeyboard)
	}
}

func TestReplyKeyboard_Adjust(t *testing.T) {
	kb := NewReplyKeyboard(true)
	for i := range 5 {
		kb.Insert(NewKeyboardButton(string(rune('a' + i))))
	}
	markup := kb.Adjust(3).Build()
	if got := rowSizes(markup.Keyboard); !sizesEqual(got, []int{3, 2}) {
		t.Errorf("row sizes = %v, want [3 2]", got)
	}
	if !markup.ResizeKeyboard {
		t.Error("Adjust must not clobber the builder's other settings")
	}
}

// --- Pagination ---

func paginationTexts(row []InlineKeyboardButton) []string {
	texts := make([]string, len(row))
	for i, b := range row {
		texts[i] = b.Text
	}
	return texts
}

func TestPagination_RowShapes(t *testing.T) {
	p := NewPagination("res")

	cases := []struct {
		name       string
		page, tot  int
		wantTexts  []string
		wantDataAt map[int]string // index in row -> callback data
	}{
		{"first page", 1, 5, []string{"1/5", "›", "5 »"}, map[int]string{1: "res:pg:2", 2: "res:pg:5"}},
		{"second page", 2, 5, []string{"‹", "2/5", "›", "5 »"}, map[int]string{0: "res:pg:1"}},
		{"middle page", 3, 5, []string{"« 1", "‹", "3/5", "›", "5 »"}, map[int]string{0: "res:pg:1", 1: "res:pg:2", 3: "res:pg:4", 4: "res:pg:5"}},
		{"second to last", 4, 5, []string{"« 1", "‹", "4/5", "›"}, map[int]string{3: "res:pg:5"}},
		{"last page", 5, 5, []string{"« 1", "‹", "5/5"}, map[int]string{1: "res:pg:4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := p.Row(c.page, c.tot)
			got := paginationTexts(row)
			if len(got) != len(c.wantTexts) {
				t.Fatalf("row = %v, want %v", got, c.wantTexts)
			}
			for i := range got {
				if got[i] != c.wantTexts[i] {
					t.Fatalf("row = %v, want %v", got, c.wantTexts)
				}
			}
			for i, data := range c.wantDataAt {
				if row[i].CallbackData != data {
					t.Errorf("button %d data = %q, want %q", i, row[i].CallbackData, data)
				}
			}
		})
	}
}

func TestPagination_SinglePageAndClamping(t *testing.T) {
	p := NewPagination("res")
	if row := p.Row(1, 1); row != nil {
		t.Errorf("single page row = %v, want nil", row)
	}
	if row := p.Row(99, 5); paginationTexts(row)[2] != "5/5" {
		t.Errorf("out-of-range page must clamp to the last, got %v", paginationTexts(row))
	}
}

func TestPagination_FilterAndPage(t *testing.T) {
	p := NewPagination("res")

	nav := cbCtx(&CallbackQuery{Data: "res:pg:3"})
	if !p.Filter()(nav) {
		t.Error("Filter must match this pagination's navigation callback")
	}
	if page, ok := p.Page(nav); !ok || page != 3 {
		t.Errorf("Page = (%d, %v), want (3, true)", page, ok)
	}

	for name, u := range map[string]*Ctx{
		"indicator button":  cbCtx(&CallbackQuery{Data: "res:pgcur"}),
		"other prefix":      cbCtx(&CallbackQuery{Data: "other:pg:3"}),
		"garbage page":      cbCtx(&CallbackQuery{Data: "res:pg:xyz"}),
		"not a callback":    msgCtx(&Message{Text: "res:pg:3"}),
		"zero page forgery": cbCtx(&CallbackQuery{Data: "res:pg:0"}),
	} {
		if p.Filter()(u) {
			t.Errorf("%s must not match the pagination filter", name)
		}
	}
}

// --- MediaGroup ---

func TestMediaGroup_BuildHappyPath(t *testing.T) {
	media, err := NewMediaGroup().
		Photo(InputFileID("photo1"), "album caption").
		Video(InputFileURL("https://example.com/v.mp4")).
		LivePhoto(InputFileID("vid"), InputFileID("still")).
		Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(media) != 3 {
		t.Fatalf("got %d items, want 3", len(media))
	}

	photo := media[0].(*InputMediaPhoto)
	if photo.Type != "photo" || photo.Caption != "album caption" {
		t.Errorf("photo = %+v, want type auto-filled and caption set", photo)
	}
	if media[1].(*InputMediaVideo).Type != "video" {
		t.Error("video type must be auto-filled")
	}
	if lp := media[2].(*InputMediaLivePhoto); lp.Type != "live_photo" {
		t.Error("live photo type must be auto-filled")
	}
}

func TestMediaGroup_AddKeepsExplicitType(t *testing.T) {
	media, err := NewMediaGroup().
		Add(&InputMediaPhoto{Type: "photo", Media: InputFileID("a"), HasSpoiler: true}).
		Photo(InputFileID("b")).
		Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !media[0].(*InputMediaPhoto).HasSpoiler {
		t.Error("Add must pass hand-built items through untouched")
	}
}

func TestMediaGroup_SizeRules(t *testing.T) {
	if _, err := NewMediaGroup().Photo(InputFileID("only")).Build(); err == nil {
		t.Error("1 item must fail the 2-10 rule")
	}
	g := NewMediaGroup()
	for range 11 {
		g.Photo(InputFileID("x"))
	}
	if _, err := g.Build(); err == nil {
		t.Error("11 items must fail the 2-10 rule")
	}
}

func TestMediaGroup_HomogeneityRules(t *testing.T) {
	if _, err := NewMediaGroup().Audio(InputFileID("a")).Photo(InputFileID("b")).Build(); err == nil {
		t.Error("audio mixed with photo must fail")
	}
	if _, err := NewMediaGroup().Document(InputFileID("a")).Video(InputFileID("b")).Build(); err == nil {
		t.Error("document mixed with video must fail")
	}
	if _, err := NewMediaGroup().Audio(InputFileID("a")).Audio(InputFileID("b")).Build(); err != nil {
		t.Errorf("all-audio album must pass, got %v", err)
	}
	if _, err := NewMediaGroup().Document(InputFileID("a")).Document(InputFileID("b")).Build(); err != nil {
		t.Errorf("all-document album must pass, got %v", err)
	}
}

func TestMediaGroup_RejectsNonAlbumTypes(t *testing.T) {
	_, err := NewMediaGroup().
		Photo(InputFileID("a")).
		Add(&InputMediaAnimation{Media: InputFileID("b")}).
		Build()
	if err == nil {
		t.Error("animations are not allowed in media groups")
	}
}

func TestMediaGroup_Send(t *testing.T) {
	var gotChatID float64
	var gotThreadID float64
	var itemCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		gotChatID, _ = body["chat_id"].(float64)
		gotThreadID, _ = body["message_thread_id"].(float64)
		if media, ok := body["media"].([]any); ok {
			itemCount = len(media)
		}
		w.Write([]byte(`{"ok":true,"result":[
			{"message_id":1,"date":5,"chat":{"id":10,"type":"supergroup"}},
			{"message_id":2,"date":5,"chat":{"id":10,"type":"supergroup"}}
		]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)
	msg := bindMessage(&Message{
		MessageID: 7, Chat: &Chat{ID: 10}, From: &User{ID: 20},
		IsTopicMessage: true, MessageThreadID: 33,
	}, bot)
	c := ctxForBot(bot, &Update{Message: msg})

	msgs, err := NewMediaGroup().
		Photo(InputFileID("a")).
		Photo(InputFileID("b")).
		Send(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("got %d sent messages, want 2", len(msgs))
	}
	if int64(gotChatID) != 10 || itemCount != 2 {
		t.Errorf("request had chat_id=%v media items=%d, want 10 and 2", gotChatID, itemCount)
	}
	if int64(gotThreadID) != 33 {
		t.Errorf("message_thread_id = %v, want the source topic 33 propagated", gotThreadID)
	}
}

func TestMediaGroup_Send_ChatlessAndInvalid(t *testing.T) {
	c := ctxFor(&Update{InlineQuery: &InlineQuery{ID: "q", From: &User{ID: 1}}})
	if _, err := NewMediaGroup().Photo(InputFileID("a")).Photo(InputFileID("b")).Send(c); err == nil {
		t.Error("sending into a chatless update must fail")
	}

	c2 := ctxFor(&Update{Message: &Message{Chat: &Chat{ID: 1}, Date: 5}})
	if _, err := NewMediaGroup().Photo(InputFileID("a")).Send(c2); err == nil {
		t.Error("a Build validation error must surface through Send before any network call")
	}
}
