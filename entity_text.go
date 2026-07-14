package golagram

import (
	"sort"
	"unicode/utf16"
)

// textOrCaption returns Text, falling back to Caption for a media message —
// the same "whichever the message actually carries" rule [FilterRegexp] uses.
func (m *Message) textOrCaption() string {
	if m.Text != "" {
		return m.Text
	}
	return m.Caption
}

// EntityText returns the substring of the message's text (or caption, for a
// media message) that e covers. Offset/Length are UTF-16 code units per the
// wire format, so this decodes through a UTF-16 round trip rather than
// slicing the Go string directly — a direct byte slice would cut a
// multi-byte character (most emoji) in half. Returns "" for an entity that
// doesn't fit the text (wrong message, stale offsets).
func (m *Message) EntityText(e Entity) string {
	return sliceUTF16(m.textOrCaption(), int(e.Offset), int(e.Offset+e.Length))
}

func sliceUTF16(text string, start, end int) string {
	u := utf16.Encode([]rune(text))
	if start < 0 || end > len(u) || start > end {
		return ""
	}
	return string(utf16.Decode(u[start:end]))
}

// EntitySegment is one contiguous run of text annotated with every entity
// that covers it in full.
type EntitySegment struct {
	Text     string
	Entities []Entity
}

// EntitySegments splits text at every entity boundary — the reverse of the
// [Node] formatting builder: instead of building text+entities from a
// tree, this walks existing text+entities into segments a caller can
// render or re-process (re-emit as Markdown/HTML, strip one entity type,
// ...). Each segment's Entities is exactly the set
// covering it in full; a segment covered by two overlapping entities (Bot
// API 6.0+ allows entities to nest) carries both, one covered by none is
// plain text with a nil slice. Offsets/lengths are UTF-16 code units,
// matching entities themselves; out-of-range entities are clipped to text's
// bounds rather than dropped or causing a panic.
func EntitySegments(text string, entities []Entity) []EntitySegment {
	u := utf16.Encode([]rune(text))
	if len(u) == 0 {
		return nil
	}

	bounds := entityBoundaries(entities, u)

	segments := make([]EntitySegment, 0, len(bounds)-1)
	for i := 0; i < len(bounds)-1; i++ {
		start, end := bounds[i], bounds[i+1]
		if start >= end {
			continue
		}
		segments = append(segments, EntitySegment{
			Text:     string(utf16.Decode(u[start:end])),
			Entities: coveringEntities(entities, start, end),
		})
	}
	return segments
}

// entityBoundaries returns the sorted, deduplicated set of every entity's
// clipped start/end alongside 0 and len(u), so consecutive pairs bound
// exactly the runs where entity coverage is constant. A boundary landing
// between the two units of a surrogate pair (an entity offset pointing
// mid-emoji — malformed input, but Telegram doesn't validate this any more
// than golagram does) is nudged back before the pair, the same convention
// [splitPoint] uses: without it, slicing u at that boundary splits one
// astral character into two lone surrogates, and utf16.Decode turns each
// into its own U+FFFD — corrupting one character into two.
func entityBoundaries(entities []Entity, u []uint16) []int {
	textLen := len(u)
	set := map[int]bool{0: true, textLen: true}
	for _, e := range entities {
		start, end := clipRange(int(e.Offset), int(e.Offset+e.Length), textLen)
		set[snapOutOfSurrogate(u, start)] = true
		set[snapOutOfSurrogate(u, end)] = true
	}
	bounds := make([]int, 0, len(set))
	for b := range set {
		bounds = append(bounds, b)
	}
	sort.Ints(bounds)
	return bounds
}

// snapOutOfSurrogate moves b back by one unit if it falls strictly between
// a surrogate pair's two units (u[b-1] a high surrogate implies u[b] is
// its low surrogate partner), so a boundary can never split one.
func snapOutOfSurrogate(u []uint16, b int) int {
	if b > 0 && b < len(u) && isHighSurrogate(u[b-1]) {
		return b - 1
	}
	return b
}

func coveringEntities(entities []Entity, start, end int) []Entity {
	var covering []Entity
	for _, e := range entities {
		eStart, eEnd := int(e.Offset), int(e.Offset+e.Length)
		if eStart <= start && end <= eEnd {
			covering = append(covering, e)
		}
	}
	return covering
}

// clipRange clamps [start, end) to [0, max]. Both bounds are clamped on
// both ends before the final start>end fixup runs — clamping only start<0
// and end>max (as this used to) let a negative end survive its own clamp
// and then get copied back into start by the fixup, producing a still-
// negative start that panicked callers slicing a []uint16 by it.
func clipRange(start, end, max int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > max {
		start = max
	}
	if end > max {
		end = max
	}
	if start > end {
		start = end
	}
	return start, end
}
