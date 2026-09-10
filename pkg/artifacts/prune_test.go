package artifacts

import (
	"os"
	"path/filepath"
	"testing"
)

// PruneDangling removes only file artifacts whose backing file is gone; live
// files and text/link artifacts are never touched.
func TestPruneDangling(t *testing.T) {
	s, ws := testStore(t)
	writeWS(t, ws, "keep.md", "keep")
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "keep", Path: "keep.md"}); err != nil {
		t.Fatal(err)
	}
	writeWS(t, ws, "gone.md", "gone")
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "gone", Path: "gone.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "text", Title: "note", Text: "hi"}); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(ws, "gone.md")); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneDangling()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 pruned, got %d", n)
	}
	// The live file artifact and the text artifact remain.
	all, err := s.List("s", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 artifacts remaining, got %d", len(all))
	}
	for _, a := range all {
		if a.Title == "gone" {
			t.Fatal("dangling artifact must be pruned")
		}
	}
}
