package tools

import (
	"sort"
	"strings"
)

const catalogueDescRunes = 56

// OnDemandCatalogue lists, in a few words each, the tools a turn is not
// always handed. The model reads it in any language and asks for what fits
// with find_tools, so tool choice does not depend on matching the owner's
// words against a keyword list. It is the same on every turn, so it keeps the
// prompt prefix cacheable.
func OnDemandCatalogue(reg *ToolRegistry) string {
	if reg == nil {
		return ""
	}
	var lines []string
	browser, computer, external := 0, 0, 0
	for _, name := range reg.List() {
		switch {
		case coreToolNames[name] || name == "find_tools" || reg.IsHidden(name):
			continue
		case strings.HasPrefix(name, "browser_"):
			browser++
			continue
		case strings.HasPrefix(name, "computer_"):
			computer++
			continue
		case strings.HasPrefix(name, "mcp_"):
			external++
			continue
		}
		tool, ok := reg.Get(name)
		if !ok {
			continue
		}
		lines = append(lines, name+" ("+shortPhrase(tool.Description())+")")
	}
	sort.Strings(lines)
	if browser > 0 {
		lines = append(lines, "browser_* (open and operate real web pages)")
	}
	if computer > 0 {
		lines = append(lines, "computer_* (see and operate the desktop)")
	}
	if external > 0 {
		lines = append(lines, "mcp_* (tools from connected servers)")
	}
	if len(lines) == 0 {
		return ""
	}
	return "Other things Ghost can do, not handed to you every turn. When a request fits one of these, call find_tools with what you need, in a few English words, before you say Ghost cannot: " +
		strings.Join(lines, "; ") + "."
}

func shortPhrase(desc string) string {
	s := firstSentence(strings.Join(strings.Fields(desc), " "), catalogueDescRunes)
	return strings.TrimRight(s, " .,:;")
}
