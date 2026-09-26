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
//
// The space between date and time is optional: a model that drops spaces
// before numbers (a known DeepSeek artifact) echoes the label as
// "[2026-09-2610:04] …", and that shape must strip too — otherwise the
// invisible label becomes a visible date prefix on a live transcript.
var HistoryStampRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} ?\d{2}:\d{2}\] ?`)

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

// stampShapes are the label templates the streaming gate holds for: the
// canonical label and the same label after a model drops the space
// between date and time ("[2026-09-2610:04]"). A candidate matching
// either shape prefix may still grow into the label, so it is held.
var stampShapes = []string{"[0000-00-00 00:00] ", "[0000-00-0000:00] "}

// CouldStartHistoryStamp reports whether s can still grow into a leading
// history date label. It is the streaming counterpart of StripDateStamp:
// a chunk that "could" be the start of a label is held back until later
// chunks prove it is (or is not) the label, so a label split across chunk
// boundaries never reaches a live transcript. Any divergence flushes
// immediately — a reply not starting with "[" never waits.
func CouldStartHistoryStamp(s string) bool {
	for _, shape := range stampShapes {
		if len(s) > len(shape) {
			continue
		}
		ok := true
		for i := 0; i < len(s); i++ {
			want := shape[i]
			c := s[i]
			switch want {
			case '0':
				if c < '0' || c > '9' {
					ok = false
				}
			default:
				if c != want {
					ok = false
				}
			}
			if !ok {
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
