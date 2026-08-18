package commands

import (
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/doctor"
	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/tools"
)

type Runtime struct {
	Tools       *tools.ToolRegistry
	Sessions    *session.SessionManager
	Bus         *bus.MessageBus
	Commands    *Registry
	Doctor      *doctor.Doctor
	Personality string
	Model       string
	// ModelPresets lists named model presets available for switching
	// (e.g. from config model_list). Each is "provider:model" or "ollama/model".
	ModelPresets []string
	// SetActiveModel is called by /model to persist a selection and update the
	// live agent loop. It receives the canonical "provider:model" string.
	SetActiveModel func(providerModel string) error
	// CurrentModel resolves the active model for display. Falls back to Model.
	CurrentModel func() string
}

func (rt *Runtime) SetPersonality(name string) error {
	if rt.Personality == name {
		return nil
	}
	rt.Personality = name
	return nil
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
