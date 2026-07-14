package golagram

import (
	"strings"
	"testing"
)

func BenchmarkSplitText_Short(b *testing.B) {
	text := "A short message that fits in one chunk easily."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitText(text)
	}
}

// BenchmarkSplitText_Long forces several chunks at the default 4096-unit
// limit — roughly 23,500 characters of mixed words/newlines/spaces, so
// splitPoint's newline/space search runs its full course each cut.
func BenchmarkSplitText_Long(b *testing.B) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog.\n", 500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitText(text)
	}
}

// BenchmarkSplitTextWithEntities_ManyEntities exercises the entity-clip
// path SplitText itself never touches — a long text with entities spread
// throughout, several of which straddle a chunk boundary and must be
// clipped/carried to the next chunk.
func BenchmarkSplitTextWithEntities_ManyEntities(b *testing.B) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog.\n", 500)
	units := len(text) // ASCII-only input: byte length == UTF-16 unit length
	entities := make([]Entity, 0, units/20)
	for offset := 0; offset+10 < units; offset += 20 {
		entities = append(entities, Entity{Type: "bold", Offset: int64(offset), Length: 10})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitTextWithEntities(text, entities)
	}
}
