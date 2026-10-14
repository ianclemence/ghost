package utils

import (
	"strings"
	"unicode"
)

var statementFiller = map[string]bool{
	"the": true, "and": true, "their": true, "your": true, "his": true, "her": true, "has": true, "have": true,
	"with": true, "that": true, "this": true, "from": true, "for": true, "are": true, "was": true, "were": true,
	"user": true, "owner": true, "likes": true, "like": true,
}

func contentWords(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 3 && !statementFiller[w] {
			out[w] = struct{}{}
		}
	}
	return out
}

// SameStatement reports whether text says the same thing as value in other
// words: at least three of the value's content words, and four in five of
// them, appear in text. "Favourite coffee is a flat white with oat milk"
// matches "Their favourite coffee is a flat white with oat milk"; a line
// that merely shares a word or two does not.
func SameStatement(value, text string) bool {
	want := contentWords(value)
	if len(want) < 3 {
		return false
	}
	have := contentWords(text)
	found := 0
	for w := range want {
		if _, ok := have[w]; ok {
			found++
		}
	}
	return found*5 >= len(want)*4
}
