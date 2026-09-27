package agent

import "github.com/ianclemence/ghost/pkg/modes"

// offlineCapabilityNote returns a compact per-turn capability signal for the
// model when the runtime is degraded to local mode. It is empty when cloud is
// available, so ordinary turns carry no banner: offline state is surfaced only
// when it changes what Ghost can do.
func offlineCapabilityNote(m modes.Mode) string {
	if m != modes.Local {
		return ""
	}
	return "Runtime capability: local mode — cloud model providers are unavailable. " +
		"Prefer local tools; if the task needs the internet or a cloud model, say so plainly instead of retrying."
}
