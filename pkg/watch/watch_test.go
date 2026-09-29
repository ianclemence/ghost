package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Detection
// ---------------------------------------------------------------------------

func TestDetectFlightRequiresTravelContextAndFutureDate(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	cands := Detect("I'm flying BA123 to London tomorrow", now, "UTC")
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %+v", len(cands), cands)
	}
	c := cands[0]
	if c.Kind != KindFlight || c.Entity != "BA123" {
		t.Fatalf("wrong candidate: %+v", c)
	}
	if c.EventAt == nil || !c.EventAt.After(now) {
		t.Fatalf("expected a future event time, got %v", c.EventAt)
	}
	if !strings.Contains(c.Quote, "flying BA123") {
		t.Fatalf("provenance must be a verbatim span, got %q", c.Quote)
	}
}

func TestDetectRejectsReminderTurn(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	if c := Detect("Remind me about my flight BA123 tomorrow", now, "UTC"); len(c) != 0 {
		t.Fatalf("reminder turns belong to the scheduler: %+v", c)
	}
}

func TestDetectRejectsRoutineTurn(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	if c := Detect("I fly BA123 every Monday", now, "UTC"); len(c) != 0 {
		t.Fatalf("repeated cadence belongs to routines: %+v", c)
	}
}

func TestDetectRejectsThirdPerson(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	for _, msg := range []string{
		"My friend's flight BA123 is tomorrow",
		"His flight BA123 leaves tomorrow",
		"My brother's dentist appointment is Thursday",
	} {
		if c := Detect(msg, now, "UTC"); len(c) != 0 {
			t.Fatalf("%q must not become the owner's watch: %+v", msg, c)
		}
	}
}

func TestDetectRejectsPastEvent(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	if c := Detect("I flew BA123 yesterday", now, "UTC"); len(c) != 0 {
		t.Fatalf("a past flight is history, not a watch: %+v", c)
	}
}

func TestDetectRejectsNoTravelContext(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	if c := Detect("The code BA123 unlocks the door tomorrow", now, "UTC"); len(c) != 0 {
		t.Fatalf("a bare code is not travel: %+v", c)
	}
}

func TestDetectExplicitNeedsNoDate(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	cands := Detect("track my flight BA123", now, "UTC")
	if len(cands) != 1 || !cands[0].Explicit {
		t.Fatalf("an explicit request stands without a date: %+v", cands)
	}
}

func TestDetectAppointment(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	cands := Detect("I have a dentist appointment on Thursday", now, "UTC")
	if len(cands) != 1 || cands[0].Kind != KindAppointment {
		t.Fatalf("expected an appointment candidate, got %+v", cands)
	}
	if cands[0].EventAt == nil {
		t.Fatal("appointment must carry its future date")
	}
	if !strings.Contains(strings.ToLower(cands[0].Quote), "dentist") {
		t.Fatalf("quote must contain the entity: %q", cands[0].Quote)
	}
}

func TestDetectAppointmentWithoutDateIsNotWatchable(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	if c := Detect("I should book a dentist appointment sometime", now, "UTC"); len(c) != 0 {
		t.Fatalf("undated appointment is not watchable: %+v", c)
	}
}

func TestDetectDeliveryAndReservation(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	cands := Detect("My package arrives tomorrow and the hotel booking is Friday", now, "UTC")
	kinds := map[Kind]bool{}
	for _, c := range cands {
		kinds[c.Kind] = true
	}
	if !kinds[KindDelivery] || !kinds[KindReservation] {
		t.Fatalf("expected delivery + reservation, got %+v", cands)
	}
}

// ---------------------------------------------------------------------------
// Policy
// ---------------------------------------------------------------------------

func cand(kind Kind, entity string, at *time.Time) Candidate {
	return Candidate{
		Kind: kind, Entity: entity, Label: entity,
		Quote: "I'm watching " + entity, Confidence: 0.9, Origin: "deterministic",
		EventAt: at,
	}
}

