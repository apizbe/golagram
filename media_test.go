package golagram

import (
	"encoding/json"
	"testing"
)

// unmarshalMessage builds a realistic Telegram update envelope (message_id,
// date, chat, from) merged with the given content fields, and parses it the
// same way an incoming getUpdates payload would be.
func unmarshalMessage(t *testing.T, contentJSON string) *Message {
	t.Helper()
	raw := `{
		"message_id": 1,
		"date": 1700000000,
		"chat": {"id": 100, "type": "private"},
		"from": {"id": 200, "first_name": "Test"},
		` + contentJSON + `
	}`

	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}
	return &m
}

func TestMessage_Photo(t *testing.T) {
	m := unmarshalMessage(t, `
		"caption": "Look at this!",
		"photo": [
			{"file_id": "AgAD1", "file_unique_id": "AQAD1", "width": 90, "height": 90, "file_size": 1234},
			{"file_id": "AgAD2", "file_unique_id": "AQAD2", "width": 320, "height": 320, "file_size": 5678}
		]
	`)

	if len(m.Photo) != 2 {
		t.Fatalf("expected 2 photo sizes, got %d", len(m.Photo))
	}
	if m.Photo[1].FileID != "AgAD2" || m.Photo[1].Width != 320 || m.Photo[1].FileSize != 5678 {
		t.Errorf("unexpected second PhotoSize: %+v", m.Photo[1])
	}
	if m.Caption != "Look at this!" {
		t.Errorf("Caption = %q, want %q", m.Caption, "Look at this!")
	}
	if !FilterPhoto()(msgCtx(m)) {
		t.Error("FilterPhoto should match")
	}
	if FilterDocument()(msgCtx(m)) {
		t.Error("FilterDocument should not match a photo message")
	}
}

func TestMessage_Document(t *testing.T) {
	m := unmarshalMessage(t, `
		"document": {
			"file_id": "BAAD1", "file_unique_id": "AQAD1",
			"file_name": "report.pdf", "mime_type": "application/pdf", "file_size": 204800
		}
	`)

	if m.Document == nil {
		t.Fatal("expected Document to be set")
	}
	if m.Document.FileName != "report.pdf" || m.Document.MimeType != "application/pdf" || m.Document.FileSize != 204800 {
		t.Errorf("unexpected Document: %+v", m.Document)
	}
	if !FilterDocument()(msgCtx(m)) {
		t.Error("FilterDocument should match")
	}
	if FilterPhoto()(msgCtx(m)) {
		t.Error("FilterPhoto should not match a document message")
	}
}

func TestMessage_Sticker(t *testing.T) {
	m := unmarshalMessage(t, `
		"sticker": {
			"file_id": "CAAC1", "file_unique_id": "AgAD1", "type": "regular",
			"width": 512, "height": 512, "is_animated": false, "is_video": false,
			"emoji": "😀", "set_name": "MyPack"
		}
	`)

	if m.Sticker == nil {
		t.Fatal("expected Sticker to be set")
	}
	if m.Sticker.Emoji != "😀" || m.Sticker.SetName != "MyPack" || m.Sticker.Type != "regular" {
		t.Errorf("unexpected Sticker: %+v", m.Sticker)
	}
	if !FilterSticker()(msgCtx(m)) {
		t.Error("FilterSticker should match")
	}
}

func TestMessage_Voice(t *testing.T) {
	m := unmarshalMessage(t, `
		"voice": {"file_id": "AwAC1", "file_unique_id": "AgAD1", "duration": 5, "mime_type": "audio/ogg", "file_size": 12345}
	`)

	if m.Voice == nil {
		t.Fatal("expected Voice to be set")
	}
	if m.Voice.Duration != 5 || m.Voice.MimeType != "audio/ogg" {
		t.Errorf("unexpected Voice: %+v", m.Voice)
	}
	if !FilterVoice()(msgCtx(m)) {
		t.Error("FilterVoice should match")
	}
}

