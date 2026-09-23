package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
)

// TestDraftLoopE2E exercises Phase B against the real configured model:
// seed evidence, draft, parse, verify. Requires DEEPSEEK_API_KEY (or any
// provider key visible to LoadConfig); skipped otherwise so CI stays hermetic.
func TestDraftLoopE2E(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("needs DEEPSEEK_API_KEY")
	}
	ws := t.TempDir()
	st, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := personalcontext.RawValue("launch Ghost")
	_, err = st.Create(personalcontext.Entry{ID: "goal-1", Kind: personalcontext.KindGoal,
		Subject: "user", Predicate: "goal/primary", Value: raw, Status: personalcontext.StatusCurrent,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceCommand, Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}}})
	if err != nil {
		t.Fatal(err)
	}
	sig := ideas.Signals{Now: time.Now().UTC()}
	for _, e := range st.Current() {
		sig.Memories = append(sig.Memories, ideas.MemoryFact{
			ID: e.ID, Predicate: e.Predicate, Value: personalcontext.Value(e), UpdatedAt: e.CreatedAt,
		})
	}
	cfg, err := config.LoadConfig(os.Getenv("GHOST_CONFIG_DIR") + "/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, err := providers.CreateProvider(cfg)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	ev, evidenceText := buildDraftEvidence(sig)
	if !strings.Contains(evidenceText, "goal-1") {
		t.Fatalf("evidence must cite the seeded goal:\n%s", evidenceText)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	text, err := ideas.DraftIdeas(ctx, p, cfg.Agents.Defaults.Model, evidenceText)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("empty draft response")
	}
	t.Logf("draft response:\n%s", text)
	for _, d := range ideas.ParseDrafts(text) {
		idea := ideas.VerifyDraft(d, ev, time.Now().UTC())
		if strings.Contains(idea.Title+idea.Body, "[memory:") || strings.Contains(idea.Title+idea.Body, "[routine:") {
			t.Fatalf("markers must strip from user prose: %+v", idea)
		}
		t.Logf("idea verified=%v unverified=%v sources=%d: %s", !idea.Unverified, idea.Unverified, len(idea.Sources), idea.Title)
	}
}