func TestPolicyAllows(t *testing.T) {
	p := Default()
	p.Sources = []string{"sandbox"}
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	v := p.Allow(cand(KindAppointment, "dentist", &future), nil, now, "sandbox")
	if !v.Allow || v.Reason != ReasonAllowed {
		t.Fatalf("expected allowed, got %+v", v)
	}
}

func TestPolicyRejectsNoSource(t *testing.T) {
	p := Default()
	p.Sources = nil
	now := time.Now().UTC()
	v := p.Allow(cand(KindAppointment, "dentist", nil), nil, now, "")
	if v.Allow || v.Reason != ReasonSourceUnavailable {
		t.Fatalf("expected source_unavailable, got %+v", v)
	}
}

func TestPolicyRejectsPastAndHorizon(t *testing.T) {
	p := Default()
	p.Sources = []string{"sandbox"}
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	if v := p.Allow(cand(KindFlight, "BA123", &past), nil, now, "sandbox"); v.Allow || v.Reason != ReasonPastEvent {
		t.Fatalf("expected event_in_past, got %+v", v)
	}
	far := now.Add(DefaultHorizon + time.Hour)
	if v := p.Allow(cand(KindFlight, "BA123", &far), nil, now, "sandbox"); v.Allow || v.Reason != ReasonHorizon {
		t.Fatalf("expected event_beyond_horizon, got %+v", v)
	}
}

func TestPolicyRejectsDuplicateAndDisabled(t *testing.T) {
	p := Default()
	p.Sources = []string{"sandbox"}
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	existing := []Watch{{Kind: KindAppointment, Entity: "dentist", Status: StatusActive}}
	if v := p.Allow(cand(KindAppointment, "dentist", &future), existing, now, "sandbox"); v.Allow || v.Reason != ReasonDuplicate {
		t.Fatalf("expected duplicate, got %+v", v)
	}
	p.Enabled = false
	if v := p.Allow(cand(KindAppointment, "haircut", &future), nil, now, "sandbox"); v.Allow || v.Reason != ReasonProactiveDisabled {
		t.Fatalf("expected proactive_disabled, got %+v", v)
	}
	// An explicit request still runs under a disabled policy.
	c := cand(KindFlight, "BA123", &future)
	c.Explicit = true
	if v := p.Allow(c, nil, now, "sandbox"); !v.Allow {
		t.Fatalf("explicit requests bypass the master switch: %+v", v)
	}
}

