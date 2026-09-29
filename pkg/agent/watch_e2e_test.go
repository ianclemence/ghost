package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/proactive"
	"github.com/ianclemence/ghost/pkg/watch"
)

// End-to-end watch tests at the agent layer: the real loop (governance,
// canonical events, noticer, outbox) with the local sandbox as the only
// source — no network, no model decisions. The golden suite proves the
// same contract through full conversations; these prove it fast, in
// isolation, and with the seams (failure injection, budget, quiet hours)
// driven directly.

// newWatchE2ELoop wires the proactive harness plus an available sandbox
// watch source and a pinned probe override (deterministic even if an
// aviation key exists in the environment).
func newWatchE2ELoop(t *testing.T) *AgentLoop {
	t.Helper()
	al, _, _ := newProactiveLoop(t)
	enableWatchSandbox(t, al)
	return al
}

func enableWatchSandbox(t *testing.T, al *AgentLoop) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(al.workspace, watch.WatchSourceDir), 0700); err != nil {
		t.Fatalf("mkdir watch source: %v", err)
	}
	al.SetWatchProbeOverride(watch.NewSandbox(al.workspace))
}

// watchEventCount reads the canonical stream (never a test-local tally).
func watchEventCount(t *testing.T, al *AgentLoop, typ string) int {
	t.Helper()
	var n int
	if err := al.DB().QueryRow(`SELECT COUNT(*) FROM canonical_events WHERE type = ?`, typ).Scan(&n); err != nil {
		t.Fatalf("count %s events: %v", typ, err)
	}
	return n
}

