package archtest

// Workspace semantic contract: the model-facing and human-facing files in
// workspace/ must preserve Ghost's authority boundaries. These tests guard
// the revamp invariants — knowledge/memory/skill/routine prose must never
// claim authority, reintroduce retired machinery, or handle raw secrets.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readWorkspaceFile(t *testing.T, rel ...string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(append([]string{repoRoot(t), "workspace"}, rel...)...))
	if err != nil {
		t.Fatalf("workspace/%s unreadable: %v", filepath.Join(rel...), err)
	}
	return src
}

func workspaceExists(rel ...string) bool {
	cwd, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(append([]string{cwd, "workspace"}, rel...)...))
	return err == nil
}

// The behavioral contract must teach runtime authority, never model
// sovereignty. SOUL and the identity record must agree.
func TestWorkspaceContract_NoSovereigntyInModelPrompt(t *testing.T) {
	for _, f := range [][]string{{"GHOST.md"}, {"SOUL.md"}, {"knowledge", "self", "identity.md"}} {
		if !workspaceExists(f...) {
			continue
		}
		src := string(readWorkspaceFile(t, f...))
		// Exact legacy sovereignty phrases (negations like 'do not assume
		// authority' are fine and expected).
		for _, banned := range []string{"Sovereign", "assume authority over local tools", "administrator of this", "Take action on the local system", "Do not ask for permission"} {
			if strings.Contains(src, banned) {
				t.Errorf("workspace/%s contains %q: the model operates within runtime authority, never above it",
					filepath.Join(f...), banned)
			}
		}
	}
	main := string(readWorkspaceFile(t, "GHOST.md"))
	if !strings.Contains(main, "not the authority") {
		t.Error("workspace/GHOST.md must state the model is not the authority")
	}
}

// No file may present cron as a live tool/system: one scheduler only.
func TestWorkspaceContract_NoCronToolLanguage(t *testing.T) {
	for _, f := range [][]string{{"GHOST.md"}, {"HEARTBEAT.md"}, {"README.md"}} {
		src := string(readWorkspaceFile(t, f...))
		for _, banned := range []string{"`cron`", "ghost cron", "crontab -e"} {
			if strings.Contains(src, banned) {
				t.Errorf("workspace/%s references %q: routines + scheduler only",
					filepath.Join(f...), banned)
			}
		}
	}
	root := repoRoot(t)
	err := filepath.Walk(filepath.Join(root, "workspace", "skills"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "SKILL.md" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, banned := range []string{"ghost cron", "crontab -e", "`cron`"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s references %q: routines + scheduler only", rel, banned)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Skills must never handle raw secrets or bypass approval. The patterns
// below are the dangerous practices themselves (store scraping, plaintext
// credential stores, token piping/URLs, unattended-execution flags) —
// prose that merely names a file to forbid it is fine.
func TestWorkspaceContract_NoCredentialHandlingInSkills(t *testing.T) {
	root := repoRoot(t)
	banned := []string{
		`grep "github.com" ~/.git-credentials`,
		`grep "github.com" "$HOME/.git-credentials"`,
		"credential.helper store",
		"auth login --with-token",
		"--yolo",
		"--full-auto",
	}
	err := filepath.Walk(filepath.Join(root, "workspace", "skills"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".sh") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s contains %q: credentials belong to the Vault, approval cannot be bypassed", rel, b)
			}
		}
		// Secret-bearing remote URLs (user:token@host) leak into shell
		// history and logs. Plain https://host/user/repo URLs are fine.
		for _, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "set-url") && strings.Contains(line, "https://") && strings.Contains(line, "@") {
				t.Errorf("%s embeds credentials in a remote URL: %q", rel, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// HEARTBEAT.md is advisory prose: it must not carry machine-parseable
// cron schedule lines (those would silently become real schedules).
func TestWorkspaceContract_HeartbeatHasNoMachineSchedules(t *testing.T) {
	cronLine := regexp.MustCompile(`-\s+(.+?):\s+([\d\*\/\-,]+\s+[\d\*\/\-,]+\s+[\d\*\/\-,]+\s+[\d\*\/\-,]+\s+[\d\*\/\-,]+)\s+"(.+?)"`)
	src := string(readWorkspaceFile(t, "HEARTBEAT.md"))
	if m := cronLine.FindString(src); m != "" {
		t.Errorf("workspace/HEARTBEAT.md contains a machine-parseable schedule line %q: routines own scheduling, prose must stay prose", m)
	}
}

// Retired knowledge scaffolding must stay retired; required contract
// files must exist.
func TestWorkspaceContract_KnowledgeTreeShape(t *testing.T) {
	for _, f := range [][]string{
		{"knowledge", "notes", "index.md"},
		{"knowledge", "notes", "areas.md"},
		{"knowledge", "self", "methodology.md"},
		{"knowledge", "notes", "references", "ghost-os.md"},
		{"knowledge", "notes", "system-architecture.md"},
	} {
		if workspaceExists(f...) {
			t.Errorf("workspace/%s exists: retired scaffolding must not return", filepath.Join(f...))
		}
	}
	for _, f := range [][]string{
		{"knowledge", "README.md"},
		{"knowledge", "self", "identity.md"},
		{"knowledge", "notes", "skill-observations.md"},
		{"knowledge", "notes", "wikilinks.md"},
		{"knowledge", "logs", "README.md"},
		{"knowledge", "ops", "inbox.md"},
	} {
		if !workspaceExists(f...) {
			t.Errorf("workspace/%s missing: required knowledge contract file", filepath.Join(f...))
		}
	}
}

// Phantom prompt files must not appear: only GHOST.md is the behavioral
// contract (case-sensitive), and no TOOLS.md is loaded by anything.
func TestWorkspaceContract_NoPhantomPromptFiles(t *testing.T) {
	for _, f := range [][]string{{"ghost.md"}, {"TOOLS.md"}} {
		if workspaceExists(f...) {
			t.Errorf("workspace/%s exists: nothing loads it, remove to avoid a competing prompt", filepath.Join(f...))
		}
	}
	if !workspaceExists([]string{"GHOST.md"}...) {
		t.Fatal("workspace/GHOST.md missing: the model-facing behavioral contract")
	}
}
