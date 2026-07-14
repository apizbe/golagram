package golagram

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Rich Message limits Telegram documents (sendRichMessage / Rich Message
// formatting options) — enforced here so an oversized message fails at
// RenderRichMessage, before a wasted network round trip, the same pattern
// [validateOutgoingText] and [validateReplyMarkup] already use.
const (
	maxRichMessageChars       = 32768
	maxRichMessageBlocks      = 500
	maxRichMessageDepth       = 16
	maxRichMessageTableColumn = 20
)

// Rich Message builders — construct a [RichBlock]/[RichText] tree with these
// (or a struct literal directly, same as [InlineKeyboardButton] — every
// constructor here just fills in the same generated types [RenderRichMessage]
// already has to handle for reading), then render it:
//
//	msg, err := gg.RenderRichMessage(
//		gg.RichHeading(2, gg.RichPlain("Order Confirmed")),
//		gg.RichParagraph(gg.RichPlain("Order "), gg.RichBold(gg.RichPlain("#1234")), gg.RichPlain(" shipped.")),
//	)
//	bot.SendRichMessage(ctx, &gg.SendRichMessageRequest{ChatID: gg.ChatIDFromInt(chatID), RichMessage: msg})
//
// Media has two shapes. The generated types ([RichBlockPhoto],
// [RichBlockVideo], [RichBlockAudio], [RichBlockVoiceNote],
// [RichBlockAnimation]) are decode-only: they carry Telegram file
// references ([PhotoSize], [Video], [Audio], ...) that only exist once
// media has already been uploaded, so there's nothing for a builder to
// originate and [RenderRichMessage] refuses to render one. To author new
// media from a URL, use [RichPhoto]/[RichVideo]/[RichAudio] instead (group
// several with [RichCollage]/[RichSlideshow]); voice notes and animations
// have no builder — their HTML tags overlap audio/video with a
// distinguishing convention this package doesn't guess at, so author those
// directly as HTML via [InputRichMessage.Html]. [RichBlockMap] has no such
// gap (plain lat/long/zoom) and is fully supported.

// combine folds spans into one RichText: none becomes empty plain text, one
// is returned as-is, more than one becomes a [RichTextSequence] — the same
// "Array of RichText" shape [unmarshalRichText] decodes on the way in.
func combine(spans []RichText) RichText {
	switch len(spans) {
	case 0:
		return RichPlainText("")
	case 1:
		return spans[0]
	default:
		return RichTextSequence(spans)
	}
}

// RichPlain wraps s as a [RichText] leaf — Telegram's "String for plain
// text" alternative, decoded as [RichPlainText].
func RichPlain(s string) RichText { return RichPlainText(s) }

// RichBold wraps spans in bold (<b>).
func RichBold(spans ...RichText) RichText {
	return &RichTextBold{Type: "bold", Text: combine(spans)}
}

// RichItalic wraps spans in italics (<i>).
func RichItalic(spans ...RichText) RichText {
	return &RichTextItalic{Type: "italic", Text: combine(spans)}
}

// RichUnderline underlines spans (<u>).
func RichUnderline(spans ...RichText) RichText {
	return &RichTextUnderline{Type: "underline", Text: combine(spans)}
}

// RichStrikethrough strikes through spans (<s>).
func RichStrikethrough(spans ...RichText) RichText {
	return &RichTextStrikethrough{Type: "strikethrough", Text: combine(spans)}
}

// RichSpoiler hides spans behind a spoiler (<tg-spoiler>).
func RichSpoiler(spans ...RichText) RichText {
	return &RichTextSpoiler{Type: "spoiler", Text: combine(spans)}
}

// RichCode renders spans as inline fixed-width code (<code>).
func RichCode(spans ...RichText) RichText {
	return &RichTextCode{Type: "code", Text: combine(spans)}
}

// RichInlineMath renders a LaTeX expression inline (<tg-math>). latex is
// sent verbatim, not HTML-escaped — escaping would corrupt LaTeX's own
// backslashes and braces.
func RichInlineMath(latex string) RichText {
	return &RichTextMathematicalExpression{Type: "mathematical_expression", Expression: latex}
}

// RichLink wraps spans in a link to url (<a href="...">); with no spans,
// the URL itself is the visible label.
func RichLink(url string, spans ...RichText) RichText {
	text := combine(spans)
	if len(spans) == 0 {
		text = RichPlainText(url)
	}
	return &RichTextURL{Type: "url", Text: text, URL: url}
}

// RichUserMention links spans to user by ID (<a href="tg://user?id=...">) —
// works even without a username, unlike an @mention. With no spans, the
// user's first name is the visible label.
func RichUserMention(user *User, spans ...RichText) RichText {
	text := combine(spans)
	if len(spans) == 0 && user != nil {
		text = RichPlainText(user.FirstName)
	}
	return &RichTextTextMention{Type: "text_mention", Text: text, User: user}
}

// RichAnchorLink links spans to the in-message anchor named name
// (<a href="#name">) — jumps to [RichAnchorBlock]/[RichAnchorPoint] with a
// matching name, or to the top of the message if name is "".
func RichAnchorLink(name string, spans ...RichText) RichText {
	text := combine(spans)
	if len(spans) == 0 {
		text = RichPlainText(name)
	}
	return &RichTextAnchorLink{Type: "anchor_link", Text: text, AnchorName: name}
}

// RichAnchorPoint marks an inline jump target named name (<a name="...">),
// for [RichAnchorLink] to link to. See [RichAnchorBlock] for a block-level
// (standalone) anchor instead.
func RichAnchorPoint(name string) RichText {
	return &RichTextAnchor{Type: "anchor", Name: name}
}

// RichParagraph is a plain text block (<p>).
func RichParagraph(spans ...RichText) RichBlock {
	return &RichBlockParagraph{Type: "paragraph", Text: combine(spans)}
}

// RichHeading is a section heading (<h1>-<h6>); size is clamped to 1-6
// (1 largest, 6 smallest, per Telegram's own field doc).
func RichHeading(size int, spans ...RichText) RichBlock {
	if size < 1 {
		size = 1
	}
	if size > 6 {
		size = 6
	}
	return &RichBlockSectionHeading{Type: "heading", Text: combine(spans), Size: int64(size)}
}

// RichDivider is a horizontal rule (<hr/>).
func RichDivider() RichBlock {
	return &RichBlockDivider{Type: "divider"}
}

// RichFooter is a footer block (<footer>).
func RichFooter(spans ...RichText) RichBlock {
	return &RichBlockFooter{Type: "footer", Text: combine(spans)}
}

// RichPreformatted is a code block (<pre>), tagged with language if
// non-empty (<pre><code class="language-...">).
func RichPreformatted(language string, spans ...RichText) RichBlock {
	return &RichBlockPreformatted{Type: "pre", Text: combine(spans), Language: language}
}

// RichBlockquote is a block quotation (<blockquote>) wrapping blocks, with
// credit (<cite>) if non-nil — pass nil for none.
func RichBlockquote(credit RichText, blocks ...RichBlock) RichBlock {
	return &RichBlockBlockQuotation{Type: "blockquote", Blocks: blocks, Credit: credit}
}

// RichPullquote is a pull quotation (<aside>) — a short quotation pulled
// out for emphasis, as opposed to [RichBlockquote]'s attribution of quoted
// source material. credit (<cite>) may be nil.
func RichPullquote(credit RichText, spans ...RichText) RichBlock {
	return &RichBlockPullQuotation{Type: "pullquote", Text: combine(spans), Credit: credit}
}

// RichAnchorBlock marks a standalone jump target named name — the
// block-level counterpart to [RichAnchorPoint], for when the anchor isn't
// attached to any particular span of text.
func RichAnchorBlock(name string) RichBlock {
	return &RichBlockAnchor{Type: "anchor", Name: name}
}

// RichMathBlock renders a LaTeX expression as its own block
// (<tg-math-block>), as opposed to [RichInlineMath] within a paragraph.
func RichMathBlock(latex string) RichBlock {
	return &RichBlockMathematicalExpression{Type: "mathematical_expression", Expression: latex}
}

// RichMap renders a static map (<tg-map>) centered on lat/long; zoom is
// clamped to 13-20 per Telegram's documented range.
func RichMap(lat, long float64, zoom int) RichBlock {
	if zoom < 13 {
		zoom = 13
	}
	if zoom > 20 {
		zoom = 20
	}
	return &RichBlockMap{Type: "map", Location: &Location{Latitude: lat, Longitude: long}, Zoom: int64(zoom)}
}

// RichThinking is an AI-style "thinking…" placeholder block (<tg-thinking>)
// — legal only inside [TelegramBot.SendRichMessageDraft]; never send it via
// [TelegramBot.SendRichMessage], and it never comes back on a stored
// message (see [RichBlockThinking]'s own doc).
func RichThinking(spans ...RichText) RichBlock {
	return &RichBlockThinking{Type: "thinking", Text: combine(spans)}
}

// RichList is a list block (<ul>/<ol>) built from items — see [RichItem],
// [RichOrderedItem], and [RichCheckItem]. Whether it renders as ordered,
// unordered, or a checklist is inferred from the items themselves (see
// [renderRichList]), not passed here, since that's exactly the information
// Telegram's own [RichBlockListItem] fields already carry.
func RichList(items ...RichBlockListItem) RichBlock {
	return &RichBlockList{Type: "list", Items: items}
}

// RichItem is a plain (unordered, unchecked) list item wrapping blocks —
// almost always a single [RichParagraph].
func RichItem(blocks ...RichBlock) RichBlockListItem {
	return RichBlockListItem{Blocks: blocks}
}

// RichOrderedItem is a list item that participates in decimal numbering —
// set the returned value's Type/Value fields directly (see
// [RichBlockListItem]'s doc) for a different numbering style or an
// explicit start value.
func RichOrderedItem(blocks ...RichBlock) RichBlockListItem {
	return RichBlockListItem{Blocks: blocks, Type: "1"}
}

// RichCheckItem is a checklist item (<input type="checkbox">).
func RichCheckItem(checked bool, blocks ...RichBlock) RichBlockListItem {
	return RichBlockListItem{Blocks: blocks, HasCheckbox: true, IsChecked: checked}
}

// RichCell is a table data cell (<td>), left/top-aligned by default — set
// the returned value's Align/Valign fields directly to override, since
// Telegram's own [RichBlockTableCell] fields have no "default" value
// (they're required, not omitempty) and an empty string isn't one of the
// three the docs allow.
func RichCell(spans ...RichText) RichBlockTableCell {
	return RichBlockTableCell{Text: combine(spans), Align: "left", Valign: "top"}
}

// RichHeaderCell is [RichCell] marked as a header cell (<th>).
func RichHeaderCell(spans ...RichText) RichBlockTableCell {
	c := RichCell(spans...)
	c.IsHeader = true
	return c
}

// RichTable is a table (<table>), one []RichBlockTableCell per row (see
// [RichCell]/[RichHeaderCell]); caption may be nil.
func RichTable(bordered, striped bool, caption RichText, rows ...[]RichBlockTableCell) RichBlock {
	return &RichBlockTable{Type: "table", Cells: rows, IsBordered: bordered, IsStriped: striped, Caption: caption}
}

// RichDetails is a collapsible section (<details>), always showing summary
// with blocks revealed on expand (open, or always if open is true).
func RichDetails(open bool, summary RichText, blocks ...RichBlock) RichBlock {
	return &RichBlockDetails{Type: "details", Summary: summary, Blocks: blocks, IsOpen: open}
}

// RichBlockAuthoredMedia is a URL-sourced media leaf — <img>/<video>/
// <audio>, chosen by which constructor built it ([RichPhoto], [RichVideo],
// [RichAudio]). Unlike the generated RichBlockPhoto/Video/Audio (decode-
// only, carrying Telegram's post-upload file references), this is how
// [RenderRichMessage] originates new media: point it at a URL and Telegram
// fetches it. Set Spoiler/Caption on the returned value directly, same as
// [RichCell]'s Align/Valign — Caption renders as <figure>/<figcaption>,
// with Caption.Credit as <cite>, same shape as [RichBlockPhoto]'s own
// caption.
type RichBlockAuthoredMedia struct {
	tag     string
	URL     string
	Spoiler bool
	Caption *RichBlockCaption
}

func (*RichBlockAuthoredMedia) isRichBlock()      {}
func (v *RichBlockAuthoredMedia) GetType() string { return "authored_" + v.tag }

// RichPhoto originates a new photo by URL (<img src="...">). See
// [RichBlockAuthoredMedia] for Spoiler/Caption and how to group several
// into a collage or slideshow.
func RichPhoto(url string) *RichBlockAuthoredMedia {
	return &RichBlockAuthoredMedia{tag: "img", URL: url}
}

// RichVideo is [RichPhoto]'s <video src="..."> counterpart.
func RichVideo(url string) *RichBlockAuthoredMedia {
	return &RichBlockAuthoredMedia{tag: "video", URL: url}
}

// RichAudio is [RichPhoto]'s <audio src="..."> counterpart.
func RichAudio(url string) *RichBlockAuthoredMedia {
	return &RichBlockAuthoredMedia{tag: "audio", URL: url}
}

// RichCollage groups media into a collage (<tg-collage>) — build the items
// with [RichPhoto]/[RichVideo]/[RichAudio]. Set Caption on the returned
// value directly for an overall caption, same as each item's own.
func RichCollage(items ...RichBlock) *RichBlockCollage {
	return &RichBlockCollage{Type: "collage", Blocks: items}
}

// RichSlideshow is [RichCollage]'s <tg-slideshow> counterpart — media shown
// one at a time instead of tiled.
func RichSlideshow(items ...RichBlock) *RichBlockSlideshow {
	return &RichBlockSlideshow{Type: "slideshow", Blocks: items}
}

// renderState carries render-wide bookkeeping (currently just the running
// block count) through the recursive renderRichBlock/renderRichList calls.
type renderState struct {
	blocks int
}

// RenderRichMessage renders a tree of top-level blocks to HTML and returns
// it ready to send — as [SendRichMessageRequest.RichMessage] or
// [SendRichMessageDraftRequest.RichMessage]. Validates against Telegram's
// documented Rich Message limits (32,768 characters; 500 blocks, including
// nested ones; 16 levels of block nesting; 20 table columns) before
// returning, so an oversized message fails here with a [ValidationError]
// instead of a generic error from Telegram after the network round trip.
//
// blocks (and anything nested inside them) can come from these
// constructors, a struct literal, or values read back off an incoming
// [Message.RichMessage] — RenderRichMessage doesn't care which, since it
// type-switches on the same generated types either way. The one thing it
// refuses is a media block ([RichBlockPhoto] and siblings, see this file's
// package-level doc) — that returns an error naming the offending type.
func RenderRichMessage(blocks ...RichBlock) (*InputRichMessage, error) {
	var sb strings.Builder
	st := &renderState{}
	for _, b := range blocks {
		if err := renderRichBlock(&sb, b, 0, st); err != nil {
			return nil, err
		}
	}
	html := sb.String()
	if n := utf8.RuneCountInString(html); n > maxRichMessageChars {
		return nil, &ValidationError{
			Field:   "rich_message",
			Message: fmt.Sprintf("rendered length %d exceeds Telegram's maximum of %d characters", n, maxRichMessageChars),
		}
	}
	return &InputRichMessage{Html: html}, nil
}

// htmlAttr escapes v for use inside a double-quoted HTML attribute value —
// not full HTML escaping (no text content runs through here, only
// attacker-adjacent values like URLs/names), just enough to keep the
// attribute's own quotes from being broken out of.
func htmlAttr(v string) string {
	r := strings.NewReplacer("&", "&amp;", `"`, "&quot;")
	return r.Replace(v)
}

// wrapText renders <tag attrs>...inner...</tag>, recursing into inner
// through renderRichText.
func wrapText(sb *strings.Builder, tag, attrs string, inner RichText) error {
	sb.WriteString("<")
	sb.WriteString(tag)
	sb.WriteString(attrs)
	sb.WriteString(">")
	if err := renderRichText(sb, inner); err != nil {
		return err
	}
	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteString(">")
	return nil
}

// renderRichText renders one RichText value — every one of the 24 named
// types plus the two primitive alternatives ([RichPlainText],
// [RichTextSequence]) from richtext.go — recursing through nested Text
// fields. Five members (Mention/Hashtag/Cashtag/BotCommand/BankCardNumber)
// have no dedicated HTML tag to author explicitly: Telegram detects them
// automatically from plain text content (an "@username", a "#tag", a bare
// phone-shaped string, ...), so they render as their own inner text and
// rely on that detection — same as [RichTextReferenceLink], which has no
// documented tag of its own either.
func renderRichText(sb *strings.Builder, t RichText) error {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case RichPlainText:
		sb.WriteString(EscapeHTML(string(v)))
		return nil
	case RichTextSequence:
		for _, span := range v {
			if err := renderRichText(sb, span); err != nil {
				return err
			}
		}
		return nil
	case *RichTextBold:
		return wrapText(sb, "b", "", v.Text)
	case *RichTextItalic:
		return wrapText(sb, "i", "", v.Text)
	case *RichTextUnderline:
		return wrapText(sb, "u", "", v.Text)
	case *RichTextStrikethrough:
		return wrapText(sb, "s", "", v.Text)
	case *RichTextSpoiler:
		return wrapText(sb, "tg-spoiler", "", v.Text)
	case *RichTextCode:
		return wrapText(sb, "code", "", v.Text)
	case *RichTextMarked:
		return wrapText(sb, "mark", "", v.Text)
	case *RichTextSubscript:
		return wrapText(sb, "sub", "", v.Text)
	case *RichTextSuperscript:
		return wrapText(sb, "sup", "", v.Text)
	case *RichTextCustomEmoji:
		fmt.Fprintf(sb, `<tg-emoji emoji-id="%s">`, htmlAttr(v.CustomEmojiID))
		sb.WriteString(EscapeHTML(v.AlternativeText))
		sb.WriteString("</tg-emoji>")
		return nil
	case *RichTextMathematicalExpression:
		sb.WriteString("<tg-math>")
		sb.WriteString(v.Expression) // LaTeX: not HTML-escaped, would corrupt \, {, }
		sb.WriteString("</tg-math>")
		return nil
	case *RichTextURL:
		return wrapText(sb, "a", ` href="`+htmlAttr(v.URL)+`"`, v.Text)
	case *RichTextEmailAddress:
		return wrapText(sb, "a", ` href="mailto:`+htmlAttr(v.EmailAddress)+`"`, v.Text)
	case *RichTextPhoneNumber:
		return wrapText(sb, "a", ` href="tel:`+htmlAttr(v.PhoneNumber)+`"`, v.Text)
	case *RichTextTextMention:
		userID := int64(0)
		if v.User != nil {
			userID = v.User.ID
		}
		return wrapText(sb, "a", fmt.Sprintf(` href="tg://user?id=%d"`, userID), v.Text)
	case *RichTextAnchorLink:
		return wrapText(sb, "a", ` href="#`+htmlAttr(v.AnchorName)+`"`, v.Text)
	case *RichTextAnchor:
		fmt.Fprintf(sb, `<a name="%s"></a>`, htmlAttr(v.Name))
		return nil
	case *RichTextReference:
		fmt.Fprintf(sb, `<tg-reference name="%s">`, htmlAttr(v.Name))
		if err := renderRichText(sb, v.Text); err != nil {
			return err
		}
		sb.WriteString("</tg-reference>")
		return nil
	case *RichTextReferenceLink:
		return renderRichText(sb, v.Text)
	case *RichTextDateTime:
		fmt.Fprintf(sb, `<tg-time unix="%d" format="%s">`, v.UnixTime, htmlAttr(v.DateTimeFormat))
		if err := renderRichText(sb, v.Text); err != nil {
			return err
		}
		sb.WriteString("</tg-time>")
		return nil
	case *RichTextMention:
		return renderRichText(sb, v.Text)
	case *RichTextHashtag:
		return renderRichText(sb, v.Text)
	case *RichTextCashtag:
		return renderRichText(sb, v.Text)
	case *RichTextBotCommand:
		return renderRichText(sb, v.Text)
	case *RichTextBankCardNumber:
		return renderRichText(sb, v.Text)
	default:
		return fmt.Errorf("golagram: RenderRichMessage: unrenderable RichText %T", t)
	}
}

