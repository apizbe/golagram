package golagram

import "fmt"

// Media-group (album) limits enforced by [NewMediaGroup] before any
// network round-trip.
const (
	MinMediaGroupSize = 2
	MaxMediaGroupSize = 10
)

// MediaGroup builds the media array for [TelegramBot.SendMediaGroup] (an
// album) and enforces the rules Telegram otherwise reports as opaque 400s:
// 2–10 items, only photos/live photos/videos/documents/audio, and
// documents or audio never mixed with other types.
//
//	group := gg.NewMediaGroup().
//		Photo(gg.InputFileUpload("a.jpg", fileA), "first!").
//		Photo(gg.InputFileID(knownFileID)).
//		Video(gg.InputFileURL("https://example.com/c.mp4"))
//	msgs, err := group.Send(c)
//
// The typed methods cover the common cases; [MediaGroup.Add] takes a
// hand-built [InputMedia] item for the long tail (parse_mode, spoilers,
// thumbnails).
type MediaGroup struct {
	items []InputMedia
}

// NewMediaGroup starts an empty album builder.
func NewMediaGroup() *MediaGroup {
	return &MediaGroup{}
}

// Photo adds a photo, optionally captioned. Note only one item of an album
// needs a caption — it becomes the album's caption when it's the only one.
func (g *MediaGroup) Photo(media InputFile, caption ...string) *MediaGroup {
	return g.Add(&InputMediaPhoto{Media: media, Caption: first(caption)})
}

// Video adds a video, optionally captioned.
func (g *MediaGroup) Video(media InputFile, caption ...string) *MediaGroup {
	return g.Add(&InputMediaVideo{Media: media, Caption: first(caption)})
}

// LivePhoto adds a live photo — its motion video plus the static photo,
// optionally captioned. Live photos can't be sent by URL, only by file_id
// or upload.
func (g *MediaGroup) LivePhoto(video, photo InputFile, caption ...string) *MediaGroup {
	return g.Add(&InputMediaLivePhoto{Media: video, Photo: photo, Caption: first(caption)})
}

// Audio adds an audio track, optionally captioned. Telegram only allows
// audio to be grouped with other audio — [MediaGroup.Build] enforces it.
func (g *MediaGroup) Audio(media InputFile, caption ...string) *MediaGroup {
	return g.Add(&InputMediaAudio{Media: media, Caption: first(caption)})
}

// Document adds a document, optionally captioned. Telegram only allows
// documents to be grouped with other documents — [MediaGroup.Build]
// enforces it.
func (g *MediaGroup) Document(media InputFile, caption ...string) *MediaGroup {
	return g.Add(&InputMediaDocument{Media: media, Caption: first(caption)})
}

// Add appends a hand-built item, for options the typed methods don't cover
// (parse_mode, caption entities, spoilers, thumbnails, cover frames). The
// item's Type field may be left empty — Build fills the canonical value.
func (g *MediaGroup) Add(item InputMedia) *MediaGroup {
	g.items = append(g.items, item)
	return g
}

// Build validates the album and returns the media array for
// [SendMediaGroupRequest.Media]. It errors on anything Telegram would 400:
// fewer than 2 or more than 10 items, an item type albums don't allow
// (animations, paid media), or documents/audio mixed with anything else.
// It also fills each item's required Type discriminator when left empty —
// forgetting it is the classic silent album killer.
func (g *MediaGroup) Build() ([]InputMedia, error) {
	if len(g.items) < MinMediaGroupSize || len(g.items) > MaxMediaGroupSize {
		return nil, &ValidationError{
			Field:   "media",
			Message: fmt.Sprintf("a media group must include %d-%d items, got %d", MinMediaGroupSize, MaxMediaGroupSize, len(g.items)),
		}
	}

	var audio, documents int
	for _, item := range g.items {
		switch m := item.(type) {
		case *InputMediaPhoto:
			setIfEmpty(&m.Type, "photo")
		case *InputMediaVideo:
			setIfEmpty(&m.Type, "video")
		case *InputMediaLivePhoto:
			setIfEmpty(&m.Type, "live_photo")
		case *InputMediaAudio:
			setIfEmpty(&m.Type, "audio")
			audio++
		case *InputMediaDocument:
			setIfEmpty(&m.Type, "document")
			documents++
		default:
			return nil, &ValidationError{
				Field:   "media",
				Message: fmt.Sprintf("%T cannot be part of a media group (allowed: photo, live photo, video, document, audio)", item),
			}
		}
	}
	if audio > 0 && audio != len(g.items) {
		return nil, &ValidationError{Field: "media", Message: "audio can only be grouped with other audio"}
	}
	if documents > 0 && documents != len(g.items) {
		return nil, &ValidationError{Field: "media", Message: "documents can only be grouped with other documents"}
	}
	return g.items, nil
}

// Send builds the album and sends it into whichever chat this update
// relates to, propagating the source message's business connection and
// forum topic like [Ctx.Answer] does. Returns the sent messages (one per
// album item).
func (g *MediaGroup) Send(c *Ctx) ([]Message, error) {
	media, err := g.Build()
	if err != nil {
		return nil, err
	}
	chat := c.Chat()
	if chat == nil {
		return nil, fmt.Errorf("MediaGroup.Send: this update has no chat to send into")
	}
	req := &SendMediaGroupRequest{ChatID: ChatIDFromInt(chat.ID), Media: media}
	if m := c.anyMessage(); m != nil {
		req.BusinessConnectionID = m.BusinessConnectionID
		if m.IsTopicMessage {
			req.MessageThreadID = m.MessageThreadID
		}
	}
	return c.Bot().SendMediaGroup(c, req)
}

func first(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

func setIfEmpty(s *string, v string) {
	if *s == "" {
		*s = v
	}
}
