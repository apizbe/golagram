package golagram

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// FuzzEntitySegments checks the property EntitySegments' own doc promises:
// it partitions the *entire* input (unlike SplitTextWithEntities, which may
// drop seam whitespace when chunking for length), so every input rune ends
// up in exactly one segment, in order.
func FuzzEntitySegments(f *testing.F) {
	type seed struct {
		text                   string
		off1, len1, off2, len2 int64
	}
	for _, s := range []seed{
		{"hello world", 0, 5, 6, 5},
		{"", 0, 0, 0, 0},
		{"word 😀 more", 5, 2, 0, 0},
		{"negative offset", -5, 3, 100, 50},
		{"overlap", 0, 4, 2, 4},
		{"zero length", 2, 0, 0, 0},
		{"fully covered", 0, 13, 0, 13},
		{"huge offset", 1 << 40, 1 << 40, 0, 0},
	} {
		f.Add(s.text, s.off1, s.len1, s.off2, s.len2)
	}

	f.Fuzz(func(t *testing.T, text string, off1, len1, off2, len2 int64) {
		entities := []Entity{
			{Type: "bold", Offset: off1, Length: len1},
			{Type: "italic", Offset: off2, Length: len2},
		}
		segments := EntitySegments(text, entities)

		var b strings.Builder
		for _, seg := range segments {
			if seg.Text == "" {
				t.Fatalf("text=%q entities=%+v: produced an empty segment", text, entities)
			}
			b.WriteString(seg.Text)
		}

		// Segments must reconstruct the input exactly, in order — no
		// dropped, duplicated, or reordered content, unlike
		// SplitTextWithEntities which intentionally drops seam whitespace.
		//
		// Compared against text's own UTF-16 round trip, not text itself:
		// EntitySegments (like SplitTextWithEntities) walks text through
		// utf16.Encode/Decode internally, and invalid UTF-8 input isn't
		// byte-preserved through that — Go's own []rune(text) conversion
		// already normalizes an invalid byte to U+FFFD before EntitySegments
		// ever sees it, same as it would for any Go code doing that
		// conversion. That's not something this function could preserve;
		// holding it to exact-byte fidelity on invalid UTF-8 would be
		// testing Go's rune conversion, not EntitySegments.
		wantRunes := utf16.Encode([]rune(text))
		want := string(utf16.Decode(wantRunes))
		if got := b.String(); got != want {
			t.Fatalf("text=%q entities=%+v: segments reconstruct to %q, want %q (text's own UTF-16 round trip)", text, entities, got, want)
		}

		if text == "" && segments != nil {
			t.Fatalf("EntitySegments(%q, ...) = %+v, want nil for empty text", text, segments)
		}

		// Every segment's entities must actually cover it in full —
		// otherwise a caller re-rendering per-segment formatting would
		// apply an entity to text it doesn't really span.
		textUnits := int64(len(utf16.Encode([]rune(text))))
		var pos int64
		for _, seg := range segments {
			segUnits := int64(len(utf16.Encode([]rune(seg.Text))))
			for _, e := range seg.Entities {
				eStart, eEnd := clipRange(int(e.Offset), int(e.Offset+e.Length), int(textUnits))
				if int64(eStart) > pos || int64(eEnd) < pos+segUnits {
					t.Fatalf("text=%q entities=%+v: segment %q at [%d,%d) claims entity %+v (clipped [%d,%d)), which doesn't fully cover it", text, entities, seg.Text, pos, pos+segUnits, e, eStart, eEnd)
				}
			}
			pos += segUnits
		}
	})
}
