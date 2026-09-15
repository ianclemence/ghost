package db

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestIsDiskFullError(t *testing.T) {
	for _, msg := range []string{
		"database or disk is full",
		"disk I/O error",
		"SQLITE_FULL",
		"no space left on device",
		"write failed: ENOSPC",
	} {
		if !IsDiskFullError(errors.New(msg)) {
			t.Errorf("%q must read as disk-full", msg)
		}
	}
	if IsDiskFullError(errors.New("syntax error")) || IsDiskFullError(nil) {
		t.Fatal("unrelated errors must not read as disk-full")
	}
	if !IsDiskFullError(ErrDiskFull) {
		t.Fatal("ErrDiskFull must match itself")
	}
}

func TestTranslateErrorKeepsRemedy(t *testing.T) {
	err := TranslateError("migration", errors.New("database or disk is full"))
	if !errors.Is(err, ErrDiskFull) {
		t.Fatalf("must wrap ErrDiskFull: %v", err)
	}
	if got := err.Error(); !containsStr(got, "ghost state prune") {
		t.Fatalf("must name the remedy: %q", got)
	}
	if err := TranslateError("migration", errors.New("syntax error")); errors.Is(err, ErrDiskFull) {
		t.Fatal("unrelated errors must pass through untyped")
	}
}

func TestCheckIntegrity(t *testing.T) {
	ws := t.TempDir()
	d, err := NewDB(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckIntegrity(d.DB); err != nil {
		t.Fatalf("fresh database must verify: %v", err)
	}
	d.Close()
	// Corrupt the file: integrity must fail loudly with the restore path.
	path := filepath.Join(ws, "ghost.db")
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("garbage-not-sqlite"), 100); err != nil {
		t.Fatal(err)
	}
	f.Close()
	raw, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := CheckIntegrity(raw); err == nil {
		t.Fatal("corrupt database must fail integrity")
	} else if !containsStr(err.Error(), "ghost state import") {
		t.Fatalf("must name the restore runbook: %v", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
