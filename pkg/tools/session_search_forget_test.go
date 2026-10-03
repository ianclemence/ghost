package tools

import (
	"strings"
	"testing"
)

// A forgotten fact is redacted from a search result string by string; the
// rest of the result, and its JSON shape, survive.
func TestRedactForgottenJSON(t *testing.T) {
	match := func(s string) bool { return strings.Contains(s, "Samitivej") }
	in := `{"hits":[{"text":"my dentist is Dr. Lee at Samitivej"},{"text":"flights to Shenzhen"}]}`
	out := redactForgottenJSON(in, match)
	if strings.Contains(out, "Samitivej") || !strings.Contains(out, "flights to Shenzhen") || !strings.Contains(out, forgottenMark) {
		t.Fatalf("redacted = %s", out)
	}
}
