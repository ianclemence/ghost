package migrations

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

func TestFreshDBReachesHead(t *testing.T) {
	db := openTestDB(t)
	ms := []Migration{
		{Version: 1, Description: "one", Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE t1 (id TEXT PRIMARY KEY)`)
			return err
		}},
		{Version: 2, Description: "two", Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE t2 (id TEXT PRIMARY KEY)`)
			return err
		}},
	}
	v, err := Migrate(db, ms)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
	for _, tbl := range []string{"t1", "t2", "schema_migrations"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM `+tbl).Scan(&n); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}
	var uv int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&uv); err != nil || uv != 2 {
		t.Fatalf("user_version = %d, %v; want 2", uv, err)
	}
}

func TestMigrationsRunExactlyOnce(t *testing.T) {
	db := openTestDB(t)
	calls := 0
	ms := []Migration{{Version: 1, Description: "one", Up: func(tx *sql.Tx) error {
		calls++
		_, err := tx.Exec(`CREATE TABLE once_only (id TEXT PRIMARY KEY)`)
		return err
	}}}
	if _, err := Migrate(db, ms); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if _, err := Migrate(db, ms); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("migration ran %d times, want exactly once", calls)
	}
}

func TestFailureStopsAndRetries(t *testing.T) {
	db := openTestDB(t)
	execAll(t, db, `CREATE TABLE keep_me (id TEXT PRIMARY KEY)`, `INSERT INTO keep_me VALUES ('a')`)
	fail := true
	ms := []Migration{
		{Version: 1, Description: "ok", Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE m1 (id TEXT PRIMARY KEY)`)
			return err
		}},
		{Version: 2, Description: "boom", Up: func(tx *sql.Tx) error {
			if fail {
				return errors.New("simulated failure")
			}
			_, err := tx.Exec(`CREATE TABLE m2 (id TEXT PRIMARY KEY)`)
			return err
		}},
		{Version: 3, Description: "never reached", Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE m3 (id TEXT PRIMARY KEY)`)
			return err
		}},
	}
	if _, err := Migrate(db, ms); err == nil {
		t.Fatal("failing migration must error")
	} else if got := err.Error(); !strings.Contains(got, "migration 2 (boom)") {
		t.Fatalf("error must name the migration, got: %v", err)
	}
	// v2 must not be recorded; v1 must be; pre-existing data intact.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("recorded = %d, want exactly v1", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM keep_me`).Scan(&n); err != nil || n != 1 {
		t.Fatal("pre-existing data must survive failed migration")
	}
	// Retry after fixing succeeds and completes the chain.
	fail = false
	if v, err := Migrate(db, ms); err != nil || v != 3 {
		t.Fatalf("retry: v=%d err=%v, want v=3", v, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM m3`).Scan(&n); err != nil {
		t.Fatalf("m3 must exist after retry: %v", err)
	}
}

func TestRegistryValidation(t *testing.T) {
	db := openTestDB(t)
	dupes := []Migration{
		{Version: 1, Description: "a", Up: func(tx *sql.Tx) error { return nil }},
		{Version: 1, Description: "b", Up: func(tx *sql.Tx) error { return nil }},
	}
	if _, err := Migrate(db, dupes); err == nil {
		t.Fatal("duplicate versions must fail validation")
	}
	shape := []Migration{
		{Version: 1, Description: "neither"},
	}
	if _, err := Migrate(db, shape); err == nil {
		t.Fatal("migration without Up/UpDB must fail validation")
	}
	if _, err := Migrate(db, []Migration{
		{Version: 2, Description: "b", Up: func(tx *sql.Tx) error { return nil }},
		{Version: 1, Description: "a", Up: func(tx *sql.Tx) error { return nil }},
	}); err != nil {
		t.Fatalf("unsorted registry must still apply in order, got: %v", err)
	}
	v, err := CurrentVersion(db)
	if err != nil || v != 2 {
		t.Fatalf("CurrentVersion = %d, %v; want 2", v, err)
	}
}

