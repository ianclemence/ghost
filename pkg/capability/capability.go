// Package capability is Ghost's canonical capability contract. A capability
// is a Ghost-owned semantic ability to accomplish a bounded task
// ("calendar.modify", "message.send", "device.control"). It is the identity
// that authorization, evidence, and canonical events operate on.
//
// Tools, providers, integrations, MCP servers, and skills are all
// implementation details beneath a capability. Nothing external defines
// capability semantics: everything that varies is replaceable, everything
// that defines trust is owned by Ghost.
//
// This is a semantic registry, not a framework. It is deliberately small:
// identity, risk, evidence requirement, and the tool implementations that
// currently fulfil it.
package capability

import (
	"sort"
	"strings"
)

// Risk is the authority class of a capability, mirrored from the permission
// broker's risk vocabulary. It is defined here so the contract does not
// depend on the broker.
type Risk string

const (
	RiskReadOnly      Risk = "read_only"
	RiskLow           Risk = "low_risk"
	RiskConsequential Risk = "consequential"
	RiskHighImpact    Risk = "high_impact"
)

// EvidenceKind names the shape of proof required before Ghost may tell the
// user a consequential capability succeeded. Empty means no evidence is
// required (read-only or purely local state).
type EvidenceKind string

const (
	// EvidenceNone: no runtime evidence required (read-only).
	EvidenceNone EvidenceKind = ""
	// EvidenceAcknowledgement: provider ack + identifier + timestamp
	// (e.g. message.send, external API write).
	EvidenceAcknowledgement EvidenceKind = "acknowledgement"
	// EvidenceStateTransition: requested state vs observed resulting state
	// (e.g. device.control).
	EvidenceStateTransition EvidenceKind = "state_transition"
	// EvidenceArtifact: artifact identifier + existence (+ size/hash).
	EvidenceArtifact EvidenceKind = "artifact"
	// EvidenceFileWrite: path + existence (+ size/hash).
	EvidenceFileWrite EvidenceKind = "file_write"
)

// Spec is the Ghost-owned contract for one capability.
type Spec struct {
	// ID is the canonical semantic identity, e.g. "calendar.modify".
	ID string
	// Title is a short human-readable name.
	Title string
	// Risk is the authority class the broker evaluates.
	Risk Risk
	// Evidence is the proof required before success may be claimed.
	Evidence EvidenceKind
	// Tools are the model-facing tool names that currently fulfil this
	// capability. Implementations are replaceable; the capability identity
	// is not.
	Tools []string
	// Description explains, in product terms, what the capability does.
	Description string
}

// RequiresEvidence reports whether success for this capability must be
// backed by runtime evidence.
func (s Spec) RequiresEvidence() bool { return s.Evidence != EvidenceNone }

// Registry is the process-wide capability registry.
type Registry struct {
	byID   map[string]Spec
	byTool map[string]string // tool name -> capability ID
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: map[string]Spec{}, byTool: map[string]string{}}
}

// Register adds a capability spec. Tools map to the capability identity.
func (r *Registry) Register(spec Spec) {
	if spec.ID == "" {
		return
	}
	r.byID[spec.ID] = spec
	for _, tool := range spec.Tools {
		if tool != "" {
			r.byTool[tool] = spec.ID
		}
	}
}

// Get returns the spec for a capability ID.
func (r *Registry) Get(id string) (Spec, bool) {
	s, ok := r.byID[id]
	return s, ok
}

// ForTool resolves the capability that a model-facing tool fulfils. Dynamic
// third-party tools (mcp_*) resolve to their governed identity.
func (r *Registry) ForTool(tool string) (Spec, bool) {
	if id, ok := r.byTool[tool]; ok {
		return r.Get(id)
	}
	if strings.HasPrefix(tool, "mcp_") {
		// Conservative semantic mapping: only well-known, read-oriented
		// shapes map to a first-class capability. Anything uncertain stays
		// mcp.execute (high impact) so an unknown third-party tool can
		// never inherit a low-risk classification.
		if id := mcpCapabilityFor(tool); id != "" {
			if spec, ok := r.Get(id); ok {
				return spec, true
			}
		}
		return r.Get("mcp.execute")
	}
	return Spec{}, false
}

