package agent

import (
	"strings"
	"testing"
)

func TestRepairTimeSpacing(t *testing.T) {
	cases := []struct{ in, want string }{
		// The reported defect, verbatim shapes.
		{"your last item is the2:00 PM Ghost logs check.", "your last item is the 2:00 PM Ghost logs check."},
		{"first thing is7:00 AM tomorrow.", "first thing is 7:00 AM tomorrow."},
		{"meet at 7:00AM sharp.", "meet at 7:00 AM sharp."},
		{"back by 3pm then.", "back by 3 pm then."},
		// Already-correct text is untouched.
		{"the 2:00 PM check.", "the 2:00 PM check."},
		{"at 12:00 works.", "at 12:00 works."},
		{"Nothing scheduled.", "Nothing scheduled."},
		// Non-times are untouched: ports follow a colon, never a letter.
		{"see http://host:8080/x.", "see http://host:8080/x."},
	}
	for _, c := range cases {
		if got := repairTimeSpacing(c.in); got != c.want {
			t.Errorf("repairTimeSpacing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if strings.Contains(repairTimeSpacing("a b c"), "  ") {
		t.Errorf("repair must never introduce double spaces")
	}
}