func TestPolicyRejectsOverCapacityUnlessExplicit(t *testing.T) {
	p := Default()
	p.Sources = []string{"sandbox"}
	p.MaxActive = 2
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	existing := []Watch{
		{Kind: KindAppointment, Entity: "a", Status: StatusActive},
		{Kind: KindAppointment, Entity: "b", Status: StatusActive},
	}
	if v := p.Allow(cand(KindAppointment, "c", &future), existing, now, "sandbox"); v.Allow || v.Reason != ReasonMaxActive {
		t.Fatalf("expected max_active, got %+v", v)
	}
	c := cand(KindAppointment, "c", &future)
	c.Explicit = true
	if v := p.Allow(c, existing, now, "sandbox"); !v.Allow {
		t.Fatalf("explicit requests bypass capacity: %+v", v)
	}
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

func TestDiffFindsMeaningfulChanges(t *testing.T) {
	base := map[string]string{"gate": "B12", "status": "scheduled", "_source": "sandbox"}
	cur := map[string]string{"gate": "A4", "status": "scheduled", "_source": "sandbox", "_checked_at": "x"}
	got := Diff(base, cur)
	if len(got) != 1 || got[0].Field != "gate" || got[0].From != "B12" || got[0].To != "A4" {
		t.Fatalf("expected one gate change, got %+v", got)
	}
}

func TestDiffIgnoresMetadataAndWhitespace(t *testing.T) {
	base := map[string]string{"gate": "B12", "_checked_at": "1"}
	cur := map[string]string{"gate": " B12 ", "_checked_at": "2"}
	if got := Diff(base, cur); len(got) != 0 {
		t.Fatalf("metadata and whitespace are not changes: %+v", got)
	}
}

func TestDiffReportsClearedValue(t *testing.T) {
	base := map[string]string{"gate": "B12"}
	cur := map[string]string{"gate": ""}
	got := Diff(base, cur)
	if len(got) != 1 || got[0].To != "" {
		t.Fatalf("a cleared gate is a real change: %+v", got)
	}
}

func TestDiffIsDeterministic(t *testing.T) {
	base := map[string]string{"z": "1", "a": "1", "m": "1"}
	cur := map[string]string{"z": "2", "a": "2", "m": "2"}
	for i := 0; i < 5; i++ {
		got := Diff(base, cur)
		if len(got) != 3 || got[0].Field != "a" || got[1].Field != "m" || got[2].Field != "z" {
			t.Fatalf("diff must be alphabetical every time: %+v", got)
		}
	}
}

func TestFingerprintSeparatesReversal(t *testing.T) {
	orig := Change{Field: "gate", From: "B12", To: "A4"}
	back := Change{Field: "gate", From: "A4", To: "B12"}
	if orig.Fingerprint("wt-1") == back.Fingerprint("wt-1") {
		t.Fatal("a reversal is a different notice than the original")
	}
	want := Change{Field: "gate", From: "B12", To: "A4"}
	if orig.Fingerprint("wt-1") != want.Fingerprint("wt-1") {
		t.Fatal("fingerprints must be stable")
	}
}

// ---------------------------------------------------------------------------
// Cadence
// ---------------------------------------------------------------------------

func TestCadenceAdaptsToProximity(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{72 * time.Hour, CadenceFar},
		{30 * time.Hour, CadenceDay},
		{6 * time.Hour, CadenceHours},
		{2 * time.Hour, CadenceNear},
		{30 * time.Minute, CadenceImminent},
		{-10 * time.Minute, CadenceImminent},
	}
	for _, c := range cases {
		at := now.Add(c.in)
		if got := Cadence(&at, now); got != c.want {
			t.Fatalf("until=%s: got %s want %s", c.in, got, c.want)
		}
	}
	if got := Cadence(nil, now); got != CadenceNoEvent {
		t.Fatalf("no event: got %s want %s", got, CadenceNoEvent)
	}
}

func TestNextCheckNeverPassesExpiry(t *testing.T) {
	now := time.Now().UTC()
	exp := now.Add(time.Hour)
	w := Watch{EventAt: &exp, ExpiresAt: &exp}
	next := NextCheckAt(w, now)
	if next.After(exp) {
		t.Fatalf("next check %s passed expiry %s", next, exp)
	}
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

func TestStoreCreateDedupesAndRequiresProvenance(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	at := now.Add(24 * time.Hour)
	w, err := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Label: "Flight BA123",
		EventAt: &at, Quote: "flying BA123 tomorrow"}.ToWatch("sess", "msg-1", now))
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == "" || w.Status != StatusActive {
		t.Fatalf("bad watch: %+v", w)
	}
	if w.ExpiresAt == nil || !w.ExpiresAt.After(now) {
		t.Fatalf("a new watch must carry a horizon: %+v", w.ExpiresAt)
	}
	again, err := s.Create(Candidate{Kind: KindFlight, Entity: "BA123",
		Quote: "flying BA123 tomorrow"}.ToWatch("sess", "msg-2", now))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != w.ID {
		t.Fatalf("restatement must coalesce: %s != %s", again.ID, w.ID)
	}
	if _, err := s.Create(Watch{Kind: KindFlight, Entity: "XX1"}); err == nil {
		t.Fatal("a watch without provenance must be refused")
	}
}

