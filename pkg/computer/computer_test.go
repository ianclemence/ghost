package computer

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *LeaseStore {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "leases.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewLeaseStore(db)
	if err != nil {
		t.Fatalf("NewLeaseStore: %v", err)
	}
	return s
}

func TestOpRiskMapping(t *testing.T) {
	cases := map[Op]string{
		OpObserve: "read_only", OpScreenshot: "read_only",
		OpInspectUI: "read_only", OpWait: "read_only",
		OpOpenApp: "low_risk", OpOpenURL: "low_risk", OpNavigate: "low_risk",
		OpScroll: "low_risk", OpClick: "low_risk", OpClose: "low_risk",
		OpType: "consequential", OpPressKey: "consequential",
		OpUpload: "consequential", OpDownload: "consequential",
		OpExecute: "high_impact",
		Op("nonsense"): "consequential",
	}
	for op, want := range cases {
		if got := OpRisk(op); got != want {
			t.Errorf("OpRisk(%q) = %q, want %q", op, got, want)
		}
	}
}

func TestLeaseAcquireConflictRelease(t *testing.T) {
	s := openTestStore(t)
	a, err := s.Acquire("macbook", "owner", "task-a", "sess-a", "personal", time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if a.State != LeaseActive {
		t.Fatalf("state = %v, want active", a.State)
	}
	// Competing task fails closed.
	if _, err := s.Acquire("macbook", "owner", "task-b", "sess-b", "personal", time.Minute); err == nil {
		t.Fatal("competing acquire must fail")
	} else if !strings.Contains(err.Error(), "busy") {
		t.Fatalf("must report busy, got: %v", err)
	}
	// Same task re-acquiring renews instead of conflicting.
	if _, err := s.Acquire("macbook", "owner", "task-a", "sess-a", "personal", time.Minute); err != nil {
		t.Fatalf("same-task reacquire must renew: %v", err)
	}
	if !s.OwnedBy("macbook", "task-a", "sess-a") {
		t.Fatal("owner check must hold")
	}
	if s.OwnedBy("macbook", "task-b", "sess-b") {
		t.Fatal("non-owner must not hold")
	}
	if err := s.Release(a.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Double release is safe.
	if err := s.Release(a.ID); err != nil {
		t.Fatalf("double release must succeed: %v", err)
	}
	// Now the competitor can take it.
	if _, err := s.Acquire("macbook", "owner", "task-b", "sess-b", "personal", time.Minute); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestLeaseExpiryAndStaleRecovery(t *testing.T) {
	s := openTestStore(t)
	l, err := s.Acquire("pi", "owner", "task-a", "sess-a", "personal", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	// Expired hold is reclaimed inline for the next acquirer.
	b, err := s.Acquire("pi", "owner", "task-b", "sess-b", "personal", time.Minute)
	if err != nil {
		t.Fatalf("expired lease must be reclaimable: %v", err)
	}
	if b.ID == l.ID {
		t.Fatal("reclaim must mint a fresh lease, not resurrect the stale one")
	}
	if s.OwnedBy("pi", "task-a", "sess-a") {
		t.Fatal("stale task must not retain ownership")
	}
	// Renewing an expired lease fails.
	if _, err := s.Renew(l.ID, time.Minute); err == nil {
		t.Fatal("renew of expired lease must fail")
	}
	// Boot recovery expires everything unreleased.
	n, err := s.RecoverStale()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 1 {
		t.Fatalf("recover must expire 1 live lease, got %d", n)
	}
	if s.OwnedBy("pi", "task-b", "sess-b") {
		t.Fatal("post-recovery ownership must be gone")
	}
}

func TestPlacementAvailability(t *testing.T) {
	now := time.Now()
	if got := AvailabilityOf(Descriptor{Placement: PlacementLocal}, now); got.State != Available {
		t.Fatalf("local must be available: %v", got)
	}
	if got := AvailabilityOf(Descriptor{Placement: PlacementPaired}, now); got.State != Offline {
		t.Fatalf("never-seen paired must be offline: %v", got)
	}
	fresh := Descriptor{Placement: PlacementPaired, LastSeen: now.Add(-time.Minute)}
	if got := AvailabilityOf(fresh, now); got.State != Available {
		t.Fatalf("fresh paired must be available: %v", got)
	}
	stale := Descriptor{Placement: PlacementPaired, LastSeen: now.Add(-time.Hour)}
	if got := AvailabilityOf(stale, now); got.State != Offline {
		t.Fatalf("stale paired must be offline: %v", got)
	}
	if got := AvailabilityOf(Descriptor{Placement: PlacementRemote}, now); got.State != Unavailable {
		t.Fatalf("remote must be unavailable with reason: %v", got)
	}
	if got := AvailabilityOf(Descriptor{Placement: PlacementSandbox}, now); got.State != Unavailable {
		t.Fatalf("sandbox must be unavailable with reason: %v", got)
	}
}

func TestEvidencePayload(t *testing.T) {
	e := BeginEvidence(OpNavigate, "macbook", "task-a", "sess-a", "work")
	if e.Outcome != "" {
		t.Fatal("unfinished evidence must have no outcome")
	}
	e.Finish(OutcomeSuccess, "youtube.com loaded")
	p := e.Payload()
	if p["operation"] != "navigate" || p["outcome"] != "success" || p["context_id"] != "work" {
		t.Fatalf("bad payload: %v", p)
	}
	if _, ok := p["ended_at"]; !ok {
		t.Fatal("finished evidence must carry ended_at")
	}
}
