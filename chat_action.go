package golagram

import (
	"sync"
	"time"
)

// ChatAction* constants ([ChatActionTyping], ChatActionUploadPhoto, ...) are
// in consts.gen.go.

// chatActionRefreshInterval is how often KeepChatAction re-sends the
// action. Telegram clears an action after ~5 seconds, so 4 keeps it
// visibly continuous without spamming the API. A var only so tests can
// shrink it.
var chatActionRefreshInterval = 4 * time.Second

// KeepChatAction shows a chat action ("typing", "upload_photo", ...) and
// keeps it alive by re-sending it every ~4s — Telegram clears an action
// after 5 seconds, so a single [Ctx.SendChatAction] goes blank during any
// real work. It stops when the returned function is called or the Ctx is
// canceled, whichever comes first:
//
//	stop := gg.KeepChatAction(c, gg.ChatActionUploadPhoto)
//	defer stop()
//	// ... render the image, then send it ...
//
// Sends are best-effort: a failed refresh (flood limit, chat gone) is
// dropped, not surfaced — the action is cosmetic, the handler's real sends
// will report anything that matters.
func KeepChatAction(c *Ctx, action string) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	stop = func() { once.Do(func() { close(done) }) }

	go func() {
		_ = c.SendChatAction(action)
		ticker := time.NewTicker(chatActionRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-c.Done():
				return
			case <-ticker.C:
				_ = c.SendChatAction(action)
			}
		}
	}()
	return stop
}

// Typing is [KeepChatAction] with the "typing" action — the common case:
//
//	defer gg.Typing(c)()
func Typing(c *Ctx) (stop func()) {
	return KeepChatAction(c, ChatActionTyping)
}
