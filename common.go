package golagram

// ErrorHandlerFunc is called with a handler's error and the [Ctx] it failed on.
type ErrorHandlerFunc func(error, *Ctx)

// FullName returns the user's first and last name joined, falling back to
// just the first name when there is no last name.
func (u *User) FullName() string {
	if u.LastName == "" {
		return u.FirstName
	}
	return u.FirstName + " " + u.LastName
}

// Entity is golagram's historical name for [MessageEntity] — a special
// entity in a message's text (mention, hashtag, command, URL, formatting,
// ...).
// The struct itself is generated (types.gen.go) with the full spec field
// set: Type, Offset, Length, URL, User, Language, CustomEmojiID, ... —
// the old hand-written 3-field version silently dropped text_link URLs and
// text_mention users.
type Entity = MessageEntity
