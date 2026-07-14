// Package api is golagram's single HTTP path to the Telegram Bot API.
// Every request — including getUpdates long-polling — goes through
// Client.Call, so base URL, error mapping, and testability are decided in
// exactly one place.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

// Error is a typed Telegram Bot API error, parsed from the error payload
// Telegram returns on non-ok responses. It preserves the fields callers need
// to react correctly: flood-wait delays and group→supergroup migrations.
type Error struct {
	Code            int    // error_code: 400, 403, 429, ...
	Description     string // human-readable description from Telegram
	RetryAfter      *int   // parameters.retry_after, set on 429 flood control
	MigrateToChatID *int64 // parameters.migrate_to_chat_id, set when a group upgraded
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("telegram: %s (%d)", e.Description, e.Code)
}

// apiResponse is Telegram's universal response envelope.
type apiResponse struct {
	Ok          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter      *int   `json:"retry_after"`
		MigrateToChatID *int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

const defaultBaseURL = "https://api.telegram.org/bot"

// defaultCallTimeout bounds calls whose context carries no deadline of its
// own. Long-polling passes an explicit deadline instead.
const defaultCallTimeout = 30 * time.Second

// Client is golagram's single HTTP path to the Telegram Bot API — every
// generated method calls [Client.Call], regardless of whether that ends up
// encoding the request as JSON or multipart/form-data.
type Client struct {
	token            string
	httpClient       *http.Client
	baseURL          string
	autoRetryMaxWait time.Duration // 0 disables auto-retry; see SetAutoRetry
}

// NewClient creates a Client pointed at the real Telegram Bot API.
func NewClient(token string) *Client {
	return NewClientWithBaseURL(token, defaultBaseURL)
}

// NewClientWithBaseURL creates a client pointed at a custom base URL — a
// fake server in tests, or a self-hosted Bot API server in production.
func NewClientWithBaseURL(token, baseURL string) *Client {
	return &Client{
		token: token,
		// No client-wide timeout: getUpdates long-polls for tens of seconds.
		// Per-call deadlines come from the context (see Call).
		httpClient: &http.Client{},
		baseURL:    baseURL,
	}
}

// sanitizeTokenError strips the bot token out of err before returning it.
// Every request URL is baseURL+token+method, since that's how Telegram
// authenticates a call — so any transport-level failure (DNS, connection
// refused, TLS, a typo'd WithBaseURL, ...) surfaces it verbatim inside Go's
// *url.Error, one %w-unwrap away from a caller's log.Printf/slog call. This
// repo has shipped one real token leak already (a hardcoded token in git
// history); this closes the same class of leak at the one place golagram's
// own code could still cause it. The error's type and wrapped chain are
// preserved (errors.Is/As on the underlying network error still works) —
// only the *url.Error's human-readable URL field is redacted in place.
func (c *Client) sanitizeTokenError(err error) error {
	var urlErr *url.Error
	if c.token != "" && errors.As(err, &urlErr) {
		urlErr.URL = strings.ReplaceAll(urlErr.URL, c.token, "<TOKEN>")
	}
	return err
}

// SetHTTPClient replaces the underlying *http.Client (proxies, custom
// transports). Call before any requests are made.
func (c *Client) SetHTTPClient(hc *http.Client) {
	if hc != nil {
		c.httpClient = hc
	}
}

// SetAutoRetry turns on transparent retry for Telegram's 429 flood-control
// error: doRequest sleeps the server-specified retry_after and retries,
// as many times as fit within maxWait's total budget (measured across all
// attempts for one Call, not per attempt) before giving up and returning
// the [*Error] like normal. maxWait <= 0 disables it (the default — a 429
// is returned immediately, same as before this existed). Call before any
// requests are made; not safe to change concurrently with in-flight calls.
func (c *Client) SetAutoRetry(maxWait time.Duration) {
	c.autoRetryMaxWait = maxWait
}

// Call invokes a Bot API method with the given params and returns the raw
// result payload. Telegram-level failures are returned as [*Error]. params
// is JSON-encoded, unless it (a pointer to a generated *Request struct)
// contains an [InputFile] carrying a local upload, in which case Call
// transparently switches to multipart/form-data instead — every generated
// method calls Call the same way regardless of which encoding it ends up
// using.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if hasUpload(params) {
		return c.callMultipart(ctx, method, params)
	}

	if params == nil {
		return c.doRequest(ctx, method, "application/json", nil)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to marshal params: %w", method, err)
	}
	return c.doRequest(ctx, method, "application/json", encoded)
}

