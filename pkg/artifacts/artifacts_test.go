package artifacts

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	ws := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewStore(db, ws)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, ws
}

func writeWS(t *testing.T, ws, rel, content string) {
	t.Helper()
	p := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestPublishFileRoundTrip(t *testing.T) {
	s, ws := testStore(t)
	writeWS(t, ws, "report.md", "# Report\nnumbers")
	a, err := s.Publish(Input{SessionKey: "mobile:default", Kind: "file", Title: "Report", Path: "report.md"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if a.State != StateAvailable || a.Path != "report.md" {
		t.Fatalf("unexpected artifact: %+v", a)
	}
	if len(a.Actions) == 0 {
		t.Fatalf("file artifact must declare render actions")
	}
	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Report" || got.State != StateAvailable {
		t.Fatalf("unexpected reload: %+v", got)
	}
	list, err := s.List("mobile:default", 50)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v %d", err, len(list))
	}
	// Unknown sessions see nothing: conversation isolation.
	other, err := s.List("other-session", 50)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-session leak: %v %d", err, len(other))
	}
}

func TestPublishRejectsTraversal(t *testing.T) {
	s, _ := testStore(t)
	for _, p := range []string{"../evil.md", "..\\evil.md", "/abs/path.md", "", "."} {
		if _, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "x", Path: p}); err == nil {
			t.Fatalf("path %q must be rejected", p)
		}
	}
}

func TestPublishRejectsProtectedEstate(t *testing.T) {
	s, ws := testStore(t)
	protected := []string{
		"personal-context/entries.jsonl",
		"state/ghost.db",
		"events/2026-01-01.ndjson",
		"knowledge/self/notes.md",
		"data/vault.db",
	}
	for _, rel := range protected {
		writeWS(t, ws, rel, "secret")
		if _, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "x", Path: rel}); err == nil {
			t.Fatalf("protected path %q must be rejected", rel)
		}
	}
}

func TestPublishRejectsMissingFile(t *testing.T) {
	s, _ := testStore(t)
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "x", Path: "nope.md"}); err == nil {
		t.Fatalf("missing file must be rejected: model cannot claim what does not exist")
	}
}

func TestPublishTextAndLink(t *testing.T) {
	s, _ := testStore(t)
	a, err := s.Publish(Input{SessionKey: "s", Kind: "text", Title: "Summary", Text: "hello"})
	if err != nil {
		t.Fatalf("Publish text: %v", err)
	}
	if a.Text != "hello" || len(a.Actions) != 0 {
		t.Fatalf("unexpected text artifact: %+v", a)
	}
	l, err := s.Publish(Input{SessionKey: "s", Kind: "link", Title: "Docs", URL: "https://example.com/x"})
	if err != nil {
		t.Fatalf("Publish link: %v", err)
	}
	if l.URL == "" || len(l.Actions) != 1 || l.Actions[0].Kind != ActionOpen {
		t.Fatalf("unexpected link artifact: %+v", l)
	}
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "link", Title: "x", URL: "javascript:alert(1)"}); err == nil {
		t.Fatalf("non-http link must be rejected")
	}
}

func TestPublishRequiresExactlyOnePayload(t *testing.T) {
	s, ws := testStore(t)
	writeWS(t, ws, "a.md", "x")
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "x"}); err == nil {
		t.Fatalf("file without path must be rejected")
	}
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "mystery", Title: "x", Text: "y"}); err == nil {
		t.Fatalf("unknown kind must be rejected")
	}
	if _, err := s.Publish(Input{SessionKey: "", Kind: "text", Title: "x", Text: "y"}); err == nil {
		t.Fatalf("missing session must be rejected")
	}
	if _, err := s.Publish(Input{SessionKey: "s", Kind: "text", Title: "", Text: "y"}); err == nil {
		t.Fatalf("missing title must be rejected")
	}
}

func TestGetMarksDeletedFileUnavailable(t *testing.T) {
	s, ws := testStore(t)
	writeWS(t, ws, "temp.md", "x")
	a, err := s.Publish(Input{SessionKey: "s", Kind: "file", Title: "x", Path: "temp.md"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := os.Remove(filepath.Join(ws, "temp.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != StateUnavailable || got.Reason == "" {
		t.Fatalf("deleted file must surface as unavailable with reason: %+v", got)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("unavailable artifact must offer no actions")
	}
}

func TestPublishRedactsSecrets(t *testing.T) {
	s, _ := testStore(t)
	a, err := s.Publish(Input{SessionKey: "s", Kind: "text", Title: "x", Text: "key is sk-live-0123456789abcdef"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if a.Text == "key is sk-live-0123456789abcdef" {
		t.Fatalf("secret-shaped text must be redacted before persistence")
	}
}

func TestGetUnknownIsNotFound(t *testing.T) {
	s, _ := testStore(t)
	if _, err := s.Get("art-does-not-exist"); err == nil {
		t.Fatalf("unknown id must fail closed")
	}
}
