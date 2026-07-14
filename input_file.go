package golagram

import (
	"io"

	"github.com/apizbe/golagram/internal/api"
)

// InputFile represents a file to send: an existing Telegram file_id, an
// HTTP(S) URL for Telegram to fetch, or local content to upload directly.
// Construct with [InputFileID], [InputFileURL], or [InputFileUpload] — the
// zero value is invalid.
//
// A request with no InputFile carrying a local upload is JSON-encoded as
// usual; adding one anywhere in the request switches that single call to
// multipart/form-data transparently (see internal/api.Client.Call).
type InputFile = api.InputFile

// InputFileID references a file already on Telegram's servers.
func InputFileID(fileID string) InputFile { return api.InputFileID(fileID) }

// InputFileURL has Telegram fetch the file from a public HTTP(S) URL.
func InputFileURL(url string) InputFile { return api.InputFileURL(url) }

// InputFileUpload uploads local content directly, read once when the
// request is sent — e.g. an *os.File, or any other io.Reader. filename is
// sent as the multipart part's filename; Telegram uses its extension to
// help guess content type.
func InputFileUpload(filename string, r io.Reader) InputFile {
	return api.InputFileUpload(filename, r)
}
