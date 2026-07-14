package api

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// testMediaRequest stands in for a generated *Request struct with an
// InputFile field alongside scalar and complex fields — internal/api can't
// import golagram's actual generated types (that would be the import cycle
// the api/golagram split exists to avoid), so this mirrors their shape.
type testMediaRequest struct {
	ChatID    string    `json:"chat_id"`
	Photo     InputFile `json:"photo"`
	Caption   string    `json:"caption,omitempty"`
	Thumbnail InputFile `json:"thumbnail,omitempty"`
	Entities  []string  `json:"entities,omitempty"`
	HasSpoler bool      `json:"has_spoiler,omitempty"`
}

func parseMultipart(t *testing.T, r *http.Request) (fields map[string]string, files map[string]*multipart.FileHeader) {
	t.Helper()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("failed to parse Content-Type: %v", err)
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	form, err := mr.ReadForm(10 << 20)
	if err != nil {
		t.Fatalf("failed to read multipart form: %v", err)
	}
	fields = map[string]string{}
	for k, v := range form.Value {
		if len(v) > 0 {
			fields[k] = v[0]
		}
	}
	files = map[string]*multipart.FileHeader{}
	for k, v := range form.File {
		if len(v) > 0 {
			files[k] = v[0]
		}
	}
	return fields, files
}

func TestClient_Call_SwitchesToMultipartForUpload(t *testing.T) {
	var gotContentType string
	var gotFields map[string]string
	var gotFiles map[string]*multipart.FileHeader
	var gotFileContent []byte

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotFields, gotFiles = parseMultipart(t, r)
		if fh, ok := gotFiles["photo"]; ok {
			f, _ := fh.Open()
			gotFileContent, _ = io.ReadAll(f)
			f.Close()
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})

	req := &testMediaRequest{
		ChatID:   "42",
		Photo:    InputFileUpload("cat.jpg", strings.NewReader("fake jpeg bytes")),
		Caption:  "a cat",
		Entities: []string{"bold", "italic"},
	}

	if _, err := client.Call(context.Background(), "sendPhoto", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	if gotFields["chat_id"] != "42" {
		t.Errorf("chat_id field = %q, want 42", gotFields["chat_id"])
	}
	if gotFields["caption"] != "a cat" {
		t.Errorf("caption field = %q, want %q", gotFields["caption"], "a cat")
	}
	if gotFields["entities"] != `["bold","italic"]` {
		t.Errorf("entities field = %q, want JSON-encoded array", gotFields["entities"])
	}
	if _, hasThumbnail := gotFields["thumbnail"]; hasThumbnail {
		t.Error("unset optional thumbnail should be omitted (omitempty), got a field")
	}
	if _, hasSpoiler := gotFields["has_spoiler"]; hasSpoiler {
		t.Error("unset optional bool should be omitted (omitempty), got a field")
	}
	if fh, ok := gotFiles["photo"]; !ok {
		t.Error("expected a file part named \"photo\"")
	} else if fh.Filename != "cat.jpg" {
		t.Errorf("uploaded filename = %q, want cat.jpg", fh.Filename)
	}
	if string(gotFileContent) != "fake jpeg bytes" {
		t.Errorf("uploaded content = %q, want %q", gotFileContent, "fake jpeg bytes")
	}
}

func TestClient_Call_StaysJSONWhenNoUpload(t *testing.T) {
	var gotContentType string

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})

	req := &testMediaRequest{
		ChatID: "42",
		Photo:  InputFileID("AgADBAAD"), // file_id reference, not an upload
	}

	if _, err := client.Call(context.Background(), "sendPhoto", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (no upload present)", gotContentType)
	}
}

func TestClient_Call_MultipartWithMultipleUploads(t *testing.T) {
	var gotFiles map[string]*multipart.FileHeader

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, gotFiles = parseMultipart(t, r)
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})

	req := &testMediaRequest{
		ChatID:    "42",
		Photo:     InputFileUpload("photo.jpg", strings.NewReader("photo bytes")),
		Thumbnail: InputFileUpload("thumb.jpg", strings.NewReader("thumb bytes")),
	}

	if _, err := client.Call(context.Background(), "sendPhoto", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := gotFiles["photo"]; !ok {
		t.Error("expected a photo file part")
	}
	if _, ok := gotFiles["thumbnail"]; !ok {
		t.Error("expected a thumbnail file part")
	}
}

