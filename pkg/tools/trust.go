package tools

import (
	"context"
	"fmt"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// Trust boundary for tool execution.
//
// Host tools (Ghost's own code) are trusted: their behavior is reviewed
// and their inputs are validated. Guest-defined tools — custom commands
// registered by skills, extensions, or retrieved content — are
// untrusted: they run under the restricted boundary (validated args,
// redacted logs, no credential access) and their outputs are marked.
// A cancellation revokes capability immediately: no tool starts on a
// dead context, so a cancelled turn cannot launch new side effects.

// TrustLevel names the boundary a tool runs under.
type TrustLevel string

const (
	// TrustTrusted marks host-reviewed code. Default for built-ins.
	TrustTrusted TrustLevel = "trusted"
	// TrustUntrusted marks guest-defined or retrieved behavior: run
	// restricted, mark outputs.
	TrustUntrusted TrustLevel = "untrusted"
)

// TrustBoundary is implemented by tools that declare their boundary.
// Tools that do not implement it are host code by construction and
// read as trusted.
type TrustBoundary interface {
	Trust() TrustLevel
}

// TrustOf returns the tool's declared boundary, defaulting to trusted
// for host code that predates the interface.
func TrustOf(t Tool) TrustLevel {
	if tb, ok := t.(TrustBoundary); ok {
		if tb.Trust() == TrustUntrusted {
			return TrustUntrusted
		}
	}
	return TrustTrusted
}

// RequireLiveCtx fails fast when cancellation already arrived: never
// START work on a dead context. In-flight work observes ctx.Err()
// itself; this guard covers the launch edge so a cancelled turn cannot
// mint new side effects between the cancel and the next select.
func RequireLiveCtx(ctx context.Context, tool string) error {
	if ctx == nil {
		return fmt.Errorf("tool %q: no context bound", tool)
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("tool %q: context already %v; launch refused", tool, ctx.Err())
	default:
		return nil
	}
}

// policyDeny renders a tool-layer refusal with the denial envelope:
// stable code for logs, reason + remedy for the model.
func policyDeny(code permissions.DenialCode, reason, remedy string) string {
	return permissions.Deny(code, reason, remedy).String()
}
