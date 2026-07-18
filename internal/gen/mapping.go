package main

import (
	"fmt"
	"regexp"
	"strings"
)

// skipTypes are Bot API types that must stay hand-written.
//
// Update carries dispatch semantics (which pointer is non-nil is the routing
// key) and is small enough to audit by hand — it is verified complete against
// the spec (26/26 fields as of 10.1).
//
// Message, CallbackQuery, and MessageEntity used to be here too; they are
// generated now (with unexported binding fields injected via
// extraStructFields), because every hand-written spec-mirroring type this
// project has ever had drifted: User was missing 10/17 fields, Message
// 91/115, Entity 6/9. The generator is the only author allowed to know field
// lists.
var skipTypes = map[string]bool{
	"Update": true,
	// InputFile is a marker type with no fields in the spec — it's
	// hand-written (input_file.go) as `type InputFile = api.InputFile`,
	// an alias with file_id/URL/local-upload constructors. Fields/params
	// typed InputFile or "InputFile or String" resolve to it (see
	// resolveType); Client.Call switches a request to
	// multipart/form-data automatically whenever one carries a local upload.
	"InputFile": true,
}

// extraStructFields injects unexported fields into specific generated
// structs — the dispatch/sugar bindings that used to force Message and
// CallbackQuery to stay hand-written (and therefore drift). The sugar
// methods themselves live in hand-written files (message.go,
// callback_query.go); Go allows a type's methods in any file of the package.
var extraStructFields = map[string][]string{
	"Message": {
		"api *api.Client",
		"fsm FSMStorage",
		"fsmStrategy FSMKeyStrategy",
		"botUsername string",
		"boundCtx context.Context",
	},
	"CallbackQuery": {
		"api *api.Client",
		"fsm FSMStorage",
		"fsmStrategy FSMKeyStrategy",
		"boundCtx context.Context",
		"answered bool",
	},
}

// aliasedTypes get a `type X = Y` alias instead of their own struct/interface.
//
// MaybeInaccessibleMessage is the one union golagram deliberately flattens:
// its two members are Message and InaccessibleMessage, and InaccessibleMessage
// is structurally a Message subset (chat, message_id, date — with date always
// 0 as the inaccessibility signal). Modeling it as *Message keeps
// CallbackQuery.Message and Message.PinnedMessage directly usable; callers
// check Message.IsInaccessible() instead of type-switching a two-member union.
var aliasedTypes = map[string]string{
	"MaybeInaccessibleMessage": "Message",
}

// unionGoNames is every union's resolved Go identifier (excluding aliased
// unions, which resolve to a struct), populated by prepareSpec. Needed by
// fieldTypeAndTag to know that a union name is already an interface
// (nilable — never pointer-wrapped) rather than a struct (needs
// pointer-wrapping so `omitempty` actually omits it: Go's encoding/json
// never considers a non-pointer struct value "empty").
var unionGoNames = map[string]bool{}

var primitiveGoTypes = map[string]bool{
	"int64": true, "string": true, "bool": true, "float64": true,
}

// mediaFileFieldFixups widens specific struct fields from string to
// InputFile even though the spec itself types them plain "String" — a
// verified spec quirk: InputMediaPhoto/Video/Audio/Document/Animation's
// media/thumbnail fields and InputSticker's sticker field all describe the
// same file_id/URL/"attach://<name>" upload pattern InputFile/"InputFile or
// String" fields use elsewhere (see e.g. InputMediaPhoto.Media's
// description), but aren't spelled that way in Telegram's own type table —
// unlike method params, which do use "InputFile or String" correctly (see
// resolveType). Keyed by "SpecTypeName.json_field_name".
var mediaFileFieldFixups = map[string]bool{
	"InputMediaPhoto.media":         true,
	"InputMediaVideo.media":         true,
	"InputMediaVideo.thumbnail":     true,
	"InputMediaAudio.media":         true,
	"InputMediaAudio.thumbnail":     true,
	"InputMediaDocument.media":      true,
	"InputMediaDocument.thumbnail":  true,
	"InputMediaAnimation.media":     true,
	"InputMediaAnimation.thumbnail": true,
	// Live photos (10.1) can't be sent by URL per their description, but the
	// file_id/attach:// halves of the pattern apply — same quirk, same fix.
	"InputMediaLivePhoto.media": true,
	"InputMediaLivePhoto.photo": true,
	"InputSticker.sticker":      true,
}