// ForToolAction resolves the capability for a tool call whose capability
// depends on the operation (e.g. the calendar tool reads vs modifies). It
// falls back to ForTool. This keeps one semantic tool surface without
// flattening read and modify authority into one identity.
func (r *Registry) ForToolAction(tool string, args map[string]interface{}) (Spec, bool) {
	if tool == "calendar" {
		if a, _ := args["action"].(string); a == "create" || a == "delete" || a == "add" || a == "remove" {
			return r.Get("calendar.modify")
		}
		return r.Get("calendar.read")
	}
	return r.ForTool(tool)
}

// ForToolAction resolves against the default registry.
func ForToolAction(tool string, args map[string]interface{}) (Spec, bool) {
	return defaultRegistry.ForToolAction(tool, args)
}

// mcpCapabilityFor maps a small, well-understood set of MCP tool name shapes
// to a Ghost capability. It is deliberately conservative: an unmatched tool
// returns "" and is governed as mcp.execute. It never maps a write-shaped
// tool onto a read capability.
func mcpCapabilityFor(tool string) string {
	t := strings.ToLower(tool)
	switch {
	case strings.Contains(t, "weather"):
		return "weather.get"
	case strings.Contains(t, "github") && (strings.Contains(t, "search") || strings.Contains(t, "repo")):
		return "repository.search"
	case strings.Contains(t, "web") && (strings.Contains(t, "search") || strings.Contains(t, "fetch")):
		return "web.search"
	}
	return ""
}

// IDs returns all registered capability IDs, sorted.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// defaultRegistry is the Ghost-owned capability set. It covers the
// capabilities the audit identified as product-meaningful and the
// consequential operations that must carry evidence. Additional
// capabilities are migrated here over time; the contract is stable.
var defaultRegistry = buildDefault()

