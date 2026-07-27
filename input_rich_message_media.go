package golagram

// InputRichMessageMediaSource is a sealed interface for the five media
// types InputRichMessageMedia.Media accepts, implemented by
// [InputMediaAnimation], [InputMediaAudio], [InputMediaPhoto],
// [InputMediaVideo], and [InputMediaVoiceNote] — Bot API 10.2's inline "or"
// field type isn't a formal spec union, so (like [ReplyMarkup]) it's
// hand-written rather than generated. Telegram only reads the media/caption
// fields off whichever concrete value is set; everything else on it is
// ignored.
type InputRichMessageMediaSource interface {
	isInputRichMessageMediaSource()
}

func (*InputMediaAnimation) isInputRichMessageMediaSource() {}
func (*InputMediaAudio) isInputRichMessageMediaSource()     {}
func (*InputMediaPhoto) isInputRichMessageMediaSource()     {}
func (*InputMediaVideo) isInputRichMessageMediaSource()     {}
func (*InputMediaVoiceNote) isInputRichMessageMediaSource() {}
