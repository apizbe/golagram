package golagram

import (
	"errors"
	"fmt"
	"testing"
)

// The description strings below are pinned to what the Bot API actually
// returns (observed live; also the strings telebot/PTB match on). If
// Telegram ever rewords them, IsMessageNotEditable silently stops matching
// and EditOrSend/EditOrReply lose their fallback — this test is the tripwire
// that makes such a change visible.
func TestIsMessageNotEditable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "message to edit not found",
			err:  &APIError{Code: 400, Description: "Bad Request: message to edit not found"},
			want: true,
		},
		{
			name: "message can't be edited",
			err:  &APIError{Code: 400, Description: "Bad Request: message can't be edited"},
			want: true,
		},
		{
			name: "wrapped still matches",
			err:  fmt.Errorf("editing menu: %w", &APIError{Code: 400, Description: "Bad Request: message can't be edited"}),
			want: true,
		},
		{
			name: "message is not modified is not a fallback case",
			err:  &APIError{Code: 400, Description: "Bad Request: message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message"},
			want: false,
		},
		{
			name: "other 400",
			err:  &APIError{Code: 400, Description: "Bad Request: chat not found"},
			want: false,
		},
		{
			name: "matching description under a different code",
			err:  &APIError{Code: 403, Description: "message can't be edited"},
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "non-API error",
			err:  errors.New("message can't be edited"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMessageNotEditable(tt.err); got != tt.want {
				t.Errorf("IsMessageNotEditable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
