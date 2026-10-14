package tools

import (
	"regexp"
	"strconv"
	"strings"
)

type designRule struct {
	re   *regexp.Regexp
	said string
}

var designRules = []designRule{
	{regexp.MustCompile(`transition(?:-property)?\s*:\s*all\b`), "it transitions `all` properties; name them (transform, opacity, background-color)"},
	{regexp.MustCompile(`(?:transition|animation)[^;{}]*ease-in(?:[^-\w]|$)`), "it eases in; use var(--ease-out) for anything the owner triggers, ease-in feels sluggish"},
	{regexp.MustCompile(`scale\(\s*0\s*\)`), "something grows from scale(0); start from scale(0.95) with opacity 0"},
	{regexp.MustCompile(`(?:transition|animation)[^;{}]*\b(?:width|height|top|left|right|bottom|margin|padding)\b`), "it animates layout properties; animate transform and opacity instead"},
	{regexp.MustCompile(`(?i)lorem ipsum|your text here|\bfeature [123]\b|john doe|click here`), "it contains placeholder copy; use the owner's real content, or say plainly that it is a draft"},
	{regexp.MustCompile(`(?i)#667eea|#764ba2|linear-gradient\([^)]*#(?:6366f1|8b5cf6|a855f7)`), "it uses the default purple-blue gradient; take the colour from the subject"},
	{regexp.MustCompile(`\b100vh\b`), "it uses 100vh, which overflows behind a phone's browser bars; use 100dvh or the content's own height"},
}

var (
	hoverRule       = regexp.MustCompile(`:hover`)
	hoverQuery      = regexp.MustCompile(`hover\s*:\s*hover`)
	movement        = regexp.MustCompile(`@keyframes|animation\s*:`)
	reducedMotion   = regexp.MustCompile(`prefers-reduced-motion`)
	imgTag          = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	altAttr         = regexp.MustCompile(`(?i)\balt\s*=`)
	motionDecl      = regexp.MustCompile(`(?:transition|animation)\s*:[^;{}]*`)
	durationToken   = regexp.MustCompile(`(\d*\.?\d+)(ms|s)\b`)
	longestOKMillis = 400.0
)

func slowMotion(html string) bool {
	for _, decl := range motionDecl.FindAllString(html, -1) {
		if strings.Contains(decl, "infinite") {
			continue
		}
		for _, m := range durationToken.FindAllStringSubmatch(decl, -1) {
			v, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				continue
			}
			if m[2] == "s" {
				v *= 1000
			}
			if v > longestOKMillis {
				return true
			}
		}
	}
	return false
}

func missingAlt(html string) bool {
	for _, tag := range imgTag.FindAllString(html, -1) {
		if !altAttr.MatchString(tag) {
			return true
		}
	}
	return false
}

const designNoteLimit = 5

// canvasDesignNotes names the craft problems a quick read of the document can
// see for certain. They are advice, not errors: the page works either way.
func canvasDesignNotes(html string) []string {
	var out []string
	for _, r := range designRules {
		if r.re.MatchString(html) {
			out = append(out, r.said)
		}
	}
	if hoverRule.MatchString(html) && !hoverQuery.MatchString(html) {
		out = append(out, "it styles :hover without @media (hover: hover) and (pointer: fine), so a tap leaves it stuck on a phone")
	}
	if movement.MatchString(html) && !reducedMotion.MatchString(html) {
		out = append(out, "it moves things but has no prefers-reduced-motion rule; keep opacity and colour, drop the movement")
	}
	if slowMotion(html) {
		out = append(out, "a transition or animation runs longer than 400ms; UI motion belongs at 150–250ms")
	}
	if missingAlt(html) {
		out = append(out, "an image has no alt text")
	}
	if len(out) > designNoteLimit {
		out = out[:designNoteLimit]
	}
	return out
}
