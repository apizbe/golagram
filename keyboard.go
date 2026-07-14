package golagram

// ReplyKeyboard is a stepwise builder for a [ReplyKeyboardMarkup] (a
// keyboard that replaces the user's system keyboard, as opposed to an
// [InlineKeyboard] attached to a message): add buttons via Row, Add, and/or
// Insert in any combination, then call Build.
type ReplyKeyboard struct {
	rows                  [][]KeyboardButton
	currentRow            []KeyboardButton
	resizeKeyboard        bool
	oneTimeKeyboard       bool
	inputFieldPlaceholder string
	selective             bool
}

// NewReplyKeyboard creates an empty reply keyboard builder. resizeKeyboard
// requests that clients resize the keyboard vertically for an optimal fit
// instead of using the default large size.
func NewReplyKeyboard(resizeKeyboard bool) *ReplyKeyboard {
	return &ReplyKeyboard{
		rows:           make([][]KeyboardButton, 0),
		resizeKeyboard: resizeKeyboard,
	}
}

// Row appends buttons together as one new row, first finishing off any
// row already under construction via Insert.
func (kb *ReplyKeyboard) Row(buttons ...KeyboardButton) *ReplyKeyboard {
	// Add current row if it has buttons
	if len(kb.currentRow) > 0 {
		kb.rows = append(kb.rows, kb.currentRow)
		kb.currentRow = make([]KeyboardButton, 0)
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
func (kb *ReplyKeyboard) Add(buttons ...KeyboardButton) *ReplyKeyboard {
	for _, btn := range buttons {
		kb.rows = append(kb.rows, []KeyboardButton{btn})
	}
	return kb
}

// Insert appends button to the row currently under construction; the next
// call to Row (with no arguments) or to Build finishes it.
func (kb *ReplyKeyboard) Insert(button KeyboardButton) *ReplyKeyboard {
	kb.currentRow = append(kb.currentRow, button)
	return kb
}

// OneTime sets whether the keyboard hides itself after its next use,
// instead of staying open until the client replaces or removes it.
func (kb *ReplyKeyboard) OneTime(oneTime bool) *ReplyKeyboard {
	kb.oneTimeKeyboard = oneTime
	return kb
}

// Placeholder sets the placeholder text shown in the empty input field
// while this keyboard is open.
func (kb *ReplyKeyboard) Placeholder(text string) *ReplyKeyboard {
	kb.inputFieldPlaceholder = text
	return kb
}

// Selective sets whether the keyboard is shown only to specific users —
// the message it's attached to must target them via reply or mention for
// this to take effect (see [ReplyKeyboardMarkup.Selective]).
func (kb *ReplyKeyboard) Selective(selective bool) *ReplyKeyboard {
	kb.selective = selective
	return kb
}

// Build finishes any row still under construction (see Insert) and returns
// the resulting [ReplyKeyboardMarkup].
func (kb *ReplyKeyboard) Build() *ReplyKeyboardMarkup {
	// Add current row if it has buttons
	if len(kb.currentRow) > 0 {
		kb.rows = append(kb.rows, kb.currentRow)
		kb.currentRow = make([]KeyboardButton, 0)
	}

	return &ReplyKeyboardMarkup{
		Keyboard:              kb.rows,
		ResizeKeyboard:        kb.resizeKeyboard,
		OneTimeKeyboard:       kb.oneTimeKeyboard,
		InputFieldPlaceholder: kb.inputFieldPlaceholder,
		Selective:             kb.selective,
	}
}

// Button constructors — convenience wrappers; you can also build a
// [KeyboardButton] literal directly.

// NewKeyboardButton creates a plain text button: pressing it just sends its
// own text as a regular message.
func NewKeyboardButton(text string) KeyboardButton {
	return KeyboardButton{
		Text: text,
	}
}

// NewKeyboardButtonContact creates a button that requests the user's phone
// contact.
func NewKeyboardButtonContact(text string) KeyboardButton {
	return KeyboardButton{
		Text:           text,
		RequestContact: true,
	}
}

// NewKeyboardButtonLocation creates a button that requests the user's
// current location.
func NewKeyboardButtonLocation(text string) KeyboardButton {
	return KeyboardButton{
		Text:            text,
		RequestLocation: true,
	}
}

// NewKeyboardButtonPoll creates a button that opens the poll-creation UI.
// pollType is "quiz" or "regular"; "" lets the user create a poll of either
// type.
func NewKeyboardButtonPoll(text string, pollType string) KeyboardButton {
	return KeyboardButton{
		Text:        text,
		RequestPoll: &KeyboardButtonPollType{Type: pollType},
	}
}

// NewKeyboardButtonWebApp creates a button that launches the Telegram Web
// App at webAppURL — only valid in private chats.
func NewKeyboardButtonWebApp(text, webAppURL string) KeyboardButton {
	return KeyboardButton{
		Text:   text,
		WebApp: &WebAppInfo{URL: webAppURL},
	}
}

// RemoveKeyboard builds a [ReplyKeyboardRemove], instructing the client to
// hide the current reply keyboard the next time it shows this message —
// selective restricts that to specific users the same way
// [ReplyKeyboard.Selective] does.
func RemoveKeyboard(selective bool) *ReplyKeyboardRemove {
	return &ReplyKeyboardRemove{
		RemoveKeyboard: true,
		Selective:      selective,
	}
}

// Quick builder methods for common patterns.

// QuickReplyKeyboard builds a full reply keyboard from rows of button
// text, one row per inner slice — a shortcut for the common case that
// needs no other options:
//
//	kb := gg.QuickReplyKeyboard([][]string{{"Button 1", "Button 2"}, {"Button 3"}}, true)
func QuickReplyKeyboard(buttons [][]string, resize bool) *ReplyKeyboardMarkup {
	kb := NewReplyKeyboard(resize)
	for _, row := range buttons {
		buttonRow := make([]KeyboardButton, len(row))
		for i, text := range row {
			buttonRow[i] = NewKeyboardButton(text)
		}
		kb.Row(buttonRow...)
	}
	return kb.Build()
}

// QuickReplyRow builds a single-row, auto-resizing reply keyboard from
// button text.
func QuickReplyRow(buttons ...string) *ReplyKeyboardMarkup {
	kb := NewReplyKeyboard(true)
	row := make([]KeyboardButton, len(buttons))
	for i, text := range buttons {
		row[i] = NewKeyboardButton(text)
	}
	kb.Row(row...)
	return kb.Build()
}

// NewReplyKeyboardMarkup builds a [ReplyKeyboardMarkup] directly from a 2D
// button array, for callers who already have their buttons laid out and
// don't need the Row/Add/Insert builder:
//
//	markup := gg.NewReplyKeyboardMarkup([][]gg.KeyboardButton{{btn1, btn2}, {btn3}}, true, false, "Placeholder")
func NewReplyKeyboardMarkup(keyboard [][]KeyboardButton, resizeKeyboard bool, oneTimeKeyboard bool, placeholder string) *ReplyKeyboardMarkup {
	return &ReplyKeyboardMarkup{
		Keyboard:              keyboard,
		ResizeKeyboard:        resizeKeyboard,
		OneTimeKeyboard:       oneTimeKeyboard,
		InputFieldPlaceholder: placeholder,
	}
}

// Adjust re-flows every button added so far into rows of the given sizes,
// exactly like [InlineKeyboard.Adjust]: sizes apply in order, the last one
// repeats, and it replaces whatever row structure Row/Add/Insert created.
func (kb *ReplyKeyboard) Adjust(sizes ...int) *ReplyKeyboard {
	var all []KeyboardButton
	for _, row := range kb.rows {
		all = append(all, row...)
	}
	all = append(all, kb.currentRow...)
	kb.rows = adjustRows(all, sizes)
	kb.currentRow = nil
	return kb
}
