package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runAffectHandler(t *testing.T, ws string) string {
	t.Helper()
	var out string
	rt := &Runtime{Workspace: ws}
	req := Request{Text: "/affect", Reply: func(s string) error { out = s; return nil }}
	if err := affectHandler(context.Background(), req, rt); err != nil {
		t.Fatal(err)
	}
	return out
}

// Ghost no longer scores the tone of the owner's messages. /affect says so
// instead of showing numbers that nothing keeps.
func TestAffectSaysNothingIsScored(t *testing.T) {
	out := runAffectHandler(t, t.TempDir())
	if !strings.Contains(out, "does not score the tone") {
		t.Fatalf("/affect must say plainly that nothing is scored: %q", out)
	}
	for _, stale := range []string{"Affinity", "Turns together", "cordial"} {
		if strings.Contains(out, stale) {
			t.Fatalf("/affect still shows %q: %q", stale, out)
		}
	}
}

// An older install may still have the file; /reset context removes it too.
func TestResetContextClearsAnOldRelationalFile(t *testing.T) {
	ws, _ := resetFixture(t)
	old := filepath.Join(ws, "personal-context", "affect.json")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(`{"valence":-0.3,"affinity":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runResetHandler(t, ws, "/reset context")
	if !strings.Contains(out, "Reset complete:") {
		t.Fatalf("expected completion, got %q", out)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the old relational file must go with /reset context")
	}
}
