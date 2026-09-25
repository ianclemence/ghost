package personalcontext

import (
	"testing"
	"time"
)

func applyOnce(t *testing.T, ws string, in Input) []Action {
	t.Helper()
	store, err := Open(ws)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	acts, err := Apply(store, in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return acts
}

func TestForgetBlocksResurrectionFromOldEvidence(t *testing.T) {
	ws := t.TempDir()
	ts := time.Now().UTC().Add(-2 * time.Hour)
	in := Input{SessionID: "s1", MessageID: "m1", Text: "my name is Ian", Timestamp: ts}

	acts := applyOnce(t, ws, in)
	if len(acts) == 0 {
		t.Fatal("expected the declaration to extract")
	}
	id := acts[0].Entry.ID

	report, err := ForgetPipeline(ws, id, "not mine")
	if err != nil {
		t.Fatalf("forget pipeline: %v", err)
	}
	if !report.Forgotten || !report.Tombstoned {
		t.Fatalf("forget report incomplete: %+v", report)
	}

	// The same old message must not resurrect the belief.
	if again := applyOnce(t, ws, in); len(again) != 0 {
		t.Fatalf("forgotten belief was resurrected from old evidence: %+v", again)
	}

	// But a newer declaration is the owner changing their mind.
	in2 := in
	in2.MessageID = "m2"
	in2.Timestamp = time.Now().UTC().Add(time.Second)
	if newer := applyOnce(t, ws, in2); len(newer) == 0 {
		t.Fatal("a declaration newer than the forgetting must be allowed")
	}
}

func TestExplainCarriesReceipts(t *testing.T) {
	ws := t.TempDir()
	in := Input{SessionID: "sess-1", MessageID: "msg-9", Text: "my name is Ian", Timestamp: time.Now().UTC()}
	acts := applyOnce(t, ws, in)
	if len(acts) == 0 {
		t.Fatal("expected the declaration to extract")
	}

	store, err := Open(ws)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ex, err := store.Explain(acts[0].Entry.ID)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if ex.Quote == "" {
		t.Error("explanation is missing the verbatim quote")
	}
	if len(ex.MessageIDs) != 1 || ex.MessageIDs[0] != "msg-9" {
		t.Errorf("message ids = %v, want [msg-9]", ex.MessageIDs)
	}
	if ex.Confidence <= 0 {
		t.Errorf("confidence = %v, want > 0", ex.Confidence)
	}
}

func TestForgetPipelineReportIsHonest(t *testing.T) {
	ws := t.TempDir()
	in := Input{SessionID: "s1", MessageID: "m1", Text: "my name is Ian", Timestamp: time.Now().UTC()}
	acts := applyOnce(t, ws, in)
	if len(acts) == 0 {
		t.Fatal("expected the declaration to extract")
	}
	if _, err := ForgetPipeline(ws, "ec_does_not_exist", ""); err == nil {
		t.Fatal("forgetting an unknown claim must fail honestly")
	}
}
