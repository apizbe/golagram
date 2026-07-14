package golagram

// RichPlainText is RichText's "String for plain text" alternative: Telegram
// represents an unstyled run of text as a bare JSON string rather than an
// object with a "type" discriminator, unlike every other RichText member —
// see [unmarshalRichText] in types.gen.go, which tries this decode first.
// Not a spec-named type, so — unlike RichTextBold and its siblings — it
// isn't generated; it's hand-written here because [RichText]'s marker
// methods are unexported and can only be implemented from within this
// package.
type RichPlainText string

func (RichPlainText) isRichText() {}

// GetType returns "" — RichPlainText has no discriminator value; Telegram
// sends it as a bare JSON string, never as an object with a "type" field.
func (RichPlainText) GetType() string { return "" }

// RichTextSequence is RichText's "Array of RichText" alternative: several
// consecutive spans (plain text followed by a bold word, say) arrive
// concatenated in one JSON array instead of one object. Not a spec-named
// type, hand-written for the same reason as [RichPlainText].
type RichTextSequence []RichText

func (RichTextSequence) isRichText() {}

// GetType returns "" — RichTextSequence has no discriminator value either.
func (RichTextSequence) GetType() string { return "" }
