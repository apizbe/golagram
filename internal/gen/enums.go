package main

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// enumMember is one named constant within an enumGroup.
type enumMember struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// enumGroup becomes one `const ( ... )` block in consts.gen.go. GoPrefix is
// prepended to each member's Name for the exported identifier (e.g. prefix
// "ChatType" + member name "Private" -> ChatTypePrivate).
type enumGroup struct {
	GoPrefix string       `json:"goPrefix"`
	Doc      string       `json:"doc"`
	Members  []enumMember `json:"members"`
}

// curatedEnums is the shape of internal/gen/enums.json: enum groups whose
// values the Bot API docs don't enumerate anywhere scrapable — see the doc
// string on each group in that file for why.
type curatedEnums struct {
	Groups []enumGroup `json:"groups"`
}

// loadCuratedEnums parses internal/gen/enums.json's hand-curated groups.
func loadCuratedEnums(data []byte) ([]enumGroup, error) {
	var c curatedEnums
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return c.Groups, nil
}

// quotedListRE pulls curly- or straight-quoted lowercase/underscore tokens
// out of scraped docs prose, e.g. Chat.type's description: `can be either
// "private", "group", "supergroup" or "channel"` (with curly quotes in the
// actual source).
var quotedListRE = regexp.MustCompile(`[“"]([a-z_]+)[”"]`)

// findItem looks up an Item by its api.json name.
func findItem(items []Item, name string) (Item, bool) {
	for _, it := range items {
		if it.Name == name {
			return it, true
		}
	}
	return Item{}, false
}

// findFieldDescription looks up field's scraped docs description among
// it.Fields (a type) or it.Params (a method).
func findFieldDescription(it Item, field string) (string, bool) {
	for _, f := range it.Fields {
		if f.Name == field {
			return f.Description, true
		}
	}
	for _, p := range it.Params {
		if p.Name == field {
			return p.Description, true
		}
	}
	return "", false
}

// scrapedEnum extracts an enum's members from the quoted value list in one
// field's docs description (a type's field or a method's param — both live
// in Item.Fields/Item.Params). It errors loudly rather than emitting an
// empty or stale const block if scripts/api.json changes shape underneath
// it — the same "fail, don't guess" philosophy as the rest of internal/gen.
func scrapedEnum(items []Item, itemName, field, goPrefix, doc string) (enumGroup, error) {
	it, ok := findItem(items, itemName)
	if !ok {
		return enumGroup{}, fmt.Errorf("consts: item %q not found in spec (scripts/api.json changed?)", itemName)
	}
	desc, ok := findFieldDescription(it, field)
	if !ok {
		return enumGroup{}, fmt.Errorf("consts: %s.%s not found in spec", itemName, field)
	}
	matches := quotedListRE.FindAllStringSubmatch(desc, -1)
	if len(matches) == 0 {
		return enumGroup{}, fmt.Errorf("consts: no quoted enum values found in %s.%s description: %q", itemName, field, desc)
	}
	seen := map[string]bool{}
	var members []enumMember
	for _, m := range matches {
		v := m[1]
		if seen[v] {
			continue
		}
		seen[v] = true
		members = append(members, enumMember{Name: fieldName(v), Value: v})
	}
	return enumGroup{GoPrefix: goPrefix, Doc: doc, Members: members}, nil
}

// buildEnumGroups assembles every enumGroup that consts.gen.go renders: the
// ones scraped straight from scripts/api.json's docs prose, followed by the
// hand-curated groups in enumsPath (internal/gen/enums.json) for values the
// docs don't enumerate anywhere scrapable.
func buildEnumGroups(items []Item, enumsJSON []byte) ([]enumGroup, error) {
	var groups []enumGroup

	chatType, err := scrapedEnum(items, "Chat", "type", "ChatType",
		"ChatType values for Chat.Type, scraped from Chat.type's docs description.")
	if err != nil {
		return nil, err
	}
	groups = append(groups, chatType)

	entityType, err := scrapedEnum(items, "MessageEntity", "type", "Entity",
		"MessageEntity.Type values, scraped from MessageEntity.type's docs description.\n"+
			"Formatting-only entities (Bold, Italic, ...) are included even though\n"+
			"[FilterHasEntity]'s doc comment steers callers toward the content-bearing\n"+
			"ones (Mention, Hashtag, URL, ...) instead.")
	if err != nil {
		return nil, err
	}
	groups = append(groups, entityType)

	buttonStyle, err := scrapedEnum(items, "InlineKeyboardButton", "style", "ButtonStyle",
		"ButtonStyle values for InlineKeyboardButton.Style / KeyboardButton.Style,\n"+
			"scraped from InlineKeyboardButton.style's docs description (KeyboardButton's\n"+
			"field carries the identical wording). If omitted, Telegram clients use an\n"+
			"app-specific default style.")
	if err != nil {
		return nil, err
	}
	groups = append(groups, buttonStyle)

	curated, err := loadCuratedEnums(enumsJSON)
	if err != nil {
		return nil, fmt.Errorf("parsing internal/gen/enums.json: %w", err)
	}
	groups = append(groups, curated...)

	return groups, nil
}
