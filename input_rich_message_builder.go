package golagram

import "fmt"

// InputRichBlock builders — Bot API 10.2's structured alternative to
// [RenderRichMessage]'s HTML-string path: construct an [InputRichBlock] tree
// with these (or a struct literal directly, same as the [RichBlock]
// builders), then assemble it with [RenderRichMessageBlocks]:
//
//	msg, err := gg.RenderRichMessageBlocks(
//		gg.InputRichHeading(2, gg.RichPlain("Order Confirmed")),
//		gg.InputRichParagraph(gg.RichPlain("Order "), gg.RichBold(gg.RichPlain("#1234")), gg.RichPlain(" shipped.")),
//	)
//	bot.SendRichMessage(ctx, &gg.SendRichMessageRequest{ChatID: gg.ChatIDFromInt(chatID), RichMessage: msg})
//
// Text spans are shared with the HTML path — [RichPlain], [RichBold], and
// the rest of richmessage_builder.go's RichText builders work here
// unchanged, since Bot API 10.2 reuses [RichText] for both directions;
// only the block level got a new parallel "Input" mirror.
//
// Media is a real improvement over the HTML path's [RichPhoto]/[RichVideo]/
// [RichAudio] (URL-only, via the hand-rolled [RichBlockAuthoredMedia]):
// [InputRichPhoto], [InputRichVideo], [InputRichAudio], [InputRichAnimation],
// and [InputRichVoiceNote] all take a real [InputFile], so a file_id or a
// multipart upload (InputFileUpload) works here, not just a URL — and
// voice notes/animations, which the HTML path can't originate at all, are
// fully supported. The one thing this path drops is spoiler support: unlike
// [RichBlockAuthoredMedia].Spoiler, neither InputRichBlock's media types nor
// [RichBlockCaption] have anywhere to put one — Telegram's own schema has no
// field for it here.

// InputRichParagraph is a plain text block (<p>).
func InputRichParagraph(spans ...RichText) InputRichBlock {
	return &InputRichBlockParagraph{Type: "paragraph", Text: combine(spans)}
}

// InputRichHeading is a section heading (<h1>-<h6>); size is clamped to 1-6
// (1 largest, 6 smallest, per Telegram's own field doc).
func InputRichHeading(size int, spans ...RichText) InputRichBlock {
	if size < 1 {
		size = 1
	}
	if size > 6 {
		size = 6
	}
	return &InputRichBlockSectionHeading{Type: "heading", Text: combine(spans), Size: int64(size)}
}

// InputRichDivider is a horizontal rule (<hr/>).
func InputRichDivider() InputRichBlock {
	return &InputRichBlockDivider{Type: "divider"}
}

// InputRichFooter is a footer block (<footer>).
func InputRichFooter(spans ...RichText) InputRichBlock {
	return &InputRichBlockFooter{Type: "footer", Text: combine(spans)}
}

// InputRichPreformatted is a code block (<pre>), tagged with language if
// non-empty (<pre><code class="language-...">).
func InputRichPreformatted(language string, spans ...RichText) InputRichBlock {
	return &InputRichBlockPreformatted{Type: "pre", Text: combine(spans), Language: language}
}

// InputRichBlockquote is a block quotation (<blockquote>) wrapping blocks,
// with credit (<cite>) if non-nil — pass nil for none.
func InputRichBlockquote(credit RichText, blocks ...InputRichBlock) InputRichBlock {
	return &InputRichBlockBlockQuotation{Type: "blockquote", Blocks: blocks, Credit: credit}
}

// InputRichPullquote is a pull quotation (<aside>) — a short quotation
// pulled out for emphasis, as opposed to [InputRichBlockquote]'s
// attribution of quoted source material. credit (<cite>) may be nil.
func InputRichPullquote(credit RichText, spans ...RichText) InputRichBlock {
	return &InputRichBlockPullQuotation{Type: "pullquote", Text: combine(spans), Credit: credit}
}

// InputRichAnchorBlock marks a standalone jump target named name —
// [RichAnchorLink] (RichText-level, shared with the HTML path) links to it.
func InputRichAnchorBlock(name string) InputRichBlock {
	return &InputRichBlockAnchor{Type: "anchor", Name: name}
}

// InputRichMathBlock renders a LaTeX expression as its own block
// (<tg-math-block>), as opposed to [RichInlineMath] within a paragraph.
func InputRichMathBlock(latex string) InputRichBlock {
	return &InputRichBlockMathematicalExpression{Type: "mathematical_expression", Expression: latex}
}

