package agent

import (
	"regexp"
)

// letterAbutsTime matches a letter glued to a clock time: "the2:00 PM",
// "is7:00 AM". Small models routinely emit this in prose; it reads as a
// typo and no prompt fully prevents it, so the output path repairs it
// deterministically before persistence.
var letterAbutsTime = regexp.MustCompile(`([A-Za-z])(\d{1,2}:\d{2}\b)`)

// digitAbutsMeridiem matches "7:00AM" / "3pm": a meridiem glued to its
// digits. Same defect class, same repair.
var digitAbutsMeridiem = regexp.MustCompile(`(?i)(\d)([ap]m\b)`)

// repairTimeSpacing inserts the spaces a model drops around clock times.
// Pure prose repair: URLs and ports never match (the char before the
// digits must be a letter, and a port's digits follow a colon), and
// already-spaced text passes through untouched.
func repairTimeSpacing(content string) string {
	content = letterAbutsTime.ReplaceAllString(content, "$1 $2")
	return digitAbutsMeridiem.ReplaceAllString(content, "$1 $2")
}
