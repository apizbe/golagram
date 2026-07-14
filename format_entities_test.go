package golagram

import (
	"reflect"
	"testing"
)

func TestNodeRenderPlainText(t *testing.T) {
	text, entities := Text("hello").Render()
	if text != "hello" || entities != nil {
		t.Fatalf("got %q, %v; want %q, nil", text, entities, "hello")
	}
}

func TestNodeRenderSingleEntity(t *testing.T) {
	text, entities := Bold("hi").Render()
	if text != "hi" {
		t.Fatalf("text = %q, want %q", text, "hi")
	}
	want := []Entity{{Type: "bold", Offset: 0, Length: 2}}
	if !reflect.DeepEqual(entities, want) {
		t.Fatalf("entities = %+v, want %+v", entities, want)
	}
}

func TestNodeRenderNestedAndConcatenated(t *testing.T) {
	msg := Text("Hi ", Bold("World"), "! Check ", TextLink("this", "https://example.com"), ".")
	text, entities := msg.Render()

	const want = "Hi World! Check this."
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}

	wantEntities := []Entity{
		{Type: "bold", Offset: 3, Length: 5},
		{Type: "text_link", Offset: 16, Length: 4, URL: "https://example.com"},
	}
	if !reflect.DeepEqual(entities, wantEntities) {
		t.Fatalf("entities = %+v, want %+v", entities, wantEntities)
	}
}

func TestNodeRenderDeeplyNested(t *testing.T) {
	// Bold(Italic("x")) must produce two entities of the same span, outer first.
	text, entities := Bold(Italic("x")).Render()
	if text != "x" {
		t.Fatalf("text = %q, want %q", text, "x")
	}
	want := []Entity{
		{Type: "bold", Offset: 0, Length: 1},
		{Type: "italic", Offset: 0, Length: 1},
	}
	if !reflect.DeepEqual(entities, want) {
		t.Fatalf("entities = %+v, want %+v", entities, want)
	}
}

func TestNodeRenderLeafConstructors(t *testing.T) {
	cases := []struct {
		name string
		node Node
		text string
		want Entity
	}{
		{"Code", Code("x=1"), "x=1", Entity{Type: "code", Length: 3}},
		{"Pre", Pre("go", "text/plain"), "go", Entity{Type: "pre", Length: 2, Language: "text/plain"}},
		{"Mention", Mention("Bob", &User{ID: 42}), "Bob", Entity{Type: "text_mention", Length: 3, User: &User{ID: 42}}},
		{"CustomEmoji", CustomEmoji("😀", "abc123"), "😀", Entity{Type: "custom_emoji", Length: 2, CustomEmojiID: "abc123"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, entities := c.node.Render()
			if text != c.text {
				t.Fatalf("text = %q, want %q", text, c.text)
			}
			if len(entities) != 1 || !reflect.DeepEqual(entities[0], c.want) {
				t.Fatalf("entities = %+v, want [%+v]", entities, c.want)
			}
		})
	}
}

func TestNodeRenderUTF16Offsets(t *testing.T) {
	// "😀" is a surrogate pair (2 UTF-16 units); the following Bold entity's
	// offset must account for that, not the 1 rune / 4 bytes it takes in Go.
	msg := Text("😀 ", Bold("hi"))
	text, entities := msg.Render()
	if text != "😀 hi" {
		t.Fatalf("text = %q", text)
	}
	want := []Entity{{Type: "bold", Offset: 3, Length: 2}}
	if !reflect.DeepEqual(entities, want) {
		t.Fatalf("entities = %+v, want %+v", entities, want)
	}
}

func TestNodesFromPanicsOnBadType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on non-string/Node argument")
		}
	}()
	Bold(42)
}
