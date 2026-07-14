package golagram

import (
	"encoding/json"
	"testing"
)

// Telegram's Bot API rejects web_app/login_url as bare strings with
// BUTTON_TYPE_INVALID — they must serialize as objects ({"url": "..."}).
func TestInlineButtonWebApp_SerializesAsObject(t *testing.T) {
	btn := NewInlineButtonWebApp("Open", "https://example.com/app")

	data, err := json.Marshal(btn)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	webApp, ok := decoded["web_app"].(map[string]any)
	if !ok {
		t.Fatalf("web_app should serialize as an object, got: %s", data)
	}
	if webApp["url"] != "https://example.com/app" {
		t.Errorf("web_app.url = %v, want https://example.com/app", webApp["url"])
	}
}

func TestInlineKeyboard_RowAndBuild(t *testing.T) {
	kb := NewInlineKeyboard().
		Row(NewInlineButton("A", "data_a"), NewInlineButton("B", "data_b")).
		Row(NewInlineButtonURL("C", "https://example.com")).
		Build()

	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.InlineKeyboard))
	}

	row0 := kb.InlineKeyboard[0]
	if len(row0) != 2 || row0[0].Text != "A" || row0[0].CallbackData != "data_a" {
		t.Errorf("unexpected row 0: %+v", row0)
	}

	row1 := kb.InlineKeyboard[1]
	if len(row1) != 1 || row1[0].URL != "https://example.com" {
		t.Errorf("unexpected row 1: %+v", row1)
	}
}

func TestInlineKeyboard_InsertBuildsCurrentRow(t *testing.T) {
	kb := NewInlineKeyboard().
		Insert(NewInlineButton("A", "a")).
		Insert(NewInlineButton("B", "b")).
		Build()

	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row from Insert calls, got %d", len(kb.InlineKeyboard))
	}
	if len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected 2 buttons in the row, got %d", len(kb.InlineKeyboard[0]))
	}
}

func TestInlineKeyboard_Add_OneButtonPerRow(t *testing.T) {
	kb := NewInlineKeyboard().
		Add(NewInlineButton("A", "a"), NewInlineButton("B", "b")).
		Build()

	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows (one per button), got %d", len(kb.InlineKeyboard))
	}
}

func TestInlineButtonConstructors(t *testing.T) {
	if b := NewInlineButtonWebApp("App", "https://app.example.com"); b.WebApp == nil || b.WebApp.URL != "https://app.example.com" {
		t.Error("expected WebApp to be set")
	}
	if b := NewInlineButtonSwitchInline("Search", "query"); b.SwitchInlineQuery != "query" {
		t.Error("expected SwitchInlineQuery to be set")
	}
	if b := NewInlineButtonSwitchInlineCurrent("Search here", "q"); b.SwitchInlineQueryCurrentChat != "q" {
		t.Error("expected SwitchInlineQueryCurrentChat to be set")
	}
}

func TestQuickInlineKeyboard(t *testing.T) {
	kb := QuickInlineKeyboard([][]string{{"Yes", "yes"}, {"No", "no"}})

	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.InlineKeyboard))
	}
	if kb.InlineKeyboard[0][0].Text != "Yes" || kb.InlineKeyboard[0][0].CallbackData != "yes" {
		t.Errorf("unexpected row 0: %+v", kb.InlineKeyboard[0])
	}
}

func TestQuickInlineRow(t *testing.T) {
	kb := QuickInlineRow(
		struct{ text, data string }{"Yes", "yes"},
		struct{ text, data string }{"No", "no"},
	)

	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected a single row, got %d rows", len(kb.InlineKeyboard))
	}
	if len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(kb.InlineKeyboard[0]))
	}
}