// InputRichMap renders a static map (<tg-map>) centered on lat/long, sized
// width x height; zoom is clamped to 0-24 — [InputRichBlockMap]'s own
// documented range, wider than the HTML <tg-map> tag's 13-20 (see
// [RichMap]).
func InputRichMap(lat, long float64, zoom, width, height int) InputRichBlock {
	if zoom < 0 {
		zoom = 0
	}
	if zoom > 24 {
		zoom = 24
	}
	return &InputRichBlockMap{
		Type:     "map",
		Location: &Location{Latitude: lat, Longitude: long},
		Zoom:     int64(zoom),
		Width:    int64(width),
		Height:   int64(height),
	}
}

// InputRichThinking is an AI-style "thinking…" placeholder block
// (<tg-thinking>) — legal only inside [TelegramBot.SendRichMessageDraft],
// same restriction as [RichThinking].
func InputRichThinking(spans ...RichText) InputRichBlock {
	return &InputRichBlockThinking{Type: "thinking", Text: combine(spans)}
}

// InputRichList is a list block (<ul>/<ol>) built from items — see
// [InputRichItem], [InputRichOrderedItem], and [InputRichCheckItem].
// Whether it renders as ordered, unordered, or a checklist is inferred by
// Telegram from the items themselves, same as [RichList].
func InputRichList(items ...InputRichBlockListItem) InputRichBlock {
	return &InputRichBlockList{Type: "list", Items: items}
}

// InputRichItem is a plain (unordered, unchecked) list item wrapping
// blocks — almost always a single [InputRichParagraph].
func InputRichItem(blocks ...InputRichBlock) InputRichBlockListItem {
	return InputRichBlockListItem{Blocks: blocks}
}

// InputRichOrderedItem is a list item that participates in decimal
// numbering — set the returned value's Type/Value fields directly (see
// [InputRichBlockListItem]'s doc) for a different numbering style or an
// explicit start value.
func InputRichOrderedItem(blocks ...InputRichBlock) InputRichBlockListItem {
	return InputRichBlockListItem{Blocks: blocks, Type: "1"}
}

// InputRichCheckItem is a checklist item (<input type="checkbox">).
func InputRichCheckItem(checked bool, blocks ...InputRichBlock) InputRichBlockListItem {
	return InputRichBlockListItem{Blocks: blocks, HasCheckbox: true, IsChecked: checked}
}

// InputRichTable is a table (<table>), one []RichBlockTableCell per row —
// build rows with [RichCell]/[RichHeaderCell], the same cell constructors
// [RichTable] uses, since InputRichBlockTable.Cells is the identical
// [][]RichBlockTableCell type. caption may be nil.
func InputRichTable(bordered, striped bool, caption RichText, rows ...[]RichBlockTableCell) InputRichBlock {
	return &InputRichBlockTable{Type: "table", Cells: rows, IsBordered: bordered, IsStriped: striped, Caption: caption}
}

// InputRichDetails is a collapsible section (<details>), always showing
// summary with blocks revealed on expand (open, or always if open is true).
func InputRichDetails(open bool, summary RichText, blocks ...InputRichBlock) InputRichBlock {
	return &InputRichBlockDetails{Type: "details", Summary: summary, Blocks: blocks, IsOpen: open}
}

// InputRichPhoto is a photo block (<img>) sourced from media — a real
// upload via [InputFileID]/[InputFileURL]/[InputFileUpload], unlike the
// HTML path's URL-only [RichPhoto]. Set Caption on the returned value
// directly, same as [InputRichCollage].
func InputRichPhoto(media InputFile) *InputRichBlockPhoto {
	return &InputRichBlockPhoto{Type: "photo", Photo: &InputMediaPhoto{Type: "photo", Media: media}}
}

// InputRichVideo is [InputRichPhoto]'s <video> counterpart.
func InputRichVideo(media InputFile) *InputRichBlockVideo {
	return &InputRichBlockVideo{Type: "video", Video: &InputMediaVideo{Type: "video", Media: media}}
}

// InputRichAudio is [InputRichPhoto]'s <audio> (music file) counterpart.
func InputRichAudio(media InputFile) *InputRichBlockAudio {
	return &InputRichBlockAudio{Type: "audio", Audio: &InputMediaAudio{Type: "audio", Media: media}}
}

