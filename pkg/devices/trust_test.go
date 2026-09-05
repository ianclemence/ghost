package devices

import (
	"testing"
)

func TestRegisterRejectsUnrestricted(t *testing.T) {
	for _, bad := range []string{"shell", "exec", "root", "unrestricted-control"} {
		if _, err := Register("g1", ClassComputer, "Pi", []string{bad}); err == nil {
			t.Fatalf("%q must be rejected at registration", bad)
		}
	}
	d, err := Register("g1", ClassComputer, "Pi", []string{"system.status", "files.read"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Trust != TrustPaired {
		t.Fatal("new devices start paired, never trusted")
	}
}

func TestCanInvoke(t *testing.T) {
	d, _ := Register("g1", ClassHub, "Hub", []string{"hass.control"})
	if d.CanInvoke("hass.control") {
		t.Fatal("offline device must not invoke")
	}
	d.Connection = ConnLocal
	if !d.CanInvoke("hass.control") {
		t.Fatal("paired+connected+declared must invoke")
	}
	if d.CanInvoke("shell") {
		t.Fatal("undeclared capability must not invoke")
	}
	d.Trust = TrustRevoked
	if d.CanInvoke("hass.control") {
		t.Fatal("revoked must not invoke")
	}
}

func TestTrustLattice(t *testing.T) {
	d, _ := Register("g1", ClassPhone, "Phone", nil)
	// paired → trusted is the explicit elevation step.
	if err := d.TrustTo(TrustTrusted); err != nil {
		t.Fatalf("explicit elevation must work: %v", err)
	}
	// Trusted never demotes silently.
	if err := d.TrustTo(TrustPaired); err == nil {
		t.Fatal("silent demotion must fail")
	}
	// Anything → revoked always works.
	if err := d.TrustTo(TrustRevoked); err != nil {
		t.Fatal(err)
	}
	// Unknown devices cannot jump straight to trusted.
	u := &Device{Trust: TrustUnknown}
	if err := u.TrustTo(TrustTrusted); err == nil {
		t.Fatal("unknown→trusted must fail")
	}
	if err := u.TrustTo(TrustPaired); err != nil {
		t.Fatal("unknown→paired (explicit pairing) must work")
	}
}
