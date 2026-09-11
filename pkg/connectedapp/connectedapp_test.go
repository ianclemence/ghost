package connectedapp

import "testing"

func TestConnectedAppLifecycle(t *testing.T) {
	r := NewRegistry()
	r.Register(App{ID: "google-calendar", Provider: "google-calendar",
		CredentialID: "google-calendar", Capabilities: []string{"calendar.read", "calendar.modify"},
		Status: StatusDisconnected})

	if r.Usable("google-calendar") {
		t.Fatal("disconnected app must not be usable")
	}
	if apps := r.ForCapability("calendar.modify"); len(apps) != 0 {
		t.Fatalf("disconnected app must not fulfil a capability: %+v", apps)
	}

	r.SetStatus("google-calendar", StatusConnected)
	if !r.Usable("google-calendar") {
		t.Fatal("connected app must be usable")
	}
	apps := r.ForCapability("calendar.modify")
	if len(apps) != 1 || apps[0].ID != "google-calendar" {
		t.Fatalf("connected app must fulfil calendar.modify: %+v", apps)
	}

	// Revocation is real: a revoked app is not usable and fulfils nothing.
	r.Revoke("google-calendar")
	if r.Usable("google-calendar") {
		t.Fatal("revoked app must not be usable")
	}
	if apps := r.ForCapability("calendar.modify"); len(apps) != 0 {
		t.Fatalf("revoked app must not fulfil capabilities: %+v", apps)
	}
	a, _ := r.Get("google-calendar")
	if !a.Revoked || a.Status != StatusDisconnected {
		t.Fatalf("revoke must mark state: %+v", a)
	}
}

func TestConnectedAppCapabilityMapping(t *testing.T) {
	r := NewRegistry()
	r.Register(App{ID: "home-assistant", Capabilities: []string{"device.read", "device.control"}, Status: StatusConnected})
	r.Register(App{ID: "other", Capabilities: []string{"device.control"}, Status: StatusConnected})
	got := r.ForCapability("device.control")
	if len(got) != 2 {
		t.Fatalf("expected 2 apps for device.control, got %d", len(got))
	}
	// Deterministic ordering by ID.
	if got[0].ID != "home-assistant" || got[1].ID != "other" {
		t.Fatalf("ordering must be deterministic: %+v", got)
	}
}
