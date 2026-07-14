package golagram

import "testing"

func TestRichMessage_PlainText_ParagraphWithMixedSpans(t *testing.T) {
	msg := &RichMessage{Blocks: []RichBlock{
		&RichBlockParagraph{Type: "paragraph", Text: RichTextSequence{
			RichPlainText("Order "),
			&RichTextBold{Type: "bold", Text: RichPlainText("#1234")},
			RichPlainText(" shipped."),
		}},
	}}
	if got := msg.PlainText(); got != "Order #1234 shipped." {
		t.Errorf("PlainText = %q", got)
	}
}

func TestRichMessage_PlainText_NilReceiver(t *testing.T) {
	var msg *RichMessage
	if got := msg.PlainText(); got != "" {
		t.Errorf("PlainText on nil = %q, want empty", got)
	}
}

func TestRichMessage_PlainText_MultipleBlocksJoinedByNewline(t *testing.T) {
	msg := &RichMessage{Blocks: []RichBlock{
		&RichBlockSectionHeading{Type: "heading", Text: RichPlainText("Title"), Size: 1},
		&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Body.")},
	}}
	want := "Title\nBody."
	if got := msg.PlainText(); got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

func TestRichBlockPlainText_List(t *testing.T) {
	l := &RichBlockList{Type: "list", Items: []RichBlockListItem{
		{Blocks: []RichBlock{&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Coffee")}}},
		{HasCheckbox: true, IsChecked: true, Blocks: []RichBlock{&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Ship order")}}},
		{HasCheckbox: true, IsChecked: false, Blocks: []RichBlock{&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Send invoice")}}},
	}}
	want := "- Coffee\n[x] Ship order\n[ ] Send invoice"
	if got := RichBlockPlainText(l); got != want {
		t.Errorf("RichBlockPlainText(list) = %q, want %q", got, want)
	}
}

func TestRichBlockPlainText_BlockquoteWithCredit(t *testing.T) {
	bq := &RichBlockBlockQuotation{
		Type:   "blockquote",
		Blocks: []RichBlock{&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Quoted.")}},
		Credit: RichPlainText("Author"),
	}
	want := "> Quoted. — Author"
	if got := RichBlockPlainText(bq); got != want {
		t.Errorf("RichBlockPlainText(blockquote) = %q, want %q", got, want)
	}
}

func TestRichBlockPlainText_BlockquoteWithoutCredit(t *testing.T) {
	bq := &RichBlockBlockQuotation{Type: "blockquote", Blocks: []RichBlock{&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Quoted.")}}}
	want := "> Quoted."
	if got := RichBlockPlainText(bq); got != want {
		t.Errorf("RichBlockPlainText(blockquote) = %q, want %q", got, want)
	}
}

func TestRichBlockPlainText_Table(t *testing.T) {
	table := &RichBlockTable{Type: "table", Cells: [][]RichBlockTableCell{
		{{Text: RichPlainText("Item")}, {Text: RichPlainText("Qty")}},
		{{Text: RichPlainText("Widget")}, {Text: RichPlainText("3")}},
	}}
	want := "Item | Qty\nWidget | 3"
	if got := RichBlockPlainText(table); got != want {
		t.Errorf("RichBlockPlainText(table) = %q, want %q", got, want)
	}
}

func TestRichBlockPlainText_Details(t *testing.T) {
	d := &RichBlockDetails{
		Type:    "details",
		Summary: RichPlainText("Summary"),
		Blocks:  []RichBlock{&RichBlockParagraph{Type: "paragraph", Text: RichPlainText("Body")}},
	}
	want := "Summary\nBody"
	if got := RichBlockPlainText(d); got != want {
		t.Errorf("RichBlockPlainText(details) = %q, want %q", got, want)
	}
}

func TestRichBlockPlainText_MediaBlocksRenderAsBracketedTags(t *testing.T) {
	cases := []struct {
		block RichBlock
		want  string
	}{
		{&RichBlockPhoto{Type: "photo"}, "[photo]"},
		{&RichBlockVideo{Type: "video"}, "[video]"},
		{&RichBlockAudio{Type: "audio"}, "[audio]"},
		{&RichBlockVoiceNote{Type: "voice_note"}, "[voice note]"},
		{&RichBlockAnimation{Type: "animation"}, "[animation]"},
		{&RichBlockCollage{Type: "collage"}, "[collage]"},
		{&RichBlockSlideshow{Type: "slideshow"}, "[slideshow]"},
		{&RichBlockMap{Type: "map", Location: &Location{}}, "[map]"},
	}
	for _, c := range cases {
		if got := RichBlockPlainText(c.block); got != c.want {
			t.Errorf("RichBlockPlainText(%T) = %q, want %q", c.block, got, c.want)
		}
	}
}

func TestRichTextPlainText_DropsStyleMarkup(t *testing.T) {
	// Bold/italic/etc. have no plain-text rendering — verify style is
	// dropped and only the underlying text survives, unlike the raw HTML
	// RenderRichMessage produces for the same tree.
	nested := &RichTextBold{Type: "bold", Text: &RichTextItalic{Type: "italic", Text: RichPlainText("x")}}
	if got := RichTextPlainText(nested); got != "x" {
		t.Errorf("RichTextPlainText(nested bold/italic) = %q, want %q", got, "x")
	}
}

func TestRichTextPlainText_Nil(t *testing.T) {
	if got := RichTextPlainText(nil); got != "" {
		t.Errorf("RichTextPlainText(nil) = %q, want empty", got)
	}
}