// forceWatchesDue clears every live watch's schedule so the next poll
// cycle probes now — the harness's clock, identical to what time passing
// would do.
func forceWatchesDue(t *testing.T, al *AgentLoop) {
	t.Helper()
	store, err := al.watchStoreFor()
	if err != nil {
		t.Fatalf("open watch store: %v", err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatalf("list watches: %v", err)
	}
	for _, w := range list {
		if !w.Live() {
			continue
		}
		if _, err := store.Wake(w.ID); err != nil {
			t.Fatalf("wake watch %s: %v", w.ID, err)
		}
	}
}

// rewriteWatchLedger edits the durable ledger directly — the same time
// machine the golden WatchScript uses (expiry, schedules), never state
// the runtime itself would refuse to write.
func rewriteWatchLedger(t *testing.T, ws string, fn func(*watch.Watch)) {
	t.Helper()
	path := filepath.Join(ws, watch.Dir, "watches.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var list []watch.Watch
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("parse ledger: %v", err)
	}
	for i := range list {
		fn(&list[i])
	}
	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		t.Fatalf("encode ledger: %v", err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
}

// writeWatchPrefs installs a PROACTIVE_PREFERENCES.md body. Safe after
// construction for everything the runtime re-reads live (quiet hours,
// watch probe budget); the noticer's push budget is construction-time.
func writeWatchPrefs(t *testing.T, al *AgentLoop, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(al.workspace, "PROACTIVE_PREFERENCES.md"), []byte(body), 0644); err != nil {
		t.Fatalf("write prefs: %v", err)
	}
}

// quietWindowAround builds a quiet window in the user's own timezone
// (the same location quiet-hours evaluation uses) that certainly contains
// now — one hour either side, wrap-safe — so the quiet-hours assertions
// are deterministic at any time the suite runs.
func quietWindowAround(t *testing.T) string {
	t.Helper()
	loc := proactive.UserLocation(nil)
	local := time.Now().In(loc)
	start, end := local.Add(-time.Hour), local.Add(time.Hour)
	return fmt.Sprintf("# test quiet window\n`quiet_hours: %02d:%02d - %02d:%02d`\n",
		start.Hour(), start.Minute(), end.Hour(), end.Minute())
}

// The owner-facing control surface: create by name (reply read back from
// the store), dedupe the restatement, list from state, stop durably —
// each with its canonical event.
func TestWatchE2EFastPathLifecycle(t *testing.T) {
	al := newWatchE2ELoop(t)

	reply, handled := al.tryWatchTurn("track my flight BA123 tomorrow", "sess-ctl")
	if !handled {
		t.Fatal("track request was not handled by the watch fast path")
	}
	if !strings.Contains(reply, "Watching Flight BA123") {
		t.Fatalf("create reply = %q, want confirmation read from the store", reply)
	}
	list, err := al.Watches()
	if err != nil || len(list) != 1 {
		t.Fatalf("watches after create = %d (%v), want 1", len(list), err)
	}
	w := list[0]
	if w.Kind != watch.KindFlight || w.Entity != "BA123" || !w.Explicit || w.Source != "sandbox" {
		t.Fatalf("created watch = %+v", w)
	}
	if !strings.Contains(w.Provenance.Quote, "BA123") || w.Provenance.Session == "" {
		t.Fatalf("watch provenance must quote the owner's own words: %+v", w.Provenance)
	}

	// Restating the same thing is one watch, answered from the store.
	reply, handled = al.tryWatchTurn("track my flight BA123 again", "sess-ctl")
	if !handled || !strings.Contains(reply, "already watching") {
		t.Fatalf("duplicate reply = %q (%v)", reply, handled)
	}
	if n := len(mustWatches(t, al)); n != 1 {
		t.Fatalf("duplicate request created %d watches", n)
	}

	// The list question is answered from runtime state.
	ans, ok := al.tryStateQueryTurn("what am I watching", "sess-ctl")
	if !ok || !strings.Contains(ans, "You have 1 watch") || !strings.Contains(ans, "BA123") {
		t.Fatalf("list reply = %q (handled=%v)", ans, ok)
	}

	// Stop closes it durably, and the reply counts what actually closed.
	reply, handled = al.tryWatchTurn("stop watching my flight", "sess-ctl")
	if !handled || !strings.Contains(reply, "Stopped watching Flight BA123") {
		t.Fatalf("stop reply = %q (%v)", reply, handled)
	}
	if got := mustWatches(t, al)[0].Status; got != watch.StatusDisabled {
		t.Fatalf("status after stop = %s, want disabled", got)
	}

	if n := watchEventCount(t, al, "watch.created"); n != 1 {
		t.Fatalf("watch.created = %d, want exactly 1", n)
	}
	if n := watchEventCount(t, al, "watch.cancelled"); n != 1 {
		t.Fatalf("watch.cancelled = %d, want 1", n)
	}
}

// With no source connected, creation refuses honestly — the kind that
// cannot resolve to an available probe never becomes a watch (an
// appointment never resolves to a flight provider, so this is
// environment-proof).
func TestWatchE2ENoSourceRefusal(t *testing.T) {
	al, _, _ := newProactiveLoop(t) // deliberately NO sandbox directory

	reply, handled := al.tryWatchTurn("track my dentist appointment tomorrow", "sess-nosrc")
	if !handled {
		t.Fatal("request not handled by the watch fast path")
	}
	if !strings.Contains(reply, "no source connected") {
		t.Fatalf("refusal = %q, want the honest source refusal", reply)
	}
	if n := len(mustWatches(t, al)); n != 0 {
		t.Fatalf("refused request still created %d watches", n)
	}
	if n := watchEventCount(t, al, "watch.created"); n != 0 {
		t.Fatalf("watch.created = %d, want 0", n)
	}
}

// The deterministic extraction pass: guards refuse everything a watch
// must not be; a dated future sentence in the owner's own words creates
// exactly one watch; restatement never duplicates.
func TestWatchE2EExtractionGuards(t *testing.T) {
	al := newWatchE2ELoop(t)
	extract := func(session, msg string) {
		al.extractWatchesInline(processOptions{
			SessionKey: session, UserMessage: msg, Channel: "web", ChatID: "chat",
			RequestID: "req-" + session,
		})
	}

	refusals := []struct{ session, msg string }{
		{"g-remind", "track my flight BA123 tomorrow and remind me to check in"},
		{"g-routine", "track my dentist appointment on Thursdays"},
		{"g-third", "track her flight BA123 tomorrow"},
		{"g-past", "My flight BA123 yesterday was a mess."},
		{"g-undated", "I fly BA123."},
	}
	for _, r := range refusals {
		extract(r.session, r.msg)
	}
	if n := len(mustWatches(t, al)); n != 0 {
		t.Fatalf("guarded sentences created %d watches", n)
	}
	if n := watchEventCount(t, al, "watch.created"); n != 0 {
		t.Fatalf("watch.created = %d from guarded sentences, want 0", n)
	}

	// The owner's own words, dated and future: a watch, with the quote
	// that justifies it.
	extract("g-create", "I'm flying BA123 tomorrow.")
	list := mustWatches(t, al)
	if len(list) != 1 {
		t.Fatalf("watches after dated sentence = %d, want 1", len(list))
	}
	w := list[0]
	if w.Explicit {
		t.Fatal("automatic watch must not claim it was explicitly requested")
	}
	if !strings.Contains(w.Provenance.Quote, "flying BA123") {
		t.Fatalf("provenance quote = %q", w.Provenance.Quote)
	}
	if w.EventAt == nil || !w.EventAt.After(time.Now()) {
		t.Fatalf("watch must be pinned to a future event, got %v", w.EventAt)
	}

	// The same thing said again: still one watch, still one event.
	extract("g-create-again", "By the way, I'm flying BA123 tomorrow.")
	if n := len(mustWatches(t, al)); n != 1 {
		t.Fatalf("restatement produced %d watches", n)
	}
	if n := watchEventCount(t, al, "watch.created"); n != 1 {
		t.Fatalf("watch.created = %d, want 1", n)
	}
}

// The full background loop against the sandbox source: baseline observes
// silently, a changed world notifies once with evidence, an already-
// delivered fingerprint never speaks again, and a genuinely new value
// speaks again — all through PollWatches, never a model.
func TestWatchE2EChangeNoticeLifecycle(t *testing.T) {
	al := newWatchE2ELoop(t)
	ws := al.workspace
	if _, handled := al.tryWatchTurn("track my flight BA123 tomorrow", "sess-poll"); !handled {
		t.Fatal("watch not created")
	}
	state := func(gate string) {
		if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
			"gate": gate, "status": "on time",
		}); err != nil {
			t.Fatalf("set state: %v", err)
		}
	}
	now := time.Now().UTC()

	// Baseline: first observation records what the world looks like.
	state("A1")
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("baseline poll delivered %d notices, want 0", got)
	}
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("unchanged poll delivered %d notices, want 0", got)
	}
	if n := watchEventCount(t, al, "watch.changed"); n != 0 {
		t.Fatalf("watch.changed = %d before any change, want 0", n)
	}

	// The gate moves: one change, one notice, one recorded fingerprint.
	state("B12")
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 1 {
		t.Fatalf("changed poll delivered %d notices, want 1", got)
	}
	w := mustWatches(t, al)[0]
	if len(w.Notified) != 1 || !strings.Contains(w.Notified[0], ":gate:") {
		t.Fatalf("notified fingerprints = %v, want the gate change", w.Notified)
	}
	if len(w.Evidence) < 3 {
		t.Fatalf("evidence = %d probes, want the poll cycle recorded", len(w.Evidence))
	}

	// Still changed (the baseline is fixed), but delivered fingerprints
	// are spent: the changed event repeats, the notice does not.
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("repeat poll delivered %d notices, want 0", got)
	}
	if n := watchEventCount(t, al, "watch.changed"); n != 2 {
		t.Fatalf("watch.changed = %d, want 2 (recomputed each poll)", n)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 1 {
		t.Fatalf("watch.notified = %d, want 1", n)
	}

	// A genuinely new value is a new fingerprint: it notifies again.
	state("C7")
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 1 {
		t.Fatalf("new value delivered %d notices, want 1", got)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 2 {
		t.Fatalf("watch.notified = %d, want 2", n)
	}

	// Everything was delivered to the live session: the held outbox stays
	// empty (quiet hours are off, the channel is external).
	if held := heldCount(ws, time.Now()); held != 0 {
		t.Fatalf("outbox holds %d notices, want 0 (delivered live)", held)
	}
}

// Failures are honest: a transient one backs off and recovers, five
// consecutive ones end the watch and say so exactly once.
func TestWatchE2EFailureLifecycle(t *testing.T) {
	al := newWatchE2ELoop(t)
	ws := al.workspace
	if _, handled := al.tryWatchTurn("track my flight BA123 tomorrow", "sess-fail"); !handled {
		t.Fatal("watch not created")
	}
	now := time.Now().UTC()
	// Observable state exists, so a post-recovery probe can succeed.
	if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
		"status": "on time",
	}); err != nil {
		t.Fatalf("set state: %v", err)
	}

	// Transient failure: counted, status unchanged, next poll recovers.
	if err := watch.SetError(ws, watch.KindFlight, "BA123", "source offline"); err != nil {
		t.Fatalf("set error: %v", err)
	}
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("failed probe delivered %d notices, want 0", got)
	}
	if n := watchEventCount(t, al, "watch.failed"); n != 1 {
		t.Fatalf("watch.failed = %d, want 1", n)
	}
	if w := mustWatches(t, al)[0]; w.Status != watch.StatusActive || w.Failures != 1 {
		t.Fatalf("after transient failure: status=%s failures=%d", w.Status, w.Failures)
	}

	if err := watch.SetError(ws, watch.KindFlight, "BA123", ""); err != nil {
		t.Fatalf("clear error: %v", err)
	}
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("recovered baseline delivered %d notices, want 0", got)
	}
	if w := mustWatches(t, al)[0]; w.Status != watch.StatusActive || w.Failures != 0 {
		t.Fatalf("after recovery: status=%s failures=%d", w.Status, w.Failures)
	}

	// Terminal: five consecutive failures end the watch and notify once.
	if err := watch.SetError(ws, watch.KindFlight, "BA123", "unavailable: source offline"); err != nil {
		t.Fatalf("set error: %v", err)
	}
	last := 0
	for i := 0; i < watch.DefaultMaxFailures; i++ {
		forceWatchesDue(t, al)
		last = al.PollWatches(now)
	}
	if w := mustWatches(t, al)[0]; w.Status != watch.StatusFailed {
		t.Fatalf("status after %d failures = %s, want failed", watch.DefaultMaxFailures, w.Status)
	}
	if n := watchEventCount(t, al, "watch.failed"); n != watch.DefaultMaxFailures+1 {
		t.Fatalf("watch.failed = %d, want %d (transient + terminal run)", n, watch.DefaultMaxFailures+1)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 1 {
		t.Fatalf("watch.notified = %d, want exactly 1 terminal failure notice", n)
	}
	if last != 1 {
		t.Fatalf("terminal failure poll delivered %d, want 1", last)
	}
}

