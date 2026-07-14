package golagram

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestEscapeHTML(t *testing.T) {
	if got := EscapeHTML(`<b> & "quotes" stay`); got != `&lt;b&gt; &amp; "quotes" stay` {
		t.Errorf("EscapeHTML = %q", got)
	}
	if got := EscapeHTML("plain text"); got != "plain text" {
		t.Errorf("EscapeHTML must pass plain text through, got %q", got)
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	in := `_*[]()~` + "`" + `>#+-=|{}.!`
	want := `\_\*\[\]\(\)\~\` + "`" + `\>\#\+\-\=\|\{\}\.\!`
	if got := EscapeMarkdownV2(in); got != want {
		t.Errorf("EscapeMarkdownV2(all specials) = %q, want %q", got, want)
	}
	// A literal backslash must survive the round trip: \. must not come out
	// as \\. (an escaped backslash followed by an *unescaped* dot — a 400).
	if got := EscapeMarkdownV2(`a\.b`); got != `a\\\.b` {
		t.Errorf("EscapeMarkdownV2(backslash) = %q, want %q", got, `a\\\.b`)
	}
	if got := EscapeMarkdownV2("hello world"); got != "hello world" {
		t.Errorf("EscapeMarkdownV2 must pass plain text through, got %q", got)
	}
}

func TestEscapeMarkdownV2Code(t *testing.T) {
	// Inside code spans only ` and \ are special — a dot must NOT be escaped.
	if got := EscapeMarkdownV2Code("fmt.Println(`x`)"); got != "fmt.Println(\\`x\\`)" {
		t.Errorf("EscapeMarkdownV2Code = %q", got)
	}
}

func TestEscapeMarkdownV2Link(t *testing.T) {
	if got := EscapeMarkdownV2Link("https://e.com/a(b)?q=1."); got != `https://e.com/a(b\)?q=1.` {
		t.Errorf("EscapeMarkdownV2Link = %q", got)
	}
}

func TestSplitText_ShortAndEmpty(t *testing.T) {
	if got := SplitText(""); got != nil {
		t.Errorf("SplitText(\"\") = %v, want nil", got)
	}
	if got := SplitText("hello"); len(got) != 1 || got[0] != "hello" {
		t.Errorf("short text = %v, want [hello] unchanged", got)
	}
	if got := SplitText(strings.Repeat("a", MaxTextLength)); len(got) != 1 {
		t.Errorf("text exactly at the limit must stay one chunk, got %d", len(got))
	}
}

func TestSplitText_PrefersNewlineThenSpace(t *testing.T) {
	// Window holds "aaa bbb\nccc d" (13 units) — the newline wins even
	// though later spaces exist.
	got := SplitText("aaa bbb\nccc ddd eee", 13)
	if got[0] != "aaa bbb" {
		t.Errorf("first chunk = %q, want split at the newline", got[0])
	}

	// No newline: the last space in the window wins.
	got = SplitText("aaa bbb ccc", 9)
	if got[0] != "aaa bbb" {
		t.Errorf("first chunk = %q, want split at the last space", got[0])
	}
	if got[1] != "ccc" {
		t.Errorf("second chunk = %q, want separator space dropped", got[1])
	}
}

func TestSplitText_HardCutLongWord(t *testing.T) {
	got := SplitText(strings.Repeat("a", 10), 4)
	want := []string{"aaaa", "aaaa", "aa"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("SplitText = %v, want %v", got, want)
	}
}

func TestSplitText_CountsUTF16NotRunesOrBytes(t *testing.T) {
	// One emoji = 2 UTF-16 units (4 UTF-8 bytes, 1 rune). Three emoji = 6
	// units: a limit of 4 fits exactly two per chunk.
	got := SplitText("😀😀😀", 4)
	if len(got) != 2 || got[0] != "😀😀" || got[1] != "😀" {
		t.Errorf("SplitText(3 emoji, limit 4) = %q, want [😀😀 😀]", got)
	}
}

func TestSplitText_NeverSplitsSurrogatePair(t *testing.T) {
	// An odd limit would land mid-emoji; the cut must retreat one unit.
	got := SplitText("😀😀", 3)
	if len(got) != 2 || got[0] != "😀" || got[1] != "😀" {
		t.Errorf("SplitText(2 emoji, limit 3) = %q, want one emoji per chunk", got)
	}
	for _, chunk := range got {
		if strings.ContainsRune(chunk, '�') {
			t.Errorf("chunk %q contains a replacement character — a surrogate pair was split", chunk)
		}
	}
}

func TestSplitText_EveryChunkPassesValidation(t *testing.T) {
	long := strings.Repeat("word word word 😀 word\n", 800) // ~17600 units
	for i, chunk := range SplitText(long) {
		if err := validateOutgoingText(chunk); err != nil {
			t.Errorf("chunk %d fails outgoing validation: %v", i, err)
		}
	}
}

func TestSplitTextWithEntities_MovesEntityToNextChunk(t *testing.T) {
	// "aaaabold!" with bold on "bold" (4..8) and limit 6: the window edge
	// lands inside the entity, so the split retreats to the entity start.
	chunks := SplitTextWithEntities("aaaabold!", []Entity{
		{Type: "bold", Offset: 4, Length: 4},
	}, 6)

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %+v", len(chunks), chunks)
	}
	if chunks[0].Text != "aaaa" || len(chunks[0].Entities) != 0 {
		t.Errorf("chunk 0 = %+v, want plain \"aaaa\"", chunks[0])
	}
	if chunks[1].Text != "bold!" {
		t.Errorf("chunk 1 text = %q, want \"bold!\"", chunks[1].Text)
	}
	e := chunks[1].Entities
	if len(e) != 1 || e[0].Offset != 0 || e[0].Length != 4 || e[0].Type != "bold" {
		t.Errorf("chunk 1 entities = %+v, want bold re-based to offset 0 len 4", e)
	}
}

