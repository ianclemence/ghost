package tools

import (
	"context"
	"strings"
)

// Web-derived taint tracks whether the current turn has touched the network.
// Model output written to memory after a web fetch may carry
// adversary-controlled bytes, so downstream stores (remember, RAG) record
// the taint and rank the content accordingly. User speech is never tainted:
// the user is the authority for what they assert.
type webDerivedKey struct{}

// WebTools is the set of tools whose output is network-origin content.
var WebTools = map[string]bool{
	"web_search": true,
	"web_fetch":  true,
}

// IsWebTool reports whether the tool returns network-origin content.
func IsWebTool(name string) bool {
	return WebTools[strings.TrimSpace(name)]
}

// WithWebDerived marks the context as web-derived for downstream memory
// writes in the same turn.
func WithWebDerived(ctx context.Context) context.Context {
	return context.WithValue(ctx, webDerivedKey{}, true)
}

// WebDerivedFromContext reports whether the turn has touched the network.
func WebDerivedFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(webDerivedKey{}).(bool)
	return v
}
