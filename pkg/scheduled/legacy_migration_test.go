package scheduled

import (
	"os"
	"path/filepath"
	"testing"
)

func newMigrateStore(t *testing.T) *Store {
	t.Helper()
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	s := NewStore(db)
	if err := s.InitSchema(); err != nil {
		t.Fatal(err)
	}
	return s
}

const legacyJobsJSON = `{
  "version": 1,
  "jobs": [
    {"id":"j1","name":"Morning","enabled":true,"lifecycle_state":"active",
     "schedule":{"kind":"cron","expr":"0 8 * * *","tz":"UTC"},
     "payload":{"message":"morning briefing","deliver":true,"channel":"telegram","to":"123"}},
    {"id":"j2","name":"Ping","enabled":true,"lifecycle_state":"active",
     "schedule":{"kind":"every","everyMs":300000,"tz":"UTC"},
     "payload":{"message":"ping the server","deliver":false,"channel":"cli","to":"direct"}},
    {"id":"j3","name":"Paused","enabled":false,"lifecycle_state":"paused",
     "schedule":{"kind":"at","atMs":1789000000000,"tz":"UTC"},
     "payload":{"message":"one off","deliver":true,"channel":"cli","to":"direct"}}
  ]
}`

func TestMigrateLegacyCronIdempotent(t *testing.T) {
	store := newMigrateStore(t)
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte(legacyJobsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyCron(path, store)
	if err != nil {
		t.Fatal(err)
	}
	if res.Migrated != 3 || len(res.Errors) != 0 {
		t.Fatalf("first pass: migrated=%d errors=%v", res.Migrated, res.Errors)
	}

	// Second pass must not duplicate.
	res2, err := MigrateLegacyCron(path, store)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Migrated != 0 || res2.Skipped != 3 {
		t.Fatalf("second pass must be a no-op: migrated=%d skipped=%d", res2.Migrated, res2.Skipped)
	}
	items, _ := store.List("", "", 100)
	if len(items) != 3 {
		t.Fatalf("expected exactly 3 items, got %d", len(items))
	}
	// Paused legacy job migrates paused; active ones scheduled.
	var paused int
	for _, it := range items {
		if it.State == StatePaused {
			paused++
		}
	}
	if paused != 1 {
		t.Fatalf("expected 1 paused migrated item, got %d", paused)
	}
}

func TestMigrateLegacyCronMissingAndMalformed(t *testing.T) {
	store := newMigrateStore(t)
	// Missing file is a no-op.
	if res, err := MigrateLegacyCron(filepath.Join(t.TempDir(), "nope.json"), store); err != nil || res.Migrated != 0 {
		t.Fatalf("missing file must be a no-op: %+v %v", res, err)
	}
	// Malformed file errors without side effects.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateLegacyCron(bad, store); err == nil {
		t.Fatal("malformed legacy file must error")
	}
	// A malformed entry is surfaced, valid entries still migrate.
	mixed := filepath.Join(t.TempDir(), "mixed.json")
	body := `{"version":1,"jobs":[{"id":"bad","name":"bad","enabled":true,"schedule":{"kind":"bogus"},"payload":{"message":"x"}},` +
		`{"id":"good","name":"good","enabled":true,"schedule":{"kind":"every","everyMs":60000},"payload":{"message":"ok","channel":"cli","to":"direct"}}]}`
	if err := os.WriteFile(mixed, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := MigrateLegacyCron(mixed, store)
	if err != nil {
		t.Fatal(err)
	}
	if res.Migrated != 1 || len(res.Errors) != 1 {
		t.Fatalf("mixed: migrated=%d errors=%v", res.Migrated, res.Errors)
	}
}
