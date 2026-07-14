package golagram

import (
	"strings"
	"testing"
)

func deepLinkBot() *TelegramBot {
	return &TelegramBot{me: &User{ID: 1000, IsBot: true, Username: "test_bot"}}
}

func TestCreateStartLink_Plain(t *testing.T) {
	b := deepLinkBot()
	link, err := b.CreateStartLink("ref_abc-123", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link != "https://t.me/test_bot?start=ref_abc-123" {
		t.Errorf("link = %q", link)
	}
}

func TestCreateStartGroupLink(t *testing.T) {
	b := deepLinkBot()
	link, err := b.CreateStartGroupLink("promo", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link != "https://t.me/test_bot?startgroup=promo" {
		t.Errorf("link = %q", link)
	}
}

func TestCreateStartLink_EncodedRoundTrip(t *testing.T) {
	b := deepLinkBot()
	payload := "user 42 → premium?"
	link, err := b.CreateStartLink(payload, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	encoded := strings.TrimPrefix(link, "https://t.me/test_bot?start=")
	if encoded == link {
		t.Fatalf("link %q missing expected prefix", link)
	}
	decoded, err := DecodeStartPayload(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != payload {
		t.Errorf("round trip = %q, want %q", decoded, payload)
	}
}

func TestCreateStartLink_Rejections(t *testing.T) {
	b := deepLinkBot()
	cases := []struct {
		name    string
		payload string
		encode  bool
	}{
		{"empty", "", false},
		{"bad char space", "two words", false},
		{"bad char unicode", "salom🙂", false},
		{"too long plain", strings.Repeat("a", 65), false},
		{"too long encoded", strings.Repeat("a", 49), true}, // 49 bytes -> 66 base64 chars
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := b.CreateStartLink(tc.payload, tc.encode); err == nil {
				t.Errorf("payload %q (encode=%v): expected an error", tc.payload, tc.encode)
			}
		})
	}
}

func TestCreateStartLink_MaxLengthBoundary(t *testing.T) {
	b := deepLinkBot()
	if _, err := b.CreateStartLink(strings.Repeat("a", 64), false); err != nil {
		t.Errorf("64 chars should pass: %v", err)
	}
	// 48 raw bytes encode to exactly 64 base64 chars.
	if _, err := b.CreateStartLink(strings.Repeat("x", 48), true); err != nil {
		t.Errorf("48 encoded bytes should pass: %v", err)
	}
}

func TestCreateStartLink_NoUsername(t *testing.T) {
	b := &TelegramBot{}
	if _, err := b.CreateStartLink("x", false); err == nil {
		t.Error("expected an error when the bot username is unknown")
	}
}

func TestDecodeStartPayload(t *testing.T) {
	if got, err := DecodeStartPayload("aGVsbG8"); err != nil || got != "hello" {
		t.Errorf("DecodeStartPayload(aGVsbG8) = %q, %v; want hello", got, err)
	}
	// Padded input (some encoders emit it) is accepted.
	if got, err := DecodeStartPayload("aGVsbG8="); err != nil || got != "hello" {
		t.Errorf("padded: got %q, %v; want hello", got, err)
	}
	if _, err := DecodeStartPayload("!!not-base64!!"); err == nil {
		t.Error("expected an error decoding invalid base64")
	}
}
