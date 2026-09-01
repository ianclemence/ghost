package tools

import "context"

// VerifiableTool is implemented by consequential tools that can confirm their
// side effect actually occurred. The agent runs Verify after a successful,
// non-async execution; a failing verify is treated as a real failure (not
// assumed success) so the model can recover rather than trust a false positive.
//
// It's opt-in and proportional: only tools where "it reported success" isn't
// proof of "the desired state changed" implement this. Read-only tools and
// trivially-reliable writes do not.
type VerifiableTool interface {
	// Verify confirms the effect described by args is present. Return an error
	// (with an actionable message) if the desired outcome did not happen.
	Verify(ctx context.Context, args map[string]interface{}) error
}