func TestStoreObserveEstablishesBaselineThenDetects(t *testing.T) {
	s, _ := New(t.TempDir())
	now := time.Now().UTC()
	w, _ := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Quote: "flying BA123"}.ToWatch("s", "m", now))

	// First probe seeds the baseline: nothing to compare, nothing to say.
	w, changes, err := s.Observe(w.ID, map[string]string{"gate": "B12"},
		Evidence{Source: "sandbox"}, now.Add(time.Hour))
	if err != nil || len(changes) != 0 {
		t.Fatalf("baseline probe must not report changes: %+v (%v)", changes, err)
	}
	if w.Baseline["gate"] != "B12" || w.Current["gate"] != "B12" {
		t.Fatalf("baseline/current not seeded: %+v", w)
	}

	// Second probe: the world moved.
	w, changes, err = s.Observe(w.ID, map[string]string{"gate": "A4"},
		Evidence{Source: "sandbox"}, now.Add(2*time.Hour))
	if err != nil || len(changes) != 1 || changes[0].To != "A4" {
		t.Fatalf("expected the gate change, got %+v (%v)", changes, err)
	}
	if w.Failures != 0 {
		t.Fatal("a successful probe clears the failure counter")
	}
	if w.LastCheckAt == nil || w.NextCheck == nil {
		t.Fatal("probe must record when it ran and when it runs next")
	}
}

func TestStoreNotifiedFingerprintsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	now := time.Now().UTC()
	w, _ := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Quote: "flying BA123"}.ToWatch("s", "m", now))
	fp := "watch:" + w.ID + ":gate:B12→A4"
	sent, _, err := s.MarkNotified(w.ID, fp)
	if err != nil || !sent {
		t.Fatalf("first delivery must send: sent=%v err=%v", sent, err)
	}
	// A fresh store over the same file (i.e. a restart) remembers it.
	s2, _ := New(dir)
	sent, _, err = s2.MarkNotified(w.ID, fp)
	if err != nil || sent {
		t.Fatalf("restart must not re-send: sent=%v err=%v", sent, err)
	}
	w2, _ := s2.Get(w.ID)
	if !w2.AlreadyNotified(fp) {
		t.Fatal("fingerprint must be persisted")
	}
}

func TestStoreFailuresEndInHonestFailure(t *testing.T) {
	s, _ := New(t.TempDir())
	now := time.Now().UTC()
	w, _ := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Quote: "flying BA123"}.ToWatch("s", "m", now))
	var last Watch
	for i := 0; i < DefaultMaxFailures; i++ {
		var err error
		last, err = s.RecordFailure(w.ID, "source unreachable", now.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.Status != StatusFailed {
		t.Fatalf("expected StatusFailed after %d failures, got %s", DefaultMaxFailures, last.Status)
	}
	if last.LastFailure == "" {
		t.Fatal("a failed watch must say why")
	}
	if last.Live() {
		t.Fatal("a failed watch must stop polling")
	}
}

func TestStoreSweepExpiresAndCancelCloses(t *testing.T) {
	s, _ := New(t.TempDir())
	now := time.Now().UTC()
	at := now.Add(time.Hour)
	w, _ := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", EventAt: &at, Quote: "flying BA123"}.ToWatch("s", "m", now))
	// Force the horizon into the past.
	exp := now.Add(-time.Minute)
	if _, err := s.mutate(w.ID, func(x *Watch) error { x.ExpiresAt = &exp; return nil }); err != nil {
		t.Fatal(err)
	}
	expired, err := s.Sweep(now)
	if err != nil || len(expired) != 1 || expired[0].Status != StatusExpired {
		t.Fatalf("sweep must expire exactly the past-horizon watch: %+v (%v)", expired, err)
	}
	// Second sweep: nothing left to expire (no repeated expiry events).
	again, _ := s.Sweep(now)
	if len(again) != 0 {
		t.Fatalf("expiry must be announced once: %+v", again)
	}
}

func TestStoreSnoozeHoldsUntil(t *testing.T) {
	s, _ := New(t.TempDir())
	now := time.Now().UTC()
	w, _ := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Quote: "flying BA123"}.ToWatch("s", "m", now))
	until := now.Add(3 * time.Hour)
	if _, err := s.Snooze(w.ID, until); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(w.ID)
	if got.Status != StatusSnoozed || got.Due(now.Add(time.Hour)) {
		t.Fatalf("snoozed watch must not be due: %+v", got)
	}
	if !got.Due(until.Add(time.Minute)) {
		t.Fatal("snoozed watch must resume when the snooze ends")
	}
	if _, err := s.Wake(w.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(w.ID)
	if got.Status != StatusActive || !got.Due(now) {
		t.Fatalf("wake must resume polling now: %+v", got)
	}
}

