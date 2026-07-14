package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update internal/gen/testdata golden files instead of checking against them")

// freshUnionState swaps unionsBySpecName/unionGoNames (populated by
// prepareSpec) for empty maps and returns a func to restore the originals.
// Without it, a mini-spec's prepareSpec call would inherit unions left
// behind by whichever other test in this package (real-spec included)
// happened to run first — Go doesn't isolate globals between tests in the
// same package — and renderUnionUnmarshalers iterates all of
// unionsBySpecName, so contamination would leak real API unions into the
// mini-spec's supposedly self-contained golden output.
func freshUnionState() (restore func()) {
	oldUnions, oldNames := unionsBySpecName, unionGoNames
	unionsBySpecName = map[string]*unionInfo{}
	unionGoNames = map[string]bool{}
	return func() { unionsBySpecName, unionGoNames = oldUnions, oldNames }
}

func TestFieldName(t *testing.T) {
	cases := map[string]string{
		"message_id":   "MessageID",
		"chat_id":      "ChatID",
		"from_chat_id": "FromChatID",
		"url":          "URL",
		"web_app_url":  "WebAppURL",
		"text":         "Text",
		"parse_mode":   "ParseMode",
		"is_bot":       "IsBot",
	}
	for in, want := range cases {
		if got := fieldName(in); got != want {
			t.Errorf("fieldName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGoTypeName(t *testing.T) {
	cases := map[string]string{
		"LoginUrl":    "LoginURL",
		"MessageId":   "MessageID",
		"RichTextUrl": "RichTextURL",
		"Message":     "Message", // unaffected names pass through
	}
	for in, want := range cases {
		if got := goTypeName(in); got != want {
			t.Errorf("goTypeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveType(t *testing.T) {
	cases := []struct {
		apiType, fieldName, want string
	}{
		{"Integer", "date", "int64"},
		{"String", "text", "string"},
		{"Boolean", "is_bot", "bool"},
		{"True", "is_forum", "bool"},
		{"Float", "latitude", "float64"},
		{"Array of String", "active_usernames", "[]string"},
		{"Array of Integer", "option_ids", "[]int64"},
		{"Array of Array of PhotoSize", "photos", "[][]PhotoSize"},
		{"Integer or String", "chat_id", "ChatID"},
		{"Integer or String", "from_chat_id", "ChatID"},
		{"InputFile or String", "photo", "InputFile"},
		{"InputFile", "sticker", "InputFile"},
		{replyMarkupUnionText, "reply_markup", "ReplyMarkup"},
		{"MessageEntity", "entities", "MessageEntity"},
		{"Array of MessageEntity", "entities", "[]MessageEntity"},
		{"MaybeInaccessibleMessage", "message", "Message"}, // flattened union

		{"StickerSet", "result", "StickerSet"},
		// The one compound "Array of X, Y and Z" case in the spec.
		{"Array of InputMediaAudio, InputMediaDocument, InputMediaLivePhoto, InputMediaPhoto and InputMediaVideo", "media", "[]InputMedia"},
	}
	for _, c := range cases {
		if got := resolveType(c.apiType, c.fieldName); got != c.want {
			t.Errorf("resolveType(%q, %q) = %q, want %q", c.apiType, c.fieldName, got, c.want)
		}
	}
}

func TestFieldTypeAndTag_PointerWrapsStructsNotPrimitivesOrInterfaces(t *testing.T) {
	unionGoNames["SomeUnion"] = true
	defer delete(unionGoNames, "SomeUnion")

	cases := []struct {
		name             string
		structName       string
		apiType          string
		fieldName        string
		description      string
		optional         bool
		wantGoType       string
		wantTagOmitempty bool
	}{
		{"required primitive", "", "Integer", "date", "", false, "int64", false},
		{"optional primitive", "", "String", "caption", "", true, "string", true},
		{"optional slice", "", "Array of String", "usernames", "", true, "[]string", true},
		{"required named struct gets pointer", "", "PhotoSize", "thumb", "", false, "*PhotoSize", false},
		{"optional named struct gets pointer", "", "PhotoSize", "thumb", "", true, "*PhotoSize", true},
		{"chat_id is never pointer-wrapped", "", "Integer or String", "chat_id", "", false, "ChatID", false},
		{"reply_markup interface is never pointer-wrapped", "", replyMarkupUnionText, "reply_markup", "", true, "ReplyMarkup", true},
		{"union interface is never pointer-wrapped", "", "SomeUnion", "origin", "", true, "SomeUnion", true},
		{"media fixup widens String to InputFile", "InputMediaPhoto", "String", "media", "", false, "InputFile", false},
		{"non-fixed-up field of the same type stays string", "InputMediaPhoto", "String", "caption", "", true, "string", true},
		// The default-true rule: an optional Boolean Telegram defaults to
		// true must be *bool, or false is unsendable through omitempty.
		{"optional default-true bool gets pointer", "", "Boolean", "is_anonymous", "True, if the poll needs to be anonymous, defaults to True", true, "*bool", true},
		{"required default-true bool stays plain", "", "Boolean", "is_anonymous", "defaults to True", false, "bool", false},
		{"optional plain bool stays plain", "", "Boolean", "disable_notification", "Sends the message silently.", true, "bool", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goType, tag := fieldTypeAndTag(c.structName, c.apiType, c.fieldName, c.description, c.optional)
			if goType != c.wantGoType {
				t.Errorf("goType = %q, want %q", goType, c.wantGoType)
			}
			hasOmitempty := tag != c.fieldName
			if hasOmitempty != c.wantTagOmitempty {
				t.Errorf("tag = %q, want omitempty=%v", tag, c.wantTagOmitempty)
			}
		})
	}
}

func TestParseReturnType(t *testing.T) {
	known := []string{"Message", "User", "StickerSet", "Update", "ChatFullInfo"}
	cases := []struct {
		returns  string
		wantType string
		wantKind string
	}{
		{"On success, the sent Message is returned.", "*Message", "object"},
		{"Returns an Array of Update objects.", "[]Update", "array"},
		{"Returns True on success.", "bool", "bool"},
		{"Returns basic information about the bot in form of a User object.", "*User", "object"},
		{"Something entirely unrecognized happens.", "json.RawMessage", "raw"},
	}
	for _, c := range cases {
		gotType, gotKind := parseReturnType(c.returns, known)
		if gotType != c.wantType || gotKind != c.wantKind {
			t.Errorf("parseReturnType(%q) = (%q, %q), want (%q, %q)",
				c.returns, gotType, gotKind, c.wantType, c.wantKind)
		}
	}
}

func TestScrapedEnum(t *testing.T) {
	items := []Item{
		{
			Name: "Chat",
			Kind: "type",
			Fields: []Field{
				{Name: "type", Type: "String", Description: `Type of the chat, can be either “private”, “group”, “supergroup” or “channel”`},
			},
		},
		{
			Name: "empty",
			Kind: "type",
			Fields: []Field{
				{Name: "type", Type: "String", Description: "no quoted values here"},
			},
		},
	}

	g, err := scrapedEnum(items, "Chat", "type", "ChatType", "doc")
	if err != nil {
		t.Fatalf("scrapedEnum: %v", err)
	}
	want := []enumMember{
		{Name: "Private", Value: "private"},
		{Name: "Group", Value: "group"},
		{Name: "Supergroup", Value: "supergroup"},
		{Name: "Channel", Value: "channel"},
	}
	if len(g.Members) != len(want) {
		t.Fatalf("got %d members, want %d: %+v", len(g.Members), len(want), g.Members)
	}
	for i, m := range g.Members {
		if m != want[i] {
			t.Errorf("member %d = %+v, want %+v", i, m, want[i])
		}
	}

	if _, err := scrapedEnum(items, "NoSuchItem", "type", "X", "doc"); err == nil {
		t.Error("scrapedEnum: expected error for missing item, got nil")
	}
	if _, err := scrapedEnum(items, "Chat", "no_such_field", "X", "doc"); err == nil {
		t.Error("scrapedEnum: expected error for missing field, got nil")
	}
	if _, err := scrapedEnum(items, "empty", "type", "X", "doc"); err == nil {
		t.Error("scrapedEnum: expected error when no quoted values are found, got nil")
	}
}

func TestLoadCuratedEnums(t *testing.T) {
	data := []byte(`{"groups":[{"goPrefix":"ParseMode","doc":"d","members":[{"name":"HTML","value":"HTML"}]}]}`)
	groups, err := loadCuratedEnums(data)
	if err != nil {
		t.Fatalf("loadCuratedEnums: %v", err)
	}
	if len(groups) != 1 || groups[0].GoPrefix != "ParseMode" || len(groups[0].Members) != 1 {
		t.Errorf("loadCuratedEnums = %+v, want one ParseMode group with one member", groups)
	}
}

// TestGoldenOutput renders internal/gen against a small, frozen,
// hand-written spec (testdata/mini_spec.json) and diffs the result against
// committed golden files — unlike TestGeneratedFilesAreUpToDate, which only
// catches drift against the *current* scripts/api.json, this catches a
// generator refactor that changes output shape even when today's real spec
// happens not to exercise the affected code path (or happens to produce the
// same bytes by coincidence). The mini spec is deliberately small enough to
// read end to end: two struct types sharing a field for the Creature union,
// and one method per return-type kind (object, array, bool, union, raw).
//
// Regenerate the golden files after an intentional generator change with:
//
//	go test ./internal/gen -run TestGoldenOutput -update
func TestGoldenOutput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "mini_spec.json"))
	if err != nil {
		t.Fatalf("reading testdata/mini_spec.json: %v", err)
	}
	spec, err := parseSpec(data)
	if err != nil {
		t.Fatalf("parsing testdata/mini_spec.json: %v", err)
	}

	defer freshUnionState()()
	if err := prepareSpec(spec); err != nil {
		t.Fatalf("prepareSpec: %v", err)
	}

	for _, tc := range []struct {
		golden string
		render func() string
	}{
		{"types.golden.go", func() string { return renderTypesFile(spec.APIVersion, spec.Items) }},
		{"methods.golden.go", func() string { return renderMethodsFile(spec.APIVersion, spec.Items) }},
	} {
		fresh, err := formatOrRaw(tc.render())
		if err != nil {
			t.Fatalf("formatting freshly generated %s: %v", tc.golden, err)
		}

		goldenPath := filepath.Join("testdata", tc.golden)
		if *updateGolden {
			if err := os.WriteFile(goldenPath, []byte(fresh), 0o644); err != nil {
				t.Fatalf("writing %s: %v", goldenPath, err)
			}
			continue
		}

		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("reading %s (run with -update to create it): %v", goldenPath, err)
		}
		if string(want) != fresh {
			t.Errorf("%s output changed from the committed golden file.\n"+
				"If this is an intentional generator change, regenerate with:\n"+
				"\tgo test ./internal/gen -run TestGoldenOutput -update\n\ngot:\n%s", tc.golden, fresh)
		}
	}
}

// TestGeneratedFilesAreUpToDate protects against someone hand-editing
// types.gen.go/methods.gen.go, or scripts/api.json changing without
// regenerating: it re-runs the generator into a temp dir and diffs against
// what's committed at the repo root.
func TestGeneratedFilesAreUpToDate(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "scripts", "api.json"))
	if err != nil {
		t.Fatalf("reading scripts/api.json: %v", err)
	}
	spec, err := parseSpec(data)
	if err != nil {
		t.Fatalf("parsing scripts/api.json: %v", err)
	}
	if err := prepareSpec(spec); err != nil {
		t.Fatalf("prepareSpec: %v", err)
	}

	enumsJSON, err := os.ReadFile(filepath.Join(root, "internal", "gen", "enums.json"))
	if err != nil {
		t.Fatalf("reading internal/gen/enums.json: %v", err)
	}
	groups, err := buildEnumGroups(spec.Items, enumsJSON)
	if err != nil {
		t.Fatalf("buildEnumGroups: %v", err)
	}

	for _, tc := range []struct {
		committedPath string
		render        func() string
	}{
		{"types.gen.go", func() string { return renderTypesFile(spec.APIVersion, spec.Items) }},
		{"methods.gen.go", func() string { return renderMethodsFile(spec.APIVersion, spec.Items) }},
		{"consts.gen.go", func() string { return renderConstsFile(spec.APIVersion, groups) }},
	} {
		committed, err := os.ReadFile(filepath.Join(root, tc.committedPath))
		if err != nil {
			t.Fatalf("reading committed %s: %v", tc.committedPath, err)
		}
		fresh, err := formatOrRaw(tc.render())
		if err != nil {
			t.Fatalf("formatting freshly generated %s: %v", tc.committedPath, err)
		}
		if string(committed) != fresh {
			t.Errorf("%s is stale: run `go generate ./...` from the repo root and commit the result", tc.committedPath)
		}
	}
}
