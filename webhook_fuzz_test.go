package golagram

import "testing"

func FuzzSecretTokenMatches(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""},
		{"abc", "abc"},
		{"abc", "abd"},
		{"abc", "ab"},
		{"abc", "abcd"},
		{"", "nonempty"},
		{"nonempty", ""},
		{"\x00\x01\x02", "\x00\x01\x02"},
		{"日本語", "日本語"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, got, want string) {
		matches := secretTokenMatches(got, want)
		wantMatch := got == want
		if matches != wantMatch {
			t.Fatalf("secretTokenMatches(%q, %q) = %v, want %v", got, want, matches, wantMatch)
		}
	})
}