// unrenderableMediaBlock reports the shared error for the decode-only media
// block types RenderRichMessage doesn't support — see this file's
// package-level doc for why. Photo/Video/Audio name their URL-based builder
// counterpart; VoiceNote/Animation have none (see the package doc) and fall
// back to raw HTML.
func unrenderableMediaBlock(b RichBlock) error {
	builder := ""
	switch b.(type) {
	case *RichBlockPhoto:
		builder = "RichPhoto"
	case *RichBlockVideo:
		builder = "RichVideo"
	case *RichBlockAudio:
		builder = "RichAudio"
	}
	if builder != "" {
		return fmt.Errorf("golagram: RenderRichMessage: %T has no source URL to render — "+
			"use gg.%s(url) to author new media instead, or InputRichMessage.Html directly", b, builder)
	}
	return fmt.Errorf("golagram: RenderRichMessage: %T has no source URL to render — "+
		`author media directly as HTML (<video src="...">, <audio src="...">, ...) `+
		"via InputRichMessage.Html instead", b)
}

// renderAuthoredMedia renders a [RichBlockAuthoredMedia] leaf — a bare
// self-closing tag, or wrapped in <figure>/<figcaption> (+<cite> for
// Caption.Credit) when Caption is set.
func renderAuthoredMedia(sb *strings.Builder, v *RichBlockAuthoredMedia) error {
	tag := "<" + v.tag + ` src="` + htmlAttr(v.URL) + `"`
	if v.Spoiler {
		tag += " tg-spoiler"
	}
	tag += "/>"
	if v.Caption == nil {
		sb.WriteString(tag)
		return nil
	}
	sb.WriteString("<figure>")
	sb.WriteString(tag)
	sb.WriteString("<figcaption>")
	if err := renderRichText(sb, v.Caption.Text); err != nil {
		return err
	}
	if err := renderCite(sb, v.Caption.Credit); err != nil {
		return err
	}
	sb.WriteString("</figcaption></figure>")
	return nil
}

