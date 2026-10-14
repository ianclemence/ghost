package agent

import (
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/providers"
)

var rereadable = map[string]bool{
	"read_file": true, "list_dir": true, "search_files": true, "web_fetch": true, "session_search": true,
}

// readLoopNote spots a turn that is going round in circles on lookups: the
// same read made twice, or several skill files read one after another with
// nothing built. It returns a note for the model, or "".
func readLoopNote(messages []providers.Message, canvasOffered bool) string {
	start := turnStart(messages)
	seen := map[string]int{}
	skillReads, otherCalls, builtCanvas := 0, 0, false
	for _, m := range messages[start:] {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function == nil {
				continue
			}
			name, args := tc.Function.Name, tc.Function.Arguments
			if name == "canvas" {
				builtCanvas = true
			}
			if rereadable[name] {
				seen[name+"\x00"+args]++
				if name == "read_file" && strings.Contains(args, "skills/") {
					skillReads++
					continue
				}
			}
			otherCalls++
		}
	}
	for key, n := range seen {
		if n >= 2 {
			name := strings.SplitN(key, "\x00", 2)[0]
			return fmt.Sprintf("You have already made this exact %s call and its result is in the conversation above. Use it and take the next step.", name)
		}
	}
	if canvasOffered && !builtCanvas && skillReads >= 3 && otherCalls == 0 {
		return "You have read enough of the design guidance. Build the page now: call canvas with the complete page, then fix whatever it reports."
	}
	return ""
}
