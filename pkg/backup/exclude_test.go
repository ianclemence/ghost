package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExclusions(t *testing.T) {
	excluded := []string{
		"config/.secrets.json",
		"config/.env",
		".env",
		"data/.gcalcli_oauth",
		".credentials/calendar-token.json",
		"config/.credentials/oauthtoken",
		"gcalcli/oauth",
		"workspace/memory/x.log",
		"config/sub/certificate.secrets.json",
	}
	for _, p := range excluded {
		if ok, _ := ShouldExclude(p); !ok {
			t.Fatalf("%s must be excluded", p)
		}
	}
	kept := []string{
		"config/config.json",
		"workspace/memory/MEMORY.md",
		"workspace/skills/weather/SKILL.md",
		"data/ghost.db",
		"pending/requests.json",
	}
	for _, p := range kept {
		if ok, reason := ShouldExclude(p); ok {
			t.Fatalf("%s must be kept, got %s", p, reason)
		}
	}
}

func TestSkipDirs(t *testing.T) {
	for _, d := range []string{".credentials", ".calendar", "sessions", "state", "journal"} {
		if !ShouldSkipDir(d) {
			t.Fatalf("%s must be pruned", d)
		}
	}
	if ShouldSkipDir("memory") || ShouldSkipDir("skills") {
		t.Fatal("user data dirs must not be pruned")
	}
}

// TestSyntheticTree builds a fake Ghost root with credential files in
// every known location and asserts a walker using ShouldExclude archives
// zero secrets.
func TestSyntheticTree(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"config/config.json":               `{"ok":true}`,
		"config/.secrets.json":             `{"ProviderAPIKeys":{"x":"SECRET-1"}}`,
		"config/.env":                      `AVIATION_API_KEY=SECRET-2`,
		".credentials/calendar-token.json": `{"refresh_token":"SECRET-3"}`,
		"workspace/.calendar/oauth":        `SECRET-4`,
		"workspace/memory/MEMORY.md":       `likes tea`,
		"workspace/sessions/s1.json":       `transient`,
		"workspace/state/rt.json":          `transient`,
		"data/app.log":                     `transient`,
		"pending/requests.json":            `[{"intent":"calendar tomorrow"}]`,
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var archived []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			if ShouldSkipDir(filepath.Base(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if ok, _ := ShouldExclude(filepath.ToSlash(rel)); ok {
			return nil
		}
		data, _ := os.ReadFile(path)
		archived = append(archived, string(data))
		return nil
	})
	blob := strings.Join(archived, "\n")
	for _, secret := range []string{"SECRET-1", "SECRET-2", "SECRET-3", "SECRET-4"} {
		if strings.Contains(blob, secret) {
			t.Fatalf("backup contains %s", secret)
		}
	}
	if !strings.Contains(blob, "likes tea") || !strings.Contains(blob, `"ok":true`) {
		t.Fatal("user data and config must be archived")
	}
}
