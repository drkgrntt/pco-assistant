package controllers

import "testing"

// TestUppercaseFirst covers UpdateSetSongKey's server-side normalization
// (issue: uppercase the first letter of a typed key before saving) - just
// the first rune changes, everything else (including a trailing "m" for
// minor - see keyForm's own doc comment) passes through untouched, and
// nothing gets rejected.
func TestUppercaseFirst(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase note", "bb", "Bb"},
		{"lowercase minor", "f#m", "F#m"},
		{"already uppercase", "G", "G"},
		{"empty", "", ""},
		{"single lowercase rune", "g", "G"},
		{"leading symbol untouched", "#f", "#f"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := uppercaseFirst(c.in); got != c.want {
				t.Errorf("uppercaseFirst(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
