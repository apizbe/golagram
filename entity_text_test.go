package golagram

import "testing"

func TestMessage_EntityText(t *testing.T) {
	m := &Message{Text: "hi @bob check example.com"}
	mention := Entity{Type: EntityMention, Offset: 3, Length: 4}
	url := Entity{Type: EntityURL, Offset: 14, Length: 11}

	if got := m.EntityText(mention); got != "@bob" {
		t.Errorf("EntityText(mention) = %q, want %q", got, "@bob")
	}
	if got := m.EntityText(url); got != "example.com" {
		t.Errorf("EntityText(url) = %q, want %q", got, "example.com")
	}
}

func TestMessage_EntityText_FallsBackToCaption(t *testing.T) {
	m := &Message{Caption: "call me"}
	e := Entity{Type: EntityPhoneNumber, Offset: 0, Length: 7}
	if got := m.EntityText(e); got != "call me" {
		t.Errorf("EntityText on caption = %q, want %q", got, "call me")
	}
}

func TestMessage_EntityText_OutOfRange(t *testing.T) {
	m := &Message{Text: "hi"}
	if got := m.EntityText(Entity{Offset: 0, Length: 100}); got != "" {
		t.Errorf("expected \"\" for an out-of-range entity, got %q", got)
	}
}

func TestMessage_EntityText_SurrogatePairOffsets(t *testing.T) {
	// "😀" is a 2-unit UTF-16 surrogate pair; "hi" starts at unit 3, not
	// byte 4 or rune 2 — this is what would break with naive Go slicing.
	m := &Message{Text: "😀 hi"}
	e := Entity{Type: EntityMention, Offset: 3, Length: 2}
	if got := m.EntityText(e); got != "hi" {
		t.Errorf("EntityText across a surrogate pair = %q, want %q", got, "hi")
	}
}

func TestEntitySegments_PlainText(t *testing.T) {
	segs := EntitySegments("hello", nil)
	if len(segs) != 1 || segs[0].Text != "hello" || segs[0].Entities != nil {
		t.Fatalf("unexpected segments: %+v", segs)
	}
}

func TestEntitySegments_SingleEntityInMiddle(t *testing.T) {
	// "hi @bob!" — mention covers "@bob" (offset 3, length 4).
	segs := EntitySegments("hi @bob!", []Entity{{Type: EntityMention, Offset: 3, Length: 4}})

	want := []EntitySegment{
		{Text: "hi ", Entities: nil},
		{Text: "@bob", Entities: []Entity{{Type: EntityMention, Offset: 3, Length: 4}}},
		{Text: "!", Entities: nil},
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if segs[i].Text != want[i].Text {
			t.Errorf("segment %d text = %q, want %q", i, segs[i].Text, want[i].Text)
		}
		if len(segs[i].Entities) != len(want[i].Entities) {
			t.Errorf("segment %d entities = %+v, want %+v", i, segs[i].Entities, want[i].Entities)
		}
	}
}

func TestEntitySegments_OverlappingEntitiesShareASegment(t *testing.T) {
	// Both entities cover the same full span [0,4) — "bold" also italic.
	entities := []Entity{
		{Type: "bold", Offset: 0, Length: 4},
		{Type: "italic", Offset: 0, Length: 4},
	}
	segs := EntitySegments("text", entities)
	if len(segs) != 1 {
		t.Fatalf("expected exactly one segment for fully-overlapping entities, got %d: %+v", len(segs), segs)
	}
	if len(segs[0].Entities) != 2 {
		t.Errorf("expected both entities on the shared segment, got %+v", segs[0].Entities)
	}
}

func TestEntitySegments_PartiallyOverlappingEntitiesSplit(t *testing.T) {
	// "0123456789": bold [0,6), italic [3,9) — overlap only on [3,6).
	entities := []Entity{
		{Type: "bold", Offset: 0, Length: 6},
		{Type: "italic", Offset: 3, Length: 6},
	}
	segs := EntitySegments("0123456789", entities)

	wantTexts := []string{"012", "345", "678", "9"}
	if len(segs) != len(wantTexts) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(wantTexts), segs)
	}
	for i, want := range wantTexts {
		if segs[i].Text != want {
			t.Errorf("segment %d = %q, want %q", i, segs[i].Text, want)
		}
	}
	if len(segs[0].Entities) != 1 || len(segs[1].Entities) != 2 || len(segs[2].Entities) != 1 || len(segs[3].Entities) != 0 {
		t.Errorf("unexpected entity coverage: %+v", segs)
	}
}

func TestEntitySegments_Empty(t *testing.T) {
	if got := EntitySegments("", nil); got != nil {
		t.Errorf("expected nil for empty text, got %+v", got)
	}
}

// TestEntitySegments_NegativeOffsetEntity_DoesNotPanic pins a bug
// FuzzEntitySegments found immediately: an entity whose Offset and
// Offset+Length are both negative (e.g. a stale/corrupt Offset: -5,
// Length: 3) made clipRange return a still-negative start, and
// EntitySegments panicked slicing a []uint16 by it ("slice bounds out of
// range [-2:]"). Fixed in clipRange by clamping end<0 too, before the
// final start>end fixup can copy a negative end back into start.
func TestEntitySegments_NegativeOffsetEntity_DoesNotPanic(t *testing.T) {
	segments := EntitySegments("negative offset", []Entity{{Type: "bold", Offset: -5, Length: 3}})
	var got string
	for _, s := range segments {
		got += s.Text
	}
	if got != "negative offset" {
		t.Errorf("segments reconstruct to %q, want the full input text", got)
	}
}

// TestEntitySegments_BoundaryInsideSurrogatePair_DoesNotCorruptChar pins
// the second bug FuzzEntitySegments found: an entity offset landing
// between an astral character's two UTF-16 units (offset 5 in "0000😀" —
// index 4 is 😀's high surrogate, index 5 its low surrogate) made
// entityBoundaries cut the pair apart. Slicing a lone surrogate and
// decoding it produces U+FFFD, so one emoji became two replacement
// characters ("0000��" instead of "0000😀"). Fixed by having
// entityBoundaries snap a boundary back before the pair, same convention
// splitPoint already used for chunk splitting.
func TestEntitySegments_BoundaryInsideSurrogatePair_DoesNotCorruptChar(t *testing.T) {
	segments := EntitySegments("0000😀", []Entity{{Type: "bold", Offset: 5, Length: 2}})
	var got string
	for _, s := range segments {
		got += s.Text
	}
	if got != "0000😀" {
		t.Errorf("segments reconstruct to %q, want the full input text with the emoji intact", got)
	}
}
