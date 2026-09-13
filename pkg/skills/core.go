package skills

// Core skills are built-in Ghost verbs. They stay in the runtime capability
// registry + tools but are never listed, toggled, or removed via UI.
// Users import skills; they don't manage core.
//
// CTO decision: weather, aqi, currency, crypto, find-nearby are primitives
// like web_search — always ready, keyless, read-only. Hiding the toggle
// prevents users breaking daily-briefing, routines, and chat by accident.
// Connector skills (flight, calendar, homeassistant, spotify, email) stay
// visible because they carry the Connect affordance.
var coreSkills = map[string]bool{
	"weather":     true,
	"aqi":         true,
	"currency":    true,
	"crypto":      true,
	"find-nearby": true,
}

// IsHiddenCoreSkill reports whether name is a hidden built-in verb.
func IsHiddenCoreSkill(name string) bool { return coreSkills[name] }

// System-gated skills need explicit confirmation before enabling: they can
// scan, kill, or change the machine. The broker still enforces approvals at
// runtime; this is the UI-level speed bump.
var systemGatedSkills = map[string]bool{
	"system":          true,
	"network":         true,
	"process-manager": true,
	"mobile":          true,
	"hardware":        true,
	"tmux":            true,
}

// IsSystemGated reports whether enabling the skill should confirm first.
func IsSystemGated(name string) bool { return systemGatedSkills[name] }
