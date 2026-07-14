package golagram

import "strings"

// PlainText flattens m back to plain, readable text — the read-side
// counterpart to [RenderRichMessage]. There's no plain-text way to
// represent bold/italic/etc., so style is dropped entirely; paragraph
// breaks, list items, and blockquote/table structure are kept since those
// stay meaningful without it. Useful for logging, search indexing, or
// forwarding a rich message's content somewhere only plain text works.
func (m *RichMessage) PlainText() string {
	if m == nil {
		return ""
	}
	return richBlocksPlainText(m.Blocks)
}

// RichBlockPlainText flattens one [RichBlock] to plain text, recursing into
// nested blocks (list items, blockquotes, details) and delegating span-level
// content to [RichTextPlainText]. Every one of the 24 concrete RichBlock
// types is handled explicitly; the seven media types (no text to extract)
// render as a short bracketed tag instead of dropping silently.
func RichBlockPlainText(b RichBlock) string {
	switch v := b.(type) {
	case *RichBlockParagraph:
		return RichTextPlainText(v.Text)
	case *RichBlockSectionHeading:
		return RichTextPlainText(v.Text)
	case *RichBlockPreformatted:
		return RichTextPlainText(v.Text)
	case *RichBlockFooter:
		return RichTextPlainText(v.Text)
	case *RichBlockDivider:
		return "---"
	case *RichBlockMathematicalExpression:
		return v.Expression
	case *RichBlockAnchor:
		return ""
	case *RichBlockList:
		lines := make([]string, 0, len(v.Items))
		for _, item := range v.Items {
			lines = append(lines, richListItemPlainText(item))
		}
		return strings.Join(lines, "\n")
	case *RichBlockBlockQuotation:
		return "> " + richBlocksPlainText(v.Blocks) + richCreditSuffix(v.Credit)
	case *RichBlockPullQuotation:
		return RichTextPlainText(v.Text) + richCreditSuffix(v.Credit)
	case *RichBlockCollage:
		return "[collage]"
	case *RichBlockSlideshow:
		return "[slideshow]"
	case *RichBlockTable:
		return richTablePlainText(v)
	case *RichBlockDetails:
		summary := RichTextPlainText(v.Summary)
		if body := richBlocksPlainText(v.Blocks); body != "" {
			return summary + "\n" + body
		}
		return summary
	case *RichBlockMap:
		return "[map]"
	case *RichBlockAnimation:
		return "[animation]"
	case *RichBlockAudio:
		return "[audio]"
	case *RichBlockPhoto:
		return "[photo]"
	case *RichBlockVideo:
		return "[video]"
	case *RichBlockVoiceNote:
		return "[voice note]"
	case *RichBlockThinking:
		return RichTextPlainText(v.Text)
	default:
		return ""
	}
}

// RichTextPlainText flattens one [RichText] to plain text — every one of
// the 24 named types plus the two primitive alternatives ([RichPlainText],
// [RichTextSequence]) — dropping style entirely (there's no plain-text
// rendering of "bold") and returning the underlying words.
func RichTextPlainText(t RichText) string {
	if t == nil {
		return ""
	}
	switch v := t.(type) {
	case RichPlainText:
		return string(v)
	case RichTextSequence:
		parts := make([]string, len(v))
		for i, span := range v {
			parts[i] = RichTextPlainText(span)
		}
		return strings.Join(parts, "")
	case *RichTextBold:
		return RichTextPlainText(v.Text)
	case *RichTextItalic:
		return RichTextPlainText(v.Text)
	case *RichTextUnderline:
		return RichTextPlainText(v.Text)
	case *RichTextStrikethrough:
		return RichTextPlainText(v.Text)
	case *RichTextSpoiler:
		return RichTextPlainText(v.Text)
	case *RichTextMarked:
		return RichTextPlainText(v.Text)
	case *RichTextCode:
		return RichTextPlainText(v.Text)
	case *RichTextSubscript:
		return RichTextPlainText(v.Text)
	case *RichTextSuperscript:
		return RichTextPlainText(v.Text)
	case *RichTextCustomEmoji:
		return v.AlternativeText
	case *RichTextMathematicalExpression:
		return v.Expression
	case *RichTextURL:
		return RichTextPlainText(v.Text)
	case *RichTextEmailAddress:
		return RichTextPlainText(v.Text)
	case *RichTextPhoneNumber:
		return RichTextPlainText(v.Text)
	case *RichTextBankCardNumber:
		return RichTextPlainText(v.Text)
	case *RichTextMention:
		return RichTextPlainText(v.Text)
	case *RichTextHashtag:
		return RichTextPlainText(v.Text)
	case *RichTextCashtag:
		return RichTextPlainText(v.Text)
	case *RichTextBotCommand:
		return RichTextPlainText(v.Text)
	case *RichTextAnchor:
		return ""
	case *RichTextAnchorLink:
		return RichTextPlainText(v.Text)
	case *RichTextReference:
		return RichTextPlainText(v.Text)
	case *RichTextReferenceLink:
		return RichTextPlainText(v.Text)
	case *RichTextDateTime:
		return RichTextPlainText(v.Text)
	case *RichTextTextMention:
		return RichTextPlainText(v.Text)
	default:
		return ""
	}
}

func richListItemPlainText(item RichBlockListItem) string {
	prefix := "-"
	switch {
	case item.HasCheckbox && item.IsChecked:
		prefix = "[x]"
	case item.HasCheckbox:
		prefix = "[ ]"
	}
	return prefix + " " + richBlocksPlainText(item.Blocks)
}

func richBlocksPlainText(blocks []RichBlock) string {
	lines := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if s := RichBlockPlainText(b); s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

func richCreditSuffix(credit RichText) string {
	if credit == nil {
		return ""
	}
	return " — " + RichTextPlainText(credit)
}

func richTablePlainText(t *RichBlockTable) string {
	rows := make([]string, 0, len(t.Cells))
	for _, row := range t.Cells {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = RichTextPlainText(cell.Text)
		}
		rows = append(rows, strings.Join(cells, " | "))
	}
	return strings.Join(rows, "\n")
}
