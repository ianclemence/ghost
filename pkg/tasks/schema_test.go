package tasks

import (
	"database/sql"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

// v1JobsDDL is the frozen historical record of what migration v1 created.
// It is intentionally a literal here: tests must replay the past, not call
// the present. If this literal ever stops matching pkg/db's baseline, this
// test still passes — and that is correct, because old devices really do
// carry this shape.
const v1JobsDDL = `CREATE TABLE jobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	progress REAL NOT NULL DEFAULT 0,
	checkpoints JSON,
	payload JSON,
	session_key TEXT,
	error TEXT,
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL,
	started_at DATETIME,
	finished_at DATETIME,
	updated_at DATETIME NOT NULL)`

func columnSet(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(jobs)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	var cid int
	var name, ctype string
	var notnull int
	var dflt sql.NullString
	var pk int
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func openMem(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestV2ConvergesToCurrentSchema proves an old v1 database upgraded via
// EnsureV2Columns reaches exactly the shape EnsureSchema creates fresh.
// If these ever diverge, new installs and upgraded devices would behave
// differently — the test fails loudly instead.
func TestV2ConvergesToCurrentSchema(t *testing.T) {
	upgraded := openMem(t)
	if _, err := upgraded.Exec(v1JobsDDL); err != nil {
		t.Fatal(err)
	}
	if err := EnsureV2Columns(upgraded); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	fresh := openMem(t)
	if err := EnsureSchema(fresh); err != nil {
		t.Fatal(err)
	}

	if got, want := columnSet(t, upgraded), columnSet(t, fresh); !reflect.DeepEqual(got, want) {
		t.Fatalf("upgraded shape %v != fresh shape %v", got, want)
	}
}

// TestV2IsIdempotent proves a retry after a crashed migration completes
// instead of failing on half-applied state.
func TestV2IsIdempotent(t *testing.T) {
	db := openMem(t)
	if _, err := db.Exec(v1JobsDDL); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := EnsureV2Columns(db); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	s := NewStore(db, nil)
	j, err := s.CreateWithScope("k", "sess", "ian", "personal", map[string]interface{}{"a": 1})
	if err != nil {
		t.Fatalf("create after upgrade: %v", err)
	}
	got, err := s.Get(j.ID)
	if err != nil {
		t.Fatalf("get after upgrade: %v", err)
	}
	if got.Owner != "ian" || got.ContextID != "personal" || got.Generation == "" {
		t.Fatalf("scope/generation lost: %+v", got)
	}
}

// TestWaitingLifecycle exercises the new waiting/pause/resume/expire
// transitions and the generation guard end to end.
func TestWaitingLifecycle(t *testing.T) {
	db := openMem(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db, nil)
	j, err := s.Create("k", "sess", nil)
	if err != nil {
		t.Fatal(err)
	}
	gen1 := j.Generation
	if gen1 == "" {
		t.Fatal("create must mint a generation")
	}
	if !s.CheckGeneration(j.ID, gen1) {
		t.Fatal("fresh generation must verify")
	}

	w, err := s.SetWaiting(j.ID, StatusWaitingPermission, "need camera")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != StatusWaitingPermission {
		t.Fatalf("status=%s", w.Status)
	}

	gen2, err := s.RotateGeneration(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gen2 == gen1 {
		t.Fatal("rotation must mint a new generation")
	}
	if s.CheckGeneration(j.ID, gen1) {
		t.Fatal("old generation must not verify after rotation")
	}
	if !s.CheckGeneration(j.ID, gen2) {
		t.Fatal("new generation must verify")
	}
	if s.CheckGeneration(j.ID, "gen-bogus") {
		t.Fatal("bogus generation must not verify")
	}

	p, err := s.Pause(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusPaused {
		t.Fatalf("status=%s", p.Status)
	}
	r, err := s.Resume(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusPending {
		t.Fatalf("status=%s", r.Status)
	}

	if err := s.SetEvidence(j.ID, "did step one"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetResumeState(j.ID, "cursor:step-two"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(j.ID)
	if got.Evidence != "did step one" || got.ResumeState != "cursor:step-two" {
		t.Fatalf("evidence/resume lost: %+v", got)
	}

	e, err := s.Expire(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusExpired {
		t.Fatalf("status=%s", e.Status)
	}

	c, err := s.CancelWithReason(j.ID, "superseded")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusCancelled || c.Error != "superseded" {
		t.Fatalf("cancel reason lost: %+v", c)
	}

	if _, err := s.SetWaiting(j.ID, StatusPaused, "x"); err == nil {
		t.Fatal("SetWaiting must reject non-waiting statuses")
	}
}
