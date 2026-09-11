package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// resetFixture builds an isolated workspace + config dir and points
// GHOST_CONFIG_DIR at it so the handler can never touch the real repo.
func resetFixture(t *testing.T) (ws, cfgDir string) {
	t.Helper()
	ws = t.TempDir()
	cfgDir = filepath.Join(ws, "cfg")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHOST_CONFIG_DIR", cfgDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "deepseek"
	cfg.Agents.Defaults.Model = "deepseek/deepseek-flash"
	cfg.Agents.Defaults.FallbackModels = []string{"deepseek/deepseek-flash"}
	cfg.Agents.ModelList = []config.ModelPreset{{Name: "local", Provider: "ollama", Model: "qwen3:0.6b"}}
	if err := config.SaveConfig(filepath.Join(cfgDir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, ".secrets.json"), []byte(`{"k":"v"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "USER.md"), []byte("# User\n- **Name**: Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	memDir := filepath.Join(ws, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Memory\n- old fact\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return ws, cfgDir
}

func runResetHandler(t *testing.T, ws, text string) string {
	t.Helper()
	var out string
	rt := &Runtime{Workspace: ws}
	req := Request{
		Text:       text,
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "s1",
		Reply: func(s string) error {
			out = s
			return nil
		},
	}
	if err := resetHandler(context.Background(), req, rt); err != nil {
		t.Fatalf("resetHandler(%q): %v", text, err)
	}
	return out
}

// /reset all wipes everything: secrets gone, identity doc re-templated,
// model default back to local.
func TestResetAllWipesEverything(t *testing.T) {
	ws, cfgDir := resetFixture(t)

	out := runResetHandler(t, ws, "/reset all")
	if !strings.Contains(out, "Reset complete:") {
		t.Fatalf("expected completion, got %q", out)
	}
	for _, scope := range []string{"Chats:", "Memory:", "Activity:", "Automations:", "Personal Context:", "Paired devices:", "Secrets:"} {
		if !strings.Contains(out, scope) {
			t.Errorf("expected %q in output %q", scope, out)
		}
	}
	if _, err := os.Stat(filepath.Join(cfgDir, ".secrets.json")); !os.IsNotExist(err) {
		t.Error("secrets file must be deleted by /reset all")
	}
	userDoc, err := os.ReadFile(filepath.Join(ws, "USER.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(userDoc) != UserDocTemplate {
		t.Errorf("USER.md must be restored to template, got %q", userDoc)
	}
	got, err := config.LoadConfig(filepath.Join(cfgDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents.Defaults.Provider != "ollama" {
		t.Errorf("model default must be local after reset, got %q", got.Agents.Defaults.Provider)
	}
}

// /reset all --exclude spares the named scopes.
func TestResetAllExclude(t *testing.T) {
	ws, cfgDir := resetFixture(t)

	out := runResetHandler(t, ws, "/reset all --exclude=devices,secrets")
	if !strings.Contains(out, "Reset complete:") {
		t.Fatalf("expected completion, got %q", out)
	}
	if strings.Contains(out, "Secrets:") || strings.Contains(out, "Paired devices:") {
		t.Errorf("excluded scopes must not run: %q", out)
	}
	if !strings.Contains(out, "Kept (--exclude): devices, secrets") {
		t.Errorf("expected kept list, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, ".secrets.json")); err != nil {
		t.Error("excluded secrets file must survive")
	}
}

// Selective reset touches only the named scope and never secrets.
func TestResetSelectiveKeepsSecrets(t *testing.T) {
	ws, cfgDir := resetFixture(t)

	out := runResetHandler(t, ws, "/reset chats memory")
	if !strings.Contains(out, "Chats:") || !strings.Contains(out, "Memory:") {
		t.Fatalf("expected chats+memory results, got %q", out)
	}
	if strings.Contains(out, "Secrets:") {
		t.Errorf("selective reset must not touch secrets: %q", out)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, ".secrets.json")); err != nil {
		t.Error("secrets file must survive selective reset")
	}
}

// /reset secrets wipes keys on its own.
func TestResetSecretsScope(t *testing.T) {
	ws, cfgDir := resetFixture(t)

	out := runResetHandler(t, ws, "/reset secrets")
	if !strings.Contains(out, "Secrets:") {
		t.Fatalf("expected secrets result, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, ".secrets.json")); !os.IsNotExist(err) {
		t.Error("secrets file must be deleted by /reset secrets")
	}
}

// Bare /reset shows help instead of destroying anything.
func TestResetBareShowsHelp(t *testing.T) {
	ws, cfgDir := resetFixture(t)

	out := runResetHandler(t, ws, "/reset")
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "/reset all") {
		t.Fatalf("expected help, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, ".secrets.json")); err != nil {
		t.Error("help must not delete anything")
	}
}

// Unknown scopes and legacy flags are rejected, never executed.
func TestResetRejectsUnknown(t *testing.T) {
	ws, cfgDir := resetFixture(t)

	for _, text := range []string{"/reset bogus", "/reset --yes", "/reset all --yes", "/reset all --include-secrets"} {
		out := runResetHandler(t, ws, text)
		if strings.Contains(out, "Reset complete:") {
			t.Errorf("%q must not execute, got %q", text, out)
		}
		if _, err := os.Stat(filepath.Join(cfgDir, ".secrets.json")); err != nil {
			t.Fatalf("%q must not delete anything", text)
		}
	}
}

// Aliases canonicalize to the same scopes.
func TestResetAliases(t *testing.T) {
	ws, _ := resetFixture(t)

	out := runResetHandler(t, ws, "/reset sessions events cron")
	if !strings.Contains(out, "Chats:") || !strings.Contains(out, "Activity:") || !strings.Contains(out, "Automations:") {
		t.Fatalf("aliases must canonicalize, got %q", out)
	}
}

// EnsureUserDoc seeds the template when missing and never clobbers.
func TestEnsureUserDoc(t *testing.T) {
	ws := t.TempDir()
	if err := EnsureUserDoc(ws); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "USER.md"))
	if err != nil || string(data) != UserDocTemplate {
		t.Fatalf("template must be seeded, got %q / %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(ws, "USER.md"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureUserDoc(ws); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(ws, "USER.md"))
	if string(data) != "mine" {
		t.Fatal("existing USER.md must never be overwritten")
	}
}

// A reset must be structurally unable to touch another installation's
// keys (regression: the old directory sweep deleted a live appliance's
// secrets from a unit test run).
func TestResetSecretsNeverLeavesActiveConfig(t *testing.T) {
	ws, _ := resetFixture(t)
	decoy := t.TempDir()
	for _, name := range []string{".secrets.json", ".env", ".master-key"} {
		if err := os.WriteFile(filepath.Join(decoy, name), []byte("decoy"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := runResetHandler(t, ws, "/reset all")
	if !strings.Contains(out, "Reset complete:") {
		t.Fatalf("expected completion, got %q", out)
	}
	for _, name := range []string{".secrets.json", ".env", ".master-key"} {
		if _, err := os.Stat(filepath.Join(decoy, name)); err != nil {
			t.Errorf("decoy installation file %q must survive: %v", name, err)
		}
	}
}

// RunReset is the CLI entry point; it must return the same reply as the
// chat handler so a CLI reset reports what happened.
func TestRunResetReturnsReply(t *testing.T) {
	ws, _ := resetFixture(t)
	out, err := RunReset(context.Background(), &Runtime{Workspace: ws}, "/reset chats")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Chats:") {
		t.Fatalf("RunReset must return the handler reply, got %q", out)
	}
}
