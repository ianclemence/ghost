package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/affect"
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

func TestAffectShowsNeutralOnFresh(t *testing.T) {
	out := runAffectHandler(t, t.TempDir())
	if !strings.Contains(out, "cordial") || !strings.Contains(out, "Turns together") {
		t.Fatalf("fresh state must render neutral: %q", out)
	}
	if strings.Contains(out, "sk-") {
		t.Fatal("affect display must never carry secret-shaped content")
	}
}

func TestAffectShowsAggregates(t *testing.T) {
	ws := t.TempDir()
	s := affect.New()
	s = s.Turn(0.9, 0.7, 0.9, false, time.Now())
	if err := affect.Save(ws, s); err != nil {
		t.Fatal(err)
	}
	out := runAffectHandler(t, ws)
	if !strings.Contains(out, "warm") {
		t.Fatalf("warm aggregate must display: %q", out)
	}
}

func TestResetContextClearsAffect(t *testing.T) {
	ws, _ := resetFixture(t)
	if err := affect.Save(ws, affect.New()); err != nil {
		t.Fatal(err)
	}
	out := runResetHandler(t, ws, "/reset context")
	if !strings.Contains(out, "Reset complete:") {
		t.Fatalf("expected completion, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(ws, "personal-context", "affect.json")); !os.IsNotExist(err) {
		t.Fatal("affect aggregate must die with /reset context")
	}
}