func buildDefault() *Registry {
	r := NewRegistry()
	specs := []Spec{
		// --- Memory (Ghost-owned knowledge) ---
		{ID: "memory.remember", Title: "Remember", Risk: RiskLow, Tools: []string{"remember"},
			Description: "Store a durable fact or preference about the user."},
		{ID: "memory.recall", Title: "Recall", Risk: RiskReadOnly, Tools: []string{"memory_recall", "context_get", "session_search"},
			Description: "Retrieve what Ghost knows or remembers."},
		{ID: "memory.forget", Title: "Forget", Risk: RiskLow, Tools: []string{"memory_curate"},
			Description: "Retire a belief or memory entry."},
		{ID: "memory.summarize", Title: "Summarize", Risk: RiskReadOnly, Tools: []string{"compact_context"},
			Description: "Compress conversation context."},

		// --- Information (read-only) ---
		{ID: "web.search", Title: "Web search", Risk: RiskReadOnly, Tools: []string{"web_search"},
			Description: "Search the web."},
		{ID: "web.fetch", Title: "Web fetch", Risk: RiskReadOnly, Tools: []string{"web_fetch"},
			Description: "Fetch a web page."},
		{ID: "repository.search", Title: "Search repository", Risk: RiskReadOnly,
			Description: "Search a connected source repository."},
		{ID: "weather.get", Title: "Weather", Risk: RiskReadOnly, Tools: []string{"weather_now"},
			Description: "Current weather for a location."},
		{ID: "aqi.get", Title: "Air quality", Risk: RiskReadOnly, Tools: []string{"aqi_now"},
			Description: "Air quality for a location."},
		{ID: "currency.convert", Title: "Currency", Risk: RiskReadOnly, Tools: []string{"currency_convert"},
			Description: "Convert currency."},
		{ID: "crypto.price", Title: "Crypto price", Risk: RiskReadOnly, Tools: []string{"crypto_price"},
			Description: "Cryptocurrency price."},
		{ID: "places.nearby", Title: "Nearby places", Risk: RiskReadOnly, Tools: []string{"places_nearby"},
			Description: "Find nearby places."},
		{ID: "flight.status", Title: "Flight status", Risk: RiskReadOnly, Tools: []string{"flight_status"},
			Description: "Track a flight."},

		// --- Communication (consequential, evidence required) ---
		{ID: "message.send", Title: "Send message", Risk: RiskConsequential, Evidence: EvidenceAcknowledgement,
			Tools:       []string{"message"},
			Description: "Send a message to the user or a contact."},

		// --- Calendar (consequential, evidence required) ---
		{ID: "calendar.read", Title: "Read calendar", Risk: RiskReadOnly,
			Tools:       []string{"calendar"},
			Description: "Read calendar events."},
		{ID: "calendar.modify", Title: "Modify calendar", Risk: RiskConsequential, Evidence: EvidenceAcknowledgement,
			Tools:       []string{"calendar"},
			Description: "Create, change, or delete calendar events."},

		// --- Devices (consequential, evidence required) ---
		{ID: "device.read", Title: "Read device state", Risk: RiskReadOnly, Tools: []string{"device", "hass"},
			Description: "Read smart-home device state."},
		{ID: "device.control", Title: "Control device", Risk: RiskConsequential, Evidence: EvidenceStateTransition,
			Tools:       []string{"device", "hass"},
			Description: "Change smart-home device state."},

		// --- Browser / computer (gated; evidence enforced by their gates) ---
		{ID: "browser.inspect", Title: "Browse (observe)", Risk: RiskReadOnly,
			Tools:       []string{"browser_navigate", "browser_snapshot"},
			Description: "Observe web pages."},
		{ID: "browser.control", Title: "Browse (control)", Risk: RiskConsequential, Evidence: EvidenceStateTransition,
			Tools:       []string{"browser_click", "browser_type", "browser_press"},
			Description: "Interact with web pages."},
		{ID: "computer.inspect", Title: "Inspect screen", Risk: RiskReadOnly,
			Tools:       []string{"computer_inspect_ui", "computer_screenshot"},
			Description: "Observe the computer screen."},
		{ID: "computer.control", Title: "Control computer", Risk: RiskConsequential, Evidence: EvidenceStateTransition,
			Tools:       []string{"computer_click", "computer_type", "computer_press_key"},
			Description: "Control the computer UI."},

		// --- Files (local writes, evidence required) ---
		{ID: "file.read", Title: "Read files", Risk: RiskReadOnly,
			Tools:       []string{"read_file", "list_dir", "search_files", "grep_search"},
			Description: "Read files in the workspace."},
		{ID: "file.write", Title: "Write files", Risk: RiskLow, Evidence: EvidenceFileWrite,
			Tools:       []string{"write_file", "append_file", "edit_file"},
			Description: "Create or modify files."},

		// --- Artifacts (handoff) ---
		{ID: "artifact.create", Title: "Publish artifact", Risk: RiskLow, Evidence: EvidenceArtifact,
			Tools:       []string{"publish_artifact"},
			Description: "Publish a durable handoff artifact."},

		// --- Automation ---
		{ID: "routine.create", Title: "Create routine", Risk: RiskLow,
			Tools:       []string{"schedule", "cron"},
			Description: "Create a scheduled or recurring action."},
		{ID: "routine.modify", Title: "Modify routine", Risk: RiskLow,
			Tools:       []string{"schedule", "cron"},
			Description: "Change a scheduled or recurring action."},
		{ID: "routine.cancel", Title: "Cancel routine", Risk: RiskLow,
			Tools:       []string{"schedule", "cron"},
			Description: "Cancel a scheduled or recurring action."},

		// --- Execution primitives (infrastructure, high impact) ---
		{ID: "exec.shell", Title: "Shell command", Risk: RiskHighImpact, Tools: []string{"exec"},
			Description: "Run a shell command."},
		{ID: "exec.sandbox", Title: "Sandboxed code", Risk: RiskHighImpact, Tools: []string{"sandbox"},
			Description: "Run code in a sandbox."},
		{ID: "system.update", Title: "System update", Risk: RiskHighImpact, Tools: []string{"update"},
			Description: "Update Ghost."},
		{ID: "exec.scheduled", Title: "Scheduled command", Risk: RiskHighImpact,
			Description: "Run a shell command from a schedule."},
		{ID: "device.io", Title: "Device IO", Risk: RiskConsequential, Tools: []string{"i2c", "spi"},
			Description: "Low-level hardware IO."},
		{ID: "skills.manage", Title: "Manage skills", Risk: RiskHighImpact, Tools: []string{"skill_manage"},
			Description: "Create, patch, or remove skills."},
		{ID: "mcp.execute", Title: "External tool", Risk: RiskHighImpact,
			Description: "Execute a third-party MCP tool."},
	}
	for _, s := range specs {
		r.Register(s)
	}
	return r
}

// Default returns the process-wide Ghost capability registry.
func Default() *Registry { return defaultRegistry }

// ForTool resolves the capability for a model-facing tool from the default
// registry.
func ForTool(tool string) (Spec, bool) { return defaultRegistry.ForTool(tool) }

// Get returns a capability spec by ID from the default registry.
func Get(id string) (Spec, bool) { return defaultRegistry.Get(id) }

// IDs returns the canonical capability IDs of the default registry.
func IDs() []string { return defaultRegistry.IDs() }
