package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/skills"
)

// CalendarTool is the semantic calendar surface: the model asks for a
// calendar operation, not for gcalcli or an API endpoint. The Google
// Calendar integration (gcalcli) is an internal implementation detail;
// provider/CLI specifics never reach the model. Consequential actions
// (create/delete) carry runtime evidence.
type CalendarTool struct {
	workspace string
	// run executes gcalcli with args and returns stdout. Overridable in tests.
	run func(ctx context.Context, args []string) (string, error)
}

func NewCalendarTool(workspace string) *CalendarTool {
	t := &CalendarTool{workspace: workspace}
	t.run = t.runGcalcli
	return t
}

func (t *CalendarTool) Name() string { return "calendar" }

func (t *CalendarTool) Description() string {
	return "Read or change the user's calendar. Use this for \"what's on my calendar\", \"add an event\", \"move/delete my appointment\". Handles Google Calendar internally; never ask the user for provider or CLI details."
}

func (t *CalendarTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"list", "create", "delete"}},
			"title":  map[string]interface{}{"type": "string", "description": "Event title (create)"},
			"when":   map[string]interface{}{"type": "string", "description": "Natural time, e.g. 'Friday 15:00' (create)"},
			"query":  map[string]interface{}{"type": "string", "description": "Event title to match (delete)"},
		},
		"required": []string{"action"},
	}
}

func (t *CalendarTool) Timeout() time.Duration { return 45 * time.Second }

func (t *CalendarTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action := strings.ToLower(sarg(args, "action"))
	switch action {
	case "list", "agenda", "read", "":
		out, err := t.run(ctx, append(skills.CalendarConfigArgs(), "agenda"))
		if err != nil {
			return calendarUnavailable(err)
		}
		if strings.TrimSpace(out) == "" {
			return NewToolResult("Your calendar is clear for the upcoming days.")
		}
		return NewToolResult(out)

	case "create", "add":
		title := strings.TrimSpace(sarg(args, "title"))
		when := strings.TrimSpace(sarg(args, "when"))
		if title == "" {
			return ErrorResult("calendar create needs a title.")
		}
		quick := strings.TrimSpace(title + " " + when)
		out, err := t.run(ctx, append(skills.CalendarConfigArgs(), "quick", quick))
		if err != nil {
			return calendarUnavailable(err)
		}
		res := NewToolResult(fmt.Sprintf("Added to your calendar: %s. %s", quick, strings.TrimSpace(out)))
		res.Evidence = map[string]interface{}{
			"type":      "acknowledgement",
			"operation": "create",
			"summary":   quick,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		return res

	case "delete", "remove":
		query := strings.TrimSpace(sarg(args, "query"))
		if query == "" {
			query = strings.TrimSpace(sarg(args, "title"))
		}
		if query == "" {
			return ErrorResult("calendar delete needs the event title to match.")
		}
		out, err := t.run(ctx, append(skills.CalendarConfigArgs(), "delete", query))
		if err != nil {
			return calendarUnavailable(err)
		}
		res := NewToolResult(fmt.Sprintf("Removed from your calendar: %s. %s", query, strings.TrimSpace(out)))
		res.Evidence = map[string]interface{}{
			"type":      "acknowledgement",
			"operation": "delete",
			"summary":   query,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		return res

	default:
		return ErrorResult("calendar needs action list, create, or delete.")
	}
}

// runGcalcli executes the calendar integration. The command is fixed by the
// tool; model input only supplies title/time/query arguments, never shell.
func (t *CalendarTool) runGcalcli(ctx context.Context, args []string) (string, error) {
	if _, err := exec.LookPath("gcalcli"); err != nil {
		return "", fmt.Errorf("calendar integration unavailable")
	}
	argv := append([]string{"gcalcli"}, args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	HardenCommand(cmd, t.workspace, nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return string(out), nil
}

// calendarUnavailable turns an integration failure into an honest,
// product-language outcome: the model must not claim success.
func calendarUnavailable(err error) *ToolResult {
	return ErrorResult("Calendar isn't connected or is temporarily unavailable. Connect Google Calendar in Ghost settings under Integrations, then try again.")
}