// callMultipart encodes params as multipart/form-data: an InputFile upload
// becomes a file part, everything else becomes a form field — scalars
// (string/number/bool) as their raw value, structs/slices/maps (including
// custom-JSON types like ChatID) as their JSON encoding, matching
// Telegram's documented multipart convention for complex fields. An upload
// nested inside a field (e.g. sendMediaGroup's Media []InputMedia, one of
// whose elements has an InputFile upload for its Media/Thumbnail) is hoisted
// out into its own "attach://<name>" file part, matching how Telegram
// expects a media group's local uploads to be referenced from the JSON-
// encoded media array — see hoistUploads.
func (c *Client) callMultipart(ctx context.Context, method string, params any) (json.RawMessage, error) {
	v := reflect.ValueOf(params)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	t := v.Type()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	var hoisted []hoistedUpload

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		name, omitempty := jsonTagName(field)
		if name == "-" {
			continue
		}
		fv := v.Field(i)

		if field.Type == inputFileType {
			f := fv.Interface().(InputFile)
			if f.IsUpload() {
				part, err := w.CreateFormFile(name, f.filename)
				if err != nil {
					return nil, fmt.Errorf("%s: creating form file %q: %w", method, name, err)
				}
				if _, err := io.Copy(part, f.reader); err != nil {
					return nil, fmt.Errorf("%s: writing upload %q: %w", method, name, err)
				}
				continue
			}
			if f.value == "" && omitempty {
				continue
			}
			if err := w.WriteField(name, f.value); err != nil {
				return nil, fmt.Errorf("%s: writing field %q: %w", method, name, err)
			}
			continue
		}

		if omitempty && fv.IsZero() {
			continue
		}

		var strValue string
		if containsUpload(fv) {
			data, err := json.Marshal(hoistUploads(fv, &hoisted).Interface())
			if err != nil {
				return nil, fmt.Errorf("%s: encoding field %q: %w", method, name, err)
			}
			strValue = string(data)
		} else {
			var err error
			strValue, err = formValue(fv.Interface())
			if err != nil {
				return nil, fmt.Errorf("%s: encoding field %q: %w", method, name, err)
			}
		}
		if err := w.WriteField(name, strValue); err != nil {
			return nil, fmt.Errorf("%s: writing field %q: %w", method, name, err)
		}
	}

	for _, u := range hoisted {
		part, err := w.CreateFormFile(u.name, u.file.filename)
		if err != nil {
			return nil, fmt.Errorf("%s: creating form file %q: %w", method, u.name, err)
		}
		if _, err := io.Copy(part, u.file.reader); err != nil {
			return nil, fmt.Errorf("%s: writing upload %q: %w", method, u.name, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("%s: closing multipart writer: %w", method, err)
	}

	return c.doRequest(ctx, method, w.FormDataContentType(), body.Bytes())
}

// doRequest is the one place an HTTP request actually goes out — the JSON
// path and the multipart path both funnel through it once their body and
// Content-Type are ready. body is fully materialized up front (not a
// streaming io.Reader) specifically so a retry can resend the exact same
// bytes without needing to re-invoke whatever produced them — that matters
// for multipart bodies, since the underlying upload io.Reader is consumed
// on first use and can't be replayed, but the encoded multipart bytes can.
func (c *Client) doRequest(ctx context.Context, method, contentType string, body []byte) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		// max(...): otherwise the fallback deadline would cut retries short
		// before autoRetryMaxWait's own budget does.
		timeout := max(defaultCallTimeout, c.autoRetryMaxWait)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+c.token+"/"+method, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("%s: failed to create request: %w", method, c.sanitizeTokenError(err))
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s: request failed: %w", method, c.sanitizeTokenError(err))
		}

		var envelope apiResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&envelope)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("%s: failed to decode response (HTTP %d): %w",
				method, resp.StatusCode, decodeErr)
		}

		if envelope.Ok {
			return envelope.Result, nil
		}

		apiErr := &Error{
			Code:        envelope.ErrorCode,
			Description: envelope.Description,
		}
		if envelope.Parameters != nil {
			apiErr.RetryAfter = envelope.Parameters.RetryAfter
			apiErr.MigrateToChatID = envelope.Parameters.MigrateToChatID
		}

		wait, retry := c.retryDelay(apiErr, start)
		if !retry {
			return nil, apiErr
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// retryDelay reports how long to sleep before retrying apiErr, and whether
// to retry at all: only for a 429 with a retry_after, only when
// SetAutoRetry is on, and only if this attempt's wait still fits within
// the total budget measured from the call's first attempt (start).
func (c *Client) retryDelay(apiErr *Error, start time.Time) (wait time.Duration, retry bool) {
	if c.autoRetryMaxWait <= 0 || apiErr.Code != 429 || apiErr.RetryAfter == nil {
		return 0, false
	}
	wait = time.Duration(*apiErr.RetryAfter) * time.Second
	if time.Since(start)+wait > c.autoRetryMaxWait {
		return 0, false
	}
	return wait, true
}

var inputFileType = reflect.TypeFor[InputFile]()

// hasUpload reports whether params (nil, or a pointer to a generated
// *Request struct) contains an InputFile carrying local content anywhere
// within it — directly (SendPhotoRequest.Photo) or nested inside a slice or
// pointer (SendMediaGroupRequest.Media []InputMedia, whose elements are
// *InputMediaPhoto/etc. with their own InputFile fields) — the signal to
// encode the request as multipart/form-data instead of JSON.
func hasUpload(params any) bool {
	if params == nil {
		return false
	}
	return containsUpload(reflect.ValueOf(params))
}

// HasUpload is hasUpload, exported for golagram's webhook_reply.go: a
// request carrying a local file upload can't be JSON-encoded, so it can't
// be embedded in a webhook HTTP response body either — the same condition
// that forces multipart/form-data in Call.
func HasUpload(params any) bool {
	return hasUpload(params)
}

// containsUpload reports whether v is, or contains anywhere within it
// (through pointers, interfaces, struct fields, and slice/array elements),
// an InputFile carrying a local upload.
func containsUpload(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return false
		}
		return containsUpload(v.Elem())
	case reflect.Struct:
		if v.Type() == inputFileType {
			return v.Interface().(InputFile).IsUpload()
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				continue // unexported
			}
			if containsUpload(v.Field(i)) {
				return true
			}
		}
		return false
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if containsUpload(v.Index(i)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// hoistedUpload is an InputFile upload found by hoistUploads somewhere
// below a request's top level, paired with the generated "attach://<name>"
// identifier substituted in its place.
type hoistedUpload struct {
	name string
	file InputFile
}

// hoistUploads returns a value structurally identical to v, except every
// InputFile upload reachable within it (through pointers, interfaces,
// struct fields, and slice elements) is replaced by an
// InputFileID("attach://<name>") reference — safe to JSON-marshal — with
// each replacement appended to *uploads so the caller can write it as its
// own multipart file part under that name. This is what lets
// sendMediaGroup's InputMedia elements (or createNewStickerSet's
// InputSticker elements) carry a local upload at all: Telegram's multipart
// convention for a field like "media" is a JSON-encoded array where a
// local file is referenced by name from within that JSON, with the actual
// bytes uploaded as a sibling file part — not embedded in the JSON itself.
func hoistUploads(v reflect.Value, uploads *[]hoistedUpload) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		inner := hoistUploads(v.Elem(), uploads)
		p := reflect.New(inner.Type())
		p.Elem().Set(inner)
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := hoistUploads(v.Elem(), uploads)
		nv := reflect.New(v.Type()).Elem()
		nv.Set(inner)
		return nv
	case reflect.Struct:
		if v.Type() == inputFileType {
			f := v.Interface().(InputFile)
			if !f.IsUpload() {
				return v
			}
			name := fmt.Sprintf("golagram_upload_%d", len(*uploads))
			*uploads = append(*uploads, hoistedUpload{name: name, file: f})
			return reflect.ValueOf(InputFileID("attach://" + name))
		}
		nv := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				continue // unexported; none of golagram's generated types have these
			}
			nv.Field(i).Set(hoistUploads(v.Field(i), uploads))
		}
		return nv
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		nv := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			nv.Index(i).Set(hoistUploads(v.Index(i), uploads))
		}
		return nv
	default:
		return v
	}
}

// jsonTagName extracts the field name and omitempty flag from a struct
// field's `json:"..."` tag, matching encoding/json's own tag parsing.
func jsonTagName(field reflect.StructField) (name string, omitempty bool) {
	tag := field.Tag.Get("json")
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = field.Name
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

// formValue renders v as a multipart form field value: scalars (string,
// numbers, bools, and custom-JSON scalar types like ChatID) as their raw
// text, everything else (structs, slices, maps) as their JSON encoding —
// the distinction Telegram's multipart convention actually cares about.
func formValue(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return s, nil // v marshaled to a JSON string — use the unquoted content
	}
	return string(data), nil // number, bool, or complex JSON — use as-is
}