// Expiry: an explicit watch says a proper goodbye; an automatic one ends
// silently — nobody asked for it, so its ending is not a message.
func TestWatchE2EExpiry(t *testing.T) {
	al := newWatchE2ELoop(t)
	ws := al.workspace
	now := time.Now().UTC()

	if _, handled := al.tryWatchTurn("track my flight BA123", "sess-exp"); !handled {
		t.Fatal("explicit watch not created")
	}
	rewriteWatchLedger(t, ws, func(w *watch.Watch) {
		if w.Live() {
			past := now.Add(-time.Minute)
			w.ExpiresAt = &past
		}
	})
	_ = al.PollWatches(now)
	if w := mustWatches(t, al)[0]; w.Status != watch.StatusExpired {
		t.Fatalf("explicit watch status = %s, want expired", w.Status)
	}
	if n := watchEventCount(t, al, "watch.expired"); n != 1 {
		t.Fatalf("watch.expired = %d, want 1", n)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 1 {
		t.Fatalf("watch.notified = %d, explicit expiry must say goodbye once", n)
	}

	// Automatic watch: same ending, no message.
	al2 := newWatchE2ELoop(t)
	al2.extractWatchesInline(processOptions{
		SessionKey: "sess-exp-auto", UserMessage: "I have a dentist appointment tomorrow.",
		Channel: "web", ChatID: "chat", RequestID: "req-exp-auto",
	})
	if len(mustWatches(t, al2)) != 1 {
		t.Fatal("automatic watch not created")
	}
	rewriteWatchLedger(t, al2.workspace, func(w *watch.Watch) {
		if w.Live() {
			past := now.Add(-time.Minute)
			w.ExpiresAt = &past
		}
	})
	_ = al2.PollWatches(now)
	if w := mustWatches(t, al2)[0]; w.Status != watch.StatusExpired {
		t.Fatalf("automatic watch status = %s, want expired", w.Status)
	}
	if n := watchEventCount(t, al2, "watch.expired"); n != 1 {
		t.Fatalf("watch.expired = %d, want 1", n)
	}
	if n := watchEventCount(t, al2, "watch.notified"); n != 0 {
		t.Fatalf("watch.notified = %d, automatic expiry must be silent", n)
	}
}

// The daily probe budget is a hard ceiling: once spent, a due watch does
// not touch the source at all — no probe, no evidence, no notice.
func TestWatchE2EProbeBudget(t *testing.T) {
	al := newWatchE2ELoop(t)
	ws := al.workspace
	writeWatchPrefs(t, al, "# test\n`max_watch_checks_per_day: 1`\n")
	if _, handled := al.tryWatchTurn("track my flight BA123 tomorrow", "sess-budget"); !handled {
		t.Fatal("watch not created")
	}
	if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
		"gate": "A1", "status": "on time",
	}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	now := time.Now().UTC()

	// Probe 1 of 1: baseline.
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("baseline delivered %d, want 0", got)
	}
	// The world changes, but the budget is spent.
	if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
		"gate": "B12", "status": "on time",
	}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("over-budget poll delivered %d, want 0", got)
	}
	w := mustWatches(t, al)[0]
	if len(w.Evidence) != 1 {
		t.Fatalf("evidence = %d probes, budget must stop the probe itself", len(w.Evidence))
	}
	if n := watchEventCount(t, al, "watch.changed"); n != 0 {
		t.Fatalf("watch.changed = %d, want 0 (never probed)", n)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 0 {
		t.Fatalf("watch.notified = %d, want 0", n)
	}
}

