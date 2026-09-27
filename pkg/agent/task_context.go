package agent

import (
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/tasks"
)

// renderTaskSummary renders a compact, runtime-authoritative summary of a
// session's in-flight durable tasks for the model. It carries the goal, status,
// progress and current step — never the full task history — so multi-step work
// continues across turns without being reconstructed from conversation. The
// runtime remains authoritative: the summary is stated as runtime state.
func renderTaskSummary(jobs []tasks.Job) string {
	if len(jobs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Active task state (runtime-authoritative; never restate a different status):")
	for _, j := range jobs {
		line := fmt.Sprintf("\n- [%s] %s — status: %s", j.ID, taskGoal(j), j.Status)
		if j.Progress > 0 {
			pct := j.Progress
			if pct <= 1 {
				pct *= 100
			}
			line += fmt.Sprintf(", progress: %.0f%%", pct)
		}
		if step := currentCheckpoint(j); step != "" {
			line += ", at: " + step
		}
		if j.Error != "" {
			line += ", last error: " + j.Error
		}
		b.WriteString(line)
	}
	return b.String()
}

func taskGoal(j tasks.Job) string {
	for _, k := range []string{"goal", "objective", "request", "summary"} {
		if v, ok := j.Payload[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if strings.TrimSpace(j.Kind) != "" {
		return j.Kind
	}
	return "task"
}

func currentCheckpoint(j tasks.Job) string {
	if len(j.Checkpoints) == 0 {
		return ""
	}
	return j.Checkpoints[len(j.Checkpoints)-1]
}

// activeTaskSummaryForSession returns the compact task summary for a session,
// or "" when nothing is in flight.
func (al *AgentLoop) activeTaskSummaryForSession(sessionKey string) string {
	if al == nil || al.jobs == nil {
		return ""
	}
	jobs, err := al.jobs.ListActiveBySession(sessionKey)
	if err != nil || len(jobs) == 0 {
		return ""
	}
	return renderTaskSummary(jobs)
}
