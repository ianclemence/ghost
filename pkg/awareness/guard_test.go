package awareness

import (
	"strings"
	"testing"
	"time"
)

func newTestGuard() (*Guard, *[]string, *time.Time) {
	var alerts []string
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	g := NewGuard(func(t string) { alerts = append(alerts, t) })
	g.now = func() time.Time { return now }
	return g, &alerts, &now
}

func TestHonestMistakesDoNotAlert(t *testing.T) {
	g, alerts, _ := newTestGuard()
	for i := 0; i < 4; i++ {
		g.Note(AuthFailed, "192.168.0.9")
	}
	g.Note(PairFailed, "192.168.0.9")
	g.Note(PairFailed, "192.168.0.9")
	if len(*alerts) != 0 {
		t.Fatalf("a few failures are a mistake, not an attack: %v", *alerts)
	}
}

func TestRepeatedFailuresAlertOnceThenStayQuiet(t *testing.T) {
	g, alerts, now := newTestGuard()
	for i := 0; i < 20; i++ {
		g.Note(AuthFailed, "192.168.0.66")
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "192.168.0.66") || !strings.Contains((*alerts)[0], "turned every attempt away") {
		t.Fatalf("want exactly one alert naming the source, got %v", *alerts)
	}
	*now = now.Add(40 * time.Minute)
	for i := 0; i < 6; i++ {
		g.Note(AuthFailed, "192.168.0.66")
	}
	if len(*alerts) != 2 {
		t.Fatalf("after the cooldown a continuing attack is reported again, got %d", len(*alerts))
	}
}

func TestPairingGuessingAndNetworkSweeps(t *testing.T) {
	g, alerts, _ := newTestGuard()
	for i := 0; i < 3; i++ {
		g.Note(PairFailed, "10.0.0.5")
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "pair") {
		t.Fatalf("three wrong pairing codes is guessing: %v", *alerts)
	}
	g2, alerts2, _ := newTestGuard()
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		g2.Note(AuthFailed, ip)
		g2.Note(AuthFailed, ip)
	}
	if len(*alerts2) != 1 || !strings.Contains((*alerts2)[0], "sweeping the network") {
		t.Fatalf("many sources failing at once is a sweep: %v", *alerts2)
	}
}

func TestOldFailuresAreForgotten(t *testing.T) {
	g, alerts, now := newTestGuard()
	for i := 0; i < 4; i++ {
		g.Note(AuthFailed, "10.0.0.7")
	}
	*now = now.Add(15 * time.Minute)
	g.Note(AuthFailed, "10.0.0.7")
	if len(*alerts) != 0 {
		t.Fatal("failures older than the window must not count")
	}
}

func TestSourceStripsThePort(t *testing.T) {
	if Source("192.168.0.9:51234") != "192.168.0.9" || Source("[::1]:80") != "::1" || Source("weird") != "weird" {
		t.Fatal("source parsing")
	}
}
