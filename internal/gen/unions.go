package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// unionInfo is everything the generator knows about one spec union: its
// members and — the part that makes polymorphic decoding possible — each
// member's discriminator value, extracted from the member type's own field
// descriptions ("The member's status in the chat, always “creator”",
// "Type of the result, must be photo", ...).
type unionInfo struct {
	specName  string
	goName    string
	discField string // JSON field the members discriminate on: "type", "status", or "source"
	members   []unionMemberInfo
	// decodable means a generated unmarshal<Union> function exists: every
	// member has a discriminator value and no two members share one.
	// Non-decodable unions (InputMessageContent has no discriminator field
	// at all; InlineQueryResult reuses values across Cached/non-Cached
	// members) are input-only in the spec, so encoding — which works for
	// any interface — is all they need.
	decodable bool
}

// unionMemberInfo is one concrete member of a unionInfo.
type unionMemberInfo struct {
	specName  string
	goName    string
	discValue string
}

// unionsBySpecName is populated by prepareSpec before any rendering happens.
var unionsBySpecName = map[string]*unionInfo{}

// discFieldNames are the only field names Telegram uses as union
// discriminators (verified exhaustively against api.json 10.1).
var discFieldNames = map[string]bool{"type": true, "status": true, "source": true}

var discValueREs = []*regexp.Regexp{
	// “always “creator”” — curly quotes, as Telegram's HTML renders them.
	regexp.MustCompile(`always “([^”]+)”`),
	regexp.MustCompile(`always "([^"]+)"`),
	// “Type of the result, must be photo” / “must be “photo””.
	regexp.MustCompile(`must be “([^”]+)”`),
	regexp.MustCompile(`must be "([^"]+)"`),
	regexp.MustCompile(`must be ([a-z0-9_]+)`),
}

// discriminatorOf finds the (field, value) pair pinning item to a constant
// discriminator, looking only at fields named type/status/source so prose
// like "must be positive" on unrelated fields can't produce a false match.
func discriminatorOf(item *Item) (field, value string, ok bool) {
	for _, f := range item.Fields {
		if !discFieldNames[f.Name] {
			continue
		}
		for _, re := range discValueREs {
			if m := re.FindStringSubmatch(f.Description); m != nil {
				return f.Name, m[1], true
			}
		}
	}
	return "", "", false
}

// prepareSpec fills the generator's lookup tables (unionsBySpecName,
// unionGoNames) from the parsed spec. Both main() and the determinism test
// call this, so the two can't drift. It returns an error when the spec
// violates an assumption the generated decode logic depends on, rather than
// generating silently-wrong code.
func prepareSpec(spec *Spec) error {
	items := map[string]*Item{}
	for i := range spec.Items {
		items[spec.Items[i].Name] = &spec.Items[i]
	}

	for i := range spec.Items {
		it := &spec.Items[i]
		if it.Kind != "union" {
			continue
		}
		if _, aliased := aliasedTypes[it.Name]; aliased {
			continue // flattened to a struct (MaybeInaccessibleMessage → Message)
		}

		u := &unionInfo{specName: it.Name, goName: goTypeName(it.Name), decodable: true}
		seenValues := map[string]bool{}
		for _, m := range it.Members {
			member := unionMemberInfo{specName: m, goName: goTypeName(m)}
			if mi, found := items[m]; found {
				if field, value, ok := discriminatorOf(mi); ok {
					if u.discField == "" {
						u.discField = field
					} else if u.discField != field {
						return fmt.Errorf("union %s: members disagree on discriminator field (%q vs %q)", it.Name, u.discField, field)
					}
					member.discValue = value
					if seenValues[value] {
						u.decodable = false // ambiguous (InlineQueryResult: "audio" twice)
					}
					seenValues[value] = true
				} else {
					u.decodable = false
				}
			} else {
				u.decodable = false
			}
			u.members = append(u.members, member)
		}
		if u.discField == "" {
			u.decodable = false
		}
		unionsBySpecName[it.Name] = u
		unionGoNames[u.goName] = true
	}

	// The struct-level UnmarshalJSON emission assumes union references are
	// either direct or one "Array of" deep. Verify nothing deeper appeared.
	for _, it := range spec.Items {
		if it.Kind != "type" || skipTypes[it.Name] {
			continue
		}
		for _, f := range it.Fields {
			if strings.HasPrefix(f.Type, "Array of Array of ") {
				inner := strings.TrimPrefix(f.Type, "Array of Array of ")
				if _, isUnion := unionsBySpecName[inner]; isUnion {
					return fmt.Errorf("%s.%s: nested array of union %q — generator needs extending", it.Name, f.Name, inner)
				}
			}
		}
	}
	return nil
}

// decodableUnionsSorted returns the unions that get an unmarshal function,
// in a stable order for deterministic output.
func decodableUnionsSorted() []*unionInfo {
	var out []*unionInfo
	for _, u := range unionsBySpecName {
		if u.decodable {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].goName < out[j].goName })
	return out
}

// unmarshalFuncName is the generated per-union decode helper's name.
func unmarshalFuncName(u *unionInfo) string {
	return "unmarshal" + u.goName
}

// unionCommonField is one field shared by every concrete member of a union —
// same JSON name and identical resolved Go type on all of them — safe to
// promote to a Get<Field>() accessor on the union interface itself, so a
// caller holding just the interface (e.g. straight out of GetChatMember)
// doesn't have to type-switch across every member just to read something
// every member actually has (User, Status, ...).
type unionCommonField struct {
	goName string
	goType string
}

