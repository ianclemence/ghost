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

// The Desk must surface only things Ghost made for the owner — never raw
// workspace files. An earlier version walked the workspace and exposed
// internal files (proactive outboxes, logs) and sizes; that is not a product.
func TestDeskInputsAreArtifactsOnly(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	// An internal-looking file in the workspace must NOT become a Desk item.
	if err := os.WriteFile(filepath.Join(ws, "outbox.jsonl"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "report.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(artifacts.Input{SessionKey: "mobile:default", Kind: "file", Title: "Report", Path: "report.md"}); err != nil {
		t.Fatal(err)
	}

	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()

	items := desk.List(gatherDeskInputs())
	if len(items) != 1 {
		t.Fatalf("want exactly the 1 artifact, got %d: %+v", len(items), items)
	}
	if items[0].Title != "Report" {
		t.Errorf("wrong item surfaced: %+v", items[0])
	}
	for _, it := range items {
		if it.Title == "outbox.jsonl" {
			t.Errorf("an internal workspace file must never surface")
		}
	}
}

func TestDeskSurfacesArtifactsAcrossConversations(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(artifacts.Input{SessionKey: "mobile:default", Kind: "file", Title: "One", Path: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(artifacts.Input{SessionKey: "other:chat", Kind: "text", Title: "Two", Text: "hi"}); err != nil {
		t.Fatal(err)
	}

	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()

	if got := len(gatherDeskInputs().Artifacts); got != 2 {
		t.Fatalf("Desk must be cross-conversation, got %d", got)
	}
}
