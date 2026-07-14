package golagram

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateToken(t *testing.T) {
	valid := "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 35-char secret
	if err := ValidateToken(valid); err != nil {
		t.Errorf("expected a well-shaped token to pass, got: %v", err)
	}

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no colon", "123456789AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"non-numeric bot ID", "abc:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"secret too short", "123456789:AAAA"},
		{"secret too long", "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"secret has invalid character", "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA!"},
		{"trailing space", valid + " "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateToken(c.token); err == nil {
				t.Errorf("expected %q to be rejected", c.token)
			}
		})
	}
}

func TestValidateOutgoingText(t *testing.T) {
	t.Run("accepts normal text", func(t *testing.T) {
		if err := validateOutgoingText("hello"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects empty text", func(t *testing.T) {
		err := validateOutgoingText("")
		var vErr *ValidationError
		if !errors.As(err, &vErr) || vErr.Field != "text" {
			t.Errorf("expected ValidationError on text, got %v", err)
		}
	})

	t.Run("accepts text at the limit", func(t *testing.T) {
		if err := validateOutgoingText(strings.Repeat("a", MaxTextLength)); err != nil {
			t.Errorf("unexpected error at exactly %d chars: %v", MaxTextLength, err)
		}
	})

	t.Run("rejects text over the limit", func(t *testing.T) {
		err := validateOutgoingText(strings.Repeat("a", MaxTextLength+1))
		var vErr *ValidationError
		if !errors.As(err, &vErr) {
			t.Errorf("expected ValidationError, got %v", err)
		}
	})
}

func TestValidateReplyMarkup(t *testing.T) {
	t.Run("nil markup passes", func(t *testing.T) {
		if err := validateReplyMarkup(nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid inline keyboard passes", func(t *testing.T) {
		kb := NewInlineKeyboard().Row(NewInlineButton("OK", "ok")).Build()
		if err := validateReplyMarkup(kb); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("callback_data over 64 bytes is rejected before any network call", func(t *testing.T) {
		kb := NewInlineKeyboard().
			Row(NewInlineButton("Too big", strings.Repeat("x", MaxCallbackDataLength+1))).
			Build()

		err := validateReplyMarkup(kb)
		var vErr *ValidationError
		if !errors.As(err, &vErr) || vErr.Field != "callback_data" {
			t.Errorf("expected ValidationError on callback_data, got %v", err)
		}
	})

	t.Run("reply keyboards are not callback-checked", func(t *testing.T) {
		kb := QuickReplyRow("A", "B")
		if err := validateReplyMarkup(kb); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in     string
		want   *CommandObject
		wantOK bool
	}{
		{"/start", &CommandObject{Command: "start"}, true},
		{"/start@my_bot", &CommandObject{Command: "start", Mention: "my_bot"}, true},
		{"/start ref_12345", &CommandObject{Command: "start", Args: "ref_12345"}, true},
		{"/start@my_bot ref_12345", &CommandObject{Command: "start", Mention: "my_bot", Args: "ref_12345"}, true},
		{"/help arg1 arg2", &CommandObject{Command: "help", Args: "arg1 arg2"}, true},
		{"hello", nil, false},
		{"", nil, false},
		{"/", nil, false},
		{"/@my_bot", nil, false},
	}

	for _, c := range cases {
		got, ok := ParseCommand(c.in)
		if ok != c.wantOK {
			t.Errorf("ParseCommand(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if *got != *c.want {
			t.Errorf("ParseCommand(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestMessage_Command(t *testing.T) {
	m := &Message{Text: "/start ref_777"}
	cmd := m.Command()
	if cmd == nil || cmd.Command != "start" || cmd.Args != "ref_777" {
		t.Errorf("unexpected CommandObject: %+v", cmd)
	}

	if (&Message{Text: "not a command"}).Command() != nil {
		t.Error("expected nil CommandObject for non-command text")
	}
}

func TestValidateOutgoingText_CountsUTF16Units(t *testing.T) {
	// 2049 emoji = 2049 runes but 4098 UTF-16 units — over Telegram's real
	// limit, and must be rejected before the network call.
	if err := validateOutgoingText(strings.Repeat("😀", 2049)); err == nil {
		t.Error("4098 UTF-16 units must fail validation even though the rune count is under 4096")
	}
	if err := validateOutgoingText(strings.Repeat("😀", 2048)); err != nil {
		t.Errorf("4096 UTF-16 units must pass, got %v", err)
	}
}
