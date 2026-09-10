package tools

import (
	"os/exec"
	"strings"
	"testing"
)

func TestIsSecretEnvName(t *testing.T) {
	secret := []string{"DEEPSEEK_API_KEY", "GHOST_TOKEN", "OPENAI_API_KEY", "DB_PASSWORD",
		"AWS_SECRET_ACCESS_KEY", "SESSION_ID", "AUTH_HEADER", "SOME_CREDENTIAL"}
	for _, n := range secret {
		if !IsSecretEnvName(n) {
			t.Fatalf("%s must be treated as secret", n)
		}
	}
	safe := []string{"PATH", "HOME", "LANG", "TZ", "TERM", "USER"}
	for _, n := range safe {
		if IsSecretEnvName(n) {
			t.Fatalf("%s must not be treated as secret", n)
		}
	}
}

func TestEnvGuardStripsSecrets(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "super-secret")
	t.Setenv("GHOST_SECRET", "nope")
	t.Setenv("PATH", "/usr/bin:/bin")

	env := EnvGuard{}.Env("/tmp/ws")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "super-secret") || strings.Contains(joined, "nope") {
		t.Fatalf("secret leaked into subprocess env:\n%s", joined)
	}
	if !strings.Contains(joined, "HOME=/tmp/ws") {
		t.Fatalf("HOME must be confined to workspace:\n%s", joined)
	}
	if !strings.Contains(joined, "PATH=") {
		t.Fatal("PATH must be preserved")
	}
}

func TestEnvGuardScreensExtras(t *testing.T) {
	env := EnvGuard{Extra: []string{"FOO=bar", "API_KEY=evil"}}.Env("/tmp/ws")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "FOO=bar") {
		t.Fatal("safe extra must pass")
	}
	if strings.Contains(joined, "API_KEY") {
		t.Fatalf("secret-shaped extra must be dropped:\n%s", joined)
	}
}

func TestHardenCommandNeverInherits(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "leak-me")
	cmd := exec.Command("true")
	HardenCommand(cmd, "/tmp/ws", nil)
	for _, kv := range cmd.Env {
		if strings.Contains(kv, "leak-me") {
			t.Fatal("HardenCommand must not inherit secrets")
		}
	}
	if cmd.Dir != "/tmp/ws" {
		t.Fatalf("expected workspace dir, got %q", cmd.Dir)
	}
}
