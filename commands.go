package golagram

import "strings"

// CommandObject is a parsed bot command. For the text
// "/start@my_bot ref_12345" it is {Command: "start", Mention: "my_bot",
// Args: "ref_12345"} — which makes deep-link/referral flows one field access.
type CommandObject struct {
	Command string // "start" from "/start ..."
	Mention string // "my_bot" from "/start@my_bot" ("" if none)
	Args    string // everything after the first space ("" if none)
}

// ParseCommand parses a message text as a bot command. It returns false when
// the text is not a command (doesn't start with "/", or has an empty name).
func ParseCommand(text string) (*CommandObject, bool) {
	if !strings.HasPrefix(text, "/") {
		return nil, false
	}

	head, args, _ := strings.Cut(text[1:], " ")
	name, mention, _ := strings.Cut(head, "@")
	if name == "" {
		return nil, false
	}

	return &CommandObject{
		Command: name,
		Mention: mention,
		Args:    strings.TrimSpace(args),
	}, true
}

// Command returns the parsed command this message carries, or nil if the
// message is not a command. Reads text-or-caption, so it agrees with
// [FilterCommand] on a captioned command. Handlers registered with
// [FilterCommand] use it to read arguments:
//
//	cmd := m.Command()        // for "/start ref_12345"
//	payload := cmd.Args       // "ref_12345"
func (e *Message) Command() *CommandObject {
	cmd, ok := ParseCommand(e.textOrCaption())
	if !ok {
		return nil
	}
	return cmd
}
