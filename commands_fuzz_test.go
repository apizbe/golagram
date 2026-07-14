package golagram

import (
	"strings"
	"testing"
)

func FuzzParseCommand(f *testing.F) {
	for _, seed := range []string{
		"/start",
		"/start@my_bot",
		"/start@my_bot ref_12345",
		"/start   leading and trailing spaces  ",
		"",
		"/",
		"not a command",
		"/@",
		"/a@b@c d",
		"/日本語 引数",
		"/start\t\narg",
		"//double",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		cmd, ok := ParseCommand(text)

		if !ok {
			if cmd != nil {
				t.Fatalf("ParseCommand(%q) = (%v, false), want (nil, false)", text, cmd)
			}
			return
		}

		if cmd.Command == "" {
			t.Fatalf("ParseCommand(%q) returned ok=true with an empty Command", text)
		}
		// Command comes from text[1:] cut at the first literal space, then
		// cut again at the first "@" — by construction it can contain
		// neither (other whitespace like tab/newline is not a cut point).
		if strings.ContainsAny(cmd.Command, "@ ") {
			t.Fatalf("ParseCommand(%q) Command = %q, contains a separator it should have been cut on", text, cmd.Command)
		}
		if cmd.Args != strings.TrimSpace(cmd.Args) {
			t.Fatalf("ParseCommand(%q) Args = %q, not trimmed", text, cmd.Args)
		}
	})
}
