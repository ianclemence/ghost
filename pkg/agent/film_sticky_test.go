package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/film"
)

// A follow-up after a drawing ("where is it?", "make the sky darker") must
// keep the film tool. The router only sees the follow-up's words, so without
// recent-film state the tool disappears between turns.
func TestRecentFilmWorkKeepsTheToolAfterADrawing(t *testing.T) {
	dir := t.TempDir()
	al := &AgentLoop{workspace: dir}
	if al.recentFilmWork() {
		t.Fatal("no film has been made yet")
	}
	page := filepath.Join(dir, film.Dir, "fox", "fox.html")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !al.recentFilmWork() {
		t.Fatal("a just-drawn film must keep the film tool available")
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(page, old, old); err != nil {
		t.Fatal(err)
	}
	if al.recentFilmWork() {
		t.Fatal("an old film must not keep the tool on every turn")
	}
}
