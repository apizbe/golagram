package api

import (
	"encoding/json"
	"fmt"
	"io"
)

// InputFile represents a file to send: an existing Telegram file_id, an
// HTTP(S) URL for Telegram to fetch, or local content to upload directly.
// The zero value is invalid — always construct one of the three ways.
type InputFile struct {
	value    string
	filename string
	reader   io.Reader
}

// InputFileID references a file already on Telegram's servers.
func InputFileID(fileID string) InputFile { return InputFile{value: fileID} }

// InputFileURL has Telegram fetch the file from a public HTTP(S) URL.
func InputFileURL(url string) InputFile { return InputFile{value: url} }

// InputFileUpload uploads local content directly, read once when the
// request is sent. filename is sent as the multipart part's filename —
// Telegram uses it (and its extension) to help guess content type.
func InputFileUpload(filename string, r io.Reader) InputFile {
	return InputFile{filename: filename, reader: r}
}

// IsUpload reports whether this InputFile carries local content to upload,
// as opposed to a file_id/URL reference — [Client.Call] uses it to decide
// whether a request needs multipart/form-data instead of plain JSON.
func (f InputFile) IsUpload() bool { return f.reader != nil }

// MarshalJSON lets a file_id/URL InputFile marshal like a plain string —
// the path taken when a request has no uploads at all and stays on the
// regular JSON call path. A local upload can never reach this: [Client.Call]
// detects it first and switches to multipart, whose encoder reads
// filename/reader directly instead of marshaling.
func (f InputFile) MarshalJSON() ([]byte, error) {
	if f.reader != nil {
		return nil, fmt.Errorf("golagram: InputFile upload cannot be JSON-marshaled directly (this indicates Client.Call failed to detect it and switch to multipart)")
	}
	return json.Marshal(f.value)
}
