package golagram

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSendPhoto_LocalUpload_UsesMultipart is the end-to-end case the whole
// pipeline exists for: a generated method (SendPhoto) called with a local
// upload actually produces a multipart/form-data request, with the upload
// as a file part and every other field still present as a form field.
func TestSendPhoto_LocalUpload_UsesMultipart(t *testing.T) {
	var gotContentType string
	var gotChatID, gotCaption string
	var gotFilename string
	var gotFileContent []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_, params, err := mime.ParseMediaType(gotContentType)
		if err != nil {
			t.Fatalf("failed to parse Content-Type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		form, err := mr.ReadForm(10 << 20)
		if err != nil {
			t.Fatalf("failed to read multipart form: %v", err)
		}
		if v := form.Value["chat_id"]; len(v) > 0 {
			gotChatID = v[0]
		}
		if v := form.Value["caption"]; len(v) > 0 {
			gotCaption = v[0]
		}
		if fhs := form.File["photo"]; len(fhs) > 0 {
			gotFilename = fhs[0].Filename
			f, _ := fhs[0].Open()
			gotFileContent, _ = io.ReadAll(f)
			f.Close()
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":99,"date":1700000000,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)

	msg, err := bot.SendPhoto(context.Background(), &SendPhotoRequest{
		ChatID:  ChatIDFromInt(1),
		Photo:   InputFileUpload("cat.jpg", strings.NewReader("fake jpeg bytes")),
		Caption: "a cat",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.MessageID != 99 {
		t.Errorf("MessageID = %d, want 99", msg.MessageID)
	}

	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	if gotChatID != "1" {
		t.Errorf("chat_id = %q, want 1", gotChatID)
	}
	if gotCaption != "a cat" {
		t.Errorf("caption = %q, want %q", gotCaption, "a cat")
	}
	if gotFilename != "cat.jpg" {
		t.Errorf("uploaded filename = %q, want cat.jpg", gotFilename)
	}
	if string(gotFileContent) != "fake jpeg bytes" {
		t.Errorf("uploaded content = %q, want %q", gotFileContent, "fake jpeg bytes")
	}
}

// TestSendPhoto_FileID_StaysJSON confirms the common case (referencing an
// existing file_id, no local upload anywhere) is unaffected by the
// multipart pipeline — same plain JSON request as every other method.
func TestSendPhoto_FileID_StaysJSON(t *testing.T) {
	var gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()

	bot := newTestBot(server)

	_, err := bot.SendPhoto(context.Background(), &SendPhotoRequest{
		ChatID: ChatIDFromInt(1),
		Photo:  InputFileID("AgADBAAD"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json for a file_id reference", gotContentType)
	}
}

// TestSendMediaGroup_LocalUpload_HoistsIntoAttachReference exercises the
// nested case: SendMediaGroupRequest.Media is []InputMedia (a union
// interface slice), and one element's local upload must be hoisted into
// its own attach:// file part rather than embedded in the media JSON —
// Telegram's multipart convention for media groups.
func TestSendMediaGroup_LocalUpload_HoistsIntoAttachReference(t *testing.T) {
	var gotMediaJSON string
	var gotFilename string
	var gotFileContent []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("failed to parse Content-Type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		form, err := mr.ReadForm(10 << 20)
		if err != nil {
			t.Fatalf("failed to read multipart form: %v", err)
		}
		if v := form.Value["media"]; len(v) > 0 {
			gotMediaJSON = v[0]
		}
		for name, fhs := range form.File {
			if len(fhs) == 0 {
				continue
			}
			gotFilename = fhs[0].Filename
			f, _ := fhs[0].Open()
			gotFileContent, _ = io.ReadAll(f)
			f.Close()
			_ = name
		}
		w.Write([]byte(`{"ok":true,"result":[{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"}},{"message_id":2,"date":1700000000,"chat":{"id":1,"type":"private"}}]}`))
	}))
	defer server.Close()

	bot := newTestBot(server)

	msgs, err := bot.SendMediaGroup(context.Background(), &SendMediaGroupRequest{
		ChatID: ChatIDFromInt(1),
		Media: []InputMedia{
			&InputMediaPhoto{Type: "photo", Media: InputFileID("existing_file_id")},
			&InputMediaPhoto{Type: "photo", Media: InputFileUpload("cat.jpg", strings.NewReader("cat bytes")), Caption: "a cat"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages back, got %d", len(msgs))
	}

	if gotFilename != "cat.jpg" || string(gotFileContent) != "cat bytes" {
		t.Errorf("uploaded file = %q/%q, want cat.jpg/\"cat bytes\"", gotFilename, gotFileContent)
	}

	var decoded []struct {
		Media   string `json:"media"`
		Caption string `json:"caption,omitempty"`
	}
	if err := json.Unmarshal([]byte(gotMediaJSON), &decoded); err != nil {
		t.Fatalf("media field wasn't valid JSON: %v (%s)", err, gotMediaJSON)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 media items in the JSON, got %d", len(decoded))
	}
	if decoded[0].Media != "existing_file_id" {
		t.Errorf("element 0 media = %q, want the untouched file_id", decoded[0].Media)
	}
	if !strings.HasPrefix(decoded[1].Media, "attach://") {
		t.Errorf("element 1 media = %q, want an attach:// reference, not the raw upload", decoded[1].Media)
	}
}
