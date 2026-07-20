package golagram

import (
	"errors"
	"strings"

	"github.com/apizbe/golagram/internal/api"
)

// APIError is a typed Telegram Bot API error. Every failed API call returns
// one (wrapped or not), carrying the error code, Telegram's description, and
// the actionable parameters: RetryAfter on 429 flood control and
// MigrateToChatID when a group was upgraded to a supergroup.
type APIError = api.Error

// AsAPIError extracts the *APIError from err, if there is one.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// IsFlood reports whether err is Telegram's 429 flood-control error.
// Use RetryAfter from [AsAPIError] for the wait duration.
func IsFlood(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == 429
}

// IsBlockedByUser reports whether err means the user blocked the bot —
// the signal to stop messaging (and usually to drop them from broadcasts).
func IsBlockedByUser(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == 403 &&
		strings.Contains(apiErr.Description, "blocked by the user")
}

// IsChatNotFound reports whether err means the target chat doesn't exist
// (or the bot has never seen it).
func IsChatNotFound(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == 400 &&
		strings.Contains(apiErr.Description, "chat not found")
}

// IsMessageNotEditable reports whether err means the edit target is gone or
// off-limits: Telegram's "message to edit not found" (deleted, or never
// existed) and "message can't be edited" (too old, someone else's message,
// or an inaccessible callback message). [Ctx.EditOrSend]/[Ctx.EditOrReply]
// fall back to sending a fresh message exactly when this is true. Note
// "message is not modified" is deliberately not included — the message is
// alive and already shows this content, so sending fresh would duplicate it.
func IsMessageNotEditable(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == 400 &&
		(strings.Contains(apiErr.Description, "message to edit not found") ||
			strings.Contains(apiErr.Description, "message can't be edited"))
}

// IsConflict reports whether err is Telegram's 409 Conflict — another
// getUpdates long-poll (or a webhook) is already active for this bot token.
// [TelegramBot.Run] logs this distinctly from a generic getUpdates
// failure, since the fix (stop the other instance, or wait out a rolling
// deploy) is different from a transient network error.
func IsConflict(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == 409
}