func TestInputFile_MarshalJSON(t *testing.T) {
	t.Run("file_id/URL marshals as a plain string", func(t *testing.T) {
		data, err := InputFileID("abc123").MarshalJSON()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != `"abc123"` {
			t.Errorf("got %s, want \"abc123\"", data)
		}
	})

	t.Run("an upload errors instead of marshaling silently wrong", func(t *testing.T) {
		_, err := InputFileUpload("f.jpg", strings.NewReader("x")).MarshalJSON()
		if err == nil {
			t.Fatal("expected an error marshaling an upload directly")
		}
	})
}

// testMedia mirrors the InputMedia union: a sealed interface implemented by
// concrete media types, each carrying its own InputFile fields — exactly
// sendMediaGroup's shape, reconstructed locally since internal/api can't
// import golagram's real generated types.
type testMedia interface{ isTestMedia() }

type testMediaPhoto struct {
	Type      string    `json:"type"`
	Media     InputFile `json:"media"`
	Caption   string    `json:"caption,omitempty"`
	Thumbnail InputFile `json:"thumbnail,omitempty"`
}

func (*testMediaPhoto) isTestMedia() {}

type testMediaGroupRequest struct {
	ChatID string      `json:"chat_id"`
	Media  []testMedia `json:"media"`
}

// testStickerSetRequest mirrors createNewStickerSet's shape: a slice of
// plain structs (not an interface/union) each carrying an InputFile field.
type testSticker struct {
	Sticker InputFile `json:"sticker"`
	Format  string    `json:"format"`
}

type testStickerSetRequest struct {
	Name     string        `json:"name"`
	Stickers []testSticker `json:"stickers"`
}

// testSingleStickerRequest mirrors addStickerToSet's shape: a single
// pointer-to-struct field (not inside a slice at all) carrying an
// InputFile field.
type testSingleStickerRequest struct {
	Name    string       `json:"name"`
	Sticker *testSticker `json:"sticker"`
}

func TestClient_Call_HoistsUploadsNestedInASlice(t *testing.T) {
	var gotFields map[string]string
	var gotFiles map[string]*fileHeaderContent

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotFields, gotFiles = parseMultipartWithContent(t, r)
		w.Write([]byte(`{"ok":true,"result":[{"message_id":1}]}`))
	})

	req := &testMediaGroupRequest{
		ChatID: "42",
		Media: []testMedia{
			&testMediaPhoto{Type: "photo", Media: InputFileID("existing_file_id"), Caption: "one"},
			&testMediaPhoto{Type: "photo", Media: InputFileUpload("cat.jpg", strings.NewReader("cat bytes")), Caption: "two"},
			&testMediaPhoto{Type: "photo", Media: InputFileID("https://example.com/x.jpg"), Thumbnail: InputFileUpload("thumb.jpg", strings.NewReader("thumb bytes"))},
		},
	}

	if _, err := client.Call(context.Background(), "sendMediaGroup", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mediaJSON, ok := gotFields["media"]
	if !ok {
		t.Fatal("expected a \"media\" form field")
	}

	var decoded []struct {
		Type      string `json:"type"`
		Media     string `json:"media"`
		Caption   string `json:"caption,omitempty"`
		Thumbnail string `json:"thumbnail,omitempty"`
	}
	if err := json.Unmarshal([]byte(mediaJSON), &decoded); err != nil {
		t.Fatalf("media field wasn't valid JSON: %v (%s)", err, mediaJSON)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 media items, got %d", len(decoded))
	}

	// Element 0: file_id reference stays as-is, untouched.
	if decoded[0].Media != "existing_file_id" {
		t.Errorf("element 0 media = %q, want the untouched file_id", decoded[0].Media)
	}

	// Element 1: local upload got replaced with an attach:// reference,
	// and a matching file part must exist with the right content.
	if !strings.HasPrefix(decoded[1].Media, "attach://") {
		t.Errorf("element 1 media = %q, want an attach:// reference", decoded[1].Media)
	}
	attachName1 := strings.TrimPrefix(decoded[1].Media, "attach://")
	fh, ok := gotFiles[attachName1]
	if !ok {
		t.Fatalf("no file part found for %q", attachName1)
	}
	if fh.filename != "cat.jpg" || string(fh.content) != "cat bytes" {
		t.Errorf("file part %q = %+v, want filename cat.jpg content \"cat bytes\"", attachName1, fh)
	}

	// Element 2: URL reference untouched, but its Thumbnail (a separate
	// upload) got its own independent attach:// reference and file part.
	if decoded[2].Media != "https://example.com/x.jpg" {
		t.Errorf("element 2 media = %q, want the untouched URL", decoded[2].Media)
	}
	if !strings.HasPrefix(decoded[2].Thumbnail, "attach://") {
		t.Errorf("element 2 thumbnail = %q, want an attach:// reference", decoded[2].Thumbnail)
	}
	attachName2 := strings.TrimPrefix(decoded[2].Thumbnail, "attach://")
	if attachName2 == attachName1 {
		t.Error("expected distinct attach:// names for two different uploads")
	}
	fh2, ok := gotFiles[attachName2]
	if !ok {
		t.Fatalf("no file part found for %q", attachName2)
	}
	if fh2.filename != "thumb.jpg" || string(fh2.content) != "thumb bytes" {
		t.Errorf("file part %q = %+v, want filename thumb.jpg content \"thumb bytes\"", attachName2, fh2)
	}
}

