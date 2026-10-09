package artifacts

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDeleteAVersionOrTheWholeThing(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ws := t.TempDir()
	st, err := NewStore(db, ws)
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pub := func(title, path string) *Artifact {
		write(path)
		a, err := st.Publish(Input{SessionKey: "main", Kind: "file", Title: title, Path: path})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	v1 := pub("Chess", "canvas/chess-v1.html")
	v2 := pub("Chess", "canvas/chess-v2.html")
	write("canvas/chess.saved.json")
	m1 := pub("Q3", "motion/q3-v1.json")
	write("motion/q3-v1.mp4")
	video := pub("Q3", "motion/q3-v1.mp4")
	up := pub("Report", "uploads/abc/report.pdf")

	if vs, _ := st.Versions(v2.ID); len(vs) != 2 {
		t.Fatalf("versions: %d", len(vs))
	}
	// One version: its file goes, the other stays.
	if n, err := st.Delete(v1.ID); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := os.Stat(filepath.Join(ws, "canvas/chess-v1.html")); !os.IsNotExist(err) {
		t.Fatal("the version's file stays")
	}
	if _, err := os.Stat(filepath.Join(ws, "canvas/chess-v2.html")); err != nil {
		t.Fatal("the other version went too")
	}
	// The whole thing: every version and what it kept.
	if n, _ := st.DeleteAll(v2.ID); n != 1 {
		t.Fatalf("delete all: %d", n)
	}
	if _, err := os.Stat(filepath.Join(ws, "canvas/chess.saved.json")); !os.IsNotExist(err) {
		t.Fatal("the canvas's saved data stays")
	}
	// A motion takes its video with it.
	if _, err := st.Delete(m1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(video.ID); err == nil {
		t.Fatal("the video is still listed")
	}
	if _, err := os.Stat(filepath.Join(ws, "motion/q3-v1.mp4")); !os.IsNotExist(err) {
		t.Fatal("the video file stays")
	}
	// An upload leaves the shelf but stays in Files.
	if _, err := st.Delete(up.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "uploads/abc/report.pdf")); err != nil {
		t.Fatal("an upload's file was deleted")
	}
}
