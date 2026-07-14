package golagram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// CallbackData packs and unpacks a typed struct into callback_data strings.
// Declare the payload once, get type-safe buttons and filters with the
// 64-byte limit enforced at pack time instead of buttons silently not
// sending:
//
//	type BuyItem struct {
//		ItemID int64
//		Qty    int
//	}
//	var buyCB = golagram.NewCallbackData[BuyItem]("buy")
//
//	kb.Add(buyCB.Button("Buy now", BuyItem{ItemID: 42, Qty: 1}))     // data: "buy:42:1"
//
//	r.CallbackQuery(buyCB.Filter()).Handle(func(c *gg.Ctx) error {
//		item, _ := buyCB.Unpack(c.CallbackQuery.Data)
//		...
//	})
type CallbackData[T any] struct {
	prefix     string
	fields     []reflect.StructField
	hmacSecret []byte // nil => unsigned (the default)
}

const callbackDataSep = ":"

// hmacTagLen is the truncated HMAC-SHA256 tag length, in bytes, used by
// WithHMAC. Telegram's 64-byte callback_data limit doesn't leave room for a
// full 32-byte tag on top of a real payload, so the tag is deliberately
// shortened — trading some resistance to brute-forcing the tag itself for
// having any payload budget left to sign. Not user-configurable: 8 bytes
// (11 base64 chars once encoded) is a fixed compromise, not a tunable
// security/space knob.
const hmacTagLen = 8

// ErrCallbackDataTampered is returned by [CallbackData.Unpack] (and
// anything built on it — [CallbackData.FromCtx], [CallbackData.FilterWhere])
// when a [CallbackData.WithHMAC]-signed payload's tag doesn't match its
// content: either genuinely tampered with, or forged via a client that
// isn't the real Telegram app relaying an actual button tap (Telegram's own
// callback mechanism doesn't cryptographically bind callback_data to "the
// button we sent" — see [CallbackData.WithHMAC]'s doc).
var ErrCallbackDataTampered = errors.New("golagram: callback data failed HMAC verification")

// WithHMAC enables tamper detection on this schema: Pack appends a secret-
// keyed, truncated HMAC-SHA256 tag to the packed payload, and Unpack
// verifies it before decoding, returning ErrCallbackDataTampered on any
// mismatch instead of silently decoding forged or corrupted data. Use it
// for callback payloads that carry something an attacker forging
// callback_data shouldn't be able to alter or replay unmodified into
// meaning something else — e.g. a price, a discount, or an approval
// decision — not for payloads where a bad Unpack is merely a harmless
// no-op.
//
// secret should be a fixed, private, high-entropy value the bot process
// holds (e.g. derived from the bot token or a dedicated config secret) —
// not per-user or per-payload. Returns cd for chaining:
//
//	var buyCB = golagram.NewCallbackData[BuyItem]("buy").WithHMAC(secretKey)
func (cd *CallbackData[T]) WithHMAC(secret []byte) *CallbackData[T] {
	if len(secret) == 0 {
		panic("golagram: CallbackData.WithHMAC: secret must not be empty")
	}
	cd.hmacSecret = secret
	return cd
}

// NewCallbackData declares a callback-data schema: prefix identifies the
// button family, T's exported fields (in declaration order) are the payload.
// Supported field types: string, bool, all int/uint sizes, float64. Panics
// at wiring time on an unsupported schema — same rationale as
// [Router.Include]'s cycle panic: fail at startup, not on the first click.
func NewCallbackData[T any](prefix string) *CallbackData[T] {
	if prefix == "" || strings.Contains(prefix, callbackDataSep) {
		panic(fmt.Sprintf("golagram: NewCallbackData prefix %q must be non-empty and contain no %q", prefix, callbackDataSep))
	}
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("golagram: NewCallbackData[%s]: payload must be a struct", t))
	}
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported fields are not part of the payload
		}
		switch f.Type.Kind() {
		case reflect.String, reflect.Bool, reflect.Float64,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fields = append(fields, f)
		default:
			panic(fmt.Sprintf("golagram: NewCallbackData[%s]: unsupported field %s %s", t, f.Name, f.Type))
		}
	}
	return &CallbackData[T]{prefix: prefix, fields: fields}
}

// Pack encodes v as "prefix:field1:field2:...". Returns a [*ValidationError]
// when the result would exceed Telegram's 64-byte callback_data limit, or
// when a string field contains the ":" separator (which would corrupt the
// encoding).
func (cd *CallbackData[T]) Pack(v T) (string, error) {
	parts := make([]string, 0, len(cd.fields)+1)
	parts = append(parts, cd.prefix)
	rv := reflect.ValueOf(v)
	for _, f := range cd.fields {
		fv := rv.FieldByIndex(f.Index)
		var s string
		switch f.Type.Kind() {
		case reflect.String:
			s = fv.String()
			if strings.Contains(s, callbackDataSep) {
				return "", &ValidationError{
					Field:   "callback_data",
					Message: fmt.Sprintf("field %s value %q contains the separator %q", f.Name, s, callbackDataSep),
				}
			}
		case reflect.Bool:
			s = strconv.FormatBool(fv.Bool())
		case reflect.Float64:
			s = strconv.FormatFloat(fv.Float(), 'g', -1, 64)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			s = strconv.FormatUint(fv.Uint(), 10)
		default: // int kinds
			s = strconv.FormatInt(fv.Int(), 10)
		}
		parts = append(parts, s)
	}
	packed := strings.Join(parts, callbackDataSep)
	if cd.hmacSecret != nil {
		packed += callbackDataSep + cd.hmacTag(packed)
	}
	if len(packed) > MaxCallbackDataLength {
		return "", &ValidationError{
			Field:   "callback_data",
			Message: fmt.Sprintf("packed %q: %d bytes exceeds Telegram's maximum of %d", packed, len(packed), MaxCallbackDataLength),
		}
	}
	return packed, nil
}

