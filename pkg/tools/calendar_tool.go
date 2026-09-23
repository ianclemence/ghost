package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/provider"
	calprov "github.com/ianclemence/ghost/pkg/providers/calendar"
	"github.com/ianclemence/ghost/pkg/skills"
)

// CalendarTool is the semantic calendar surface: the model asks for a
// calendar operation, not for an API endpoint. Google Calendar executes
// through the direct API provider; the gcalcli subprocess remains for one
// release as an opt-out fallback (GHOST_CALENDAR_GCALCLI=0 disables it)
// and is removed afterwards. Consequential actions (create/delete) carry
// runtime evidence.
type CalendarTool struct {
	workspace string
	// run executes gcalcli with args and returns stdout. Overridable in tests.
	// Legacy fallback only; the direct API path does not use it.
	run func(ctx context.Context, args []string) (string, error)
	// newSvc builds the direct API service. Overridable in tests.
	newSvc func() *calprov.Service
}

func NewCalendarTool(workspace string) *CalendarTool {
	t := &CalendarTool{workspace: workspace}
	t.run = t.runGcalcli
	return t
}

func (t *CalendarTool) svc() *calprov.Service {
	if t.newSvc != nil {
		return t.newSvc()
	}
	return calprov.New(calprov.Config{})
}

// legacyFallback reports whether the one-release gcalcli fallback may run.
// Default on for continuity; GHOST_CALENDAR_GCALCLI=0 disables it. Removed
// next release along with the subprocess path.
func legacyFallback() bool {
	return strings.TrimSpace(os.Getenv("GHOST_CALENDAR_GCALCLI")) != "0"
}

func (t *CalendarTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action := strings.ToLower(sarg(args, "action"))
	var res *ToolResult
	switch action {
	case "list", "agenda", "read", "":
		res = t.agenda(ctx)
	case "create", "add":
		title := strings.TrimSpace(sarg(args, "title"))
		when := strings.TrimSpace(sarg(args, "when"))
		if title == "" {
			return ErrorResult("calendar create needs a title.")
		}
		res = t.create(ctx, strings.TrimSpace(title+" "+when))
	case "delete", "remove":
		query := strings.TrimSpace(sarg(args, "query"))
		if query == "" {
			query = strings.TrimSpace(sarg(args, "title"))
		}
		if query == "" {
			return ErrorResult("calendar delete needs the event title to match.")
		}
		res = t.delete(ctx, query)
	default:
		return ErrorResult("calendar needs action list, create, or delete.")
	}
	if res.IsError && res.TimedOut {
		return res
	}
	if res.IsError && legacyFallback() {
		if legacy, ok := t.legacyExecute(ctx, args); ok {
			logger.WarnCF("calendar", "direct API unavailable, legacy gcalcli fallback used",
				map[string]interface{}{"action": action})
			return legacy
		}
	}
	return res
}

// agenda lists upcoming events through the direct API.
func (t *CalendarTool) agenda(ctx context.Context) *ToolResult {
	events, r := t.svc().Agenda(ctx, 10)
	if r.Err != nil {
		if r.Failure == provider.FailNotConfigured {
			return calendarUnavailable(fmt.Errorf("calendar not connected"))
		}
		return providerError(product.OutcomeForProviderFailure("calendar", r.Failure, r.Err).UserMessage)
	}
	if len(events) == 0 {
		return NewToolResult("Your calendar is clear for the upcoming days.")
	}
	var b strings.Builder
	for _, e := range events {
		when := e.Start
		if e.End != "" && e.End != e.Start {
			when += "–" + e.End
		}
		fmt.Fprintf(&b, "- %s (%s)\n", e.Summary, when)
	}
	return NewToolResult(strings.TrimRight(b.String(), "\n"))
}

// create adds an event from natural language through events.quickAdd.
func (t *CalendarTool) create(ctx context.Context, text string) *ToolResult {
	ev, r := t.svc().QuickAdd(ctx, text)
	if r.Err != nil {
		if r.Failure == provider.FailNotConfigured {
			return calendarUnavailable(fmt.Errorf("calendar not connected"))
		}
		return providerError(product.OutcomeForProviderFailure("calendar", r.Failure, r.Err).UserMessage)
	}
	res := NewToolResult(fmt.Sprintf("Added to your calendar: %s.", ev.Summary))
	res.Evidence = map[string]interface{}{
		"type":       "acknowledgement",
		"operation":  "create",
		"summary":    ev.Summary,
		"event_id":   ev.ID,
		"event_link": ev.Link,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	return res
}

// delete removes the first event matching the query.
func (t *CalendarTool) delete(ctx context.Context, query string) *ToolResult {
	ev, r := t.svc().DeleteByQuery(ctx, query)
	if r.Err != nil {
		if r.Failure == provider.FailNotConfigured {
			return calendarUnavailable(fmt.Errorf("calendar not connected"))
		}
		if r.Failure == provider.FailInvalid {
			return ErrorResult(fmt.Sprintf("No calendar event matches %q.", query))
		}
		return providerError(product.OutcomeForProviderFailure("calendar", r.Failure, r.Err).UserMessage)
	}
	res := NewToolResult(fmt.Sprintf("Removed from your calendar: %s.", ev.Summary))
	res.Evidence = map[string]interface{}{
		"type":       "acknowledgement",
		"operation":  "delete",
		"summary":    ev.Summary,
		"event_id":   ev.ID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	return res
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

// legacyExecute runs one action through the gcalcli subprocess. One-release
// fallback only: it runs when the direct API path errors and the operator
// has not disabled it. Returns ok=false when gcalcli is absent or fails,
// so the direct API's honest error stands.
func (t *CalendarTool) legacyExecute(ctx context.Context, args map[string]interface{}) (*ToolResult, bool) {
	action := strings.ToLower(sarg(args, "action"))
	var out string
	var err error
	switch action {
	case "list", "agenda", "read", "":
		out, err = t.run(ctx, append(skills.CalendarConfigArgs(), "agenda"))
		if err != nil {
			return nil, false
		}
		if strings.TrimSpace(out) == "" {
			return NewToolResult("Your calendar is clear for the upcoming days."), true
		}
		return NewToolResult(out), true

	case "create", "add":
		title := strings.TrimSpace(sarg(args, "title"))
		when := strings.TrimSpace(sarg(args, "when"))
		quick := strings.TrimSpace(title + " " + when)
		out, err = t.run(ctx, append(skills.CalendarConfigArgs(), "quick", quick))
		if err != nil {
			return nil, false
		}
		res := NewToolResult(fmt.Sprintf("Added to your calendar: %s. %s", quick, strings.TrimSpace(out)))
		res.Evidence = map[string]interface{}{
			"type":      "acknowledgement",
			"operation": "create",
			"summary":   quick,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		return res, true

	case "delete", "remove":
		query := strings.TrimSpace(sarg(args, "query"))
		if query == "" {
			query = strings.TrimSpace(sarg(args, "title"))
		}
		out, err = t.run(ctx, append(skills.CalendarConfigArgs(), "delete", query))
		if err != nil {
			return nil, false
		}
		res := NewToolResult(fmt.Sprintf("Removed from your calendar: %s. %s", query, strings.TrimSpace(out)))
		res.Evidence = map[string]interface{}{
			"type":      "acknowledgement",
			"operation": "delete",
			"summary":   query,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		return res, true

	default:
		return nil, false
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
