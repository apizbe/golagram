package golagram

import (
	"strings"
	"unicode/utf16"
)

// Formatting utilities: parse-mode escaping and length-limit splitting.
//
// Escaping is the #1 support question of every bot library — a user name
// containing "_" or "." breaks MarkdownV2 with an opaque 400 — so golagram
// ships it. Splitting is the #2: a naive splitter cuts a 5000-character
// text mid-entity (or mid-emoji) and gets the same 400.

// EscapeHTML escapes text for parse_mode "HTML": &, <, and > — the three
// characters Telegram's HTML dialect requires escaped outside tags.
//
//	c.Answer("<b>"+gg.EscapeHTML(userInput)+"</b>", &gg.SendMessageOptions{ParseMode: "HTML"})
func EscapeHTML(s string) string {
	return htmlEscaper.Replace(s)
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// EscapeMarkdownV2 escapes text for parse_mode "MarkdownV2" — all 18
// characters the spec reserves, plus backslash itself so a literal "\" in
// the input survives the round trip.
//
// Only for regular text: inside an inline code/pre span use
// [EscapeMarkdownV2Code]; inside the parenthesized URL of a link or
// custom-emoji definition use [EscapeMarkdownV2Link] — the spec escapes
// fewer characters there, and over-escaping renders stray backslashes.
func EscapeMarkdownV2(s string) string {
	return markdownV2Escaper.Replace(s)
}

var markdownV2Escaper = strings.NewReplacer(
	"\\", "\\\\",
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
	"~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
	"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
)

// EscapeMarkdownV2Code escapes text for the inside of a MarkdownV2 inline
// code span or pre block, where only ` and \ are special.
func EscapeMarkdownV2Code(s string) string {
	return markdownV2CodeEscaper.Replace(s)
}

var markdownV2CodeEscaper = strings.NewReplacer("\\", "\\\\", "`", "\\`")

// EscapeMarkdownV2Link escapes text for the parenthesized URL of a
// MarkdownV2 link or custom-emoji definition, where only ) and \ are
// special.
func EscapeMarkdownV2Link(s string) string {
	return markdownV2LinkEscaper.Replace(s)
}

var markdownV2LinkEscaper = strings.NewReplacer("\\", "\\\\", ")", "\\)")

// TextChunk is one sendable piece of a long message, as returned by
// [SplitTextWithEntities]: the text plus the entities that fall inside it,
// re-based so they can be passed straight to [SendMessageOptions.Entities].
type TextChunk struct {
	Text     string
	Entities []Entity
}

// SplitText splits text into chunks that each fit Telegram's message
// length limit, so a long text can be sent as consecutive messages:
//
//	for _, part := range gg.SplitText(long) {
//		if _, err := c.Answer(part); err != nil {
//			return err
//		}
//	}
//
// Split points prefer a newline, then a space, and only cut mid-word when
// a single word exceeds the whole limit; separator whitespace at the seam
// is dropped. Lengths are measured in UTF-16 code units — what Telegram
// actually counts (an emoji counts as 2) — and a cut never lands inside a
// surrogate pair, so no chunk ever draws a 400 for length or breaks a
// character in half. Empty input yields nil. A custom limit (e.g. 1024 for
// captions) can be passed as the optional second argument.
//
// For text that carries formatting entities, use [SplitTextWithEntities] —
// splitting the plain text alone would cut through them.
func SplitText(text string, limit ...int) []string {
	chunks := SplitTextWithEntities(text, nil, limit...)
	if chunks == nil {
		return nil
	}
	out := make([]string, len(chunks))
	for i, ch := range chunks {
		out[i] = ch.Text
	}
	return out
}

// SplitTextWithEntities splits text and its formatting entities into
// chunks that each fit Telegram's message length limit, without cutting
// through an entity: a split point that would land inside one (a link, a
// code block, ...) moves back to the entity's start so the whole entity
// carries over to the next chunk. Only an entity too long to fit any
// chunk on its own is split, into one valid entity per chunk — the
// formatting stays intact on both sides.
//
// Each chunk's entities are clipped and re-based to that chunk, ready for
// [SendMessageOptions.Entities]. Offsets, lengths, and the limit are all in
// UTF-16 code units, matching MessageEntity's wire format.
func SplitTextWithEntities(text string, entities []Entity, limit ...int) []TextChunk {
	if text == "" {
		return nil
	}
	max := MaxTextLength
	if len(limit) > 0 && limit[0] > 0 {
		max = limit[0]
	}

	u := utf16.Encode([]rune(text))
	var chunks []TextChunk

	start := 0
	for start < len(u) {
		end := splitPoint(u, entities, start, max)

		// Drop separator whitespace at the seam: trailing on this chunk,
		// leading on the next. A hard mid-word cut has neither, so this
		// never eats content.
		cut := end
		for cut > start && isSplitSpace(u[cut-1]) {
			cut--
		}
		if cut > start {
			chunks = append(chunks, TextChunk{
				Text:     string(utf16.Decode(u[start:cut])),
				Entities: clipEntities(entities, start, cut),
			})
		}
		start = end
		for start < len(u) && isSplitSpace(u[start]) {
			start++
		}
	}
	return chunks
}

// splitPoint picks where the chunk starting at start should end: the last
// newline in the window, else the last space, else the window edge —
// then, if that point falls inside an entity, retreats to the entity's
// start (re-preferring a newline/space in the shrunken window). When no
// point in the window avoids cutting an entity, the entity is longer than
// the limit and gets cut at the window edge; clipEntities then splits it
// into a valid entity on each side.
func splitPoint(u []uint16, entities []Entity, start, limit int) int {
	w := start + limit
	if w >= len(u) {
		return len(u)
	}
	// Never split a surrogate pair: a high surrogate as the last unit
	// means the window edge lands mid-character.
	if isHighSurrogate(u[w-1]) {
		w--
		if w == start {
			// limit 1 and a 2-unit character: unfittable either way, so
			// overshoot by one unit rather than stall.
			w = start + 2
		}
	}

	p := w
	for {
		q := lastBreak(u, start, p)
		q = entityStartBefore(entities, start, q, limit)
		if q <= start {
			return w // defensive; entityStartBefore keeps q > start
		}
		if q == p {
			return q
		}
		p = q
	}
}

// lastBreak returns the position just after the last newline in
// (start, p], else just after the last space, else p itself.
func lastBreak(u []uint16, start, p int) int {
	lastSpace := -1
	for i := p; i > start; i-- {
		switch u[i-1] {
		case '\n':
			return i
		case ' ':
			if lastSpace == -1 {
				lastSpace = i
			}
		}
	}
	if lastSpace != -1 {
		return lastSpace
	}
	return p
}

// entityStartBefore retreats q out of any *avoidable* entity that strictly
// contains it, to that entity's start. An entity is avoidable when moving
// it wholly into the next chunk can work: it starts inside this chunk and
// fits within the limit on its own. Unavoidable entities — longer than the
// limit, or begun before this chunk (already split once) — are ignored
// here and clipped by clipEntities instead; retreating for them would
// forbid every split point.
func entityStartBefore(entities []Entity, start, q, limit int) int {
	for {
		moved := false
		for _, e := range entities {
			off, end := int(e.Offset), int(e.Offset+e.Length)
			if off < q && q < end && off > start && int(e.Length) <= limit {
				q = off
				moved = true
			}
		}
		if !moved {
			return q
		}
	}
}

// clipEntities returns copies of the entities that overlap [start, end),
// clipped to it and re-based so offset 0 is the chunk start. An entity
// crossing the boundary comes out truncated — its other half lands in the
// neighboring chunk's clip.
func clipEntities(entities []Entity, start, end int) []Entity {
	var out []Entity
	for _, e := range entities {
		off := max(int(e.Offset), start)
		stop := min(int(e.Offset+e.Length), end)
		if off >= stop {
			continue
		}
		c := e
		c.Offset = int64(off - start)
		c.Length = int64(stop - off)
		out = append(out, c)
	}
	return out
}

func isSplitSpace(c uint16) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func isHighSurrogate(c uint16) bool {
	return c >= 0xD800 && c <= 0xDBFF
}