// fieldTypeAndTag resolves a field/param's Go type and decides whether it
// needs pointer-wrapping, matching the convention already established by
// golagram's hand-written types (Message.Chat *Chat, Document.Thumbnail
// *PhotoSize, ...): every named struct type is a pointer, slices and
// primitives are plain values, and interfaces (ReplyMarkup, any union) are
// left bare since they're already nilable. Pointer-wrapping isn't just
// style: without it, an optional struct-typed field's `omitempty` JSON tag
// is silently a no-op, since a zero-value struct is never "empty" to
// encoding/json — only a nil pointer is.
//
// structName is the enclosing type's (or synthesized request struct's)
// spec name, used only to look up mediaFileFieldFixups — it never matches
// for method params, since those are keyed by concrete type names like
// "InputMediaPhoto", not request struct names.
//
// description drives one rule: an *optional* Boolean whose spec text says
// it "defaults to True" (sendPoll.is_anonymous,
// promoteChatMember.can_restrict_members in 10.1) becomes *bool — with a
// plain bool + omitempty, false is unsendable (encoding/json omits it and
// Telegram applies its true default), so e.g. a non-anonymous poll would
// be impossible to request. Detected from the description at generator
// runtime, like union discriminators, so a future spec revision picks up
// new such fields without a hand-kept list.
//
// See resolveType for how apiType itself maps to a Go type.
func fieldTypeAndTag(structName, apiType, jsonName, description string, optional bool) (goType, jsonTag string) {
	if mediaFileFieldFixups[structName+"."+jsonName] {
		goType = "InputFile"
	} else {
		goType = resolveType(apiType, jsonName)
	}
	jsonTag = jsonName
	if optional {
		if goType == "ChatID" {
			// omitzero (not omitempty): ChatID's zero value marshals as the
			// literal integer 0 via MarshalJSON, and Telegram treats chat_id:0
			// as a real, rejectable target rather than "absent" — unlike
			// InputFile below, this isn't cosmetic. omitzero uses ChatID's
			// IsZero() method (see render.go's generated preamble) so an unset
			// optional ChatID field (e.g. ReplyParameters.ChatID, meaning
			// "reply in the current chat") is actually omitted.
			jsonTag += ",omitzero"
		} else {
			jsonTag += ",omitempty"
		}
	}
	if optional && goType == "bool" && defaultsTrueRe.MatchString(description) {
		return "*bool", jsonTag
	}

	switch {
	case strings.HasPrefix(goType, "[]"):
	case primitiveGoTypes[goType]:
	case goType == "ChatID" || goType == "InputFile":
		// Value types by convention (like ReplyMarkup/unions below, minus
		// the interface nilability): InputFileID(...)/InputFileURL(...)/
		// InputFileUpload(...) return InputFile by value, so pointer-wrapping
		// would force callers into an awkward two-line "assign to a var,
		// then take its address" just to populate one field. Cost: an
		// unset *optional* InputFile field (e.g. thumbnail) on the
		// plain-JSON call path (no upload anywhere in the request) encodes
		// as `"thumbnail":""` instead of being omitted — encoding/json's
		// omitempty never considers a struct-kind value empty, pointer or
		// not, but InputFile's own MarshalJSON at least degrades that to an
		// empty string rather than a marshal error. Cosmetic, not a
		// functional gap — Telegram treats an empty optional string param
		// the same as an absent one. The multipart path (see
		// internal/api/client.go: callMultipart) special-cases InputFile
		// directly and skips it correctly regardless. ChatID gets its own
		// tag rule above instead — its zero-value degradation isn't cosmetic.
	case goType == "ReplyMarkup" || unionGoNames[goType]:
	default:
		goType = "*" + goType
	}
	return goType, jsonTag
}

// defaultsTrueRe spots optional booleans Telegram defaults to true — the
// ones where omitting false and sending false mean different things.
var defaultsTrueRe = regexp.MustCompile(`(?i)defaults to true`)

