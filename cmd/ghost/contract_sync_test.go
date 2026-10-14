package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func seedFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for n, c := range files {
		m["workspace/"+n] = &fstest.MapFile{Data: []byte(c)}
	}
	return m
}

func put(t *testing.T, ws, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, ws, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestContractFollowsReleaseForGhostOwnedFiles(t *testing.T) {
	ws := t.TempDir()
	put(t, ws, "GHOST.md", "old contract")
	rep, err := syncContract(seedFS(map[string]string{"GHOST.md": "new contract"}), "workspace", ws)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, ws, "GHOST.md") != "new contract" || len(rep.Updated) != 1 {
		t.Fatalf("GHOST.md did not follow the release: %+v", rep)
	}
	backups, _ := filepath.Glob(filepath.Join(ws, "state", "contract-backups", "GHOST.md.*"))
	if len(backups) != 1 {
		t.Fatal("the owner's differing version must be kept before it is replaced")
	}
}

func TestContractNeverOverwritesOwnerPersona(t *testing.T) {
	ws := t.TempDir()
	put(t, ws, "SOUL.md", "my own voice")
	rep, err := syncContract(seedFS(map[string]string{"SOUL.md": "stock voice"}), "workspace", ws)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, ws, "SOUL.md") != "my own voice" || len(rep.Kept) != 1 {
		t.Fatalf("an owner's persona was overwritten: %+v", rep)
	}
}

func TestContractUpdatesUntouchedPersonaOnLaterRelease(t *testing.T) {
	ws := t.TempDir()
	first := seedFS(map[string]string{"SOUL.md": "v1"})
	if _, err := syncContract(first, "workspace", ws); err != nil {
		t.Fatal(err)
	}
	if read(t, ws, "SOUL.md") != "v1" {
		t.Fatal("a missing file must be seeded")
	}
	if _, err := syncContract(seedFS(map[string]string{"SOUL.md": "v2"}), "workspace", ws); err != nil {
		t.Fatal(err)
	}
	if read(t, ws, "SOUL.md") != "v2" {
		t.Fatal("an unedited persona must follow the release")
	}
	put(t, ws, "SOUL.md", "edited")
	if _, err := syncContract(seedFS(map[string]string{"SOUL.md": "v3"}), "workspace", ws); err != nil {
		t.Fatal(err)
	}
	if read(t, ws, "SOUL.md") != "edited" {
		t.Fatal("an edit made after the last sync must survive")
	}
}