// renderMediaGroup renders a collage/slideshow: tag wrapping each child
// block, with an optional trailing <figcaption> (+<cite>) for the group's
// own Caption — the same shape [renderAuthoredMedia] uses per item, without
// the <figure> wrapper since tag already contains the group.
func renderMediaGroup(sb *strings.Builder, tag string, blocks []RichBlock, caption *RichBlockCaption, depth int, st *renderState) error {
	sb.WriteByte('<')
	sb.WriteString(tag)
	sb.WriteByte('>')
	for _, inner := range blocks {
		if err := renderRichBlock(sb, inner, depth+1, st); err != nil {
			return err
		}
	}
	if caption != nil {
		sb.WriteString("<figcaption>")
		if err := renderRichText(sb, caption.Text); err != nil {
			return err
		}
		if err := renderCite(sb, caption.Credit); err != nil {
			return err
		}
		sb.WriteString("</figcaption>")
	}
	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteByte('>')
	return nil
}

// renderRichBlock renders one RichBlock value, tracking depth (Telegram's
// 16-level nesting limit) and st.blocks (the 500-block limit, counting
// every nested block reached recursively — through list items,
// blockquotes, and details, same as Telegram's own count).
func renderRichBlock(sb *strings.Builder, b RichBlock, depth int, st *renderState) error {
	if depth > maxRichMessageDepth {
		return &ValidationError{
			Field:   "rich_message",
			Message: fmt.Sprintf("nesting exceeds Telegram's limit of %d levels", maxRichMessageDepth),
		}
	}
	st.blocks++
	if st.blocks > maxRichMessageBlocks {
		return &ValidationError{
			Field:   "rich_message",
			Message: fmt.Sprintf("more than Telegram's limit of %d blocks (including nested)", maxRichMessageBlocks),
		}
	}

	switch v := b.(type) {
	case *RichBlockParagraph:
		return wrapText(sb, "p", "", v.Text)
	case *RichBlockSectionHeading:
		size := v.Size
		if size < 1 {
			size = 1
		}
		if size > 6 {
			size = 6
		}
		tag := fmt.Sprintf("h%d", size)
		return wrapText(sb, tag, "", v.Text)
	case *RichBlockPreformatted:
		sb.WriteString("<pre>")
		if v.Language != "" {
			sb.WriteString(`<code class="language-`)
			sb.WriteString(htmlAttr(v.Language))
			sb.WriteString(`">`)
		}
		if err := renderRichText(sb, v.Text); err != nil {
			return err
		}
		if v.Language != "" {
			sb.WriteString("</code>")
		}
		sb.WriteString("</pre>")
		return nil
	case *RichBlockFooter:
		return wrapText(sb, "footer", "", v.Text)
	case *RichBlockDivider:
		sb.WriteString("<hr/>")
		return nil
	case *RichBlockMathematicalExpression:
		sb.WriteString("<tg-math-block>")
		sb.WriteString(v.Expression)
		sb.WriteString("</tg-math-block>")
		return nil
	case *RichBlockAnchor:
		fmt.Fprintf(sb, `<a name="%s"></a>`, htmlAttr(v.Name))
		return nil
	case *RichBlockList:
		return renderRichList(sb, v, depth, st)
	case *RichBlockBlockQuotation:
		sb.WriteString("<blockquote>")
		for _, inner := range v.Blocks {
			if err := renderRichBlock(sb, inner, depth+1, st); err != nil {
				return err
			}
		}
		if err := renderCite(sb, v.Credit); err != nil {
			return err
		}
		sb.WriteString("</blockquote>")
		return nil
	case *RichBlockPullQuotation:
		sb.WriteString("<aside>")
		if err := renderRichText(sb, v.Text); err != nil {
			return err
		}
		if err := renderCite(sb, v.Credit); err != nil {
			return err
		}
		sb.WriteString("</aside>")
		return nil
	case *RichBlockTable:
		return renderRichTable(sb, v)
	case *RichBlockDetails:
		sb.WriteString("<details")
		if v.IsOpen {
			sb.WriteString(" open")
		}
		sb.WriteString("><summary>")
		if err := renderRichText(sb, v.Summary); err != nil {
			return err
		}
		sb.WriteString("</summary>")
		for _, inner := range v.Blocks {
			if err := renderRichBlock(sb, inner, depth+1, st); err != nil {
				return err
			}
		}
		sb.WriteString("</details>")
		return nil
	case *RichBlockMap:
		fmt.Fprintf(sb, `<tg-map lat="%v" long="%v" zoom="%d"/>`, v.Location.Latitude, v.Location.Longitude, v.Zoom)
		return nil
	case *RichBlockThinking:
		return wrapText(sb, "tg-thinking", "", v.Text)
	case *RichBlockAuthoredMedia:
		return renderAuthoredMedia(sb, v)
	case *RichBlockCollage:
		return renderMediaGroup(sb, "tg-collage", v.Blocks, v.Caption, depth, st)
	case *RichBlockSlideshow:
		return renderMediaGroup(sb, "tg-slideshow", v.Blocks, v.Caption, depth, st)
	case *RichBlockAnimation, *RichBlockAudio, *RichBlockPhoto, *RichBlockVideo, *RichBlockVoiceNote:
		return unrenderableMediaBlock(b)
	default:
		return fmt.Errorf("golagram: RenderRichMessage: unrenderable RichBlock %T", b)
	}
}

