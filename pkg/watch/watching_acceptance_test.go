package watch

import (
	"testing"
	"time"
)

// Acceptance criteria for "scheduled watching" (see the capability spec):
//
//  1. A watched target is re-checked on its schedule across restarts: the
//     ledger is durable and a notice already delivered is never re-sent.
//  2. Change detection is stateful: a target that changes and changes back
//     produces exactly one notification, not two.
//  3. Failure is visible: a failed probe is recorded as a failure, never as
//     "no change", and enough failures end in StatusFailed — an honest
//     "I couldn't watch this".
//  4. Latency is documented and bounded: the next-check time is a function
//     of distance to the event, never better than the cadence claims.
//
// These are falsifiable: remove the persistent baseline, the failure
// counter, or the durable Notified list and they fail.

func newWatch(t *testing.T) (*Store, Watch) {
	t.Helper()
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w, err := st.Create(Watch{
		Kind: KindFlight, Entity: "TG123", Label: "TG123",
		Provenance: Provenance{Session: "s1", MessageID: "m1", Quote: "watch my flight TG123", At: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, w
}

func TestAcceptance_ChangeAndBackNotifiesOnce(t *testing.T) {
	st, w := newWatch(t)
	if _, err := st.ResetBaseline(w.ID, map[string]string{"gate": "A", "status": "on time"}); err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(10 * time.Minute)

	// The world moves: one meaningful change, one notice.
	_, changes, err := st.Observe(w.ID, map[string]string{"gate": "B", "status": "on time"}, Evidence{Field: "gate", Source: "flight"}, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Field != "gate" {
		t.Fatalf("changes = %+v, want exactly the gate move", changes)
	}
	if sent, _, _ := st.MarkNotified(w.ID, changes[0].Fingerprint(w.ID)); !sent {
		t.Fatal("the first change was not delivered")
	}

	// It changes back to the baseline. That is not a second thing to say.
	_, changes2, err := st.Observe(w.ID, map[string]string{"gate": "A", "status": "on time"}, Evidence{Field: "gate", Source: "flight"}, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes2) != 0 {
		t.Fatalf("change-and-back produced a second notice: %+v", changes2)
	}
}

func TestAcceptance_RestartDoesNotRenotify(t *testing.T) {
	ws := t.TempDir()
	st, err := New(ws)
	if err != nil {
		t.Fatal(err)
	}
	w, err := st.Create(Watch{Kind: KindFlight, Entity: "TG123",
		Provenance: Provenance{Quote: "watch my flight TG123", At: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetBaseline(w.ID, map[string]string{"gate": "A"}); err != nil {
		t.Fatal(err)
	}
	_, changes, err := st.Observe(w.ID, map[string]string{"gate": "B"}, Evidence{Field: "gate", Source: "flight"}, time.Now().Add(time.Hour))
	if err != nil || len(changes) != 1 {
		t.Fatalf("observe: changes=%v err=%v", changes, err)
	}
	fp := changes[0].Fingerprint(w.ID)
	if sent, _, _ := st.MarkNotified(w.ID, fp); !sent {
		t.Fatal("first delivery should record the fingerprint")
	}

	// The process dies and restarts: a fresh store over the same ledger.
	restarted, err := New(ws)
	if err != nil {
		t.Fatal(err)
	}
	if sent, _, _ := restarted.MarkNotified(w.ID, fp); sent {
		t.Fatal("the same notice was re-sent after a restart; the dedupe record did not survive")
	}
	// And the watch is still there, with its baseline intact.
	got, err := restarted.Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Baseline["gate"] != "A" {
		t.Fatalf("baseline lost across restart: %+v", got.Baseline)
	}
}

func TestAcceptance_ProbeFailureIsVisibleNotNoChange(t *testing.T) {
	st, w := newWatch(t)
	if _, err := st.ResetBaseline(w.ID, map[string]string{"gate": "A"}); err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(10 * time.Minute)

	for i := 0; i < DefaultMaxFailures; i++ {
		if _, err := st.RecordFailure(w.ID, "fetch failed: connection refused", next); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A broken check is not "all clear": it is a failure, said plainly.
	if got.Status != StatusFailed {
		t.Fatalf("status after %d failures = %s, want failed", DefaultMaxFailures, got.Status)
	}
	if got.LastFailure == "" {
		t.Fatal("the failure was recorded without a reason")
	}
	// Failure must not fabricate a state change.
	if got.Current["gate"] != "" && got.Current["gate"] != "A" {
		t.Fatalf("a failed probe invented a state: %+v", got.Current)
	}
	// A later real success clears the failure and is not a notice.
	_, changes, err := st.Observe(w.ID, map[string]string{"gate": "A"}, Evidence{Field: "gate", Source: "flight"}, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("recovery to baseline produced a spurious change: %+v", changes)
	}
	got, _ = st.Get(w.ID)
	if got.Failures != 0 {
		t.Fatalf("failures = %d after a successful probe, want 0", got.Failures)
	}
}

func TestAcceptance_CadenceIsBoundedAndHonest(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		until time.Duration
		want  time.Duration
	}{
		{72 * time.Hour, CadenceFar},
		{24 * time.Hour, CadenceDay},
		{6 * time.Hour, CadenceHours},
		{2 * time.Hour, CadenceNear},
		{30 * time.Minute, CadenceImminent},
	}
	for _, c := range cases {
		at := now.Add(c.until)
		if got := Cadence(&at, now); got != c.want {
			t.Errorf("%v out: cadence = %v, want %v", c.until, got, c.want)
		}
	}
	if got := Cadence(nil, now); got != CadenceNoEvent {
		t.Errorf("no event: cadence = %v, want %v", got, CadenceNoEvent)
	}
	// Worst-case delay is the cadence, and it never runs past the horizon.
	event := now.Add(30 * time.Minute)
	w := Watch{EventAt: &event, Status: StatusActive}
	exp := now.Add(5 * time.Minute)
	w.ExpiresAt = &exp
	if next := NextCheckAt(w, now); next.After(exp) {
		t.Fatalf("next check %v scheduled past the horizon %v", next, exp)
	}
}
