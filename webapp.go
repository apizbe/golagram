package golagram

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// WebAppUser is the user object inside validated WebApp init data. It is
// defined by the Mini Apps docs, not the Bot API spec, so it is
// hand-written rather than generated.
type WebAppUser struct {
	ID                    int64  `json:"id"`
	IsBot                 bool   `json:"is_bot,omitempty"`
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name,omitempty"`
	Username              string `json:"username,omitempty"`
	LanguageCode          string `json:"language_code,omitempty"`
	IsPremium             bool   `json:"is_premium,omitempty"`
	AddedToAttachmentMenu bool   `json:"added_to_attachment_menu,omitempty"`
	AllowsWriteToPM       bool   `json:"allows_write_to_pm,omitempty"`
	PhotoURL              string `json:"photo_url,omitempty"`
}

// WebAppChat is the chat object inside validated WebApp init data (present
// when the Mini App was launched from a group/supergroup/channel's attach
// menu).
type WebAppChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username,omitempty"`
	PhotoURL string `json:"photo_url,omitempty"`
}

// WebAppInitData is the parsed, verified payload of
// window.Telegram.WebApp.initData. Only [ValidateWebAppInitData] (bot-token
// HMAC) or [ValidateWebAppInitDataThirdParty] (Telegram's own Ed25519 key,
// no bot token needed) produce one — if you're holding a WebAppInitData,
// whichever of the two you called has confirmed the data came from
// Telegram.
type WebAppInitData struct {
	QueryID      string
	User         *WebAppUser
	Receiver     *WebAppUser
	Chat         *WebAppChat
	ChatType     string
	ChatInstance string
	StartParam   string
	// CanSendAfter is the delay after which [TelegramBot.AnswerWebAppQuery]
	// may be called (zero if Telegram sent none).
	CanSendAfter time.Duration
	AuthDate     time.Time
	// Hash is the bot-token HMAC field — populated whichever validator ran,
	// but only actually checked by [ValidateWebAppInitData].
	Hash string
	// Signature is the Ed25519 field — populated whichever validator ran,
	// but only actually checked by [ValidateWebAppInitDataThirdParty].
	Signature string
	// Raw holds every field as received, including ones this struct
	// doesn't model — all of them participated in the verified hash.
	Raw url.Values
}

// ValidateWebAppInitData authenticates a Mini App's initData string
// (posted to your backend by the WebApp's own JavaScript) against the bot
// token, per the Mini Apps spec: secret = HMAC-SHA256(bot_token, key
// "WebAppData"); hash = hex(HMAC-SHA256(data-check-string, secret)).
// This is security-critical — never trust user/query_id from initData
// that hasn't passed here.
//
// maxAge bounds how old the payload's auth_date may be (replay
// protection); pass 0 to skip the freshness check. Telegram recommends
// checking — an old but validly-signed payload can be replayed by anyone
// who once saw it.
func ValidateWebAppInitData(initData, botToken string, maxAge time.Duration) (*WebAppInitData, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, &ValidationError{Field: "init data", Message: "not a valid query string: " + err.Error()}
	}
	gotHash := values.Get("hash")
	if gotHash == "" {
		return nil, &ValidationError{Field: "init data", Message: "no hash field — not Telegram init data"}
	}

	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(botToken))
	secret := mac.Sum(nil)

	mac = hmac.New(sha256.New, secret)
	mac.Write([]byte(dataCheckString(values, "hash")))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(gotHash)) {
		return nil, &ValidationError{Field: "init data", Message: "hash mismatch — data is not from Telegram or the wrong bot token was used"}
	}

	d, err := parseWebAppInitDataFields(values, maxAge)
	if err != nil {
		return nil, err
	}
	d.Hash = gotHash
	d.Signature = values.Get("signature")
	return d, nil
}

// WebAppEnvironment selects which of Telegram's published Ed25519 public
// keys [ValidateWebAppInitDataThirdParty] verifies a signature against —
// production bots and Telegram's test environment sign with different
// keys.
type WebAppEnvironment int

const (
	// WebAppProd is Telegram's production environment.
	WebAppProd WebAppEnvironment = iota
	// WebAppTest is Telegram's test environment (a separate DC bots opt
	// into for development; see Telegram's docs on testing your bot).
	WebAppTest
)