// renderCite renders <cite>credit</cite> if credit is non-nil, nothing
// otherwise — the shared "optional credit" tail on blockquote/pullquote.
func renderCite(sb *strings.Builder, credit RichText) error {
	if credit == nil {
		return nil
	}
	return wrapText(sb, "cite", "", credit)
}

// renderRichList renders a list block, inferring ordered/unordered/
// checklist from the items themselves rather than a separate flag —
// [RichBlockList] has none, only its items carry that information
// (HasCheckbox, Type, Value), so this is the only place it can come from.
// Any item with a checkbox makes the whole list a checklist (<ul> with
// checkbox inputs); otherwise any item with Type or Value set makes it
// ordered (<ol>); otherwise it's a plain unordered list (<ul>).
func renderRichList(sb *strings.Builder, l *RichBlockList, depth int, st *renderState) error {
	checklist, ordered := false, false
	for _, item := range l.Items {
		if item.HasCheckbox {
			checklist = true
		}
		if item.Type != "" || item.Value != 0 {
			ordered = true
		}
	}
	tag := "ul"
	if ordered && !checklist {
		tag = "ol"
	}
	sb.WriteByte('<')
	sb.WriteString(tag)
	sb.WriteByte('>')
	for _, item := range l.Items {
		sb.WriteString("<li>")
		if item.HasCheckbox {
			if item.IsChecked {
				sb.WriteString(`<input type="checkbox" checked> `)
			} else {
				sb.WriteString(`<input type="checkbox"> `)
			}
		}
		for _, inner := range item.Blocks {
			if err := renderRichBlock(sb, inner, depth+1, st); err != nil {
				return err
			}
		}
		sb.WriteString("</li>")
	}
	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteByte('>')
	return nil
}

