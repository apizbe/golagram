package golagram

import (
	"encoding/json"
	"testing"
)

func TestRenderRichMessageBlocks_Paragraph_HeadingAndText(t *testing.T) {
	msg, err := RenderRichMessageBlocks(
		InputRichHeading(2, RichPlain("Order Confirmed")),
		InputRichParagraph(RichPlain("Order "), RichBold(RichPlain("#1234")), RichPlain(" shipped.")),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msg.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(msg.Blocks))
	}
	heading, ok := msg.Blocks[0].(*InputRichBlockSectionHeading)
	if !ok || heading.Size != 2 {
		t.Fatalf("blocks[0] = %#v, want a size-2 heading", msg.Blocks[0])
	}
	para, ok := msg.Blocks[1].(*InputRichBlockParagraph)
	if !ok {
		t.Fatalf("blocks[1] = %T, want *InputRichBlockParagraph", msg.Blocks[1])
	}
	seq, ok := para.Text.(RichTextSequence)
	if !ok || len(seq) != 3 {
		t.Fatalf("paragraph text = %#v, want a 3-element sequence", para.Text)
	}
	if msg.Html != "" {
		t.Errorf("Html should stay empty on the blocks path, got %q", msg.Html)
	}
}

func TestRenderRichMessageBlocks_MarshalsAsBlocksField(t *testing.T) {
	msg, err := RenderRichMessageBlocks(InputRichParagraph(RichPlain("hi")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var flat map[string]any
	if err := json.Unmarshal(raw, &flat); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	if _, ok := flat["blocks"]; !ok {
		t.Fatalf("marshaled InputRichMessage missing blocks field: %s", raw)
	}
	if _, ok := flat["html"]; ok {
		t.Errorf("marshaled InputRichMessage should omit html on the blocks path: %s", raw)
	}
}

func TestInputRichMap_ClampsZoomTo0And24(t *testing.T) {
	cases := []struct {
		zoom     int
		wantZoom int64
	}{
		{-5, 0}, {12, 12}, {24, 24}, {99, 24},
	}
	for _, c := range cases {
		b := InputRichMap(41.9, 12.5, c.zoom, 300, 200)
		m, ok := b.(*InputRichBlockMap)
		if !ok {
			t.Fatalf("InputRichMap returned %T, want *InputRichBlockMap", b)
		}
		if m.Zoom != c.wantZoom || m.Width != 300 || m.Height != 200 {
			t.Errorf("InputRichMap(zoom=%d) = %+v, want zoom=%d width=300 height=200", c.zoom, m, c.wantZoom)
		}
	}
}

func TestInputRichMedia_WrapsRealInputFileUploads(t *testing.T) {
	photo := InputRichPhoto(InputFileID("file123"))
	if photo.Photo == nil || photo.Photo.Media != InputFileID("file123") {
		t.Errorf("InputRichPhoto did not wrap the given InputFile: %+v", photo.Photo)
	}
	photo.Caption = &RichBlockCaption{Text: RichPlain("caption")}

	video := InputRichVideo(InputFileURL("https://example.com/a.mp4"))
	if video.Video == nil || video.Video.Media != InputFileURL("https://example.com/a.mp4") {
		t.Errorf("InputRichVideo did not wrap the given InputFile: %+v", video.Video)
	}

	// Voice notes and animations have no HTML-path equivalent — this is new
	// surface the 10.2 blocks path adds outright.
	voice := InputRichVoiceNote(InputFileID("voice1"))
	if voice.VoiceNote == nil || voice.VoiceNote.Media != InputFileID("voice1") {
		t.Errorf("InputRichVoiceNote did not wrap the given InputFile: %+v", voice.VoiceNote)
	}
	anim := InputRichAnimation(InputFileID("anim1"))
	if anim.Animation == nil || anim.Animation.Media != InputFileID("anim1") {
		t.Errorf("InputRichAnimation did not wrap the given InputFile: %+v", anim.Animation)
	}

	msg, err := RenderRichMessageBlocks(photo, video, voice, anim)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msg.Blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(msg.Blocks))
	}
}

func TestInputRichCollage_CaptionSetAfterConstruction(t *testing.T) {
	group := InputRichCollage(
		InputRichPhoto(InputFileID("p1")),
		InputRichPhoto(InputFileID("p2")),
	)
	group.Caption = &RichBlockCaption{Text: RichPlain("Gallery")}
	msg, err := RenderRichMessageBlocks(group)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	collage, ok := msg.Blocks[0].(*InputRichBlockCollage)
	if !ok || len(collage.Blocks) != 2 || collage.Caption == nil || collage.Caption.Text != RichText(RichPlainText("Gallery")) {
		t.Errorf("unexpected collage: %+v", collage)
	}
}