// replyMarkupUnion is the one recurring "or"-style union that's already
// hand-written as the ReplyMarkup interface (reply_markup.go). It never
// appears as a formal `kind: "union"` item — it only shows up as inline "or"
// text on `reply_markup` params — so it's matched by exact string instead.
const replyMarkupUnionText = "InlineKeyboardMarkup or ReplyKeyboardMarkup or ReplyKeyboardRemove or ForceReply"

var chatIDFieldNames = map[string]bool{"chat_id": true, "from_chat_id": true}

var arrayOfRE = regexp.MustCompile(`^Array of (.+)$`)

// compoundArrayElementFixups handles the one param in the spec where "Array
// of X" spells out a union's members inline instead of naming the union —
// sendMediaGroup's media param lists 5 of InputMedia's 6 members (excluding
// InputMediaAnimation, which media groups don't accept). Using the full
// InputMedia union here is slightly more permissive than the spec's prose,
// but Telegram's own API rejects an invalid combination with a normal error,
// so nothing is silently wrong — it just avoids a one-off narrower union
// type for a single field.
var compoundArrayElementFixups = map[string]string{
	"InputMediaAudio, InputMediaDocument, InputMediaLivePhoto, InputMediaPhoto and InputMediaVideo": "InputMedia",
}

// resolveType maps a Bot API type string (as it appears in a field's or
// param's "type") to a Go type expression, given the field's own JSON name
// (needed only to special-case chat_id/from_chat_id, the sole fields that
// use "Integer or String").
func resolveType(apiType, jsonFieldName string) string {
	if m := arrayOfRE.FindStringSubmatch(apiType); m != nil {
		inner := m[1]
		if fixed, ok := compoundArrayElementFixups[inner]; ok {
			inner = fixed
		}
		return "[]" + resolveType(inner, jsonFieldName)
	}

	switch apiType {
	case "Integer":
		return "int64"
	case "String":
		return "string"
	case "Boolean", "True":
		return "bool"
	case "Float":
		return "float64"
	case "Integer or String":
		if chatIDFieldNames[jsonFieldName] {
			return "ChatID"
		}
		// Not observed in the spec today (verified: only chat_id and
		// from_chat_id use this pattern) — fall back to string rather than
		// silently mistyping a field this generator has never seen.
		return "string"
	case "InputFile or String", "InputFile":
		return "InputFile"
	case replyMarkupUnionText:
		return "ReplyMarkup"
	default:
		if alias, ok := aliasedTypes[apiType]; ok {
			return alias
		}
		return goTypeName(apiType)
	}
}

// isOptionalTypeField reports whether a type's field (not a method param —
// those carry their own Required marker) is optional, per the "Optional. "
// description prefix convention scripts/parse_botapi.py emits.
func isOptionalTypeField(f Field) bool {
	return strings.HasPrefix(f.Description, "Optional.")
}

// unionFieldRef describes one union-typed field of a struct — the
// information renderStruct needs to emit a polymorphic UnmarshalJSON.
type unionFieldRef struct {
	goName    string // Go field name, e.g. "NewChatMember"
	jsonName  string // JSON key, e.g. "new_chat_member"
	union     *unionInfo
	isSlice   bool // "Array of <union>"
	optional  bool
	decodable bool // union has a generated unmarshal function
}

// unionFieldFor returns the union this field's type (transitively)
// references, or nil. Only direct union types and "Array of <union>" occur
// in the spec; deeper nesting would need more machinery and is checked for
// by prepareSpec.
func unionFieldFor(apiType string) (u *unionInfo, isSlice bool) {
	if m := arrayOfRE.FindStringSubmatch(apiType); m != nil {
		u = unionsBySpecName[m[1]]
		return u, true
	}
	return unionsBySpecName[apiType], false
}

// goDoc renders description as a Go doc comment, each line prefixed with
// prefix (indentation) then "// ", verbatim — Telegram's scraped prose is
// not reformatted or link-escaped, so a description containing something
// that looks like [bracket] syntax passes through as-is.
func goDoc(prefix, description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(description, "\n") {
		fmt.Fprintf(&b, "%s// %s\n", prefix, strings.TrimSpace(line))
	}
	return b.String()
}
