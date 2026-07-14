package golagram

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// Node is golagram's composable, entity-based formatting builder. Unlike
// parse_mode strings (HTML/MarkdownV2), building a Node produces the exact
// [MessageEntity] list Telegram wants, so there's nothing to escape and
// nesting always renders correctly:
//
//	msg := gg.Text("Hi ", gg.Bold(user.FirstName), "! See ", gg.TextLink("the docs", "https://example.com"), ".")
//	text, entities := msg.Render()
//	c.Answer(text, &gg.SendMessageOptions{Entities: entities})
type Node struct {
	kind     string // MessageEntity.Type; "" for a plain grouping/text node
	text     string // leaf content; unused when children is non-nil
	children []Node

	url           string
	user          *User
	language      string
	customEmojiID string
}

// Text groups plain strings and other Nodes into one Node, with no
// formatting of its own — the entry point for building a message:
// gg.Text("a ", gg.Bold("b"), " c").
func Text(parts ...any) Node {
	return Node{children: nodesFrom(parts)}
}

// Bold renders its content bold.
func Bold(parts ...any) Node { return wrap("bold", parts) }

// Italic renders its content italic.
func Italic(parts ...any) Node { return wrap("italic", parts) }

// Underline renders its content underlined.
func Underline(parts ...any) Node { return wrap("underline", parts) }

// Strikethrough renders its content struck through.
func Strikethrough(parts ...any) Node { return wrap("strikethrough", parts) }

// Spoiler renders its content as a tap-to-reveal spoiler.
func Spoiler(parts ...any) Node { return wrap("spoiler", parts) }

// Blockquote renders its content as a block quotation.
func Blockquote(parts ...any) Node { return wrap("blockquote", parts) }

// ExpandableBlockquote renders its content as a collapsed-by-default block
// quotation the user can expand.
func ExpandableBlockquote(parts ...any) Node { return wrap("expandable_blockquote", parts) }

// Code renders text as inline monospace. Unlike the other constructors it
// takes a plain string, not nested Nodes — Telegram's "code" entity has no
// meaningful nested formatting.
func Code(text string) Node {
	return Node{kind: "code", text: text}
}

// Pre renders text as a monospace block, optionally syntax-highlighted for
// the given language (pass "" for none).
func Pre(text, language string) Node {
	return Node{kind: "pre", text: text, language: language}
}

// TextLink renders text as a clickable link to url.
func TextLink(text, url string) Node {
	return Node{kind: "text_link", text: text, url: url}
}

// Mention renders text as a clickable mention of user — for users without a
// @username, where a plain "@username" text_mention/mention entity can't be
// built from text alone.
func Mention(text string, user *User) Node {
	return Node{kind: "text_mention", text: text, user: user}
}

// CustomEmoji renders text as a custom emoji sticker. text is normally the
// emoji's own placeholder character; customEmojiID comes from a sticker set
// ([TelegramBot.GetCustomEmojiStickers]).
func CustomEmoji(text, customEmojiID string) Node {
	return Node{kind: "custom_emoji", text: text, customEmojiID: customEmojiID}
}

// Render walks the Node tree into the plain text and [MessageEntity] list
// Telegram expects, with every entity's Offset/Length in UTF-16 code units.
func (n Node) Render() (string, []Entity) {
	return n.render(0)
}

func (n Node) render(offset int) (string, []Entity) {
	if n.children == nil {
		length := utf16Len(n.text)
		if n.kind == "" {
			return n.text, nil
		}
		return n.text, []Entity{{
			Type:          n.kind,
			Offset:        int64(offset),
			Length:        int64(length),
			URL:           n.url,
			User:          n.user,
			Language:      n.language,
			CustomEmojiID: n.customEmojiID,
		}}
	}

	var b strings.Builder
	var entities []Entity
	pos := offset
	for _, child := range n.children {
		text, es := child.render(pos)
		b.WriteString(text)
		entities = append(entities, es...)
		pos += utf16Len(text)
	}
	text := b.String()
	if n.kind != "" {
		entities = append([]Entity{{
			Type:   n.kind,
			Offset: int64(offset),
			Length: int64(pos - offset),
		}}, entities...)
	}
	return text, entities
}

func wrap(kind string, parts []any) Node {
	return Node{kind: kind, children: nodesFrom(parts)}
}

func nodesFrom(parts []any) []Node {
	out := make([]Node, 0, len(parts))
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			out = append(out, Node{text: v})
		case Node:
			out = append(out, v)
		default:
			panic(fmt.Sprintf("golagram: formatting nodes only accept string or Node, got %T", p))
		}
	}
	return out
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
