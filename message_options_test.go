package golagram

import (
	"encoding/json"
	"testing"
)

// SendMessageOptions.applyTo bridges the sugar option bag into the generated
// SendMessageRequest; every option must land on its request field.
func TestSendMessageOptions_ApplyTo(t *testing.T) {
	keyboard := NewInlineKeyboard().Row(NewInlineButton("OK", "ok")).Build()

	o := &SendMessageOptions{
		ParseMode:            "HTML",
		DisableNotification:  true,
		ProtectContent:       true,
		MessageEffectID:      "fx",
		MessageThreadID:      77,
		ReplyParameters:      &ReplyParameters{MessageID: 42},
		ReplyMarkup:          keyboard,
		BusinessConnectionID: "biz",
	}
	req := &SendMessageRequest{ChatID: ChatIDFromInt(100), Text: "hello"}
	o.applyTo(req)

	if req.ParseMode != "HTML" || !req.DisableNotification || !req.ProtectContent ||
		req.MessageEffectID != "fx" || req.MessageThreadID != 77 ||
		req.ReplyParameters == nil || req.ReplyParameters.MessageID != 42 ||
		req.ReplyMarkup == nil || req.BusinessConnectionID != "biz" {
		t.Errorf("applyTo lost options: %+v", req)
	}

	// nil options are a no-op, not a panic.
	var nilOpts *SendMessageOptions
	nilOpts.applyTo(req)
}

// The generated request must marshal as one flat JSON object exactly shaped
// like Telegram's sendMessage parameters — with unset optionals omitted so
// Telegram's defaults win.
func TestSendMessageRequest_MarshalsFlatAndOmitsUnset(t *testing.T) {
	req := &SendMessageRequest{ChatID: ChatIDFromInt(100), Text: "hello"}
	(&SendMessageOptions{ParseMode: "HTML", ReplyParameters: &ReplyParameters{MessageID: 42}}).applyTo(req)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var flat map[string]any
	if err := json.Unmarshal(raw, &flat); err != nil {
		t.Fatalf("failed to re-decode: %v", err)
	}

	if flat["chat_id"].(float64) != 100 || flat["text"] != "hello" || flat["parse_mode"] != "HTML" {
		t.Errorf("required/option fields wrong: %v", flat)
	}
	rp, ok := flat["reply_parameters"].(map[string]any)
	if !ok || rp["message_id"].(float64) != 42 {
		t.Errorf("reply_parameters wrong: %v", flat["reply_parameters"])
	}
	if len(flat) != 4 {
		t.Errorf("expected exactly chat_id, text, parse_mode, reply_parameters — got: %v", flat)
	}
}

func TestEditMessageTextRequest_MarshalsFlatJSON(t *testing.T) {
	keyboard := NewInlineKeyboard().Row(NewInlineButton("OK", "ok")).Build()

	raw, err := json.Marshal(&EditMessageTextRequest{
		ChatID:      ChatIDFromInt(7),
		MessageID:   99,
		Text:        "edited",
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var flat map[string]any
	json.Unmarshal(raw, &flat)

	if flat["chat_id"].(float64) != 7 || flat["message_id"].(float64) != 99 ||
		flat["text"] != "edited" || flat["parse_mode"] != "HTML" || flat["reply_markup"] == nil {
		t.Errorf("unexpected marshaled edit request: %v", flat)
	}
}
