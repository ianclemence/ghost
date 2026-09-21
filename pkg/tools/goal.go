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
	return "Manage standing goals (durable owner intents like \"take care of school emails\"). Actions: create, list, pause, resume, complete, progress."
}

func (t *GoalTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"create", "list", "pause", "resume", "complete", "progress"}},
			"text":   map[string]interface{}{"type": "string", "description": "Goal text (create)"},
			"scope":  map[string]interface{}{"type": "string", "description": "Scope bound (create)"},
			"id":     map[string]interface{}{"type": "string", "description": "Goal ID (pause/resume/complete/progress)"},
			"note":   map[string]interface{}{"type": "string", "description": "Progress note (progress)"},
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