// Quiet hours hold the non-urgent change in the outbox for later; an
// urgent change (a cancellation) breaks through to the live channel.
// The window is built around now, so the test is deterministic at any
// hour it runs.
func TestWatchE2EQuietHoursHoldAndUrgentBreakthrough(t *testing.T) {
	al := newWatchE2ELoop(t)
	ws := al.workspace
	now := time.Now().UTC()
	writeWatchPrefs(t, al, quietWindowAround(t))
	if _, handled := al.tryWatchTurn("track my flight BA123 tomorrow", "sess-quiet"); !handled {
		t.Fatal("watch not created")
	}
	if !al.ProactiveQuiet(now) {
		t.Fatal("test quiet window must contain now")
	}

	if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
		"gate": "A1", "status": "on time",
	}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	if got := al.PollWatches(now); got != 0 {
		t.Fatalf("baseline delivered %d, want 0", got)
	}

	// Gate change: approved by the noticer, held by quiet hours.
	if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
		"gate": "B12", "status": "on time",
	}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 1 {
		t.Fatalf("quiet change delivered %d, want 1 (approved then held)", got)
	}
	if held := heldCount(ws, time.Now()); held != 1 {
		t.Fatalf("outbox holds %d, want 1 held for morning", held)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 1 {
		t.Fatalf("watch.notified = %d, want 1 (approval is canonical even when held)", n)
	}

	// Cancellation: urgent, breaks through quiet hours — not held.
	if err := watch.SetState(ws, watch.KindFlight, "BA123", map[string]string{
		"gate": "B12", "status": "cancelled",
	}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	forceWatchesDue(t, al)
	if got := al.PollWatches(now); got != 1 {
		t.Fatalf("urgent change delivered %d, want 1", got)
	}
	if held := heldCount(ws, time.Now()); held != 1 {
		t.Fatalf("outbox holds %d, urgent notice must break through quiet hours", held)
	}
	if n := watchEventCount(t, al, "watch.notified"); n != 2 {
		t.Fatalf("watch.notified = %d, want 2", n)
	}
}

// The whole path through a real turn: the sentence arrives as a message,
// the deferred extraction pass creates the durable watch, and the
// restatement does not duplicate it.
func TestWatchE2ETurnCreatesWatch(t *testing.T) {
	al := newWatchE2ELoop(t)
	ctx := context.Background()

	if _, err := al.ProcessDirectWithChannel(ctx, "I'm flying BA123 tomorrow.",
		"sess-turn", "web", "chat", nil, nil, nil); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	al.FlushDeferred() // deferred extraction, drained like production settles it

	list := mustWatches(t, al)
	if len(list) != 1 || list[0].Kind != watch.KindFlight || list[0].Entity != "BA123" {
		t.Fatalf("watches after turn = %+v", list)
	}
	if list[0].Explicit {
		t.Fatal("sentence-spawned watch must be automatic, not explicit")
	}

	if _, err := al.ProcessDirectWithChannel(ctx, "By the way, I'm flying BA123 tomorrow.",
		"sess-turn", "web", "chat", nil, nil, nil); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	al.FlushDeferred()
	if n := len(mustWatches(t, al)); n != 1 {
		t.Fatalf("restatement through a real turn produced %d watches", n)
	}
	if n := watchEventCount(t, al, "watch.created"); n != 1 {
		t.Fatalf("watch.created = %d, want 1", n)
	}
}

func mustWatches(t *testing.T, al *AgentLoop) []watch.Watch {
	t.Helper()
	list, err := al.Watches()
	if err != nil {
		t.Fatalf("list watches: %v", err)
	}
	return list
}