func TestMessage_Video(t *testing.T) {
	m := unmarshalMessage(t, `
		"video": {
			"file_id": "BAAC1", "file_unique_id": "AgAD1",
			"width": 1280, "height": 720, "duration": 30, "mime_type": "video/mp4", "file_size": 5242880
		}
	`)

	if m.Video == nil {
		t.Fatal("expected Video to be set")
	}
	if m.Video.Width != 1280 || m.Video.Height != 720 || m.Video.Duration != 30 {
		t.Errorf("unexpected Video: %+v", m.Video)
	}
	if !FilterVideo()(msgCtx(m)) {
		t.Error("FilterVideo should match")
	}
}

func TestMessage_VideoNote(t *testing.T) {
	m := unmarshalMessage(t, `
		"video_note": {"file_id": "DQAC1", "file_unique_id": "AgAD1", "length": 240, "duration": 10, "file_size": 102400}
	`)

	if m.VideoNote == nil {
		t.Fatal("expected VideoNote to be set")
	}
	if m.VideoNote.Length != 240 || m.VideoNote.Duration != 10 {
		t.Errorf("unexpected VideoNote: %+v", m.VideoNote)
	}
	if !FilterVideoNote()(msgCtx(m)) {
		t.Error("FilterVideoNote should match")
	}
}

func TestMessage_Audio(t *testing.T) {
	m := unmarshalMessage(t, `
		"audio": {
			"file_id": "CQAC1", "file_unique_id": "AgAD1", "duration": 180,
			"performer": "Artist", "title": "Song", "mime_type": "audio/mpeg", "file_size": 3145728
		}
	`)

	if m.Audio == nil {
		t.Fatal("expected Audio to be set")
	}
	if m.Audio.Performer != "Artist" || m.Audio.Title != "Song" || m.Audio.Duration != 180 {
		t.Errorf("unexpected Audio: %+v", m.Audio)
	}
	if !FilterAudio()(msgCtx(m)) {
		t.Error("FilterAudio should match")
	}
}

func TestMessage_Animation(t *testing.T) {
	m := unmarshalMessage(t, `
		"animation": {
			"file_id": "CgAC1", "file_unique_id": "AgAD1",
			"width": 480, "height": 270, "duration": 3, "file_name": "funny.gif", "mime_type": "video/mp4", "file_size": 204800
		}
	`)

	if m.Animation == nil {
		t.Fatal("expected Animation to be set")
	}
	if m.Animation.FileName != "funny.gif" || m.Animation.Duration != 3 {
		t.Errorf("unexpected Animation: %+v", m.Animation)
	}
	if !FilterAnimation()(msgCtx(m)) {
		t.Error("FilterAnimation should match")
	}
}

func TestMessage_Contact(t *testing.T) {
	m := unmarshalMessage(t, `
		"contact": {"phone_number": "+15551234567", "first_name": "John", "last_name": "Doe", "user_id": 123456789}
	`)

	if m.Contact == nil {
		t.Fatal("expected Contact to be set")
	}
	if m.Contact.PhoneNumber != "+15551234567" || m.Contact.UserID != 123456789 {
		t.Errorf("unexpected Contact: %+v", m.Contact)
	}
	if !FilterContact()(msgCtx(m)) {
		t.Error("FilterContact should match")
	}
}

func TestMessage_Location(t *testing.T) {
	m := unmarshalMessage(t, `
		"location": {"latitude": 37.7749, "longitude": -122.4194}
	`)

	if m.Location == nil {
		t.Fatal("expected Location to be set")
	}
	if m.Location.Latitude != 37.7749 || m.Location.Longitude != -122.4194 {
		t.Errorf("unexpected Location: %+v", m.Location)
	}
	if !FilterLocation()(msgCtx(m)) {
		t.Error("FilterLocation should match")
	}
}

func TestMessage_Venue(t *testing.T) {
	m := unmarshalMessage(t, `
		"venue": {
			"location": {"latitude": 37.7749, "longitude": -122.4194},
			"title": "Coffee Shop", "address": "123 Main St"
		}
	`)

	if m.Venue == nil {
		t.Fatal("expected Venue to be set")
	}
	if m.Venue.Title != "Coffee Shop" || m.Venue.Location.Latitude != 37.7749 {
		t.Errorf("unexpected Venue: %+v", m.Venue)
	}
	if !FilterVenue()(msgCtx(m)) {
		t.Error("FilterVenue should match")
	}
}

