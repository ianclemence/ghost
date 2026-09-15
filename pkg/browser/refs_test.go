package browser

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *SessionStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSessionStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseRefs(t *testing.T) {
	got := ParseRefs(`@e1 [button] "Submit" and @e2, again @e1, ignore e3 and @ex`)
	if len(got) != 2 || got[0] != "@e1" || got[1] != "@e2" {
		t.Fatalf("got %v", got)
	}
	// Real agent-browser wire forms normalize to canonical refs.
	real := `- heading "T" [level=1, ref=e1]
- textbox [ref=e3]
- button "Continue" [ref=e2]`
	got = ParseRefs(real)
	want := map[string]bool{"@e1": true, "@e2": true, "@e3": true}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for _, r := range got {
		if !want[r] {
			t.Fatalf("unexpected %q in %v", r, got)
		}
	}
	jsonSnap := `{"success":true,"data":{"refs":{"e1":{"role":"heading"},"e3":{"role":"textbox"}},"snapshot":"- textbox [ref=e3]"}}`
	got = ParseRefs(jsonSnap)
	if len(got) != 2 {
		t.Fatalf("json forms: got %v", got)
	}
}

func TestEpochLifecycle(t *testing.T) {
	s := testStore(t)
	if err := s.CheckRef("sess", "@e1"); err == nil {
		t.Fatal("unknown session must fail closed")
	}
	epoch := s.Observe("sess", []string{"@e1", "@e2"})
	if epoch != 1 {
		t.Fatalf("first epoch = %d", epoch)
	}
	if err := s.CheckRef("sess", "@e1"); err != nil {
		t.Fatalf("live ref must pass: %v", err)
	}
	if err := s.CheckRef("sess", "@e9"); err == nil {
		t.Fatal("unobserved ref must fail")
	} else if _, ok := err.(*StaleRefError); !ok {
		t.Fatalf("must be StaleRefError, got %T", err)
	}
	s.Mutate("sess")
	if err := s.CheckRef("sess", "@e1"); err == nil {
		t.Fatal("mutation must close the epoch")
	}
	epoch = s.Observe("sess", []string{"@e3"})
	if epoch != 3 {
		t.Fatalf("epoch must advance across mutate+observe, got %d", epoch)
	}
	if err := s.CheckRef("sess", "@e3"); err != nil {
		t.Fatalf("fresh epoch ref must pass: %v", err)
	}
	if got := s.RefEpoch("sess"); got != 3 {
		t.Fatalf("RefEpoch = %d", got)
	}
}

func TestTaintLog(t *testing.T) {
	s := testStore(t)
	if got := s.TaintedDomains("sess"); len(got) != 0 {
		t.Fatalf("clean session must have no taint: %v", got)
	}
	s.RecordTaint(TaintSpan{SessionID: "sess", URL: "https://evil.test/x", Domain: "evil.test"})
	s.RecordTaint(TaintSpan{SessionID: "sess", URL: "https://evil.test/y", Domain: "evil.test"})
	s.RecordTaint(TaintSpan{SessionID: "sess", URL: "https://shop.test/", Domain: "shop.test"})
	got := s.TaintedDomains("sess")
	if len(got) != 2 || got[0] != "evil.test" || got[1] != "shop.test" {
		t.Fatalf("got %v", got)
	}
	// Bound respected.
	for i := 0; i < maxTaintSpans+10; i++ {
		s.RecordTaint(TaintSpan{SessionID: "sess", Domain: "flood.test"})
	}
	s.refsMu.Lock()
	n := len(s.taintLog["sess"])
	s.refsMu.Unlock()
	if n > maxTaintSpans {
		t.Fatalf("taint log unbounded: %d", n)
	}
}

func TestRevalidate(t *testing.T) {
	s := testStore(t)
	sess, err := s.GetOrCreate("ghost", "personal", "task1", "default", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revalidate(sess.ID, "ghost", "personal", "task1"); err != nil {
		t.Fatalf("matching resume must pass: %v", err)
	}
	if _, err := s.Revalidate(sess.ID, "ghost", "work", "task1"); err == nil {
		t.Fatal("context mismatch must fail")
	}
	if _, err := s.Revalidate(sess.ID, "ghost", "personal", "other"); err == nil {
		t.Fatal("task mismatch must fail")
	}
	if _, err := s.Revalidate("nope", "ghost", "personal", "task1"); err == nil {
		t.Fatal("unknown session must fail")
	}
}

func TestSweepRefs(t *testing.T) {
	s := testStore(t)
	s.Observe("ghost-session", []string{"@e1"})
	s.RecordTaint(TaintSpan{SessionID: "ghost-session", Domain: "x.test"})
	s.SweepRefs() // no rows in table: everything sweeps
	if got := s.RefEpoch("ghost-session"); got != -1 {
		t.Fatal("swept epoch must report -1")
	}
	if got := s.TaintedDomains("ghost-session"); len(got) != 0 {
		t.Fatal("swept taint must clear")
	}
}

// TestSkillFootprint pins the context budget for version-pinned skill
// docs: growth fails CI and forces a conscious decision.
func TestSkillFootprint(t *testing.T) {
	stub, core := SkillStub(), SkillCore()
	if !strings.Contains(stub, SkillVersion) || !strings.Contains(core, SkillVersion) {
		t.Fatal("skill docs must carry their version")
	}
	if len(stub) > 1024 {
		t.Fatalf("stub %d bytes exceeds 1KB budget", len(stub))
	}
	if len(core) > 4096 {
		t.Fatalf("core %d bytes exceeds 4KB budget", len(core))
	}
	t.Logf("footprint: stub=%dB core=%dB", len(stub), len(core))
}
