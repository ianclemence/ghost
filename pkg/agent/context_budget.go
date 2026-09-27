package agent

import (
	"github.com/ianclemence/ghost/pkg/providers"
)

// ContextBudget is a model-aware input budget for one turn: the selected
// model's context capacity minus a reserved output allowance.
type ContextBudget struct {
	ModelContext   int
	ReservedOutput int
}

// InputLimit is the token budget available to the request.
func (b ContextBudget) InputLimit() int {
	if b.ModelContext <= b.ReservedOutput {
		return b.ModelContext
	}
	return b.ModelContext - b.ReservedOutput
}

// ContextBudgetFor resolves a budget from the model spec through the provider
// capability table (pkg/providers.Describe). It never invents a universal
// number: unknown models fall back to the conservative 8192-token default the
// capability table already uses, with a quarter reserved for output.
func ContextBudgetFor(modelSpec string) ContextBudget {
	capability := providers.Describe(modelSpec)
	ctx := capability.MaxContext
	if ctx <= 0 {
		ctx = 8192
	}
	reserved := ctx / 4
	if reserved > 8192 {
		reserved = 8192
	}
	if reserved < 1024 {
		reserved = 1024
	}
	return ContextBudget{ModelContext: ctx, ReservedOutput: reserved}
}

// estimateMessageTokens is a cheap, model-agnostic size estimate (~4 bytes per
// token) plus per-message overhead. It exists to bound context, not to meter.
func estimateMessageTokens(m providers.Message) int {
	n := len([]rune(m.Content))/4 + 8
	for range m.ToolCalls {
		n += 16
	}
	return n
}

// FitContext bounds a message list to budgetTokens by trimming history
// oldest-first. The first message (the behavioural core, which carries
// identity, permission and evidence rules) and the last (the current request)
// are never removed, and at least one history message is kept when present.
// Nothing is silently truncated mid-message: whole history turns are dropped.
func FitContext(messages []providers.Message, budgetTokens int) []providers.Message {
	if budgetTokens <= 0 || len(messages) <= 2 {
		return messages
	}
	total := 0
	for _, m := range messages {
		total += estimateMessageTokens(m)
	}
	if total <= budgetTokens {
		return messages
	}
	keepFirst := messages[:1]
	last := messages[len(messages)-1]
	middle := messages[1 : len(messages)-1]
	over := total - budgetTokens
	drop := 0
	for drop < len(middle) && over > 0 {
		over -= estimateMessageTokens(middle[drop])
		drop++
	}
	out := make([]providers.Message, 0, 2+len(middle)-drop)
	out = append(out, keepFirst...)
	out = append(out, middle[drop:]...)
	out = append(out, last)
	return out
}