func TestMessage_Poll(t *testing.T) {
	m := unmarshalMessage(t, `
		"poll": {
			"id": "poll123", "question": "Favorite color?",
			"options": [{"text": "Red", "voter_count": 3}, {"text": "Blue", "voter_count": 5}],
			"total_voter_count": 8, "is_closed": false, "is_anonymous": true,
			"type": "regular", "allows_multiple_answers": false
		}
	`)

	if m.Poll == nil {
		t.Fatal("expected Poll to be set")
	}
	if len(m.Poll.Options) != 2 || m.Poll.Options[1].Text != "Blue" || m.Poll.Options[1].VoterCount != 5 {
		t.Errorf("unexpected Poll options: %+v", m.Poll.Options)
	}
	if m.Poll.TotalVoterCount != 8 || !m.Poll.IsAnonymous {
		t.Errorf("unexpected Poll: %+v", m.Poll)
	}
	if !FilterPoll()(msgCtx(m)) {
		t.Error("FilterPoll should match")
	}
}

func TestMessage_Dice(t *testing.T) {
	m := unmarshalMessage(t, `
		"dice": {"emoji": "🎲", "value": 4}
	`)

	if m.Dice == nil {
		t.Fatal("expected Dice to be set")
	}
	if m.Dice.Emoji != "🎲" || m.Dice.Value != 4 {
		t.Errorf("unexpected Dice: %+v", m.Dice)
	}
	if !FilterDice()(msgCtx(m)) {
		t.Error("FilterDice should match")
	}
	if !FilterDiceValue(4)(msgCtx(m)) {
		t.Error("FilterDiceValue(4) should match a roll of 4")
	}
	if FilterDiceValue(6)(msgCtx(m)) {
		t.Error("FilterDiceValue(6) should not match a roll of 4")
	}
	if !FilterDiceValue(1, 4, 6)(msgCtx(m)) {
		t.Error("FilterDiceValue should match if any listed value matches")
	}
	if FilterDiceValue(4)(msgCtx(&Message{Text: "hi"})) {
		t.Error("FilterDiceValue should not match a non-dice message")
	}
}

func TestFilterHasEntity(t *testing.T) {
	m := &Message{
		Text: "hi @bob check example.com",
		Entities: []Entity{
			{Type: EntityMention, Offset: 3, Length: 4},
			{Type: EntityURL, Offset: 14, Length: 11},
		},
	}

	if !FilterHasEntity(EntityMention)(msgCtx(m)) {
		t.Error("expected EntityMention to match")
	}
	if !FilterHasEntity(EntityHashtag, EntityURL)(msgCtx(m)) {
		t.Error("expected a match when any listed type is present")
	}
	if FilterHasEntity(EntityHashtag, EntityEmail)(msgCtx(m)) {
		t.Error("expected no match when none of the listed types are present")
	}
	if FilterHasEntity(EntityMention)(msgCtx(&Message{Text: "hi"})) {
		t.Error("expected no match on a message with no entities")
	}

	captioned := &Message{
		Caption:         "call me",
		CaptionEntities: []Entity{{Type: EntityPhoneNumber, Offset: 0, Length: 7}},
	}
	if !FilterHasEntity(EntityPhoneNumber)(msgCtx(captioned)) {
		t.Error("expected FilterHasEntity to check CaptionEntities too")
	}
}

func TestMessage_PlainText_NoContentFiltersMatch(t *testing.T) {
	m := unmarshalMessage(t, `"text": "hello"`)

	filters := map[string]Filter{
		"Photo": FilterPhoto(), "Document": FilterDocument(), "Sticker": FilterSticker(),
		"Voice": FilterVoice(), "Video": FilterVideo(), "VideoNote": FilterVideoNote(),
		"Audio": FilterAudio(), "Animation": FilterAnimation(), "Contact": FilterContact(),
		"Location": FilterLocation(), "Venue": FilterVenue(), "Poll": FilterPoll(), "Dice": FilterDice(),
	}
	for name, f := range filters {
		if f(msgCtx(m)) {
			t.Errorf("Filter%s should not match a plain text message", name)
		}
	}
}
