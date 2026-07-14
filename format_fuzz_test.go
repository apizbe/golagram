package golagram

import (
	"testing"
	"unicode/utf16"
)

// FuzzSplitTextWithEntities generalizes TestSplitTextWithEntities_EntitiesNeverOutOfRange's
// hand-rolled sweep into real fuzzing — entities can't be passed to f.Fuzz
// directly (only scalar/string/[]byte corpus types are supported), so two
// entities are reconstructed from scalar offset/length pairs each run.
func FuzzSplitTextWithEntities(f *testing.F) {
	type seed struct {
		text                   string
		off1, len1, off2, len2 int64
		limit                  int
	}
	for _, s := range []seed{
		{"hello world", 0, 5, 6, 5, 0},
		{"", 0, 0, 0, 0, 0},
		{"word 😀 more text here", 5, 2, 0, 4, 5},
		{"negative offset", -5, 3, 100, 50, 3},
		{"overlap", 0, 4, 2, 4, 3},
		{"zero length", 2, 0, 0, 0, 0},
		{"single word waaaaaaaaaaaaaaaaaaaaaaaaay too long for the limit", 0, 60, 0, 0, 5},
		{"newline\nsplit\nhere", 0, 7, 8, 5, 6},
	} {
		f.Add(s.text, s.off1, s.len1, s.off2, s.len2, s.limit)
	}

	f.Fuzz(func(t *testing.T, text string, off1, len1, off2, len2 int64, limit int) {
		entities := []Entity{
			{Type: "bold", Offset: off1, Length: len1},
			{Type: "italic", Offset: off2, Length: len2},
		}
		chunks := SplitTextWithEntities(text, entities, limit)

		max := MaxTextLength
		if limit > 0 {
			max = limit
		}
		// splitPoint documents one deliberate exception: limit 1 against a
		// lone 2-unit (surrogate-pair) character is unsplittable without
		// corrupting it, so it overshoots to exactly 2 units rather than
		// stall or break the pair. max must allow for that one case.
		if max < 2 {
			max = 2
		}

		var total int
		for _, ch := range chunks {
			if ch.Text == "" {
				t.Fatalf("text=%q entities=%+v limit=%d: produced an empty chunk", text, entities, limit)
			}
			u := utf16.Encode([]rune(ch.Text))
			if len(u) > max {
				t.Fatalf("text=%q entities=%+v limit=%d: chunk %q has %d UTF-16 units, exceeds limit %d", text, entities, limit, ch.Text, len(u), max)
			}
			total += len(u)
			for _, e := range ch.Entities {
				if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > int64(len(u)) {
					t.Fatalf("text=%q entities=%+v limit=%d: chunk %q (%d units) has out-of-range entity %+v", text, entities, limit, ch.Text, len(u), e)
				}
			}
		}

		// Chunking only ever drops separator whitespace at seams — it
		// never drops non-whitespace content — so the total can shrink
		// versus the input but never grow.
		if inputUnits := len(utf16.Encode([]rune(text))); total > inputUnits {
			t.Fatalf("text=%q entities=%+v limit=%d: chunks total %d UTF-16 units, more than input's %d", text, entities, limit, total, inputUnits)
		}
	})
}