// renderRichTable renders a table block, erroring if any row exceeds
// Telegram's documented 20-column limit.
func renderRichTable(sb *strings.Builder, t *RichBlockTable) error {
	sb.WriteString("<table")
	if t.IsBordered {
		sb.WriteString(" bordered")
	}
	if t.IsStriped {
		sb.WriteString(" striped")
	}
	sb.WriteString(">")
	if t.Caption != nil {
		if err := wrapText(sb, "caption", "", t.Caption); err != nil {
			return err
		}
	}
	for _, row := range t.Cells {
		if len(row) > maxRichMessageTableColumn {
			return &ValidationError{
				Field: "rich_message",
				Message: fmt.Sprintf("table row has %d columns, exceeding Telegram's limit of %d",
					len(row), maxRichMessageTableColumn),
			}
		}
		sb.WriteString("<tr>")
		for _, cell := range row {
			tag := "td"
			if cell.IsHeader {
				tag = "th"
			}
			sb.WriteByte('<')
			sb.WriteString(tag)
			if cell.Colspan > 1 {
				fmt.Fprintf(sb, ` colspan="%d"`, cell.Colspan)
			}
			if cell.Rowspan > 1 {
				fmt.Fprintf(sb, ` rowspan="%d"`, cell.Rowspan)
			}
			if cell.Align != "" {
				sb.WriteString(` align="`)
				sb.WriteString(htmlAttr(cell.Align))
				sb.WriteByte('"')
			}
			if cell.Valign != "" {
				sb.WriteString(` valign="`)
				sb.WriteString(htmlAttr(cell.Valign))
				sb.WriteByte('"')
			}
			sb.WriteString(">")
			if err := renderRichText(sb, cell.Text); err != nil {
				return err
			}
			sb.WriteString("</")
			sb.WriteString(tag)
			sb.WriteByte('>')
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("</table>")
	return nil
}
