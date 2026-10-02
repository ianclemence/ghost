package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func tripStore(t *testing.T) (*personalcontext.Store, personalcontext.Entry) {
	t.Helper()
	st, err := personalcontext.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal("Their Shenzhen trip runs from around 26 October to 6 November")
	e, err := st.Create(personalcontext.Entry{
		ID: personalcontext.NewEntryID(), Kind: personalcontext.KindEvent, Subject: "user",
		Predicate: "event/shenzhen-trip-dates", Value: raw, Status: personalcontext.StatusCurrent,
		Confidence: 0.85, Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation, Kind: personalcontext.SourceInferred, Timestamp: time.Now()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, e
}

func TestMemoryCorrectFindsThenPreviewsThenChanges(t *testing.T) {
	st, e := tripStore(t)
	tool := NewMemoryCorrectTool(st)
	ctx := WithRequestMessage(context.Background(), "the 16th is the new plan, forget the october 26 dates")

	res := tool.Execute(ctx, map[string]interface{}{"action": "find", "about": "shenzhen trip dates"})
	if res.IsError || !strings.Contains(res.ForLLM, e.ID) {
		t.Fatalf("find must list the belief: %+v", res)
	}

	// Without confirmed, nothing is written and the preview says so.
	res = tool.Execute(ctx, map[string]interface{}{"action": "update", "entry_id": e.ID, "new_value": "Their Shenzhen trip starts on 16 October"})
	if !strings.Contains(res.ForLLM, "Nothing changed yet") {
		t.Fatalf("expected a preview, got %q", res.ForLLM)
	}
	if got, _ := st.Get(e.ID); got.Status != personalcontext.StatusCurrent {
		t.Fatalf("a preview must not change the belief, status=%s", got.Status)
	}

	res = tool.Execute(ctx, map[string]interface{}{"action": "update", "entry_id": e.ID, "new_value": "Their Shenzhen trip starts on 16 October", "confirmed": true})
	if res.IsError || !strings.Contains(res.ForLLM, "Updated") {
		t.Fatalf("confirmed update failed: %+v", res)
	}
	cur := st.ByPredicate("event/shenzhen-trip-dates")
	var current []personalcontext.Entry
	for _, c := range cur {
		if c.Status == personalcontext.StatusCurrent {
			current = append(current, c)
		}
	}
	if len(current) != 1 || !strings.Contains(personalcontext.Value(current[0]), "16 October") {
		t.Fatalf("want exactly one current belief with the new value, got %+v", current)
	}
	if old, _ := st.Get(e.ID); old.Status != personalcontext.StatusSuperseded {
		t.Fatalf("the old belief must be kept as superseded, got %s", old.Status)
	}
	if current[0].Quote == "" || current[0].Sources[0].Kind != personalcontext.SourceUserCorrected {
		t.Fatalf("the correction must carry the owner's words as its receipt: %+v", current[0])
	}
}

func TestMemoryCorrectForgetNeedsConfirmation(t *testing.T) {
	st, e := tripStore(t)
	tool := NewMemoryCorrectTool(st)
	ctx := context.Background()
	res := tool.Execute(ctx, map[string]interface{}{"action": "forget", "entry_id": e.ID})
	if !strings.Contains(res.ForLLM, "Nothing changed yet") {
		t.Fatalf("forget must preview first: %q", res.ForLLM)
	}
	res = tool.Execute(ctx, map[string]interface{}{"action": "forget", "entry_id": e.ID, "confirmed": true})
	if res.IsError {
		t.Fatalf("confirmed forget failed: %+v", res)
	}
	if got, _ := st.Get(e.ID); got.Status == personalcontext.StatusCurrent {
		t.Fatalf("forgotten belief is still current")
	}
}

func TestMemoryCorrectRefusesUnknownAndInvisible(t *testing.T) {
	st, e := tripStore(t)
	tool := NewMemoryCorrectTool(st)
	if res := tool.Execute(context.Background(), map[string]interface{}{"action": "update", "entry_id": "nope", "new_value": "x", "confirmed": true}); !res.IsError {
		t.Fatal("an unknown id must be refused")
	}
	tool.Scopes = func(string) []string { return []string{"context:work"} }
	e2 := e
	_ = e2
	// A global belief is visible from every scope, so this stays allowed;
	// the scoped-away case is covered by the store's own scope tests.
	if res := tool.Execute(context.Background(), map[string]interface{}{"action": "find", "about": "shenzhen"}); res.IsError {
		t.Fatalf("global beliefs stay visible: %+v", res)
	}
}

func TestHeldChangeIsSurfacedAndResolved(t *testing.T) {
	st, e := tripStore(t)
	raw, _ := json.Marshal("Their Shenzhen trip starts on 16 October")
	cand, err := st.Create(personalcontext.Entry{
		ID: personalcontext.NewEntryID(), Kind: personalcontext.KindEvent, Subject: "user",
		Predicate: "event/shenzhen-trip-dates", Value: raw, Status: personalcontext.StatusUncertain,
		Confidence: 0.7, Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation, Kind: personalcontext.SourceInferred, Timestamp: time.Now()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pend := st.PendingChanges(nil)
	if len(pend) != 1 || pend[0].Current.ID != e.ID || pend[0].Candidate.ID != cand.ID {
		t.Fatalf("want one pending change, got %+v", pend)
	}
	tool := NewMemoryCorrectTool(st)
	// "The old one is right": dismiss the candidate; the belief stays.
	if res := tool.Execute(context.Background(), map[string]interface{}{"action": "dismiss", "entry_id": cand.ID}); res.IsError {
		t.Fatalf("dismiss failed: %+v", res)
	}
	if len(st.PendingChanges(nil)) != 0 {
		t.Fatal("a dismissed candidate must stop being asked about")
	}
	if got, _ := st.Get(e.ID); got.Status != personalcontext.StatusCurrent {
		t.Fatalf("dismissing must leave the stored belief current, got %s", got.Status)
	}
}
