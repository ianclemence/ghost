package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	runningMu      sync.RWMutex
	runningVersion = "dev"
)

// SetRunningVersion records the release this process is, for tools that
// report on or hand off across an update.
func SetRunningVersion(v string) {
	if v == "" {
		return
	}
	runningMu.Lock()
	runningVersion = v
	runningMu.Unlock()
}

// RunningVersion is the release this process is.
func RunningVersion() string {
	runningMu.RLock()
	defer runningMu.RUnlock()
	return runningVersion
}

// SelfCheckRow is one line of Ghost's own health check.
type SelfCheckRow struct {
	Name    string
	Label   string
	Status  string // ok, info, warning, error
	Message string
}

// SelfCheckTool lets Ghost run its own health check (the same one the console
// and /v1/doctor run) and report what it found. It is read-only: it
// looks, it does not repair.
type SelfCheckTool struct {
	run func(context.Context) []SelfCheckRow
}

func NewSelfCheckTool(run func(context.Context) []SelfCheckRow) *SelfCheckTool {
	return &SelfCheckTool{run: run}
}

func (t *SelfCheckTool) Name() string { return "self_check" }

func (t *SelfCheckTool) Description() string {
	return "Run Ghost's own health check and report the result: whether the database, the AI model, tools, browser, skills, storage, routines and connected services are working, and which release is running. " +
		"Read-only. Use for 'check yourself', 'are you healthy', 'is everything working', 'run diagnostics', and after an update to confirm it took. " +
		"Report what it found plainly; do not claim health it did not show."
}

func (t *SelfCheckTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func (t *SelfCheckTool) Timeout() time.Duration { return 60 * time.Second }

func (t *SelfCheckTool) Execute(ctx context.Context, _ map[string]interface{}) *ToolResult {
	if t.run == nil {
		return ErrorResult("The health check isn't available right now.")
	}
	return NewToolResult(FormatSelfCheck(RunningVersion(), t.run(ctx), false))
}

// SelfCheckVerdict is the overall state of a set of rows: "healthy",
// "needs attention" (warnings only) or "problems" (any error).
func SelfCheckVerdict(rows []SelfCheckRow) string {
	verdict := "healthy"
	for _, r := range rows {
		switch r.Status {
		case "error":
			return "problems"
		case "warning":
			verdict = "needs attention"
		}
	}
	return verdict
}

// FormatSelfCheck renders the rows for the model or the owner. With
// onlyProblems, rows that are fine are left out and counted instead.
func FormatSelfCheck(version string, rows []SelfCheckRow, onlyProblems bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ghost %s: %s.\n", version, SelfCheckVerdict(rows))
	fine := 0
	for _, r := range rows {
		bad := r.Status == "error" || r.Status == "warning"
		if onlyProblems && !bad {
			fine++
			continue
		}
		label := r.Label
		if label == "" {
			label = r.Name
		}
		msg := strings.TrimSpace(r.Message)
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg != "" {
			fmt.Fprintf(&b, "- %s [%s]: %s\n", label, r.Status, msg)
		} else {
			fmt.Fprintf(&b, "- %s [%s]\n", label, r.Status)
		}
	}
	if onlyProblems && fine > 0 {
		fmt.Fprintf(&b, "%d other check(s) are fine.\n", fine)
	}
	return strings.TrimRight(b.String(), "\n")
}
