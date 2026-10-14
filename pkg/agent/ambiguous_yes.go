package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/skills"
)

func (al *AgentLoop) waitingOnOwner(session string) []string {
	items := al.pendingApprovalLines(session)
	if al == nil || al.workspace == "" {
		return items
	}
	if p, ok := skills.NewPendingStore(al.workspace).OpenForSession(session); ok && p != nil {
		q := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p.Question), "Say yes to confirm."))
		if q == "" {
			q = strings.TrimSpace(p.Intent)
		}
		if q != "" {
			items = append(items, q)
		}
	}
	return items
}

func isBareAnswer(text string) bool {
	return isAffirmation(text) || isDenial(text)
}

// ambiguousAnswerText is the reply to a bare yes or no when more than one
// thing is waiting: a yes that lands on whichever was asked last makes the
// owner approve something they never read.
func ambiguousAnswerText(items []string) string {
	var b strings.Builder
	b.WriteString("More than one thing is waiting on you, so I haven't answered for you:\n")
	for _, it := range items {
		b.WriteString("- " + it + "\n")
	}
	b.WriteString("Tap Allow or Deny on the one you mean, or tell me which.")
	return b.String()
}

func (al *AgentLoop) ambiguousAnswer(session, text string) (string, bool) {
	if !isBareAnswer(text) {
		return "", false
	}
	items := al.waitingOnOwner(session)
	if len(items) < 2 {
		return "", false
	}
	return ambiguousAnswerText(items), true
}