// MustPack is Pack for values known valid at compile/wiring time; panics on
// error.
func (cd *CallbackData[T]) MustPack(v T) string {
	s, err := cd.Pack(v)
	if err != nil {
		panic(err)
	}
	return s
}

// hmacTag computes the truncated, base64url-encoded HMAC-SHA256 tag for
// payload under cd.hmacSecret.
func (cd *CallbackData[T]) hmacTag(payload string) string {
	mac := hmac.New(sha256.New, cd.hmacSecret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:hmacTagLen])
}

// Unpack decodes a callback_data string produced by Pack. If this schema
// was built with [CallbackData.WithHMAC], the trailing tag is verified
// first — [ErrCallbackDataTampered] on any mismatch — before the payload is
// decoded.
func (cd *CallbackData[T]) Unpack(data string) (T, error) {
	var v T
	payload := data
	if cd.hmacSecret != nil {
		idx := strings.LastIndex(data, callbackDataSep)
		if idx < 0 {
			return v, fmt.Errorf("golagram: CallbackData %q: %w", data, ErrCallbackDataTampered)
		}
		var tagStr string
		payload, tagStr = data[:idx], data[idx+1:]
		wantTag, err := base64.RawURLEncoding.DecodeString(tagStr)
		if err != nil || len(wantTag) != hmacTagLen {
			return v, fmt.Errorf("golagram: CallbackData %q: %w", data, ErrCallbackDataTampered)
		}
		mac := hmac.New(sha256.New, cd.hmacSecret)
		mac.Write([]byte(payload))
		if gotTag := mac.Sum(nil)[:hmacTagLen]; !hmac.Equal(gotTag, wantTag) {
			return v, fmt.Errorf("golagram: CallbackData %q: %w", data, ErrCallbackDataTampered)
		}
	}
	parts := strings.Split(payload, callbackDataSep)
	if parts[0] != cd.prefix {
		return v, fmt.Errorf("golagram: CallbackData: %q does not start with prefix %q", data, cd.prefix)
	}
	if len(parts)-1 != len(cd.fields) {
		return v, fmt.Errorf("golagram: CallbackData %q: got %d fields, schema has %d", data, len(parts)-1, len(cd.fields))
	}
	rv := reflect.ValueOf(&v).Elem()
	for i, f := range cd.fields {
		part := parts[i+1]
		fv := rv.FieldByIndex(f.Index)
		switch f.Type.Kind() {
		case reflect.String:
			fv.SetString(part)
		case reflect.Bool:
			b, err := strconv.ParseBool(part)
			if err != nil {
				return v, fmt.Errorf("golagram: CallbackData %q field %s: %w", data, f.Name, err)
			}
			fv.SetBool(b)
		case reflect.Float64:
			fl, err := strconv.ParseFloat(part, 64)
			if err != nil {
				return v, fmt.Errorf("golagram: CallbackData %q field %s: %w", data, f.Name, err)
			}
			fv.SetFloat(fl)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return v, fmt.Errorf("golagram: CallbackData %q field %s: %w", data, f.Name, err)
			}
			fv.SetUint(u)
		default: // int kinds
			n, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				return v, fmt.Errorf("golagram: CallbackData %q field %s: %w", data, f.Name, err)
			}
			fv.SetInt(n)
		}
	}
	return v, nil
}

// FromCtx unpacks the current callback query's data. Errors if the update
// isn't a callback query or its data doesn't match this schema — pair with
// [CallbackData.Filter] on the registration so it can't be.
func (cd *CallbackData[T]) FromCtx(c *Ctx) (T, error) {
	var v T
	if c.CallbackQuery == nil {
		return v, fmt.Errorf("golagram: CallbackData.FromCtx: update is not a callback query")
	}
	return cd.Unpack(c.CallbackQuery.Data)
}

// Filter matches callback queries whose data carries this schema's prefix.
func (cd *CallbackData[T]) Filter() Filter {
	return func(c *Ctx) bool {
		if c.CallbackQuery == nil {
			return false
		}
		data := c.CallbackQuery.Data
		return data == cd.prefix || strings.HasPrefix(data, cd.prefix+callbackDataSep)
	}
}

// FilterWhere matches callback queries whose data unpacks into this schema
// AND satisfies pred — a typed predicate over the payload:
//
//	buyCB.FilterWhere(func(b BuyItem) bool { return b.Qty > 1 })
func (cd *CallbackData[T]) FilterWhere(pred func(T) bool) Filter {
	return func(c *Ctx) bool {
		if c.CallbackQuery == nil {
			return false
		}
		v, err := cd.Unpack(c.CallbackQuery.Data)
		if err != nil {
			return false
		}
		return pred(v)
	}
}

// Button builds an inline keyboard button carrying the packed payload.
// Panics on a payload that fails Pack (over 64 bytes / separator in a
// string field) — a button that Telegram would silently refuse to send is
// a wiring bug, surface it at build time.
func (cd *CallbackData[T]) Button(text string, v T) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, CallbackData: cd.MustPack(v)}
}
