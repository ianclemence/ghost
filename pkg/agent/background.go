package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/tools"
)

// maxBackgroundNoteChars bounds the completion text persisted into session
// history: enough for the model to continue the work, never a full dump.
const maxBackgroundNoteChars = 2000

// PollBackground returns the session's running detached tasks and drains
// its completions. Each drained completion persists a system note into
// session history (model-visible on the next turn, transcript-invisible
// per history visibility rules) and is returned once for the caller to
// present. Draining twice never double-reports: the registry clears on
// drain. Read-only reporting — no authority is granted or exercised here.
func (al *AgentLoop) PollBackground(sessionKey string) ([]tools.BackgroundTask, []tools.BackgroundDone) {
	if al == nil || al.tools == nil {
		return nil, nil
	}
	running := al.tools.BackgroundRunning(sessionKey)
	done := al.tools.DrainBackgroundDone(sessionKey)
	for _, d := range done {
		al.noteBackgroundDone(sessionKey, d)
	}
	return running, done
}

// noteBackgroundDone records one completion as session history. Failures
// are stated plainly so the next turn recovers instead of assuming.
func (al *AgentLoop) noteBackgroundDone(sessionKey string, d tools.BackgroundDone) {
	if al.sessions == nil {
		return
	}
	result := strings.TrimSpace(d.Result)
	if len(result) > maxBackgroundNoteChars {
		result = result[:maxBackgroundNoteChars] + "…"
	}
	elapsed := d.Elapsed.Round(time.Second)
	var note string
	if d.OK {
		note = fmt.Sprintf("Background task '%s' finished in %s: %s", d.Label, elapsed, result)
	} else {
		if result == "" {
			result = "no result reported"
		}
		note = fmt.Sprintf("Background task '%s' failed after %s: %s", d.Label, elapsed, result)
	}
	al.sessions.AddMessage(sessionKey, "system", note)
	al.sessions.Save(sessionKey)
}