func TestSplitTextWithEntities_SplitsOversizedEntity(t *testing.T) {
	// A single bold entity longer than the limit must be cut, one valid
	// entity per chunk — not dropped and not left out of range.
	chunks := SplitTextWithEntities(strings.Repeat("a", 10), []Entity{
		{Type: "bold", Offset: 0, Length: 10},
	}, 4)

	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3: %+v", len(chunks), chunks)
	}
	wantLens := []int64{4, 4, 2}
	for i, ch := range chunks {
		if len(ch.Entities) != 1 {
			t.Fatalf("chunk %d has %d entities, want 1", i, len(ch.Entities))
		}
		e := ch.Entities[0]
		if e.Offset != 0 || e.Length != wantLens[i] {
			t.Errorf("chunk %d entity = {off %d len %d}, want {off 0 len %d}", i, e.Offset, e.Length, wantLens[i])
		}
	}
}

func TestSplitTextWithEntities_UTF16OffsetsWithAstralChars(t *testing.T) {
	// Two emoji (4 units) + space + "bold": entity offset 5 in UTF-16.
	text := "😀😀 bold"
	chunks := SplitTextWithEntities(text, []Entity{
		{Type: "bold", Offset: 5, Length: 4},
	}, 6)

	if len(chunks) != 2 || chunks[0].Text != "😀😀" || chunks[1].Text != "bold" {
		t.Fatalf("chunks = %+v, want [😀😀] [bold]", chunks)
	}
	e := chunks[1].Entities
	if len(e) != 1 || e[0].Offset != 0 || e[0].Length != 4 {
		t.Errorf("entity = %+v, want re-based to offset 0 len 4", e)
	}
}

func TestSplitTextWithEntities_EntityEndingAtSplitStays(t *testing.T) {
	// bold covers exactly the first word; splitting right after it is fine.
	chunks := SplitTextWithEntities("bold rest", []Entity{
		{Type: "bold", Offset: 0, Length: 4},
	}, 5)

	if len(chunks) != 2 || chunks[0].Text != "bold" || chunks[1].Text != "rest" {
		t.Fatalf("chunks = %+v, want [bold] [rest]", chunks)
	}
	if len(chunks[0].Entities) != 1 || len(chunks[1].Entities) != 0 {
		t.Errorf("entity must stay whole in chunk 0: %+v / %+v", chunks[0].Entities, chunks[1].Entities)
	}
}

func TestSplitTextWithEntities_NestedEntitiesClipTogether(t *testing.T) {
	// blockquote over everything, bold nested inside the second half.
	text := "one two three four"
	chunks := SplitTextWithEntities(text, []Entity{
		{Type: "blockquote", Offset: 0, Length: 18},
		{Type: "bold", Offset: 8, Length: 5}, // "three"
	}, 10)

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	// chunk 0 "one two" carries only the clipped blockquote.
	if len(chunks[0].Entities) != 1 || chunks[0].Entities[0].Type != "blockquote" {
		t.Errorf("chunk 0 entities = %+v, want just the clipped blockquote", chunks[0].Entities)
	}
	// chunk 1 "three four" carries the rest of the blockquote and the whole bold.
	if len(chunks[1].Entities) != 2 {
		t.Fatalf("chunk 1 entities = %+v, want blockquote+bold", chunks[1].Entities)
	}
	for _, e := range chunks[1].Entities {
		end := e.Offset + e.Length
		if e.Offset < 0 || end > int64(len(utf16.Encode([]rune(chunks[1].Text)))) {
			t.Errorf("entity %+v out of range for chunk text %q", e, chunks[1].Text)
		}
		if e.Type == "bold" && (e.Offset != 0 || e.Length != 5) {
			t.Errorf("bold = %+v, want offset 0 len 5 on %q", e, chunks[1].Text)
		}
	}
}

func TestSplitTextWithEntities_EntitiesNeverOutOfRange(t *testing.T) {
	// Fuzz-ish sweep: many limits over a text with overlapping entities;
	// every produced entity must lie inside its chunk.
	text := strings.Repeat("word 😀 more\n", 30)
	units := len(utf16.Encode([]rune(text)))
	entities := []Entity{
		{Type: "bold", Offset: 3, Length: int64(units) - 6},
		{Type: "italic", Offset: 20, Length: 40},
		{Type: "code", Offset: int64(units) - 15, Length: 10},
	}
	for limit := 5; limit <= 60; limit += 7 {
		for _, ch := range SplitTextWithEntities(text, entities, limit) {
			chunkUnits := int64(len(utf16.Encode([]rune(ch.Text))))
			if chunkUnits == 0 || chunkUnits > int64(limit) {
				t.Fatalf("limit %d: chunk %q has %d units", limit, ch.Text, chunkUnits)
			}
			for _, e := range ch.Entities {
				if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > chunkUnits {
					t.Fatalf("limit %d: entity %+v out of range in chunk %q (%d units)", limit, e, ch.Text, chunkUnits)
				}
			}
		}
	}
}
