package golagram

// ReplyMarkup is a sealed interface for all reply markup types, implemented
// by [InlineKeyboardMarkup], [ReplyKeyboardMarkup], [ReplyKeyboardRemove],
// and [ForceReply].
type ReplyMarkup interface {
	isReplyMarkup()
}

// NewForceReply builds a [ForceReply] markup. placeholder ("" for none) is
// shown in the input field while the reply is active; selective limits it to
// the mentioned/replied-to users.
func NewForceReply(placeholder string, selective bool) *ForceReply {
	return &ForceReply{
		ForceReply:            true,
		InputFieldPlaceholder: placeholder,
		Selective:             selective,
	}
}

// Implement the interface for all markup types
func (*InlineKeyboardMarkup) isReplyMarkup() {}
func (*ReplyKeyboardMarkup) isReplyMarkup()  {}
func (*ReplyKeyboardRemove) isReplyMarkup()  {}
func (*ForceReply) isReplyMarkup()           {}
