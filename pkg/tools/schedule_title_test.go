package tools

import "testing"

func TestDerivedItemTitle(t *testing.T) {
	cases := map[string]string{
		"Prepare my weekly design review brief.": "Prepare my weekly design review brief.",
		"prepare my weekly design review brief":  "Prepare my weekly design review brief",
		"send the team a status update":          "Send the team a status update",
		"":                                       "",
		"ab":                                     "",
		"do the thing, every monday at 9am":      "Do the thing",
	}
	for in, want := range cases {
		if got := derivedItemTitle(in); got != want {
			t.Errorf("derivedItemTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