// key returns the public key this environment verifies against.
func (env WebAppEnvironment) key() ed25519.PublicKey {
	if env == WebAppTest {
		return webAppEd25519TestKey
	}
	return webAppEd25519ProdKey
}

// webAppEd25519ProdKey/TestKey are Telegram's published Ed25519 public
// keys for third-party Mini App init-data validation (Bot API "Validating
// data received via the Mini App" docs, Ed25519 section) — fixed values
// Telegram itself controls, not derived from anything caller-supplied.
var (
	webAppEd25519ProdKey = mustHexEd25519Key("e7bf03a2fa4602af4580703d88dda5bb59f32ed8b02a56c187fe7d34caed242d")
	webAppEd25519TestKey = mustHexEd25519Key("40055058a4ee38156a06562e52eece92a771bcd8346a8c4615cb7376eddf72ec")
)

func mustHexEd25519Key(hexKey string) ed25519.PublicKey {
	b, err := hex.DecodeString(hexKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("golagram: invalid embedded WebApp Ed25519 public key")
	}
	return ed25519.PublicKey(b)
}

// ValidateWebAppInitDataThirdParty authenticates initData via Telegram's
// third-party Ed25519 flow instead of [ValidateWebAppInitData]'s bot-token
// HMAC: it verifies the init data's "signature" field against one of
// Telegram's own published public keys (chosen by env), so a server that
// never holds the bot token at all — a separate backend behind the actual
// bot, for instance — can still confirm a payload really came from
// Telegram for botID. Per Telegram's Mini Apps docs, the signed message is
// "{botID}:WebAppData\n" followed by every field except hash and
// signature, sorted by key, as "key=value" lines joined with "\n"; the
// signature itself arrives base64url-encoded.
//
// maxAge bounds how old auth_date may be, same as
// [ValidateWebAppInitData]; pass 0 to skip the freshness check.
func ValidateWebAppInitDataThirdParty(initData string, botID int64, env WebAppEnvironment, maxAge time.Duration) (*WebAppInitData, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, &ValidationError{Field: "init data", Message: "not a valid query string: " + err.Error()}
	}
	gotSig := values.Get("signature")
	if gotSig == "" {
		return nil, &ValidationError{Field: "init data", Message: "no signature field — not eligible for third-party (Ed25519) validation"}
	}
	sig, err := decodeBase64URL(gotSig)
	if err != nil {
		return nil, &ValidationError{Field: "init data", Message: "signature is not valid base64url: " + err.Error()}
	}

	message := strconv.FormatInt(botID, 10) + ":WebAppData\n" + dataCheckString(values, "hash", "signature")
	if !ed25519.Verify(env.key(), []byte(message), sig) {
		return nil, &ValidationError{Field: "init data", Message: "Ed25519 signature mismatch — data is not from Telegram, botID is wrong, or the wrong environment (prod/test) was checked"}
	}

	d, err := parseWebAppInitDataFields(values, maxAge)
	if err != nil {
		return nil, err
	}
	d.Hash = values.Get("hash")
	d.Signature = gotSig
	return d, nil
}

