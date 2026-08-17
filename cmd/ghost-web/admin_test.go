package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/appliance"
)

func TestLoginThrottleLockout(t *testing.T) {
	tl := newLoginThrottle()
	ip := "10.0.0.5"

	// First five attempts are allowed.
	for i := 0; i < maxLoginAttempts; i++ {
		if ok, _ := tl.allowed(ip); !ok {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
		tl.recordFailure(ip)
	}

	// Sixth attempt is blocked.
	if ok, wait := tl.allowed(ip); ok {
		t.Fatalf("attempt %d should be blocked", maxLoginAttempts+1)
	} else if wait <= 0 {
		t.Fatalf("expected positive cooldown wait, got %v", wait)
	}

	// Successful login resets the counter.
	tl.recordSuccess(ip)
	if ok, _ := tl.allowed(ip); !ok {
		t.Fatalf("should be allowed after success reset")
	}
}

func TestLoginThrottleDifferentIPs(t *testing.T) {
	tl := newLoginThrottle()
	for i := 0; i < maxLoginAttempts; i++ {
		tl.recordFailure("10.0.0.1")
	}
	if ok, _ := tl.allowed("10.0.0.2"); !ok {
		t.Fatalf("different IP should not be blocked")
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"short", "••••••••"},
		{"sk-abcdef1234567890", "••••••••7890"},
	}
	for _, c := range cases {
		if got := maskKey(c.in); got != c.want {
			t.Errorf("maskKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUpdateEnvFile(t *testing.T) {
	if fb == nil {
		fb = &appliance.SetupState{}
	}
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	fb.EnvPath = envPath

	if err := os.WriteFile(envPath, []byte("BRIDGE_SECRET=old\nTZ=UTC\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := updateEnvFile("DEEPSEEK_API_KEY", "newkey"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(envPath)
	got := string(b)
	for _, want := range []string{"BRIDGE_SECRET=old", "TZ=UTC", "DEEPSEEK_API_KEY=newkey"} {
		if !contains(got, want) {
			t.Errorf("env file missing %q:\n%s", want, got)
		}
	}

	// Updating an existing key should replace, not duplicate.
	if err := updateEnvFile("DEEPSEEK_API_KEY", "updated"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(envPath)
	got = string(b)
	if count := countOccurrences(got, "DEEPSEEK_API_KEY="); count != 1 {
		t.Errorf("expected 1 DEEPSEEK_API_KEY line, got %d:\n%s", count, got)
	}
	if !contains(got, "DEEPSEEK_API_KEY=updated") {
		t.Errorf("expected updated value:\n%s", got)
	}
}

func TestSkillDescriptionParsing(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	skillDir := filepath.Join(skillsDir, "testskill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: testskill
description: A test skill description.
---
# Real instructions here`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	original := fb.Workspace
	fb.Workspace = dir
	defer func() { fb.Workspace = original }()

	entries, err := os.ReadDir(workspaceSkillsDir())
	if err != nil {
		t.Fatal(err)
	}
	var desc string
	for _, e := range entries {
		if e.Name() != "testskill" {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(workspaceSkillsDir(), e.Name(), "SKILL.md"))
		desc = skillSummary(string(b))
	}
	if desc != "A test skill description." {
		t.Fatalf("got description %q, want %q", desc, "A test skill description.")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
