package connector

import (
	"fmt"

	"github.com/ianclemence/ghost/pkg/capability"
)

// Finding is one result of the connector review pipeline.
type Finding struct {
	Level   string // "error" | "warning" | "info"
	Field   string
	Message string
}

// Review runs the connector review pipeline: schema validation plus the
// capability-risk audit and provenance checks a registry would apply before
// listing. It is deterministic and offline. Errors block install/listing;
// warnings are for the author and the directory to surface.
func Review(m *Manifest) []Finding {
	var out []Finding
	if m == nil {
		return []Finding{{Level: "error", Message: "nil manifest"}}
	}
	for _, e := range m.Validate() {
		out = append(out, Finding{Level: "error", Field: e.Field, Message: e.Msg})
	}

	// OAuth without declared scopes asks the user for unbounded access.
	if m.Auth.Kind == "oauth" && len(m.Auth.Scopes) == 0 {
		out = append(out, Finding{Level: "warning", Field: "auth.scopes",
			Message: "oauth connector declares no scopes; request the narrowest set"})
	}
	// Provenance: anything not local should say where it came from.
	if m.Provenance.Source == "" {
		out = append(out, Finding{Level: "warning", Field: "provenance.source",
			Message: "no provenance source; a directory cannot attest this connector"})
	}
	if m.Provenance.SignedBy != "" && m.Provenance.Signature == "" {
		out = append(out, Finding{Level: "warning", Field: "provenance.signature",
			Message: "signed_by is set but no signature is present"})
	}
	// High-impact capabilities are the sharp edge; keep them deliberate.
	high := 0
	for _, c := range m.Capabilities {
		if c.Risk == capability.RiskHighImpact {
			high++
		}
		// A consequential capability that reaches the network with no
		// bounded tool path is broad; flag it for review.
		if c.Risk != capability.RiskReadOnly && c.NetworkRequired && len(c.AllowedTools) == 0 && m.Kind == KindNative {
			out = append(out, Finding{Level: "info", Field: "capabilities",
				Message: fmt.Sprintf("capability %q is %s, network-bound, and names no allowed tools", c.ID, c.Risk)})
		}
	}
	if high > 2 {
		out = append(out, Finding{Level: "warning", Field: "capabilities",
			Message: fmt.Sprintf("%d high_impact capabilities; split or justify before listing", high)})
	}

	if len(out) == 0 {
		out = append(out, Finding{Level: "info", Message: "no findings"})
	}
	return out
}

// HasErrors reports whether any finding blocks install/listing.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Level == "error" {
			return true
		}
	}
	return false
}
