package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/awareness"
)

// Repeated failed access from another machine on the network reaches the
// owner; the same failures from the Pod itself never do.
func TestRepeatedFailedAccessFromTheNetworkIsReported(t *testing.T) {
	var alerts []string
	old := securityGuard
	securityGuard = awareness.NewGuard(func(text string) { alerts = append(alerts, text) })
	defer func() { securityGuard = old }()

	h := authMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	hit := func(remote string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/files", nil)
		req.RemoteAddr = remote
		req.Host = "127.0.0.1:8766"
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec.Code
	}
	for i := 0; i < 8; i++ {
		if code := hit("127.0.0.1:5000"); code != 200 {
			t.Fatalf("loopback is the Pod itself and is never turned away: %d", code)
		}
	}
	if len(alerts) != 0 {
		t.Fatal("the Pod's own requests must never raise a security alert")
	}
	for i := 0; i < 6; i++ {
		if code := hit("192.168.0.66:40000"); code != http.StatusUnauthorized {
			t.Fatalf("an anonymous LAN request must be refused, got %d", code)
		}
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "192.168.0.66") {
		t.Fatalf("the owner must hear about the repeated attempts, once: %v", alerts)
	}
}

func TestFirstNoteLine(t *testing.T) {
	if got := firstNoteLine("Release notes\n- **Lists stay apart.** Bullets no longer run together.\n- more"); got != "Lists stay apart. Bullets no longer run together." {
		t.Fatalf("got %q", got)
	}
	if firstNoteLine("") != "" {
		t.Fatal("empty notes")
	}
}
