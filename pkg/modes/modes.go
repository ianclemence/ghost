// Package modes is Ghost's single user-facing intelligence control.
//
// Instead of model names, routing tables, temperatures, and provider
// knobs, the owner chooses an outcome:
//
//	Local  — local intelligence and execution only. Cloud is never called.
//	Hybrid — local first; cloud reasoning when useful and permitted.
//	Cloud  — cloud reasoning preferred; memory, permissions, identity,
//	         and appliance control stay local no matter what.
//
// The mode governs the BRAIN only. Governance (permissions, credentials,
// identity, events) is identical in every mode: "cloud-assisted" never
// means "cloud-authorized". Resolution: explicit setting (state file,
// then GHOST_INTELLIGENCE_MODE) wins; otherwise derived (any cloud key
// configured → hybrid, else local).
package modes

import (
	"os"
	"path/filepath"
	"strings"
)

// Mode is the intelligence posture.
type Mode string

const (
	Local  Mode = "local"
	Hybrid Mode = "hybrid"
	Cloud  Mode = "cloud"
)

// Valid reports whether m is a known mode.
func Valid(m Mode) bool { return m == Local || m == Hybrid || m == Cloud }

// Describe returns the product-language explanation (no model names,
// no provider internals).
func Describe(m Mode) string {
	switch m {
	case Local:
		return "Local — Ghost thinks on your machine only. Cloud is never used."
	case Cloud:
		return "Cloud — Ghost prefers cloud reasoning. Memory, permissions, and control stay on your machine."
	default:
		return "Hybrid — Ghost thinks locally first and uses cloud reasoning when useful."
	}
}

// Resolve determines the effective mode: explicit file setting, then
// env override, then derivation from credential presence.
func Resolve(workspace string, cloudConfigured bool) Mode {
	if data, err := os.ReadFile(modePath(workspace)); err == nil {
		if m := Mode(strings.TrimSpace(strings.ToLower(string(data)))); Valid(m) {
			return m
		}
	}
	if env := strings.TrimSpace(strings.ToLower(os.Getenv("GHOST_INTELLIGENCE_MODE"))); env != "" {
		if m := Mode(env); Valid(m) {
			return m
		}
	}
	if cloudConfigured {
		return Hybrid
	}
	return Local
}

// Set persists an explicit owner choice (the only knob).
func Set(workspace string, m Mode) error {
	if !Valid(m) {
		return errInvalidMode
	}
	if err := os.MkdirAll(filepath.Dir(modePath(workspace)), 0755); err != nil {
		return err
	}
	return os.WriteFile(modePath(workspace), []byte(string(m)+"\n"), 0600)
}

func modePath(workspace string) string {
	return filepath.Join(workspace, "state", "intelligence-mode")
}

// IsCloudProvider reports whether a provider name denotes cloud
// reasoning (used to filter fallback candidates in local mode).
// Local providers: ollama, vllm, and local-* prefixed engines.
func IsCloudProvider(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "ollama") || strings.HasPrefix(lower, "vllm") ||
		strings.HasPrefix(lower, "local") {
		return false
	}
	return true
}

type modeError string

func (e modeError) Error() string { return string(e) }

const errInvalidMode = modeError("unknown intelligence mode (local, hybrid, cloud)")
