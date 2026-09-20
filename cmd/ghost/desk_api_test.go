package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/desk"
	_ "modernc.org/sqlite"
)

// TestGatherWorkspaceDocumentsRespectsEstate verifies the Desk never turns an
// internal estate file into an owner-visible document, while ordinary
// workspace files do surface.
func TestGatherWorkspaceDocumentsRespectsEstate(t *testing.T) {
	ws := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("reports/q1.md")
	mk("notes/todo.md")
	// Protected estate:
	mk("personal-context/entries.jsonl")
	mk("state/session.json")
	mk("events/2026-09-01.ndjson")
	mk("knowledge/self/user-profile.md")
	mk("ghost.db")

	prev := apiWorkspaceDir
	apiWorkspaceDir = ws
	defer func() { apiWorkspaceDir = prev }()

	docs := gatherWorkspaceDocuments()
	seen := map[string]bool{}
	for _, d := range docs {
		seen[d.RelPath] = true
	}
	if !seen["reports/q1.md"] || !seen["notes/todo.md"] {
		t.Errorf("ordinary workspace files must surface, got %v", seen)
	}
	for _, p := range []string{"personal-context/entries.jsonl", "state/session.json", "events/2026-09-01.ndjson", "knowledge/self/user-profile.md", "ghost.db"} {
		if seen[p] {
			t.Errorf("protected path %q must never surface", p)
		}
	}
}

// TestGatherDeskToolsOnlyNonBundled verifies the Desk shows workspace tools
// that Ghost/owner built (non-bundled), not ghost's built-in abilities.
func TestGatherDeskToolsOnlyNonBundled(t *testing.T) {
	ws := t.TempDir()
	skillsDir := filepath.Join(ws, "skills")
	writeSkill := func(name, body string) {
		dir := filepath.Join(skillsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("my-tracker", "# My Tracker\n\nTracks my spending each week.")
	writeSkill("weather", "# Weather\n\nBundled ability.")

	prev := apiWorkspaceDir
	apiWorkspaceDir = ws
	defer func() { apiWorkspaceDir = prev }()

	// No manifest => nothing is bundled, so both surface. Marking weather
	// bundled requires a manifest; write one to prove the distinction.
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"skills":{"weather":{"origin":"abc"}}}`
	if err := os.WriteFile(filepath.Join(skillsDir, ".bundled_manifest"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	tools := gatherDeskTools()
	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.Name] = true
	}
	if !names["my-tracker"] {
		t.Errorf("owner-built tool must surface, got %v", names)
	}
	if names["weather"] {
		t.Errorf("bundled ability must not surface as an owner tool, got %v", names)
	}
}

// TestGatherDeskArtifactsCrossConversation verifies the Desk surfaces
// artifacts from every conversation through the new ListAll authority.
func TestGatherDeskArtifactsCrossConversation(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "report.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(artifacts.Input{SessionKey: "mobile:default", Kind: "file", Title: "Report", Path: "report.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(artifacts.Input{SessionKey: "other:chat", Kind: "text", Title: "Note", Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()

	arts := gatherDeskArtifacts()
	if len(arts) != 2 {
		t.Fatalf("Desk must surface artifacts across conversations, got %d", len(arts))
	}
}

// TestDeskFeedEndToEnd exercises the full projection: gather -> normalize,
// proving an owner sees files, tools, and artifacts in one deterministic feed
// with no protected item.
func TestDeskFeedEndToEnd(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	mustWrite := func(rel, body string) {
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("reports/plan.md", "# Plan")
	mustWrite("personal-context/entries.jsonl", "secret")
	mustWrite("skills/helper/SKILL.md", "# Helper\n\nHelps.")

	st, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(artifacts.Input{SessionKey: "mobile:default", Kind: "file", Title: "Plan", Path: "reports/plan.md"}); err != nil {
		t.Fatal(err)
	}

	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()

	items := desk.List(gatherDeskInputs(nil))
	kinds := map[desk.Kind]int{}
	for _, it := range items {
		kinds[it.Kind]++
		if it.Protected {
			t.Errorf("a surfaced item must never be protected: %+v", it)
		}
		if it.ID == "" || it.Title == "" {
			t.Errorf("item missing identity: %+v", it)
		}
	}
	if kinds[desk.KindDocument] != 1 {
		t.Errorf("want 1 document (plan.md), got %d", kinds[desk.KindDocument])
	}
	if kinds[desk.KindArtifact] != 1 {
		t.Errorf("want 1 artifact, got %d", kinds[desk.KindArtifact])
	}
	if kinds[desk.KindTool] != 1 {
		t.Errorf("want 1 tool (helper), got %d", kinds[desk.KindTool])
	}
}
