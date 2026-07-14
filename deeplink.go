package golagram

import (
	"encoding/base64"
	"fmt"
)

// MaxStartPayloadLength is Telegram's limit on a deep-link start payload
// (the part after ?start= / ?startgroup=), counted after encoding.
const MaxStartPayloadLength = 64

// EncodeStartPayload encodes arbitrary text into the URL-safe base64 form
// (no padding) that survives Telegram's deep-link charset restriction
// (A-Z a-z 0-9 _ -). Pair with [DecodeStartPayload] in the /start handler.
// 64 encoded characters fit 48 raw bytes — [TelegramBot.CreateStartLink]
// reports an error if the result is too long.
func EncodeStartPayload(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeStartPayload reverses [EncodeStartPayload] on the payload a deep
// link delivered (c.Command().Args in a /start handler). Padding is
// accepted and ignored, since some encoders include it.
func DecodeStartPayload(payload string) (string, error) {
	// RawURLEncoding rejects padded input; strip padding rather than fail.
	for len(payload) > 0 && payload[len(payload)-1] == '=' {
		payload = payload[:len(payload)-1]
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", &ValidationError{Field: "start payload", Message: "not valid URL-safe base64: " + err.Error()}
	}
	return string(raw), nil
}

// validateStartPayload enforces Telegram's deep-link payload rules: 1-64
// characters from A-Z a-z 0-9 _ -.
func validateStartPayload(payload string) error {
	if payload == "" {
		return &ValidationError{Field: "start payload", Message: "must not be empty (for a plain bot link, use https://t.me/<username> directly)"}
	}
	if len(payload) > MaxStartPayloadLength {
		return &ValidationError{Field: "start payload", Message: fmt.Sprintf("must be at most %d characters, got %d (an encoded payload fits %d raw bytes)", MaxStartPayloadLength, len(payload), MaxStartPayloadLength/4*3)}
	}
	for i := 0; i < len(payload); i++ {
		c := payload[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return &ValidationError{Field: "start payload", Message: fmt.Sprintf("character %q not allowed (only A-Z a-z 0-9 _ -); pass encode=true to base64-wrap arbitrary text", c)}
		}
	}
	return nil
}

// CreateStartLink returns a https://t.me/<bot>?start=<payload> deep link
// that opens a private chat with this bot and delivers the payload to the
// /start handler (route it with [FilterCommandStartDeepLink], read it via
// c.Command().Args).
//
// With encode=false the payload must already satisfy Telegram's rules
// (1-64 chars of A-Z a-z 0-9 _ -); with encode=true it is base64-wrapped
// first (decode with [DecodeStartPayload]), which fits 48 raw bytes.
func (b *TelegramBot) CreateStartLink(payload string, encode bool) (string, error) {
	return b.deepLink("start", payload, encode)
}

// CreateStartGroupLink is [TelegramBot.CreateStartLink] for the "add me to
// a group" flow: https://t.me/<bot>?startgroup=<payload>. Telegram shows a
// group picker, adds the bot, and delivers the payload like a /start deep
// link.
func (b *TelegramBot) CreateStartGroupLink(payload string, encode bool) (string, error) {
	return b.deepLink("startgroup", payload, encode)
}

func (b *TelegramBot) deepLink(param, payload string, encode bool) (string, error) {
	username := b.botUsername()
	if username == "" {
		return "", fmt.Errorf("create %s link: bot username unknown (getMe has not run)", param)
	}
	if encode {
		payload = EncodeStartPayload(payload)
	}
	if err := validateStartPayload(payload); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://t.me/%s?%s=%s", username, param, payload), nil
}
