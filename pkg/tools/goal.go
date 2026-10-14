package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/goals"
)

// GoalTool manages standing goals: durable owner intents the heartbeat
// evaluates. Local state only (low-risk); everything a goal triggers still
// passes the broker. The workspace is injected by the agent loop.
type GoalTool struct {
	newStore func() *goals.Store
}

func NewGoalTool(workspace string) *GoalTool {
	return &GoalTool{newStore: func() *goals.Store { return goals.NewStore(workspace) }}
}

func (t *GoalTool) Name() string { return "goal" }

func (t *GoalTool) Description() string {
	return "Manage standing goals (durable owner intents like \"take care of school emails\"). Actions: create, list, pause, resume, complete, progress, criteria, check. " +
		"For anything that takes more than one session, set criteria (one checkable part of done each) and mark each with check only after you have seen it work and can say what you saw; complete is refused until every criterion has passed."
}

func (t *GoalTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "enum": []string{"create", "list", "pause", "resume", "complete", "progress", "criteria", "check"}},
			"text":     map[string]interface{}{"type": "string", "description": "Goal text (create)"},
			"scope":    map[string]interface{}{"type": "string", "description": "Scope bound (create)"},
			"id":       map[string]interface{}{"type": "string", "description": "Goal ID (pause/resume/complete/progress)"},
			"note":     map[string]interface{}{"type": "string", "description": "Progress note (progress)"},
			"criteria": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "The checkable parts of done (create, criteria)"},
			"index":    map[string]interface{}{"type": "integer", "description": "Which criterion, from 1 (check)"},
			"evidence": map[string]interface{}{"type": "string", "description": "What you saw that shows it passes (check)"},
		},
		"required": []string{"action"},
	}
}

func (t *GoalTool) Timeout() time.Duration { return 15 * time.Second }

func (t *GoalTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	store := t.newStore()
	action := strings.ToLower(strings.TrimSpace(sarg(args, "action")))
	switch action {
	case "create", "add":
		text := strings.TrimSpace(sarg(args, "text"))
		if text == "" {
			return ErrorResult("goal create needs text.")
		}
		g, err := store.Create(text, strings.TrimSpace(sarg(args, "scope")), "", nil, time.Time{})
		if err != nil {
			return ErrorResult(fmt.Sprintf("Couldn't create that goal: %v.", err))
		}
		if cs := stringList(args["criteria"]); len(cs) > 0 {
			if g, err = store.SetCriteria(g.ID, cs); err != nil {
				return ErrorResult(fmt.Sprintf("The goal was set (%s) but its criteria were refused: %v.", g.ID, err))
			}
		}
		return NewToolResult(fmt.Sprintf("Goal set (%s): %s. I'll keep working on it and report back when something needs you.", g.ID, g.Text))
	case "list", "":
		list, err := store.List(time.Now())
		if err != nil {
			return ErrorResult(fmt.Sprintf("Couldn't list goals: %v.", err))
		}
		if len(list) == 0 {
			return NewToolResult("No standing goals yet.")
		}
		var sb strings.Builder
		for _, g := range list {
			fmt.Fprintf(&sb, "- %s [%s] %s\n", g.ID, g.Status, g.Text)
		}
		return NewToolResult(strings.TrimSpace(sb.String()))
	case "pause", "resume", "complete":
		id := strings.TrimSpace(sarg(args, "id"))
		if id == "" {
			return ErrorResult("goal " + action + " needs an id.")
		}
		var (
			g   goals.Goal
			err error
		)
		switch action {
		case "pause":
			g, err = store.Pause(id)
		case "resume":
			g, err = store.Resume(id)
		default:
			g, err = store.Complete(id)
		}
		if err != nil {
			return ErrorResult(fmt.Sprintf("Couldn't %s that goal: %v.", action, err))
		}
		return NewToolResult(fmt.Sprintf("Goal %s %sd: %s.", g.ID, action, g.Text))
	case "criteria":
		id := strings.TrimSpace(sarg(args, "id"))
		g, err := store.SetCriteria(id, stringList(args["criteria"]))
		if err != nil {
			return ErrorResult(fmt.Sprintf("Couldn't set criteria: %v.", err))
		}
		return NewToolResult(fmt.Sprintf("Goal %s has %d criteria, none passed yet.", g.ID, len(g.Criteria)))
	case "check":
		id := strings.TrimSpace(sarg(args, "id"))
		n, _ := args["index"].(float64)
		g, err := store.CheckCriterion(id, int(n), sarg(args, "evidence"))
		if err != nil {
			return ErrorResult(fmt.Sprintf("Couldn't check that: %v.", err))
		}
		left := 0
		for _, c := range g.Criteria {
			if !c.Passes {
				left++
			}
		}
		return NewToolResult(fmt.Sprintf("Criterion %d passes. %d left before this goal can be completed.", int(n), left))
	case "progress":
		id := strings.TrimSpace(sarg(args, "id"))
		note := strings.TrimSpace(sarg(args, "note"))
		if id == "" || note == "" {
			return ErrorResult("goal progress needs id and note.")
		}
		if _, err := store.AppendProgress(id, note); err != nil {
			return ErrorResult(fmt.Sprintf("Couldn't record progress: %v.", err))
		}
		return NewToolResult("Progress recorded.")
	default:
		return ErrorResult("goal needs action create, list, pause, resume, complete, or progress.")
	}
}

func stringList(v interface{}) []string {
	var out []string
	list, _ := v.([]interface{})
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