func TestStoreCancelIsFinal(t *testing.T) {
	s, _ := New(t.TempDir())
	now := time.Now().UTC()
	w, _ := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Quote: "flying BA123"}.ToWatch("s", "m", now))
	if _, err := s.Cancel(w.ID, "owner said stop"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(w.ID)
	if got.Status != StatusDisabled || got.Due(now) {
		t.Fatalf("cancelled watch must be inert: %+v", got)
	}
	if _, err := s.Cancel(w.ID, "again"); err == nil {
		t.Fatal("a settled watch must not be re-cancelled silently")
	}
	// The owner's cancellation frees the slot for a new watch of the same
	// thing: dedupe only coalesces LIVE watches.
	again, err := s.Create(Candidate{Kind: KindFlight, Entity: "BA123", Quote: "track BA123"}.ToWatch("s", "m", now))
	if err != nil || again.ID == w.ID {
		t.Fatalf("a cancelled thing may be watched again: %+v (%v)", again, err)
	}
}

// ---------------------------------------------------------------------------
// Sandbox source
// ---------------------------------------------------------------------------

func TestSandboxReadsStateAndClassifiesFailures(t *testing.T) {
	ws := t.TempDir()
	s := NewSandbox(ws)
	if s.Available() {
		t.Fatal("sandbox must be unavailable until its directory exists")
	}
	if err := SetState(ws, KindFlight, "BA123", map[string]string{"gate": "B12", "status": "scheduled"}); err != nil {
		t.Fatal(err)
	}
	if !s.Available() {
		t.Fatal("sandbox must be available once state exists")
	}
	w := Watch{Kind: KindFlight, Entity: "BA123"}
	state, excerpt, err := s.Fetch(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	if state["gate"] != "B12" {
		t.Fatalf("state read failed: %+v", state)
	}
	if state["_source"] != "sandbox" {
		t.Fatal("metadata must carry the source")
	}
	if !strings.Contains(excerpt, "gate=B12") {
		t.Fatalf("excerpt must cite the state: %q", excerpt)
	}

	// Missing entity: honest failure, never fabricated state.
	ClearState(ws, KindFlight, "BA123")
	if _, _, err := s.Fetch(context.Background(), w); err == nil || ClassOf(err) != FailUnavailable {
		t.Fatalf("missing record must fail unavailable, got %v", err)
	}

	// Failure injection marker.
	if err := SetError(ws, KindFlight, "BA123", "unavailable: network down"); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Fetch(context.Background(), w)
	if err == nil || ClassOf(err) != FailUnavailable || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("marker must inject its class and message: %v", err)
	}
	SetError(ws, KindFlight, "BA123", "transient: 503")
	if _, _, err := s.Fetch(context.Background(), w); ClassOf(err) != FailTransient {
		t.Fatalf("transient marker: %v", err)
	}
	SetError(ws, KindFlight, "BA123", "")
	if _, _, err := s.Fetch(context.Background(), w); err == nil {
		t.Fatal("clearing the marker returns to the missing-record failure")
	}
}