func TestRenderRichMessageBlocks_List_Table_Details(t *testing.T) {
	msg, err := RenderRichMessageBlocks(
		InputRichList(
			InputRichItem(InputRichParagraph(RichPlain("Coffee"))),
			InputRichCheckItem(true, InputRichParagraph(RichPlain("Tea"))),
		),
		InputRichTable(true, false, RichPlain("Orders"),
			[]RichBlockTableCell{RichHeaderCell(RichPlain("Item"))},
			[]RichBlockTableCell{RichCell(RichPlain("Widget"))},
		),
		InputRichDetails(true, RichPlain("Summary"), InputRichParagraph(RichPlain("Body"))),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := msg.Blocks[0].(*InputRichBlockList)
	if !ok || len(list.Items) != 2 || !list.Items[1].HasCheckbox || !list.Items[1].IsChecked {
		t.Fatalf("unexpected list: %+v", list)
	}
	table, ok := msg.Blocks[1].(*InputRichBlockTable)
	if !ok || !table.IsBordered || len(table.Cells) != 2 {
		t.Fatalf("unexpected table: %+v", table)
	}
	details, ok := msg.Blocks[2].(*InputRichBlockDetails)
	if !ok || !details.IsOpen || len(details.Blocks) != 1 {
		t.Fatalf("unexpected details: %+v", details)
	}
}

func TestRenderRichMessageBlocks_Table_TooManyColumns(t *testing.T) {
	row := make([]RichBlockTableCell, maxRichMessageTableColumn+1)
	for i := range row {
		row[i] = RichCell(RichPlain("x"))
	}
	_, err := RenderRichMessageBlocks(InputRichTable(false, false, nil, row))
	if err == nil {
		t.Fatal("expected an error for a row exceeding the column limit")
	}
	var ve *ValidationError
	if !asValidationError(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
}

func TestRenderRichMessageBlocks_NestingDepthLimit(t *testing.T) {
	b := InputRichParagraph(RichPlain("leaf"))
	for i := 0; i <= maxRichMessageDepth+1; i++ {
		b = InputRichBlockquote(nil, b)
	}
	_, err := RenderRichMessageBlocks(b)
	if err == nil {
		t.Fatal("expected an error for nesting beyond the documented depth limit")
	}
}

func TestRenderRichMessageBlocks_BlockCountLimit(t *testing.T) {
	blocks := make([]InputRichBlock, maxRichMessageBlocks+1)
	for i := range blocks {
		blocks[i] = InputRichParagraph(RichPlain("x"))
	}
	_, err := RenderRichMessageBlocks(blocks...)
	if err == nil {
		t.Fatal("expected an error for exceeding the block count limit")
	}
	var ve *ValidationError
	if !asValidationError(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
}

// The blocks tree round-trips through JSON the same way any other decoded
// InputRichBlock value would — construct with the builders, marshal, then
// decode back through the generated polymorphic unmarshaler.
func TestInputRichBlock_RoundTripsThroughJSON(t *testing.T) {
	original := InputRichBlockquote(RichPlain("Author"),
		InputRichParagraph(RichBold(RichPlain("bold")), RichPlain(" text")))

	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := unmarshalInputRichBlock(raw)
	if err != nil {
		t.Fatalf("unmarshalInputRichBlock: %v", err)
	}
	quote, ok := decoded.(*InputRichBlockBlockQuotation)
	if !ok || len(quote.Blocks) != 1 {
		t.Fatalf("decoded = %#v, want a blockquote with one nested block", decoded)
	}
	para, ok := quote.Blocks[0].(*InputRichBlockParagraph)
	if !ok {
		t.Fatalf("nested block = %T, want *InputRichBlockParagraph", quote.Blocks[0])
	}
	bold, ok := para.Text.(RichTextSequence)[0].(*RichTextBold)
	if !ok {
		t.Fatalf("first span = %#v, want *RichTextBold", para.Text.(RichTextSequence)[0])
	}
	if plain, ok := bold.Text.(RichPlainText); !ok || plain != "bold" {
		t.Errorf("bold.Text = %#v, want RichPlainText(%q)", bold.Text, "bold")
	}
}
