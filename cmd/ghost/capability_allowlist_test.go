package main

import (
	"strings"
	"testing"
)

// The allowlist is the owner's "read but not write" control, so it has to
// store exactly what they meant: a typo must be refused (a silently wrong
// allowlist is a silent denial), and duplicates and blanks must not turn
// into phantom entries.
func TestNormalizeCapabilityAllowlist(t *testing.T) {
	got, err := normalizeCapabilityAllowlist([]string{" calendar.read ", "calendar.read", "", "email.read"})
	if err != nil {
		t.Fatalf("valid list refused: %v", err)
	}
	if len(got) != 2 || got[0] != "calendar.read" || got[1] != "email.read" {
		t.Fatalf("normalized = %v, want deduplicated, trimmed read capabilities", got)
	}

	if _, err := normalizeCapabilityAllowlist([]string{"calendar.raed"}); err == nil {
		t.Fatal("a misspelled capability must be refused, not stored as a silent denial")
	} else if !strings.Contains(err.Error(), "calendar.raed") {
		t.Fatalf("error should name the bad capability, got %v", err)
	}

	if empty, err := normalizeCapabilityAllowlist(nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty list = %v, %v; want the documented all-allowed", empty, err)
	}
}

// The control is only real if the capability IDs it accepts are the ones
// the broker enforces: the read/write pairs must exist in the registry.
func TestReadWriteCapabilitiesExist(t *testing.T) {
	for _, pair := range [][2]string{
		{"calendar.read", "calendar.modify"},
		{"email.read", "email.send"},
		{"device.read", "device.control"},
		{"browser.inspect", "browser.control"},
		{"file.read", "file.write"},
		{"memory.recall", "memory.remember"},
	} {
		if _, err := normalizeCapabilityAllowlist([]string{pair[0], pair[1]}); err != nil {
			t.Errorf("read/write pair %v not both known: %v", pair, err)
		}
	}
}
