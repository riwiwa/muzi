package scrobble

import "testing"

func TestStripBearer(t *testing.T) {
	for in, want := range map[string]string{
		"Token abc":  "abc",
		"Bearer abc": "abc",
		"abc":        "abc",
		"":           "",
	} {
		if got := stripBearer(in); got != want {
			t.Errorf("stripBearer(%q) = %q, want %q", in, got, want)
		}
	}
}
