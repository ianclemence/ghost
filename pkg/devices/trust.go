package devices

// Device trust model: future computers, phones, Home Assistant boxes,
// NAS units, and smart devices expose CAPABILITIES through Ghost —
// never unrestricted execution endpoints.
//
// A device has identity, trust state, declared capabilities, connection
// state, and permissions. The permission broker authorizes capability
// use per device; the event stream records it. Unrestricted remote shell
// is never the default.

import (
	"errors"
	"strings"
	"time"
)

// TrustState gates what a device may do.
type TrustState string

const (
	TrustUnknown TrustState = "unknown"
	TrustPaired  TrustState = "paired"
	TrustTrusted TrustState = "trusted"
	TrustRevoked TrustState = "revoked"
)

// ConnectionState tracks reachability (not authorization).
type ConnectionState string

const (
	ConnOffline ConnectionState = "offline"
	ConnOnline  ConnectionState = "online"
	ConnLocal   ConnectionState = "local"
)

// DeviceClass names the kind of device (computer, phone, hub, ...).
type DeviceClass string

const (
	ClassComputer DeviceClass = "computer"
	ClassPhone    DeviceClass = "phone"
	ClassHub      DeviceClass = "hub"
	ClassServer   DeviceClass = "server"
	ClassSensor   DeviceClass = "sensor"
)

// Device is a controllable endpoint owned by a Ghost.
type Device struct {
	ID           string            `json:"id"`
	GhostID      string            `json:"ghost_id"`
	Class        DeviceClass       `json:"class"`
	DisplayName  string            `json:"display_name"`
	Trust        TrustState        `json:"trust"`
	Connection   ConnectionState   `json:"connection"`
	Capabilities []string          `json:"capabilities"`
	Permissions  map[string]string `json:"permissions,omitempty"`
	LastSeen     time.Time         `json:"last_seen,omitempty"`
}

// Register validates a new device record. Trust starts at paired —
// NEVER trusted by default, and unrestricted shell is never granted.
func Register(ghostID string, class DeviceClass, displayName string, capabilities []string) (*Device, error) {
	if strings.TrimSpace(ghostID) == "" || strings.TrimSpace(displayName) == "" {
		return nil, errors.New("ghost and display name required")
	}
	switch class {
	case ClassComputer, ClassPhone, ClassHub, ClassServer, ClassSensor:
	default:
		return nil, errors.New("unknown device class")
	}
	for _, c := range capabilities {
		if strings.TrimSpace(c) == "" {
			return nil, errors.New("empty capability")
		}
		// Unrestricted execution endpoints are rejected at registration.
		lower := strings.ToLower(c)
		if lower == "shell" || lower == "exec" || lower == "root" || strings.Contains(lower, "unrestricted") {
			return nil, errors.New("unrestricted execution is not a registrable capability: " + c)
		}
	}
	return &Device{
		GhostID: ghostID, Class: class, DisplayName: displayName,
		Trust: TrustPaired, Connection: ConnOffline, Capabilities: capabilities,
	}, nil
}

// CanInvoke reports whether the device may invoke a capability now:
// trusted or paired (paired = explicitly approved per-action upstream),
// connected, capability declared, not revoked.
func (d *Device) CanInvoke(capability string) bool {
	if d.Trust == TrustRevoked || d.Trust == TrustUnknown {
		return false
	}
	if d.Connection == ConnOffline {
		return false
	}
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// TrustTo moves trust only along the safe lattice:
// unknown→paired→trusted, anything→revoked. Trusted never demotes
// silently back to paired (re-pair explicitly).
func (d *Device) TrustTo(next TrustState) error {
	switch {
	case next == TrustRevoked:
		d.Trust = TrustRevoked
		return nil
	case d.Trust == TrustUnknown && next == TrustPaired:
		d.Trust = TrustPaired
		return nil
	case d.Trust == TrustPaired && next == TrustTrusted:
		d.Trust = TrustTrusted
		return nil
	default:
		return errors.New("unsafe trust transition")
	}
}
