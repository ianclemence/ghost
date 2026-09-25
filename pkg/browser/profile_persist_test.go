package browser

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *SessionStore {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "browser.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSessionStore(db, t.TempDir())
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	return s
}

func TestEnsureProfileDirIsOwnerOnlyAndContextIsolated(t *testing.T) {
	base := t.TempDir()
	personal, err := EnsureProfileDir(base, "context-personal", "default")
	if err != nil {
		t.Fatalf("EnsureProfileDir: %v", err)
	}
	work, err := EnsureProfileDir(base, "context-work", "default")
	if err != nil {
		t.Fatalf("EnsureProfileDir: %v", err)
	}
	if personal == work {
		t.Fatal("different contexts must not share a profile directory")
	}
	fi, err := os.Stat(personal)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("profile dir mode = %o, want 700 (cookie jars are the login)", fi.Mode().Perm())
	}
}

func TestPurgeProfileRevokesSignIn(t *testing.T) {
	s := newTestStore(t)
	sess, err := s.GetOrCreate("owner-1", "context-personal", "task-1", "default", 0)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if _, err := os.Stat(sess.Profile); err != nil {
		t.Fatalf("profile dir should exist after session creation: %v", err)
	}
	// Pretend the owner signed in: the cookie jar is a file in the profile.
	if err := os.WriteFile(filepath.Join(sess.Profile, "Cookies"), []byte("session-cookie"), 0o600); err != nil {
		t.Fatal(err)
	}

	closed, err := s.PurgeProfile("owner-1", "context-personal", "default")
	if err != nil {
		t.Fatalf("PurgeProfile: %v", err)
	}
	if closed == 0 {
		t.Error("purging should close the context's live sessions")
	}
	if _, err := os.Stat(sess.Profile); !os.IsNotExist(err) {
		t.Fatal("purge must delete the profile directory (cookie jar)")
	}
	if got, _ := s.Get(sess.ID); got != nil {
		t.Error("session row should be gone after purge")
	}
}

func TestGetOrCreateReusesProfileAcrossSessionsForSameContext(t *testing.T) {
	s := newTestStore(t)
	first, err := s.GetOrCreate("owner-1", "ctx", "task-a", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := ProfileDir(s.BaseDir(), "ctx", "default")
	if err != nil {
		t.Fatal(err)
	}
	if first.Profile != dir {
		t.Fatalf("session profile = %q, want the context dir %q", first.Profile, dir)
	}
}