func TestClient_Call_NoNestedUpload_StaysJSON(t *testing.T) {
	var gotContentType string

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Write([]byte(`{"ok":true,"result":[{"message_id":1}]}`))
	})

	req := &testMediaGroupRequest{
		ChatID: "42",
		Media: []testMedia{
			&testMediaPhoto{Type: "photo", Media: InputFileID("a")},
			&testMediaPhoto{Type: "photo", Media: InputFileID("b")},
		},
	}

	if _, err := client.Call(context.Background(), "sendMediaGroup", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json when nothing in the group is a local upload", gotContentType)
	}
}

func TestClient_Call_HoistsUploadInStructSlice(t *testing.T) {
	var gotFields map[string]string
	var gotFiles map[string]*fileHeaderContent

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotFields, gotFiles = parseMultipartWithContent(t, r)
		w.Write([]byte(`{"ok":true,"result":true}`))
	})

	req := &testStickerSetRequest{
		Name: "myset",
		Stickers: []testSticker{
			{Sticker: InputFileUpload("s1.png", strings.NewReader("sticker one")), Format: "static"},
			{Sticker: InputFileUpload("s2.png", strings.NewReader("sticker two")), Format: "static"},
		},
	}

	if _, err := client.Call(context.Background(), "createNewStickerSet", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded []struct {
		Sticker string `json:"sticker"`
		Format  string `json:"format"`
	}
	if err := json.Unmarshal([]byte(gotFields["stickers"]), &decoded); err != nil {
		t.Fatalf("stickers field wasn't valid JSON: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 stickers, got %d", len(decoded))
	}
	for i, want := range []string{"sticker one", "sticker two"} {
		name := strings.TrimPrefix(decoded[i].Sticker, "attach://")
		fh, ok := gotFiles[name]
		if !ok {
			t.Fatalf("no file part for sticker %d (%q)", i, decoded[i].Sticker)
		}
		if string(fh.content) != want {
			t.Errorf("sticker %d content = %q, want %q", i, fh.content, want)
		}
	}
}

func TestClient_Call_HoistsUploadInSinglePointerField(t *testing.T) {
	var gotFields map[string]string
	var gotFiles map[string]*fileHeaderContent

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotFields, gotFiles = parseMultipartWithContent(t, r)
		w.Write([]byte(`{"ok":true,"result":true}`))
	})

	req := &testSingleStickerRequest{
		Name:    "myset",
		Sticker: &testSticker{Sticker: InputFileUpload("s.png", strings.NewReader("sticker content")), Format: "static"},
	}

	if _, err := client.Call(context.Background(), "addStickerToSet", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded struct {
		Sticker string `json:"sticker"`
	}
	if err := json.Unmarshal([]byte(gotFields["sticker"]), &decoded); err != nil {
		t.Fatalf("sticker field wasn't valid JSON: %v", err)
	}
	name := strings.TrimPrefix(decoded.Sticker, "attach://")
	fh, ok := gotFiles[name]
	if !ok {
		t.Fatalf("no file part for %q", decoded.Sticker)
	}
	if string(fh.content) != "sticker content" {
		t.Errorf("file content = %q, want %q", fh.content, "sticker content")
	}
}

type fileHeaderContent struct {
	filename string
	content  []byte
}

// parseMultipartWithContent is parseMultipart plus reading each file
// part's content immediately (parseMultipart itself only returns
// *multipart.FileHeader, which some callers above open themselves).
func parseMultipartWithContent(t *testing.T, r *http.Request) (map[string]string, map[string]*fileHeaderContent) {
	t.Helper()
	fields, fileHeaders := parseMultipart(t, r)
	files := map[string]*fileHeaderContent{}
	for name, fh := range fileHeaders {
		f, err := fh.Open()
		if err != nil {
			t.Fatalf("failed to open file part %q: %v", name, err)
		}
		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatalf("failed to read file part %q: %v", name, err)
		}
		files[name] = &fileHeaderContent{filename: fh.Filename, content: content}
	}
	return fields, files
}
