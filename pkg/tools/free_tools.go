package tools

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
)

// Free consequential tools are model-invokable without a committed
// capability (no skill read required) yet can cause external, durable,
// privileged, device, process, or state-changing side effects. The single
// source of truth lives here so the main agent gate (which authorizes via
// the Permission Broker) and the subagent loop (which refuses when no
// governing hook is attached) can never disagree about what must be
// governed.
//
// Risk strings mirror permissions.Risk labels and are mapped by the agent
// package; tools does not import permissions to avoid a dependency cycle.
type FreeTool struct {
	Name       string
	Capability string
	Risk       string // "low_risk" | "consequential" | "high_impact"
}

const (
	RiskLow           = "low_risk"
	RiskConsequential = "consequential"
	RiskHighImpact    = "high_impact"
)

// FreeConsequentialTools is the audit table for standalone consequential
// tools reachable from a turn. Every entry must have a concrete capability
// identity so grants stay narrow, and a risk the broker evaluates.
var FreeConsequentialTools = []FreeTool{
	{Name: "message", Capability: "message.send", Risk: RiskConsequential},
	// exec is the process primitive: arbitrary command execution. Never
	// auto-authorized without an explicit grant.
	{Name: "exec", Capability: "exec.shell", Risk: RiskHighImpact},
	// sandbox runs model-authored code in an isolated environment; it is
	// still a code-execution primitive and must be broker-gated.
	{Name: "sandbox", Capability: "sandbox.exec", Risk: RiskConsequential},
	// Physical/device register access can actuate hardware.
	{Name: "i2c", Capability: "device.io", Risk: RiskConsequential},
	{Name: "spi", Capability: "device.io", Risk: RiskConsequential},
	// Home Assistant control turns real-world devices on/off. The model
	// sees the semantic "device" surface; "hass" remains a governed alias.
	{Name: "device", Capability: "device.control", Risk: RiskConsequential},
	{Name: "hass", Capability: "device.control", Risk: RiskConsequential},
	// Calendar is one semantic surface whose risk is action-dependent
	// (read vs modify); the agent loop authorizes the specific capability.
	// The table entry governs it for paths that cannot see the action
	// (e.g. subagents) at the conservative consequential level.
	{Name: "calendar", Capability: "calendar.modify", Risk: RiskConsequential},
	// Durable scheduling creates future external side effects.
	{Name: "schedule", Capability: "schedule.create", Risk: RiskLow},
	// Artifact publishing writes only Ghost's own validated handoff store
	// (existence, bounds, and estate membership are enforced by the
	// artifacts package, not the model). Same class as scheduling.
	{Name: "publish_artifact", Capability: "artifact.publish", Risk: RiskLow},
	// Self-update replaces the running binary.
	{Name: "update", Capability: "system.update", Risk: RiskHighImpact},
	// Image generation spends an external (paid) capability.
	{Name: "image_generate", Capability: "media.image_generate", Risk: RiskConsequential},
	// Web fetches are read-only; a future method that sends is excluded
	// here and must be gated when it exists. (web_fetch is GET-only.)
}

func freeToolByName(name string) (FreeTool, bool) {
	for _, t := range FreeConsequentialTools {
		if t.Name == name {
			return t, true
		}
	}
	// Third-party MCP tools are dynamically named (mcp_<server>_<tool>) and
	// cannot be enumerated ahead of time. A well-known read-oriented tool
	// resolves to its semantic capability; everything else is governed as
	// mcp.execute with high_impact risk, so an unknown third-party tool can
	// never inherit a low-risk classification.
	if strings.HasPrefix(name, "mcp_") {
		if spec, ok := capability.ForTool(name); ok && spec.ID != "mcp.execute" {
			return FreeTool{Name: name, Capability: spec.ID, Risk: string(spec.Risk)}, true
		}
		return FreeTool{Name: name, Capability: "mcp.execute", Risk: RiskHighImpact}, true
	}
	return FreeTool{}, false
}

// IsFreeConsequentialTool reports whether the tool is a standalone
// consequential operation that must pass through a governance boundary.
func IsFreeConsequentialTool(name string) bool {
	_, ok := freeToolByName(name)
	return ok
}

// FreeToolCapability returns the broker capability identity and risk label
// for a governed standalone tool.
func FreeToolCapability(name string) (FreeTool, bool) {
	return freeToolByName(name)
}
