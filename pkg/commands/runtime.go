package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/doctor"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/personality"
	"github.com/ianclemence/ghost/pkg/rag"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/tools"
)

// ScheduleCreator is the authoritative scheduler surface for /loop and
// /remind so their durable state lives in the one scheduler.
type ScheduleCreator interface {
	CreateItem(item *scheduled.ScheduledItem) error
	ListItems(itemType scheduled.ItemType, state scheduled.ItemState, limit int) ([]*scheduled.ScheduledItem, error)
	CancelItem(id string) error
}

type Runtime struct {
	Tools       *tools.ToolRegistry
	Sessions    *session.SessionManager
	Bus         *bus.MessageBus
	Commands    *Registry
	Doctor      *doctor.Doctor
	Personality string
	Model       string
	// PersonalContext is the store backing the /context command. It is
	// optional: the agent can run without it, and a nil store makes the command
	// report the store as unavailable instead of failing the turn.
	PersonalContext *personalcontext.Store
	// ModelPresets lists named model presets available for switching
	// (e.g. from config model_list). Each is "provider:model" or "ollama/model".
	ModelPresets []string
	// ModelPresetStatus reports whether a preset (or provider:model ref)
	// can serve right now, with the reason when it cannot. Capabilities
	// gating for the model UI: unavailable knobs are marked, not hidden.
	ModelPresetStatus func(preset string) (available bool, reason string)
	// OnPersonalityChanged fires after SetPersonality stores a new name so
	// the runtime can validate it and propagate it into prompt assembly.
	// Nil = store only (legacy behavior).
	OnPersonalityChanged func(name string)
	// SetActiveModel is called by /model to persist a selection and update the
	// live agent loop. It receives the canonical "provider:model" string.
	SetActiveModel func(providerModel string) error
	// CurrentModel resolves the active model for display. Falls back to Model.
	CurrentModel func() string
	// Workspace is the filesystem workspace root for file-backed resets.
	Workspace string
	// Scheduler returns the authoritative scheduled-item surface for /loop
	// and /remind. It is late-bound because the scheduler is created after
	// the agent loop. Nil disables direct scheduling from those commands.
	Scheduler func() ScheduleCreator
	// RAG is the in-memory vector index. Reset clears it alongside the DB
	// rows so deleted memories stop surfacing until restart-free operation
	// continues. Nil when RAG is disabled — callers must nil-check.
	RAG *rag.Store
}

func (rt *Runtime) SetPersonality(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if rt.Personality == name {
		return nil
	}
	// Unknown names are rejected up front: a selection that injects
	// nothing is a lie to the user, not a preference.
	if !validPersonality(name) {
		return fmt.Errorf("unknown personality %q (see /personality for the list)", name)
	}
	rt.Personality = name
	// Live-propagate into the prompt builder so selection takes effect
	// on the next turn instead of dying in this string.
	if rt.OnPersonalityChanged != nil {
		rt.OnPersonalityChanged(name)
	}
	return nil
}

// validPersonality reports whether name resolves to a builtin or a saved
// custom personality.
func validPersonality(name string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, ok := personality.NewLoader(filepath.Join(home, ".GHOST")).Get(name)
	return ok
}

func (rt *Runtime) SetModel(target string) error {
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid format, use provider:model (e.g. openai:gpt-4o)")
	}
	rt.Model = target
	return nil
}

func (rt *Runtime) GetCurrentModel() string {
	if rt.Model != "" {
		return rt.Model
	}
	return "default"
}

type SessionStats struct {
	Messages      int
	TotalTokens   int
	ToolCalls     int
	SummaryTokens int
}

func (rt *Runtime) GetSessionStats(sessionKey string) SessionStats {
	if rt.Sessions == nil {
		return SessionStats{}
	}
	history := rt.Sessions.GetHistory(sessionKey)
	stats := SessionStats{
		Messages: len(history),
	}
	for _, msg := range history {
		stats.TotalTokens += len(msg.Content) / 4
		if len(msg.ToolCalls) > 0 {
			stats.ToolCalls += len(msg.ToolCalls)
		}
	}
	return stats
}
