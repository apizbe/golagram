package golagram

// SendMessageOptions carries the optional parameters of sendMessage for the
// sugar send paths ([Message.Answer] / [Message.Reply], [Ctx.Answer] /
// [Ctx.Reply], ...). Zero values mean "use Telegram's default", so a
// struct literal reads naturally:
//
//	message.Answer("hi", &SendMessageOptions{ParseMode: "HTML"})
//
// The full parameter set (including required fields) is the generated
// [SendMessageRequest], used with [TelegramBot.SendMessage] directly.
type SendMessageOptions struct {
	ParseMode           string
	Entities            []Entity
	LinkPreviewOptions  *LinkPreviewOptions
	DisableNotification bool
	ProtectContent      bool
	AllowPaidBroadcast  bool
	MessageEffectID     string
	// MessageThreadID targets a forum topic. Normally left unset —
	// [Message.Answer] / [Message.Reply] auto-propagate the source
	// message's own topic, so this only needs setting explicitly to send
	// into a different topic.
	MessageThreadID         int64
	DirectMessagesTopicID   int64
	SuggestedPostParameters *SuggestedPostParameters
	// ReplyParameters sets the reply target explicitly (quoting, replying
	// across chats, allow_sending_without_reply). [Message.Reply] fills in
	// a plain same-chat reply automatically when this is nil.
	ReplyParameters *ReplyParameters
	ReplyMarkup     ReplyMarkup
	// BusinessConnectionID is normally left unset — [Message.Answer] /
	// [Message.Reply] auto-propagate the source message's own
	// BusinessConnectionID, so this only needs setting explicitly to
	// override that.
	BusinessConnectionID string
}

// SMO is [SendMessageOptions]' short alias — see SendMessageOptions' doc.
type SMO = SendMessageOptions

// applyTo copies the options into a generated SendMessageRequest — the
// bridge between the sugar option bag and the spec-complete request struct.
func (o *SendMessageOptions) applyTo(req *SendMessageRequest) {
	if o == nil {
		return
	}
	req.ParseMode = o.ParseMode
	req.Entities = o.Entities
	req.LinkPreviewOptions = o.LinkPreviewOptions
	req.DisableNotification = o.DisableNotification
	req.ProtectContent = o.ProtectContent
	req.AllowPaidBroadcast = o.AllowPaidBroadcast
	req.MessageEffectID = o.MessageEffectID
	req.MessageThreadID = o.MessageThreadID
	req.DirectMessagesTopicID = o.DirectMessagesTopicID
	req.SuggestedPostParameters = o.SuggestedPostParameters
	req.ReplyParameters = o.ReplyParameters
	req.ReplyMarkup = o.ReplyMarkup
	req.BusinessConnectionID = o.BusinessConnectionID
}

// EditMessageOptions carries the optional parameters of editMessageText for
// [Message.EditText] / [Ctx.EditText].
type EditMessageOptions struct {
	ParseMode          string
	Entities           []Entity
	LinkPreviewOptions *LinkPreviewOptions
	ReplyMarkup        *InlineKeyboardMarkup
	// BusinessConnectionID is normally left unset — [Message.EditText]
	// auto-propagates the source message's own BusinessConnectionID, so
	// this only needs setting explicitly to override that.
	BusinessConnectionID string
}

// EMO is [EditMessageOptions]' short alias — see EditMessageOptions' doc.
type EMO = EditMessageOptions

// AnswerCallbackOptions carries the optional parameters of answerCallbackQuery.
type AnswerCallbackOptions struct {
	ShowAlert bool
	URL       string
	CacheTime int64
}

// ACO is [AnswerCallbackOptions]' short alias — see AnswerCallbackOptions' doc.
type ACO = AnswerCallbackOptions

// EditCaptionOptions carries the optional parameters of editMessageCaption.
type EditCaptionOptions struct {
	ParseMode             string
	CaptionEntities       []Entity
	ShowCaptionAboveMedia bool
	ReplyMarkup           *InlineKeyboardMarkup
	// BusinessConnectionID is normally left unset — [Message.EditCaption]
	// auto-propagates the source message's own BusinessConnectionID, so
	// this only needs setting explicitly to override that.
	BusinessConnectionID string
}

// ECO is [EditCaptionOptions]' short alias — see EditCaptionOptions' doc.
type ECO = EditCaptionOptions

// AnswerInlineOptions carries the optional parameters of answerInlineQuery.
type AnswerInlineOptions struct {
	CacheTime  int64
	IsPersonal bool
	NextOffset string
	Button     *InlineQueryResultsButton
}

// AIO is [AnswerInlineOptions]' short alias — see AnswerInlineOptions' doc.
type AIO = AnswerInlineOptions
