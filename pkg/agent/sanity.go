package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ianclemence/ghost/pkg/tools"
)

// Quality problems on a long-lived appliance rarely announce themselves: a
// provider change, a bad quantization or a routing slip produces replies that
// are fluent but subtly wrong, and every health check still passes. These
// checks look at what actually came back. They only observe: they log and
// record a trace event, never alter or withhold a reply, because a false alarm
// that rewrote an answer would be worse than the thing it watches for.

// outputAnomalies names what looks wrong with a reply, given what was asked.
// Empty means nothing stood out.
func outputAnomalies(userMsg, reply string) []string {
	var out []string
	if strings.ContainsRune(reply, utf8.RuneError) {
		out = append(out, "replacement_character")
	}
	if foreignScriptIntrusion(userMsg, reply) {
		out = append(out, "foreign_script")
	}
	if loops(reply) {
		out = append(out, "repetition")
	}
	return out
}

// scriptOf buckets a letter into the writing systems an owner might use.
func scriptOf(r rune) string {
	switch {
	case unicode.Is(unicode.Latin, r):
		return "latin"
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return "cjk"
	case unicode.Is(unicode.Cyrillic, r):
		return "cyrillic"
	case unicode.Is(unicode.Arabic, r), unicode.Is(unicode.Hebrew, r):
		return "rtl"
	case unicode.Is(unicode.Thai, r):
		return "thai"
	case unicode.Is(unicode.Devanagari, r), unicode.Is(unicode.Bengali, r), unicode.Is(unicode.Tamil, r):
		return "indic"
	case unicode.Is(unicode.Greek, r):
		return "greek"
	}
	return ""
}

// foreignScriptIntrusion is the telltale of a sampling fault: a few characters
// of a writing system nobody used, inside an answer written in another. It is
// silent when the owner wrote in that system, quoted it, or the reply is mostly
// in it (a translation).
func foreignScriptIntrusion(userMsg, reply string) bool {
	asked := map[string]bool{}
	for _, r := range userMsg {
		if s := scriptOf(r); s != "" {
			asked[s] = true
		}
	}
	counts := map[string]int{}
	letters := 0
	for _, r := range reply {
		if s := scriptOf(r); s != "" {
			counts[s]++
			letters++
		}
	}
	if letters < 40 {
		return false
	}
	for s, n := range counts {
		if asked[s] {
			continue
		}
		// A handful of strays among plenty of text; a majority is a translation.
		if n >= 1 && n <= 6 && n*10 < letters {
			return true
		}
	}
	return false
}

// loops reports a reply stuck repeating itself: one non-trivial line, or one
// stretch of text, over and over.
func loops(reply string) bool {
	if len(reply) < 200 {
		return false
	}
	seen := map[string]int{}
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 12 {
			if seen[line]++; seen[line] >= 5 {
				return true
			}
		}
	}
	// Same 40-byte block recurring many times in a run with no newlines.
	if len(reply) >= 800 && !strings.Contains(reply, "\n") {
		block := reply[len(reply)/2 : len(reply)/2+40]
		if strings.Count(reply, block) >= 6 {
			return true
		}
	}
	return false
}

// hashOf is a short fingerprint for telling two turns' setups apart.
func hashOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// buildVersion is the running release, set once at startup so every turn can
// say which Ghost produced it.
var buildVersion = "dev"

// SetBuildVersion records the running release for turn traces.
func SetBuildVersion(v string) {
	if v != "" {
		buildVersion = v
		tools.SetRunningVersion(v)
	}
}

// turnVersions summarises everything that can change a turn's behaviour:
// release, model, the instructions it was given, and the tools it was offered.
// When quality shifts, comparing these across the turns either side of it
// points at the change.
func turnVersions(model, systemPrompt string, toolNames []string) string {
	names := append([]string(nil), toolNames...)
	sort.Strings(names)
	return fmt.Sprintf("version=%s model=%s prompt=%s tools=%d:%s",
		buildVersion, model, hashOf(systemPrompt), len(names), hashOf(names...))
}
