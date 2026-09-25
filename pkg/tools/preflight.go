package tools

import (
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/product"
)

// NotConfigured returns the product connect message for a tool whose
// integration is absent, or "" when the tool is configured (or has no
// connect gate). It is a pure config read — no network, no side effects —
// so Governance can consult it BEFORE opening an approval card: an owner
// approval cannot make an unconnected integration run, so the honest
// "connect it first" answer must come before any ask, never after it.
// Every new connect-gated integration should register here when its tool
// gains an approval-required action.
func NotConfigured(tool string) string {
	switch tool {
	case "device", "hass":
		if !credentials.HassConfigured() {
			return product.FriendlyFor("hass", product.ErrConfigRequired)
		}
	case "flight_status":
		if !credentials.FlightConfigured() {
			return product.FriendlyFor("flight", product.ErrConfigRequired)
		}
	}
	return ""
}
