// Package connectedapp is Ghost's runtime model of a user's authenticated
// connection to an external system. A connected app is NOT a capability:
// it is the authenticated identity + credential reference + scopes through
// which one or more Ghost capabilities can be exercised. The model never
// sees an app; the runtime uses the app to resolve a capability's
// implementation.
package connectedapp

import (
	"sort"
	"time"
)

// Status is the lifecycle state of a connection.
type Status string

const (
	StatusDisconnected Status = "disconnected"
	StatusConnected    Status = "connected"
	StatusNeedsReauth  Status = "needs_reauth"
	StatusExpired      Status = "expired"
	StatusUnavailable  Status = "unavailable"
)

// App is one connected external system.
type App struct {
	// ID is the stable app identity, e.g. "google-calendar".
	ID string
	// Provider is the integration/provider name, e.g. "google-calendar",
	// "home-assistant".
	Provider string
	// DisplayName is user-facing.
	DisplayName string
	// CredentialID names the credential in the vault that authenticates it.
	CredentialID string
	// Scopes are the granted external scopes.
	Scopes []string
	// Capabilities are the Ghost capabilities this app can fulfil.
	Capabilities []string
	// Status is the connection lifecycle state.
	Status Status
	// LastValidated is when the connection was last proven good.
	LastValidated time.Time
	// Revoked marks an explicitly revoked connection.
	Revoked bool
}

// Usable reports whether the app can currently fulfil its capabilities.
func (a App) Usable() bool {
	return !a.Revoked && a.Status == StatusConnected
}

// Registry holds the connected apps for a runtime.
type Registry struct {
	apps  map[string]App
	order []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{apps: map[string]App{}} }

// Register adds or replaces an app.
func (r *Registry) Register(a App) {
	if a.ID == "" {
		return
	}
	if _, ok := r.apps[a.ID]; !ok {
		r.order = append(r.order, a.ID)
	}
	r.apps[a.ID] = a
}

// Get returns an app by ID.
func (r *Registry) Get(id string) (App, bool) {
	a, ok := r.apps[id]
	return a, ok
}

// List returns all apps in stable registration order.
func (r *Registry) List() []App {
	out := make([]App, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.apps[id])
	}
	return out
}

// Usable reports whether an app is connected and can serve.
func (r *Registry) Usable(id string) bool {
	a, ok := r.apps[id]
	return ok && a.Usable()
}

// ForCapability returns the usable apps that can fulfil a capability,
// deterministically ordered by ID.
func (r *Registry) ForCapability(capID string) []App {
	var out []App
	for _, a := range r.apps {
		if !a.Usable() {
			continue
		}
		for _, c := range a.Capabilities {
			if c == capID {
				out = append(out, a)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SetStatus updates a connection's lifecycle state.
func (r *Registry) SetStatus(id string, st Status) {
	a, ok := r.apps[id]
	if !ok {
		return
	}
	a.Status = st
	a.LastValidated = time.Now()
	r.apps[id] = a
}

// Revoke marks a connection revoked and disconnected.
func (r *Registry) Revoke(id string) {
	a, ok := r.apps[id]
	if !ok {
		return
	}
	a.Revoked = true
	a.Status = StatusDisconnected
	r.apps[id] = a
}
