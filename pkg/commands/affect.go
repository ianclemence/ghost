package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/affect"
)

// affectHandler implements /affect — read-only relational state. Only
// aggregates are shown (affinity band, mood, turn counts); raw turn text
// is never persisted and therefore can never leak here. Wiped by
// /reset context alongside the rest of personal state.
func affectHandler(ctx context.Context, req Request, rt *Runtime) error {
	ws := workspaceForRuntime(rt)
	s, err := affect.Load(ws)
	if err != nil {
		return req.Reply(fmt.Sprintf("Relational state unreadable: %v", err))
	}
	s = s.Decayed(time.Now())
	var sb strings.Builder
	sb.WriteString("### Relational State\n\n")
	sb.WriteString(fmt.Sprintf("- **Affinity**: %.2f (%s)\n", s.Affinity, s.AffinityBand()))
	sb.WriteString(fmt.Sprintf("- **Mood**: %+.2f (%s)\n", s.Valence, s.MoodBand()))
	sb.WriteString(fmt.Sprintf("- **Turns together**: %d (%d warm, %d cold)\n", s.Turns, s.WarmTurns, s.ColdTurns))
	sb.WriteString("\nAggregates only — no conversation text is stored. Cleared by `/reset context`.")
	return req.Reply(sb.String())
}
