package golagram

// InlineKeyboard is a stepwise builder for an [InlineKeyboardMarkup]: add
// buttons via Row, Add, and/or Insert in any combination, then call Build.
type InlineKeyboard struct {
	rows       [][]InlineKeyboardButton
	currentRow []InlineKeyboardButton
}

// NewInlineKeyboard creates an empty inline keyboard builder.
func NewInlineKeyboard() *InlineKeyboard {
	return &InlineKeyboard{
		rows: make([][]InlineKeyboardButton, 0),
	}
}

// Row appends buttons together as one new row, first finishing off any
// row already under construction via Insert.
func (kb *InlineKeyboard) Row(buttons ...InlineKeyboardButton) *InlineKeyboard {
	// Add current row if it has buttons
	if len(kb.currentRow) > 0 {
		kb.rows = append(kb.rows, kb.currentRow)
		kb.currentRow = make([]InlineKeyboardButton, 0)
	}

	// Add new row
	if len(buttons) > 0 {
		kb.rows = append(kb.rows, buttons)
	}

	return kb
}

// Add appends buttons to the keyboard, each on its own row — for a simple
// single-column list of buttons. See Row to group buttons onto one row
// instead.
func (kb *InlineKeyboard) Add(buttons ...InlineKeyboardButton) *InlineKeyboard {
	for _, btn := range buttons {
		kb.rows = append(kb.rows, []InlineKeyboardButton{btn})
	}
	return kb
}

// Insert appends button to the row currently under construction; the next
// call to Row (with no arguments) or to Build finishes it.
func (kb *InlineKeyboard) Insert(button InlineKeyboardButton) *InlineKeyboard {
	kb.currentRow = append(kb.currentRow, button)
	return kb
}

// Build finishes any row still under construction (see Insert) and returns
// the resulting [InlineKeyboardMarkup].
func (kb *InlineKeyboard) Build() *InlineKeyboardMarkup {
	// Add current row if it has buttons
	if len(kb.currentRow) > 0 {
		kb.rows = append(kb.rows, kb.currentRow)
		kb.currentRow = make([]InlineKeyboardButton, 0)
	}

	return &InlineKeyboardMarkup{
		InlineKeyboard: kb.rows,
	}
}

// Button constructors — convenience wrappers; you can also build an
// [InlineKeyboardButton] literal directly.

// NewInlineButton creates a callback-query button: pressing it delivers
// callbackData back to the bot as CallbackQuery.Data.
func NewInlineButton(text, callbackData string) InlineKeyboardButton {
	return InlineKeyboardButton{
		Text:         text,
		CallbackData: callbackData,
	}
}

// NewInlineButtonURL creates a button that opens url when pressed.
func NewInlineButtonURL(text, url string) InlineKeyboardButton {
	return InlineKeyboardButton{
		Text: text,
		URL:  url,
	}
}

// NewInlineButtonWebApp creates a button that launches the Telegram Web App
// at webAppURL — only valid in private chats.
func NewInlineButtonWebApp(text, webAppURL string) InlineKeyboardButton {
	return InlineKeyboardButton{
		Text:   text,
		WebApp: &WebAppInfo{URL: webAppURL},
	}
}

// NewInlineButtonSwitchInline creates a button that lets the user pick any
// chat and starts an inline query there, pre-filled with query.
func NewInlineButtonSwitchInline(text, query string) InlineKeyboardButton {
	return InlineKeyboardButton{
		Text:              text,
		SwitchInlineQuery: query,
	}
}

// NewInlineButtonSwitchInlineCurrent creates a button that starts an inline
// query pre-filled with query in the current chat.
func NewInlineButtonSwitchInlineCurrent(text, query string) InlineKeyboardButton {
	return InlineKeyboardButton{
		Text:                         text,
		SwitchInlineQueryCurrentChat: query,
	}
}

// Quick builder methods for common patterns.

// QuickInlineKeyboard builds a full inline keyboard of callback buttons
// from parallel {text, callback_data} pairs, one row per inner slice — a
// shortcut for the common case that needs no other options:
//
//	kb := gg.QuickInlineKeyboard([][]string{{"Text1", "data1"}, {"Text2", "data2"}})
func QuickInlineKeyboard(buttons [][]string) *InlineKeyboardMarkup {
	kb := NewInlineKeyboard()
	for _, row := range buttons {
		if len(row) >= 2 {
			kb.Add(NewInlineButton(row[0], row[1]))
		}
	}
	return kb.Build()
}

// QuickInlineRow builds a single-row inline keyboard of callback buttons
// from {text, data} pairs.
func QuickInlineRow(buttons ...struct{ text, data string }) *InlineKeyboardMarkup {
	kb := NewInlineKeyboard()
	row := make([]InlineKeyboardButton, 0, len(buttons))
	for _, btn := range buttons {
		row = append(row, NewInlineButton(btn.text, btn.data))
	}
	kb.Row(row...)
	return kb.Build()
}

// NewInlineKeyboardMarkup builds an [InlineKeyboardMarkup] directly from a
// 2D button array, for callers who already have their buttons laid out and
// don't need the Row/Add/Insert builder:
//
//	markup := gg.NewInlineKeyboardMarkup([][]gg.InlineKeyboardButton{{btn1, btn2}, {btn3}})
func NewInlineKeyboardMarkup(keyboard [][]InlineKeyboardButton) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}
}

// Adjust re-flows every button added so far into rows of the given sizes:
// sizes apply in order and the last one repeats for the remaining buttons.
// Adjust(2) lays everything out two per row; Adjust(1, 2)
// puts one button on the first row and two on each row after. Call it last,
// before Build — it replaces whatever row structure [InlineKeyboard.Row] /
// [InlineKeyboard.Add] / [InlineKeyboard.Insert] created:
//
//	kb := gg.NewInlineKeyboard()
//	for _, item := range items {
//		kb.Insert(gg.NewInlineButton(item.Name, item.ID))
//	}
//	markup := kb.Adjust(2).Build()
func (kb *InlineKeyboard) Adjust(sizes ...int) *InlineKeyboard {
	var all []InlineKeyboardButton
	for _, row := range kb.rows {
		all = append(all, row...)
	}
	all = append(all, kb.currentRow...)
	kb.rows = adjustRows(all, sizes)
	kb.currentRow = nil
	return kb
}

// adjustRows lays items out into rows of the given sizes, the last size
// repeating; no sizes (or non-positive ones) mean one item per row.
func adjustRows[T any](items []T, sizes []int) [][]T {
	rows := make([][]T, 0, len(items))
	sizeIdx := 0
	for len(items) > 0 {
		size := 1
		if len(sizes) > 0 {
			if sizeIdx >= len(sizes) {
				sizeIdx = len(sizes) - 1 // last size repeats
			}
			if s := sizes[sizeIdx]; s > 0 {
				size = s
			}
			sizeIdx++
		}
		size = min(size, len(items))
		rows = append(rows, items[:size:size])
		items = items[size:]
	}
	return rows
}
