package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/effort"
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
			return fmt.Sprintf("Internal note, do not mention it: you already made this exact %s call and its result is in the conversation above. Use it and take the next step.", name)
		}
	}
	if canvasOffered && !builtCanvas && skillReads >= 3 && otherCalls == 0 {
		return "Internal note, do not mention it: you have read enough of the design guidance. Call canvas now with the complete page, then fix whatever it reports."
	}
	return ""
}

var buildClaimRE = regexp.MustCompile(`(?i)(here'?s (your|the|a|my)\b|\bi('ve| have)? (built|made|created|put together|drafted)\b|\b(is|it'?s|are) (built|ready|live|done)\b|\bhas been (built|created)\b|เรียบร้อย|ให้แล้ว|สร้าง.{0,30}แล้ว|ทำ.{0,30}แล้ว|\b(he construido|te hice|aquí tienes)\b)`)

// claimsBuildWithoutBuilding is the shape of a turn that was asked for a page
// and ended by describing one: the canvas was available, never called, and the
// reply talks as though there were something to look at.
func claimsBuildWithoutBuilding(userMsg, reply string, canvasOffered bool, used []string) bool {
	if !canvasOffered || !effort.Analyze(userMsg).Build {
		return false
	}
	for _, u := range used {
		if u == "canvas" || u == "publish_artifact" {
			return false
		}
	}
	return buildClaimRE.MatchString(reply)
}

const unbuiltNote = "Internal note, do not mention it: your reply says the page is built, but you never called canvas, so the owner has nothing to look at. Call canvas now with the complete page, then say what you made."
