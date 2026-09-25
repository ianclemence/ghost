package agent

import "testing"

// Answering the location question teaches Ghost where "here" is, so the next
// "what's the weather here" doesn't ask again.
func TestRememberHomeLocationResolvesHere(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	if got := al.knownLocation("s1"); got != "" {
		t.Fatalf("expected no location yet, got %q", got)
	}
	al.rememberHomeLocation("s1", "Bangkok", "bangkok")
	if got := al.knownLocation("s1"); got != "Bangkok" {
		t.Fatalf("knownLocation = %q, want Bangkok", got)
	}
	al.rememberHomeLocation("s1", "Chiang Mai", "chiang mai")
	if got := al.knownLocation("s1"); got != "Chiang Mai" {
		t.Fatalf("a new location must supersede, got %q", got)
	}
	al.rememberHomeLocation("s1", "", "   ")
	if got := al.knownLocation("s1"); got != "Chiang Mai" {
		t.Fatalf("an empty answer must not erase the stored location, got %q", got)
	}
}
