package personalcontext

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func seedBelief(t *testing.T, s *Store, status Status, val string) Entry {
	t.Helper()
	raw, _ := json.Marshal(val)
	e, err := s.Create(Entry{
		ID: NewEntryID(), Kind: KindEvent, Subject: "user", Predicate: "event/trip", Value: raw,
		Status: status, Confidence: 0.8,
		Sources: []Source{{Type: SourceConversation, Kind: SourceInferred, Timestamp: time.Now()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCorrectKeepsHistoryAndDropsOpenCandidates(t *testing.T) {
	st, _ := Open(t.TempDir())
	cur := seedBelief(t, st, StatusCurrent, "26 October")
	cand := func() Entry {
		raw, _ := json.Marshal("16 October")
		e, err := st.Create(Entry{ID: NewEntryID(), Kind: KindEvent, Subject: "user", Predicate: "event/trip", Value: raw,
			Status: StatusUncertain, Confidence: 0.5, Sources: []Source{{Type: SourceConversation, Kind: SourceInferred, Timestamp: time.Now()}}})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}()

	old, next, err := st.Correct(cur.ID, "  27   October ", "27 October", "console")
	if err != nil {
		t.Fatal(err)
	}
	if Value(old) != "26 October" || Value(next) != "27 October" {
		t.Fatalf("was/now wrong: %q -> %q", Value(old), Value(next))
	}
	if got, _ := st.Get(cur.ID); got.Status != StatusSuperseded {
		t.Fatalf("old belief must be kept as superseded, got %s", got.Status)
	}
	if got, _ := st.Get(cand.ID); got.Status == StatusUncertain {
		t.Fatal("an owner correction settles the question, so the open candidate must go")
	}
	if next.Sources[0].Kind != SourceUserCorrected || next.Quote != "27 October" {
		t.Fatalf("the correction needs the owner's words as its receipt: %+v", next)
	}
	// Survives a reload: it is in the log, not just in memory.
	again, _ := Open(st.path[:strings.LastIndex(st.path, "/personal-context")])
	if len(again.PendingChanges(nil)) != 0 {
		t.Fatal("no pending change should remain after a reload")
	}
}

func TestCorrectRefusesNonsense(t *testing.T) {
	st, _ := Open(t.TempDir())
	cur := seedBelief(t, st, StatusCurrent, "26 October")
	for name, v := range map[string]string{"empty": "   ", "same": "26 october", "long": strings.Repeat("x", MaxCorrectionRunes+1)} {
		if _, _, err := st.Correct(cur.ID, v, "", ""); !errors.Is(err, ErrBadCorrection) {
			t.Errorf("%s: want ErrBadCorrection, got %v", name, err)
		}
	}
	if _, _, err := st.Correct("nope", "x", "", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	if got, _ := st.Get(cur.ID); got.Status != StatusCurrent {
		t.Fatal("a refused correction must change nothing")
	}
}

func TestForgetPipelineWithUsesTheCallersStore(t *testing.T) {
	ws := t.TempDir()
	live, _ := Open(ws)
	cur := seedBelief(t, live, StatusCurrent, "26 October")
	if _, err := ForgetPipelineWith(live, ws, cur.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if got, _ := live.Get(cur.ID); got.Status == StatusCurrent {
		t.Fatal("the live store itself must see the forgetting, not only the file")
	}
}
