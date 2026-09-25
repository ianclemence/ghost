package utils

import (
	"regexp"
)

// HistoryStampRe matches the internal history date label the context
// builder prefixes to stored messages ("[2006-01-02 15:04] …"). The label
// is invisible bookkeeping: it exists so the model can date historical
// "today"/"tomorrow" against Current Time. A model that sees the label in
// its own history sometimes imitates it in replies, so every output
// boundary strips it. Exported because the streaming path needs the same
// shape test (pkg/agent) and the history endpoints need the same strip
// (cmd/ghost).
var HistoryStampRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}\] ?`)

// StripDateStamp removes a leading internal history date label from text,
// repeatedly (a model can echo one twice). Only the anchored prefix is
// touched: a stamp quoted mid-sentence is the author's content and stays.
func StripDateStamp(s string) string {
	for {
		loc := HistoryStampRe.FindStringIndex(s)
		if loc == nil || loc[1] == 0 {
			return s
		}
		s = s[loc[1]:]
	}
}

// CouldStartHistoryStamp reports whether s can still grow into a leading
// history date label. It is the streaming counterpart of StripDateStamp:
// a chunk that "could" be the start of a label is held back until later
// chunks prove it is (or is not) the label, so a label split across chunk
// boundaries never reaches a live transcript. Any divergence flushes
// immediately — a reply not starting with "[" never waits.
func CouldStartHistoryStamp(s string) bool {
	if len(s) > len("[0000-00-00 00:00] ") {
		return false
	}
	for i := 0; i < len(s); i++ {
		want := "[0000-00-00 00:00] "[i]
		c := s[i]
		switch want {
		case '0':
			if c < '0' || c > '9' {
				return false
			}
		default:
			if c != want {
				return false
			}
		}
	}
	return true
}
