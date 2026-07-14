package golagram

import (
	"encoding/json"
	"testing"
)

func TestKeyboardButtonWebApp_SerializesAsObject(t *testing.T) {
	btn := NewKeyboardButtonWebApp("Open", "https://example.com/app")

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

func TestReplyKeyboard_RowAndBuild(t *testing.T) {
	kb := NewReplyKeyboard(true).
		Row(NewKeyboardButton("A"), NewKeyboardButton("B")).
		Row(NewKeyboardButton("C")).
		OneTime(true).
		Placeholder("type here").
		Selective(true).
		Build()

	if !kb.ResizeKeyboard {
		t.Error("expected ResizeKeyboard to be true")
	}
	if !kb.OneTimeKeyboard {
		t.Error("expected OneTimeKeyboard to be true")
	}
	if kb.InputFieldPlaceholder != "type here" {
		t.Errorf("got placeholder %q, want %q", kb.InputFieldPlaceholder, "type here")
	}
	if !kb.Selective {
		t.Error("expected Selective to be true")
	}

	if len(kb.Keyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.Keyboard))
	}
	if len(kb.Keyboard[0]) != 2 || kb.Keyboard[0][0].Text != "A" || kb.Keyboard[0][1].Text != "B" {
		t.Errorf("unexpected row 0: %+v", kb.Keyboard[0])
	}
	if len(kb.Keyboard[1]) != 1 || kb.Keyboard[1][0].Text != "C" {
		t.Errorf("unexpected row 1: %+v", kb.Keyboard[1])
	}
}

func TestReplyKeyboard_InsertBuildsCurrentRow(t *testing.T) {
	kb := NewReplyKeyboard(false).
		Insert(NewKeyboardButton("A")).
		Insert(NewKeyboardButton("B")).
		Build()

	if len(kb.Keyboard) != 1 {
		t.Fatalf("expected 1 row from Insert calls, got %d", len(kb.Keyboard))
	}
	if len(kb.Keyboard[0]) != 2 {
		t.Fatalf("expected 2 buttons in the row, got %d", len(kb.Keyboard[0]))
	}
}

func TestReplyKeyboard_Add_OneButtonPerRow(t *testing.T) {
	kb := NewReplyKeyboard(false).
		Add(NewKeyboardButton("A"), NewKeyboardButton("B")).
		Build()

	if len(kb.Keyboard) != 2 {
		t.Fatalf("expected 2 rows (one per button), got %d", len(kb.Keyboard))
	}
}

func TestQuickReplyKeyboard(t *testing.T) {
	kb := QuickReplyKeyboard([][]string{{"1", "2"}, {"3"}}, true)

	if len(kb.Keyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.Keyboard))
	}
	if kb.Keyboard[0][0].Text != "1" || kb.Keyboard[0][1].Text != "2" {
		t.Errorf("unexpected row 0: %+v", kb.Keyboard[0])
	}
	if kb.Keyboard[1][0].Text != "3" {
		t.Errorf("unexpected row 1: %+v", kb.Keyboard[1])
	}
}

func TestQuickReplyRow(t *testing.T) {
	kb := QuickReplyRow("A", "B", "C")

	if len(kb.Keyboard) != 1 {
		t.Fatalf("expected a single row, got %d rows", len(kb.Keyboard))
	}
	if len(kb.Keyboard[0]) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(kb.Keyboard[0]))
	}
}

func TestRemoveKeyboard(t *testing.T) {
	rm := RemoveKeyboard(true)
	if !rm.RemoveKeyboard {
		t.Error("expected RemoveKeyboard to be true")
	}
	if !rm.Selective {
		t.Error("expected Selective to be true")
	}
}

func TestKeyboardButtonConstructors(t *testing.T) {
	if b := NewKeyboardButtonContact("share"); !b.RequestContact {
		t.Error("expected RequestContact to be set")
	}
	if b := NewKeyboardButtonLocation("loc"); !b.RequestLocation {
		t.Error("expected RequestLocation to be set")
	}
	if b := NewKeyboardButtonPoll("poll", "quiz"); b.RequestPoll == nil || b.RequestPoll.Type != "quiz" {
		t.Error("expected RequestPoll to be set to quiz type")
	}
}