// decodeBase64URL decodes a base64url string, trying both the padded and
// unpadded alphabets — Telegram's own docs don't pin one, and a stray
// padding mismatch shouldn't be the reason a genuine signature fails.
func decodeBase64URL(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// parseWebAppInitDataFields fills every WebAppInitData field shared
// between [ValidateWebAppInitData] and [ValidateWebAppInitDataThirdParty]
// once each has verified the signature its own way — query_id, user,
// receiver, chat, chat_type, chat_instance, start_param, can_send_after,
// auth_date (plus the freshness check against maxAge). Hash/Signature/Raw
// are left for the caller, since which field the caller verified differs
// between the two.
func parseWebAppInitDataFields(values url.Values, maxAge time.Duration) (*WebAppInitData, error) {
	d := &WebAppInitData{
		QueryID:      values.Get("query_id"),
		ChatType:     values.Get("chat_type"),
		ChatInstance: values.Get("chat_instance"),
		StartParam:   values.Get("start_param"),
		Raw:          values,
	}
	if v := values.Get("auth_date"); v != "" {
		unix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, &ValidationError{Field: "init data", Message: "auth_date is not a unix timestamp: " + v}
		}
		d.AuthDate = time.Unix(unix, 0)
	}
	if v := values.Get("can_send_after"); v != "" {
		secs, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, &ValidationError{Field: "init data", Message: "can_send_after is not a number: " + v}
		}
		d.CanSendAfter = time.Duration(secs) * time.Second
	}
	for field, dst := range map[string]any{
		"user": &d.User, "receiver": &d.Receiver, "chat": &d.Chat,
	} {
		if v := values.Get(field); v != "" {
			if err := json.Unmarshal([]byte(v), dst); err != nil {
				return nil, &ValidationError{Field: "init data", Message: field + " is not valid JSON: " + err.Error()}
			}
		}
	}

	if maxAge > 0 {
		if d.AuthDate.IsZero() {
			return nil, &ValidationError{Field: "init data", Message: "no auth_date to check freshness against"}
		}
		if age := time.Since(d.AuthDate); age > maxAge {
			return nil, &ValidationError{Field: "init data", Message: "expired: signed " + age.Round(time.Second).String() + " ago, max age " + maxAge.String()}
		}
	}
	return d, nil
}

// LoginWidgetData is the parsed, HMAC-verified payload of a Telegram Login
// Widget callback. Only [ValidateLoginWidgetData] produces one.
type LoginWidgetData struct {
	ID        int64
	FirstName string
	LastName  string
	Username  string
	PhotoURL  string
	AuthDate  time.Time
	Hash      string
}

// ValidateLoginWidgetData authenticates the query parameters a Telegram
// Login Widget redirect/callback delivered, per the Login Widget spec:
// hash = hex(HMAC-SHA256(data-check-string, SHA256(bot_token))). Note the
// key derivation differs from WebApp init data (plain SHA256, no
// "WebAppData"). maxAge bounds auth_date staleness; 0 skips the check.
func ValidateLoginWidgetData(values url.Values, botToken string, maxAge time.Duration) (*LoginWidgetData, error) {
	gotHash := values.Get("hash")
	if gotHash == "" {
		return nil, &ValidationError{Field: "login data", Message: "no hash field — not a login widget payload"}
	}

	key := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(dataCheckString(values, "hash")))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(gotHash)) {
		return nil, &ValidationError{Field: "login data", Message: "hash mismatch — data is not from Telegram or the wrong bot token was used"}
	}

	d := &LoginWidgetData{
		FirstName: values.Get("first_name"),
		LastName:  values.Get("last_name"),
		Username:  values.Get("username"),
		PhotoURL:  values.Get("photo_url"),
		Hash:      gotHash,
	}
	if v := values.Get("id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, &ValidationError{Field: "login data", Message: "id is not a number: " + v}
		}
		d.ID = id
	}
	if v := values.Get("auth_date"); v != "" {
		unix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, &ValidationError{Field: "login data", Message: "auth_date is not a unix timestamp: " + v}
		}
		d.AuthDate = time.Unix(unix, 0)
	}

	if maxAge > 0 {
		if d.AuthDate.IsZero() {
			return nil, &ValidationError{Field: "login data", Message: "no auth_date to check freshness against"}
		}
		if age := time.Since(d.AuthDate); age > maxAge {
			return nil, &ValidationError{Field: "login data", Message: "expired: signed " + age.Round(time.Second).String() + " ago, max age " + maxAge.String()}
		}
	}
	return d, nil
}

// dataCheckString builds Telegram's data-check-string: every received
// field except those in exclude, as key=value lines sorted by key.
// Repeated keys (which Telegram never sends, but an attacker might) each
// contribute a line, so smuggling a duplicate can't bypass the signature.
func dataCheckString(values url.Values, exclude ...string) string {
	skip := make(map[string]bool, len(exclude))
	for _, k := range exclude {
		skip[k] = true
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if !skip[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		for _, v := range values[k] {
			lines = append(lines, k+"="+v)
		}
	}
	return strings.Join(lines, "\n")
}