// unionCommonFields computes the fields shared, by JSON name and identical
// resolved Go type, across every member of it that resolved to a real
// generated struct — the same member set the marker-method loop in
// renderUnion emits isX() for (skipTypes/unknown members are excluded,
// since they have no field list to intersect). Returns nil for a union
// with fewer than two such members, or with no field common to all of them.
func unionCommonFields(it Item, typeNames map[string]bool, typesByName map[string]Item) []unionCommonField {
	var fieldSets []map[string]unionCommonField
	for _, member := range it.Members {
		if !typeNames[member] {
			continue
		}
		mi, ok := typesByName[member]
		if !ok {
			continue
		}
		fields := map[string]unionCommonField{}
		for _, f := range mi.Fields {
			goType, _ := fieldTypeAndTag(member, f.Type, f.Name, f.Description, isOptionalTypeField(f))
			fields[f.Name] = unionCommonField{goName: fieldName(f.Name), goType: goType}
		}
		fieldSets = append(fieldSets, fields)
	}
	if len(fieldSets) < 2 {
		return nil
	}

	common := map[string]unionCommonField{}
	for jsonName, f := range fieldSets[0] {
		common[jsonName] = f
	}
	for _, fields := range fieldSets[1:] {
		for jsonName, f := range common {
			if other, ok := fields[jsonName]; !ok || other.goType != f.goType {
				delete(common, jsonName)
			}
		}
	}

	out := make([]unionCommonField, 0, len(common))
	for _, f := range common {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].goName < out[j].goName })
	return out
}

// richTextPrimitiveAlternatives is a targeted fixup, not a general rule —
// keyed by exact union spec name, same precedent as mapping.go's
// mediaFileFieldFixups. RichText is the only union in the 10.1 spec whose
// docs prose says it "can be either a String for plain text, an Array of
// RichText, or" one of its named object members — every other union
// (RichBlock included) is object-only. json.Unmarshal can't discriminate a
// bare string/array from an object by field-peeking the way the normal
// discriminator switch below does, so these two alternatives are tried,
// in this order, before falling into that switch. RichPlainText and
// RichTextSequence (the Go types these decode into) aren't spec-named
// types, so they're hand-written in richtext.go rather than generated.
const richTextPrimitiveAlternatives = "RichText"

// renderUnionUnmarshalers emits one unmarshal<Union> function per decodable
// union: peek the discriminator, switch to the concrete member type. An
// unknown discriminator value is an error, not a nil — a bot binary that
// predates a new Telegram member type should fail loudly in the error
// handler, not hand nil to a handler that expected data.
func renderUnionUnmarshalers(b *strings.Builder) {
	for _, u := range decodableUnionsSorted() {
		fmt.Fprintf(b, "// %s decodes one %s union value into its concrete member\n", unmarshalFuncName(u), u.goName)
		fmt.Fprintf(b, "// type, switching on the %q discriminator field.\n", u.discField)
		if u.specName == richTextPrimitiveAlternatives {
			b.WriteString("// RichText also accepts a bare JSON string (plain text) or a JSON\n")
			b.WriteString("// array of RichText (concatenated spans) in place of an object — both\n")
			b.WriteString("// tried first, before the discriminated object switch below.\n")
		}
		fmt.Fprintf(b, "func %s(data []byte) (%s, error) {\n", unmarshalFuncName(u), u.goName)
		if u.specName == richTextPrimitiveAlternatives {
			b.WriteString("\tvar plain string\n")
			b.WriteString("\tif err := json.Unmarshal(data, &plain); err == nil {\n")
			b.WriteString("\t\treturn RichPlainText(plain), nil\n")
			b.WriteString("\t}\n")
			b.WriteString("\tvar rawSeq []json.RawMessage\n")
			b.WriteString("\tif err := json.Unmarshal(data, &rawSeq); err == nil {\n")
			b.WriteString("\t\tseq := make(RichTextSequence, len(rawSeq))\n")
			b.WriteString("\t\tfor i, raw := range rawSeq {\n")
			b.WriteString("\t\t\tv, err := unmarshalRichText(raw)\n")
			b.WriteString("\t\t\tif err != nil {\n")
			b.WriteString("\t\t\t\treturn nil, fmt.Errorf(\"RichText[%d]: %w\", i, err)\n")
			b.WriteString("\t\t\t}\n")
			b.WriteString("\t\t\tseq[i] = v\n")
			b.WriteString("\t\t}\n")
			b.WriteString("\t\treturn seq, nil\n")
			b.WriteString("\t}\n")
		}
		fmt.Fprintf(b, "\tvar disc struct {\n\t\tValue string `json:%q`\n\t}\n", u.discField)
		fmt.Fprintf(b, "\tif err := json.Unmarshal(data, &disc); err != nil {\n")
		fmt.Fprintf(b, "\t\treturn nil, fmt.Errorf(\"%s: reading %s discriminator: %%w\", err)\n\t}\n", u.goName, u.discField)
		b.WriteString("\tswitch disc.Value {\n")
		for _, m := range u.members {
			fmt.Fprintf(b, "\tcase %q:\n", m.discValue)
			fmt.Fprintf(b, "\t\tv := new(%s)\n", m.goName)
			fmt.Fprintf(b, "\t\tif err := json.Unmarshal(data, v); err != nil {\n\t\t\treturn nil, fmt.Errorf(\"%s(%s=%s): %%w\", err)\n\t\t}\n", u.goName, u.discField, m.discValue)
			b.WriteString("\t\treturn v, nil\n")
		}
		b.WriteString("\tdefault:\n")
		fmt.Fprintf(b, "\t\treturn nil, fmt.Errorf(\"%s: unknown %s %%q (a Bot API version this build of golagram predates?)\", disc.Value)\n", u.goName, u.discField)
		b.WriteString("\t}\n}\n\n")
	}
}