func TestSandboxSlugIsStable(t *testing.T) {
	if got := Slug(KindFlight, "BA123"); got != "flight-ba123" {
		t.Fatalf("slug: %q", got)
	}
	if Slug(KindAppointment, "Dentist appointment") != Slug(KindAppointment, "dentist appointment") {
		t.Fatal("slug must be case-insensitive")
	}
	if Slug(KindFlight, "BA123") != Slug(KindFlight, "ba123") {
		t.Fatal("slug must be case-insensitive for codes")
	}
}

func TestSandboxDecodeNumbersAndBooleans(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, WatchSourceDir), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `{"gate": 12, "delayed": true, "note": "late", "nested": {"a": 1}}`
	if err := os.WriteFile(StatePath(ws, KindFlight, "BA123"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSandbox(ws)
	state, _, err := s.Fetch(context.Background(), Watch{Kind: KindFlight, Entity: "BA123"})
	if err != nil {
		t.Fatal(err)
	}
	if state["gate"] != "12" || state["delayed"] != "true" || state["note"] != "late" {
		t.Fatalf("scalar decode: %+v", state)
	}
	if state["nested"] == "" {
		t.Fatalf("nested values must stringify deterministically: %+v", state)
	}
}

// ---------------------------------------------------------------------------
// Budget
// ---------------------------------------------------------------------------

func TestBudgetCapsPerDayAndRollsOver(t *testing.T) {
	b, err := OpenBudget(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	allowed := 0
	for i := 0; i < 10; i++ {
		if b.Allow(day, 3) {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("budget must stop at the ceiling, allowed=%d", allowed)
	}
	if b.Used(day) != 3 {
		t.Fatalf("used=%d", b.Used(day))
	}
	// A restart must not reset the ceiling.
	b2, err := OpenBudget(dirOf(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if b2.Allow(day, 3) {
		t.Fatal("a restart must not refund the budget")
	}
	// Tomorrow it resets.
	if !b2.Allow(day.Add(24*time.Hour), 3) {
		t.Fatal("a new day must reset the budget")
	}
}

func dirOf(t *testing.T, b *Budget) string {
	t.Helper()
	// b.path is <workspace>/watches/budget.json → the workspace is two up.
	return filepath.Dir(filepath.Dir(b.path))
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func TestRenderNoticeCarriesEvidence(t *testing.T) {
	w := Watch{Kind: KindFlight, Entity: "BA123", Label: "Flight BA123",
		Evidence: []Evidence{{At: time.Date(2026, 9, 28, 14, 2, 0, 0, time.UTC), Source: "aviationstack"}}}
	msg := RenderNotice(w, []Change{{Field: "gate", From: "B12", To: "A4"}})
	if !strings.Contains(msg, "gate B12 → A4") {
		t.Fatalf("notice must state the change: %q", msg)
	}
	if !strings.Contains(msg, "Observed at") || !strings.Contains(msg, "aviationstack") {
		t.Fatalf("notice must cite its evidence: %q", msg)
	}
}

func TestRenderListAndEmpty(t *testing.T) {
	if got := RenderList(nil, time.Now()); !strings.Contains(got, "not watching anything") {
		t.Fatalf("empty list: %q", got)
	}
	now := time.Now().UTC()
	w := Watch{Kind: KindAppointment, Entity: "dentist", Label: "dentist appointment",
		Status: StatusActive, NextCheck: &now}
	got := RenderList([]Watch{w}, now)
	if !strings.Contains(got, "1 watch") || !strings.Contains(got, "dentist appointment") {
		t.Fatalf("list: %q", got)
	}
}

func TestReasonTextIsHonest(t *testing.T) {
	for _, r := range []Reason{ReasonSourceUnavailable, ReasonProactiveDisabled,
		ReasonHorizon, ReasonPastEvent, ReasonDuplicate, ReasonMaxActive, ReasonNoEntity} {
		if ReasonText(Verdict{Reason: r}) == "" {
			t.Fatalf("reason %s has no owner-facing text", r)
		}
	}
}
