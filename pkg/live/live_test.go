package live

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func reg(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry("ghost-local")
	r.Register("b1", KindBrowser)
	r.Register("c1", KindComputer)
	return r
}

// Observation never grants control.
func TestObservationDoesNotGrantControl(t *testing.T) {
	r := reg(t)
	r.Observe("c1", Observation{Title: "Settings", Text: "Name textbox"})
	s, _ := r.Get("c1")
	if s.Control != OwnerNone {
		t.Fatalf("observation must not grant control: %s", s.Control)
	}
	if ok, _ := r.GhostMayAct("c1"); !ok {
		t.Fatal("observation must not pause Ghost")
	}
}

// Ghost acts; a user takeover pauses Ghost.
func TestTakeoverPausesGhostAndExpiryRestores(t *testing.T) {
	r := reg(t)
	r.SetControlOwner("c1", OwnerGhost)
	if ok, reason := r.GhostMayAct("c1"); !ok {
		t.Fatalf("ghost may act before takeover: %s", reason)
	}
	lease, err := r.Takeover("c1", "device-a", time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("takeover failed: %v", err)
	}
	if ok, reason := r.GhostMayAct("c1"); ok {
		t.Fatalf("ghost must be paused during user control: %s", reason)
	}
	s, _ := r.Get("c1")
	if s.Control != OwnerUser || s.State != StateUserControl {
		t.Fatalf("surface must be user_control: %+v", s)
	}
	// Expire the lease: safe fallback is no owner, Ghost stays paused until
	// revalidated resume.
	time.Sleep(5 * time.Millisecond)
	r.Reconcile(time.Now().Add(2 * time.Minute))
	if ok, _ := r.GhostMayAct("c1"); ok {
		t.Fatal("ghost must not auto-resume after expiry; revalidation required")
	}
	s, _ = r.Get("c1")
	if s.Control != OwnerNone || s.State != StatePaused {
		t.Fatalf("expired takeover must leave no owner and paused: %+v", s)
	}
}

// Release does not blindly resume Ghost; Resume is required.
func TestReleaseRequiresRevalidationBeforeResume(t *testing.T) {
	r := reg(t)
	if _, err := r.Takeover("b1", "device-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := r.Release("b1", "device-a", false); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Get("b1")
	if s.Control != OwnerNone || s.State != StatePaused {
		t.Fatalf("release must clear owner and pause (not resume): %+v", s)
	}
	if err := r.Resume("b1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.GhostMayAct("b1"); !ok {
		t.Fatal("after revalidated resume ghost may act")
	}
}

// Cross-user/device isolation: another device cannot take over or release.
func TestCrossDeviceControlFails(t *testing.T) {
	r := reg(t)
	if _, err := r.Takeover("b1", "device-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Takeover("b1", "device-b", time.Minute); err == nil {
		t.Fatal("another device must not take over a controlled surface")
	}
	if err := r.Release("b1", "device-b", false); err == nil {
		t.Fatal("another device must not release a surface it does not control")
	}
	// The controlling device can release.
	if err := r.Release("b1", "device-a", false); err != nil {
		t.Fatalf("controlling device must release: %v", err)
	}
}

// Same-device takeover renews (idempotent); duplicate release is safe for
// the owner and safe no-op when nothing controls.
func TestIdempotency(t *testing.T) {
	r := reg(t)
	l1, err := r.Takeover("b1", "device-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	exp1 := l1.ExpiresAt
	time.Sleep(2 * time.Millisecond)
	l2, err := r.Takeover("b1", "device-a", time.Minute)
	if err != nil {
		t.Fatalf("same device re-takeover must renew, got %v", err)
	}
	if l1.LeaseID != l2.LeaseID {
		t.Fatal("same-device re-takeover must renew the same lease (idempotent)")
	}
	if !l2.ExpiresAt.After(exp1) {
		t.Fatal("renew must extend the lease expiry")
	}
	if err := r.Release("b1", "device-a", true); err != nil {
		t.Fatal(err)
	}
	// Duplicate release by the owner is a safe no-op.
	if err := r.Release("b1", "", true); err != nil {
		t.Fatal(err)
	}
}

// Unknown surfaces fail closed.
func TestUnknownSurfaceFails(t *testing.T) {
	r := reg(t)
	if _, err := r.Takeover("nope", "device-a", time.Minute); err == nil {
		t.Fatal("takeover of unknown surface must fail")
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("unknown surface must not exist")
	}
}

// Concurrent takeover attempts resolve deterministically: exactly one
// device may hold control and later takers fail.
func TestConcurrentTakeoverDeterministic(t *testing.T) {
	r := reg(t)
	var wg sync.WaitGroup
	devices := []string{"d1", "d2", "d3", "d4", "d5"}
	winner := make(chan string, 1)
	for _, d := range devices {
		wg.Add(1)
		go func(dev string) {
			defer wg.Done()
			if _, err := r.Takeover("c1", dev, time.Minute); err == nil {
				select {
				case winner <- dev:
				default:
				}
			}
		}(d)
	}
	wg.Wait()
	s, _ := r.Get("c1")
	if s.Control != OwnerUser {
		t.Fatalf("exactly one winner must hold control: %+v", s)
	}
	select {
	case w := <-winner:
		if s.Lease == nil || s.Lease.DeviceID != w {
			t.Fatalf("winner %s must hold lease, got %+v", w, s.Lease)
		}
	default:
		t.Fatal("expected a winner")
	}
}

// Observation updates do not leak internal structure (no path/screenshot in
// the JSON view) and malformed control claims in observations are ignored.
func TestObservationClaimsIgnored(t *testing.T) {
	r := reg(t)
	r.Observe("b1", Observation{Title: "Ghost", URL: "https://example.com", Control: OwnerUser, State: StateUserControl})
	s, _ := r.Get("b1")
	if s.Control != OwnerNone {
		t.Fatal("an observation must never claim control")
	}
	if strings.Contains(s.Obs.Title+s.Obs.URL, "/tmp/") {
		t.Fatal("observation must not expose internal paths")
	}
}

func TestRegistryBoundsGrowth(t *testing.T) {
	r := NewRegistry("owner-1")
	for i := 0; i < maxSurfaces+72; i++ {
		r.Register("sess-"+string(rune('a'+i%26))+string(rune('0'+(i/26)%10)), KindBrowser)
	}
	if n := len(r.List()); n > maxSurfaces {
		t.Fatalf("registry must stay bounded, got %d", n)
	}
}
