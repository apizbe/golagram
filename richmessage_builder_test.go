package golagram

import (
	"strings"
	"testing"
)

func TestRenderRichMessage_Paragraph_PlainAndBold(t *testing.T) {
	msg, err := RenderRichMessage(
		RichParagraph(RichPlain("Order "), RichBold(RichPlain("#1234")), RichPlain(" shipped.")),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<p>Order <b>#1234</b> shipped.</p>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_EscapesPlainText(t *testing.T) {
	msg, err := RenderRichMessage(RichParagraph(RichPlain("<script>alert(1)</script> & \"quotes\"")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(msg.Html, "<script>") {
		t.Errorf("expected plain text to be HTML-escaped, got %q", msg.Html)
	}
	want := `<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; "quotes"</p>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Heading_ClampsSize(t *testing.T) {
	cases := []struct {
		size int
		want string
	}{
		{0, "<h1>x</h1>"},
		{3, "<h3>x</h3>"},
		{6, "<h6>x</h6>"},
		{99, "<h6>x</h6>"},
	}
	for _, c := range cases {
		msg, err := RenderRichMessage(RichHeading(c.size, RichPlain("x")))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg.Html != c.want {
			t.Errorf("RichHeading(%d) = %q, want %q", c.size, msg.Html, c.want)
		}
	}
}

func TestRenderRichMessage_TextSpans(t *testing.T) {
	cases := []struct {
		name string
		text RichText
		want string
	}{
		{"italic", RichItalic(RichPlain("x")), "<i>x</i>"},
		{"underline", RichUnderline(RichPlain("x")), "<u>x</u>"},
		{"strikethrough", RichStrikethrough(RichPlain("x")), "<s>x</s>"},
		{"spoiler", RichSpoiler(RichPlain("x")), "<tg-spoiler>x</tg-spoiler>"},
		{"code", RichCode(RichPlain("x")), "<code>x</code>"},
		{"inline math", RichInlineMath("x^2"), "<tg-math>x^2</tg-math>"},
		{"link with label", RichLink("https://example.com", RichPlain("here")), `<a href="https://example.com">here</a>`},
		{"link without label", RichLink("https://example.com"), `<a href="https://example.com">https://example.com</a>`},
		{"user mention", RichUserMention(&User{ID: 42, FirstName: "Bob"}), `<a href="tg://user?id=42">Bob</a>`},
		{"anchor link", RichAnchorLink("notes", RichPlain("see notes")), `<a href="#notes">see notes</a>`},
		{"anchor point", RichAnchorPoint("notes"), `<a name="notes"></a>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, err := RenderRichMessage(RichParagraph(c.text))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := "<p>" + c.want + "</p>"
			if msg.Html != want {
				t.Errorf("Html = %q, want %q", msg.Html, want)
			}
		})
	}
}

func TestRenderRichMessage_AutoDetectedSpansRenderAsPlainText(t *testing.T) {
	// Mention/Hashtag/Cashtag/BotCommand/BankCardNumber have no dedicated
	// HTML tag — Telegram detects them from plain text, so the renderer
	// just emits the inner text and relies on that detection.
	spans := []RichText{
		&RichTextMention{Type: "mention", Text: RichPlainText("@alice"), Username: "alice"},
		&RichTextHashtag{Type: "hashtag", Text: RichPlainText("#golang"), Hashtag: "golang"},
		&RichTextCashtag{Type: "cashtag", Text: RichPlainText("$TON"), Cashtag: "TON"},
		&RichTextBotCommand{Type: "bot_command", Text: RichPlainText("/start"), BotCommand: "start"},
		&RichTextBankCardNumber{Type: "bank_card_number", Text: RichPlainText("4111111111111111"), BankCardNumber: "4111111111111111"},
	}
	for _, span := range spans {
		msg, err := RenderRichMessage(RichParagraph(span))
		if err != nil {
			t.Fatalf("unexpected error rendering %T: %v", span, err)
		}
		if !strings.Contains(msg.Html, "<p>") || strings.Contains(msg.Html, "<a ") {
			t.Errorf("%T rendered as %q, want plain text only", span, msg.Html)
		}
	}
}

func TestRenderRichMessage_Blockquote_WithCredit(t *testing.T) {
	msg, err := RenderRichMessage(
		RichBlockquote(RichPlain("Author"), RichParagraph(RichPlain("Quoted text."))),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<blockquote><p>Quoted text.</p><cite>Author</cite></blockquote>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Blockquote_NoCredit(t *testing.T) {
	msg, err := RenderRichMessage(RichBlockquote(nil, RichParagraph(RichPlain("Quoted."))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<blockquote><p>Quoted.</p></blockquote>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Pullquote(t *testing.T) {
	msg, err := RenderRichMessage(RichPullquote(RichPlain("Someone"), RichPlain("Ship it.")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<aside>Ship it.<cite>Someone</cite></aside>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Divider_Footer_MathBlock_AnchorBlock(t *testing.T) {
	msg, err := RenderRichMessage(
		RichDivider(),
		RichFooter(RichPlain("footer text")),
		RichMathBlock("E=mc^2"),
		RichAnchorBlock("top"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<hr/><footer>footer text</footer><tg-math-block>E=mc^2</tg-math-block>" + `<a name="top"></a>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Preformatted_WithAndWithoutLanguage(t *testing.T) {
	msg, err := RenderRichMessage(RichPreformatted("go", RichPlain("fmt.Println()")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<pre><code class="language-go">fmt.Println()</code></pre>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}

	msg, err = RenderRichMessage(RichPreformatted("", RichPlain("no lang")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = "<pre>no lang</pre>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Map(t *testing.T) {
	cases := []struct {
		zoom     int
		wantZoom int64
	}{
		{5, 13}, {16, 16}, {99, 20},
	}
	for _, c := range cases {
		msg, err := RenderRichMessage(RichMap(41.9, 12.5, c.zoom))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `<tg-map lat="41.9" long="12.5" zoom="` + itoa(c.wantZoom) + `"/>`
		if msg.Html != want {
			t.Errorf("RichMap zoom=%d: Html = %q, want %q", c.zoom, msg.Html, want)
		}
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestRenderRichMessage_List_Unordered(t *testing.T) {
	msg, err := RenderRichMessage(RichList(
		RichItem(RichParagraph(RichPlain("Coffee"))),
		RichItem(RichParagraph(RichPlain("Tea"))),
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<ul><li><p>Coffee</p></li><li><p>Tea</p></li></ul>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_List_Ordered(t *testing.T) {
	msg, err := RenderRichMessage(RichList(
		RichOrderedItem(RichParagraph(RichPlain("Preheat"))),
		RichOrderedItem(RichParagraph(RichPlain("Bake"))),
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<ol><li><p>Preheat</p></li><li><p>Bake</p></li></ol>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_List_Checklist(t *testing.T) {
	msg, err := RenderRichMessage(RichList(
		RichCheckItem(true, RichParagraph(RichPlain("Ship order"))),
		RichCheckItem(false, RichParagraph(RichPlain("Send invoice"))),
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<ul><li><input type="checkbox" checked> <p>Ship order</p></li>` +
		`<li><input type="checkbox"> <p>Send invoice</p></li></ul>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Table(t *testing.T) {
	msg, err := RenderRichMessage(RichTable(true, true, RichPlain("Orders"),
		[]RichBlockTableCell{RichHeaderCell(RichPlain("Item")), RichHeaderCell(RichPlain("Qty"))},
		[]RichBlockTableCell{RichCell(RichPlain("Widget")), RichCell(RichPlain("3"))},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<table bordered striped><caption>Orders</caption>` +
		`<tr><th align="left" valign="top">Item</th><th align="left" valign="top">Qty</th></tr>` +
		`<tr><td align="left" valign="top">Widget</td><td align="left" valign="top">3</td></tr></table>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Table_ColspanRowspan(t *testing.T) {
	cell := RichCell(RichPlain("Gadget"))
	cell.Colspan = 2
	cell.Rowspan = 3
	msg, err := RenderRichMessage(RichTable(false, false, nil, []RichBlockTableCell{cell}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<table><tr><td colspan="2" rowspan="3" align="left" valign="top">Gadget</td></tr></table>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Table_TooManyColumns(t *testing.T) {
	row := make([]RichBlockTableCell, maxRichMessageTableColumn+1)
	for i := range row {
		row[i] = RichCell(RichPlain("x"))
	}
	_, err := RenderRichMessage(RichTable(false, false, nil, row))
	if err == nil {
		t.Fatal("expected an error for a row exceeding the column limit")
	}
	var ve *ValidationError
	if !asValidationError(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
}

func TestRenderRichMessage_Details(t *testing.T) {
	msg, err := RenderRichMessage(RichDetails(true, RichPlain("Summary"), RichParagraph(RichPlain("Body"))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<details open><summary>Summary</summary><p>Body</p></details>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}

	msg, err = RenderRichMessage(RichDetails(false, RichPlain("Summary"), RichParagraph(RichPlain("Body"))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = "<details><summary>Summary</summary><p>Body</p></details>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Thinking(t *testing.T) {
	msg, err := RenderRichMessage(RichThinking(RichPlain("Thinking…")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<tg-thinking>Thinking…</tg-thinking>"
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_MediaBlocks_Error(t *testing.T) {
	// Decode-only media (file references, no URL) — RenderRichMessage still
	// refuses these. Collage/Slideshow are covered separately below, since
	// they're renderable now that their items can be [RichPhoto] etc.
	blocks := []RichBlock{
		&RichBlockPhoto{Type: "photo"},
		&RichBlockVideo{Type: "video"},
		&RichBlockAudio{Type: "audio"},
		&RichBlockVoiceNote{Type: "voice_note"},
		&RichBlockAnimation{Type: "animation"},
	}
	for _, b := range blocks {
		_, err := RenderRichMessage(b)
		if err == nil {
			t.Errorf("expected %T to error (no source URL to render)", b)
			continue
		}
		if !strings.Contains(err.Error(), "no source URL") {
			t.Errorf("%T error = %v, want it to mention 'no source URL'", b, err)
		}
	}
}

func TestRenderRichMessage_AuthoredMedia(t *testing.T) {
	cases := []struct {
		name  string
		block RichBlock
		want  string
	}{
		{"photo, bare", RichPhoto("https://example.com/a.jpg"), `<img src="https://example.com/a.jpg"/>`},
		{"video, bare", RichVideo("https://example.com/a.mp4"), `<video src="https://example.com/a.mp4"/>`},
		{"audio, bare", RichAudio("https://example.com/a.mp3"), `<audio src="https://example.com/a.mp3"/>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, err := RenderRichMessage(c.block)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msg.Html != c.want {
				t.Errorf("Html = %q, want %q", msg.Html, c.want)
			}
		})
	}

	t.Run("spoiler and caption with credit", func(t *testing.T) {
		photo := RichPhoto("https://example.com/a.jpg")
		photo.Spoiler = true
		photo.Caption = &RichBlockCaption{Text: RichPlain("First shot"), Credit: RichPlain("Photographer A")}
		msg, err := RenderRichMessage(photo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `<figure><img src="https://example.com/a.jpg" tg-spoiler/><figcaption>First shot<cite>Photographer A</cite></figcaption></figure>`
		if msg.Html != want {
			t.Errorf("Html = %q, want %q", msg.Html, want)
		}
	})
}

func TestRenderRichMessage_Collage(t *testing.T) {
	msg, err := RenderRichMessage(RichCollage(
		RichPhoto("https://example.com/1.jpg"),
		RichPhoto("https://example.com/2.jpg"),
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<tg-collage><img src="https://example.com/1.jpg"/><img src="https://example.com/2.jpg"/></tg-collage>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_Slideshow_WithCaption(t *testing.T) {
	group := RichSlideshow(RichPhoto("https://example.com/1.jpg"))
	group.Caption = &RichBlockCaption{Text: RichPlain("Gallery")}
	msg, err := RenderRichMessage(group)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<tg-slideshow><img src="https://example.com/1.jpg"/><figcaption>Gallery</figcaption></tg-slideshow>`
	if msg.Html != want {
		t.Errorf("Html = %q, want %q", msg.Html, want)
	}
}

func TestRenderRichMessage_NestingDepthLimit(t *testing.T) {
	b := RichParagraph(RichPlain("leaf"))
	for i := 0; i <= maxRichMessageDepth+1; i++ {
		b = RichBlockquote(nil, b)
	}
	_, err := RenderRichMessage(b)
	if err == nil {
		t.Fatal("expected an error for nesting beyond the documented depth limit")
	}
}

func TestRenderRichMessage_BlockCountLimit(t *testing.T) {
	blocks := make([]RichBlock, maxRichMessageBlocks+1)
	for i := range blocks {
		blocks[i] = RichParagraph(RichPlain("x"))
	}
	_, err := RenderRichMessage(blocks...)
	if err == nil {
		t.Fatal("expected an error for exceeding the documented block-count limit")
	}
}

func TestRenderRichMessage_CharLimit(t *testing.T) {
	_, err := RenderRichMessage(RichParagraph(RichPlain(strings.Repeat("x", maxRichMessageChars+1))))
	if err == nil {
		t.Fatal("expected an error for exceeding the documented character limit")
	}
	var ve *ValidationError
	if !asValidationError(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
}

// unrenderableRichBlock is a package-internal fake — RichBlock's marker
// method is unexported, so only a type in this package can implement it —
// used solely to exercise RenderRichMessage's default case: an unknown
// future concrete type must still fail loudly, not panic or silently drop.
type unrenderableRichBlock struct{}

func (unrenderableRichBlock) isRichBlock()    {}
func (unrenderableRichBlock) GetType() string { return "unknown" }

func TestRenderRichMessage_UnknownBlockType_ErrorsLoudly(t *testing.T) {
	_, err := RenderRichMessage(unrenderableRichBlock{})
	if err == nil {
		t.Fatal("expected an unrecognized RichBlock type to error, got nil")
	}
}

func asValidationError(err error, target **ValidationError) bool {
	ve, ok := err.(*ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
