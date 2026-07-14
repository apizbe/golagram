package golagram

import (
	"fmt"
	"regexp"
	"unicode/utf16"
)

// Telegram Bot API hard limits enforced before any network round-trip.
const (
	// MaxTextLength is the maximum length of a message text, in characters.
	MaxTextLength = 4096
	// MaxCallbackDataLength is the maximum size of a callback button's data,
	// in bytes.
	MaxCallbackDataLength = 64
)

// ValidationError is returned by send methods when a request would be
// rejected by Telegram, before any network call is made.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// tokenPattern matches a bot token's shape: the numeric bot ID BotFather
// assigns, a colon, then the 35-character secret. It doesn't (and can't)
// check the token is real — only that it has the right shape.
var tokenPattern = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{35}$`)

// ValidateToken checks a bot token's format — "<bot_id>:<35-character
// secret>" — without making a network call. [NewTelegramBot] calls this
// before its first request (getMe), so a malformed token (empty string, a
// pasted secret missing the bot-ID prefix, a stray space) fails immediately
// with a message that says what's wrong, instead of a generic HTTP 404 from
// Telegram a few hundred milliseconds later.
func ValidateToken(token string) error {
	if !tokenPattern.MatchString(token) {
		return &ValidationError{
			Field:   "token",
			Message: `does not match the expected "<bot_id>:<35-character secret>" format (get a valid one from @BotFather)`,
		}
	}
	return nil
}

// validateOutgoingText pre-flights a message text against Telegram's limits.
// Length is counted in UTF-16 code units, the unit Telegram actually
// enforces (an emoji counts as 2) — a rune count would under-count and let
// through texts Telegram rejects. [SplitText] measures the same way, so its
// chunks always pass here.
func validateOutgoingText(text string) error {
	if text == "" {
		return &ValidationError{Field: "text", Message: "cannot be empty"}
	}
	if units := len(utf16.Encode([]rune(text))); units > MaxTextLength {
		return &ValidationError{
			Field:   "text",
			Message: fmt.Sprintf("length %d exceeds Telegram's maximum of %d characters", units, MaxTextLength),
		}
	}
	return nil
}

// validateReplyMarkup pre-flights the parts of a markup Telegram rejects
// hard: today that is callback_data over 64 bytes, the classic silent bot
// killer (buttons simply don't send).
func validateReplyMarkup(markup ReplyMarkup) error {
	inline, ok := markup.(*InlineKeyboardMarkup)
	if !ok || inline == nil {
		return nil
	}
	for _, row := range inline.InlineKeyboard {
		for _, btn := range row {
			if len(btn.CallbackData) > MaxCallbackDataLength {
				return &ValidationError{
					Field: "callback_data",
					Message: fmt.Sprintf("button %q: %d bytes exceeds Telegram's maximum of %d",
						btn.Text, len(btn.CallbackData), MaxCallbackDataLength),
				}
			}
		}
	}
	return nil
}
