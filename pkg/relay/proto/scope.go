package proto

import (
	"net/http"
	"strings"
)

// scopeChatPaths is the conversational core a chat-scoped app may reach:
// talk, read history/recall, answer clarifications, and read-only
// self/model/health introspection. Everything else (exec, pairing,
// permissions, schedules, config) needs a full-scope token.
var scopeChatPaths = map[string]map[string]bool{
	"/v1/chat":            {"GET": true, "POST": true},
	"/v1/message":         {"GET": true, "POST": true},
	"/v1/messages":        {"GET": true, "POST": true},
	"/v1/history":         {"GET": true},
	"/v1/recall":          {"GET": true, "POST": true},
	"/v1/clarify/respond": {"POST": true},
	"/v1/health":          {"GET": true},
	"/v1/identity":        {"GET": true},
	"/v1/model":           {"GET": true},
	"/v1/activity":        {"GET": true},
}

// scopeSensitivePrefixes are credential-adjacent paths even readonly
// clients must not reach over the relay.
var scopeSensitivePrefixes = []string{
	"/v1/pairing/",
	"/v1/permissions/",
}

// ScopeAllows reports whether a client scope may call method+path.
// Unknown scopes deny — fail closed.
func ScopeAllows(scope, method, path string) bool {
	switch scope {
	case "", ScopeFull:
		return true
	case ScopeReadonly:
		if method != http.MethodGet {
			return false
		}
		for _, p := range scopeSensitivePrefixes {
			if strings.HasPrefix(path, p) {
				return false
			}
		}
		return true
	case ScopeChat:
		methods, ok := scopeChatPaths[path]
		return ok && methods[method]
	default:
		return false
	}
}
