package golagramtest

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	gg "github.com/apizbe/golagram"
)

var (
	updateIDSeq   atomic.Int64
	messageIDSeq  atomic.Int64
	callbackIDSeq atomic.Int64
)

func nextUpdateID() int64   { return updateIDSeq.Add(1) }
func nextMessageID() int64  { return messageIDSeq.Add(1) }
func nextCallbackID() int64 { return callbackIDSeq.Add(1) }

// TextMessage builds an *gg.Update carrying a plain text message from
// userID in chatID, ready for [gg.TelegramBot.HandleUpdate].
func TextMessage(chatID, userID int64, text string) *gg.Update {
	return &gg.Update{
		UpdateID: nextUpdateID(),
		Message: &gg.Message{
			MessageID: nextMessageID(),
			Date:      time.Now().Unix(),
			Chat:      &gg.Chat{ID: chatID, Type: gg.ChatTypePrivate},
			From:      &gg.User{ID: userID, FirstName: "Test User"},
			Text:      text,
		},
	}
}

// CommandMessage builds an *gg.Update carrying "/command arg1 arg2" —
// matching what [gg.ParseCommand]/[gg.FilterCommand] expect.
func CommandMessage(chatID, userID int64, command string, args ...string) *gg.Update {
	text := "/" + command
	if len(args) > 0 {
		text += " " + strings.Join(args, " ")
	}
	return TextMessage(chatID, userID, text)
}

// CallbackQueryUpdate builds an *gg.Update carrying a callback query with
// the given data, including an attached originating [gg.Message] so
// handlers that read cq.Message (chat ID, message ID for edits, ...) work.
func CallbackQueryUpdate(chatID, userID int64, data string) *gg.Update {
	return &gg.Update{
		UpdateID: nextUpdateID(),
		CallbackQuery: &gg.CallbackQuery{
			ID:   fmt.Sprintf("cbq-%d", nextCallbackID()),
			From: &gg.User{ID: userID, FirstName: "Test User"},
			Message: &gg.Message{
				MessageID: nextMessageID(),
				Date:      time.Now().Unix(),
				Chat:      &gg.Chat{ID: chatID, Type: gg.ChatTypePrivate},
			},
			ChatInstance: fmt.Sprintf("%d", chatID),
			Data:         data,
		},
	}
}