// InputRichAnimation is [InputRichPhoto]'s animated <video> counterpart —
// the HTML path has no equivalent (see this file's package doc).
func InputRichAnimation(media InputFile) *InputRichBlockAnimation {
	return &InputRichBlockAnimation{Type: "animation", Animation: &InputMediaAnimation{Type: "animation", Media: media}}
}

// InputRichVoiceNote is [InputRichPhoto]'s <audio> (voice message)
// counterpart — the HTML path has no equivalent (see this file's package
// doc).
func InputRichVoiceNote(media InputFile) *InputRichBlockVoiceNote {
	return &InputRichBlockVoiceNote{Type: "voice_note", VoiceNote: &InputMediaVoiceNote{Type: "voice_note", Media: media}}
}

// InputRichCollage groups media into a collage (<tg-collage>) — build the
// items with [InputRichPhoto]/[InputRichVideo]/[InputRichAudio]/
// [InputRichAnimation]/[InputRichVoiceNote]. Set Caption on the returned
// value directly for an overall caption, same as each item's own.
func InputRichCollage(items ...InputRichBlock) *InputRichBlockCollage {
	return &InputRichBlockCollage{Type: "collage", Blocks: items}
}

// InputRichSlideshow is [InputRichCollage]'s <tg-slideshow> counterpart —
// media shown one at a time instead of tiled.
func InputRichSlideshow(items ...InputRichBlock) *InputRichBlockSlideshow {
	return &InputRichBlockSlideshow{Type: "slideshow", Blocks: items}
}

// RenderRichMessageBlocks assembles a tree of top-level [InputRichBlock]
// values into an [InputRichMessage] via Bot API 10.2's structured block
// path — ready to send as [SendRichMessageRequest.RichMessage] or
// [SendRichMessageDraftRequest.RichMessage], the same as
// [RenderRichMessage]'s HTML-string result. Validates nesting depth (16
// levels), block count (500, including nested), and table column count (20)
// against the same documented Rich Message limits [RenderRichMessage]
// enforces; there's no character-count check here since Telegram renders
// the blocks itself rather than receiving a pre-rendered string.
//
// blocks (and anything nested inside them) can come from this file's
// constructors, a struct literal, or values read back off an incoming
// [InputRichMessage.Blocks] — same "constructors or struct literal,
// doesn't matter which" flexibility [RenderRichMessage] offers for
// [RichBlock].
func RenderRichMessageBlocks(blocks ...InputRichBlock) (*InputRichMessage, error) {
	st := &renderState{}
	for _, b := range blocks {
		if err := validateInputRichBlock(b, 0, st); err != nil {
			return nil, err
		}
	}
	return &InputRichMessage{Blocks: blocks}, nil
}

// validateInputRichBlock enforces the same depth/block-count/table-column
// limits [renderRichBlock]/[renderRichTable] do, recursing into every
// container block — there's nothing to render here (Telegram receives the
// blocks as JSON, not a pre-rendered string), only limits to check.
func validateInputRichBlock(b InputRichBlock, depth int, st *renderState) error {
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
	case *InputRichBlockBlockQuotation:
		return validateInputRichBlocks(v.Blocks, depth+1, st)
	case *InputRichBlockDetails:
		return validateInputRichBlocks(v.Blocks, depth+1, st)
	case *InputRichBlockCollage:
		return validateInputRichBlocks(v.Blocks, depth+1, st)
	case *InputRichBlockSlideshow:
		return validateInputRichBlocks(v.Blocks, depth+1, st)
	case *InputRichBlockList:
		for _, item := range v.Items {
			if err := validateInputRichBlocks(item.Blocks, depth+1, st); err != nil {
				return err
			}
		}
		return nil
	case *InputRichBlockTable:
		for _, row := range v.Cells {
			if len(row) > maxRichMessageTableColumn {
				return &ValidationError{
					Field: "rich_message",
					Message: fmt.Sprintf("table row has %d columns, exceeding Telegram's limit of %d",
						len(row), maxRichMessageTableColumn),
				}
			}
		}
		return nil
	default:
		return nil
	}
}

// validateInputRichBlocks runs [validateInputRichBlock] over a slice of
// sibling blocks at the same depth, stopping at the first error.
func validateInputRichBlocks(blocks []InputRichBlock, depth int, st *renderState) error {
	for _, b := range blocks {
		if err := validateInputRichBlock(b, depth, st); err != nil {
			return err
		}
	}
	return nil
}
