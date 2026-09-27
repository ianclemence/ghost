package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tasks"
)

// The task summary carries goal, authoritative status, progress and current
// step, and nothing else.
func TestRenderTaskSummary(t *testing.T) {
	jobs := []tasks.Job{
		{
			ID: "j1", Kind: "travel", Status: tasks.StatusRunning, Progress: 0.6,
			Checkpoints: []string{"booking flight"},
			Payload:     map[string]interface{}{"goal": "Book the Nairobi trip"},
		},
		{
			ID: "j2", Kind: "task", Status: tasks.StatusWaitingPermission,
			Payload: map[string]interface{}{"goal": "Send the invites"},
		},
	}
	got := renderTaskSummary(jobs)
	for _, want := range []string{
		"runtime-authoritative",
		"Book the Nairobi trip", "status: running", "60%", "at: booking flight",
		"Send the invites", string(tasks.StatusWaitingPermission),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n%s", want, got)
		}
	}
	if renderTaskSummary(nil) != "" {
		t.Fatal("no in-flight tasks must render empty")
	}
}
