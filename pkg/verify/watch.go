package verify

import (
	"fmt"
	"time"

	"github.com/ianclemence/ghost/pkg/watch"
)

// Selective proactive watch lifecycle: the deterministic guarantees that keep
// Ghost from watching the wrong thing, from watching it twice, and from
// claiming it noticed when it could not. This check runs on a scratch
// workspace with no model and no network — it is the creation/diff/budget
// contract, not the execution path (probing through real and sandbox sources
// is proven by pkg/agent tests and the golden suite).
func checkWatchLifecycle(e *Env) Check {
	store, err := watch.New(e.Workspace)
	if err != nil {
		return fail("Watches", "watch store opens", err.Error(), true)
	}
	now := time.Now().UTC()

	// Provenance is mandatory: a watch Ghost cannot show the origin of is
	// one it may not run.
	if _, err := store.Create(watch.Watch{Kind: watch.KindFlight, Entity: "BA123"}); err == nil {
		return fail("Watches", "provenance required", "a watch without the owner's words was accepted", true)
	}
	// Unknown kinds and empty entities are refused.
	if _, err := store.Create(watch.Watch{
		Kind: watch.Kind("asteroid"), Entity: "BA123",
		Provenance: watch.Provenance{Session: "main", MessageID: "m1", Quote: "watch my flight BA123 tomorrow", At: now},
	}); err == nil {
		return fail("Watches", "closed kind vocabulary", "an unknown watch kind was accepted", true)
	}
	if _, err := store.Create(watch.Watch{
		Kind: watch.KindFlight, Entity: "  ",
		Provenance: watch.Provenance{Session: "main", MessageID: "m1", Quote: "watch my flight tomorrow", At: now},
	}); err == nil {
		return fail("Watches", "entity required", "a watch with no entity was accepted", true)
	}

	// A restatement is the same watch, not a second watcher of the same
	// state.
	quote := watch.Provenance{Session: "main", MessageID: "m1", Quote: "watch my flight BA123 tomorrow", At: now}
	w, err := store.Create(watch.Watch{Kind: watch.KindFlight, Entity: "BA123", Label: "Flight BA123", Source: "sandbox", Provenance: quote})
	if err != nil {
		return fail("Watches", "watch recorded", err.Error(), true)
	}
	again, err := store.Create(watch.Watch{
		Kind: watch.KindFlight, Entity: " ba123 ", Source: "sandbox",
		Provenance: watch.Provenance{Session: "main", MessageID: "m2", Quote: "watch my flight BA123 tomorrow", At: now.Add(time.Minute)},
	})
	if err != nil || again.ID != w.ID {
		return fail("Watches", "restatement dedupes", "the same flight was watched twice", true)
	}

	// Extraction guards: a reminder, a routine, somebody else's affair, an
	// undated guess and a past event never become a watch.
	for _, msg := range []string{
		"Remind me to track my flight BA123 tomorrow",
		"check my flight BA123 every Thursday",
		"her flight BA123 lands tomorrow",
		"my friend's dentist appointment is tomorrow",
		"I fly BA123",                          // no stated time, not an explicit request
		"my dentist appointment was yesterday", // history, not a watch
	} {
		if got := watch.Detect(msg, now, "UTC"); len(got) != 0 {
			return fail("Watches", "extraction guards", "not a watch: "+msg, true)
		}
	}
	// A real one is, with the day resolved by the runtime's own parser.
	good := watch.Detect("my flight BA123 tomorrow", now, "UTC")
	if len(good) != 1 || good[0].Kind != watch.KindFlight || good[0].Entity != "BA123" ||
		good[0].EventAt == nil || good[0].Quote == "" {
		return fail("Watches", "a real watch is extracted with its date", "extraction did not produce a dated flight", true)
	}

	// The policy gate, in its fixed order. No source connected: refused
	// honestly rather than watched with a probe that cannot answer.
	pol := watch.Default()
	pol.Sources = nil
	if v := pol.Allow(good[0], nil, now, ""); v.Allow || v.Reason != watch.ReasonSourceUnavailable {
		return fail("Watches", "unwatchable is refused", "a candidate with no source was allowed ("+string(v.Reason)+")", true)
	}
	pol.Sources = []string{"sandbox"}
	// Past events and duplicates are refused even with a source.
	past := good[0]
	yesterday := now.Add(-time.Hour)
	past.EventAt = &yesterday
	if v := pol.Allow(past, nil, now, "sandbox"); v.Allow || v.Reason != watch.ReasonPastEvent {
		return fail("Watches", "past events are refused", "a past event was allowed", true)
	}
	if v := pol.Allow(good[0], []watch.Watch{w}, now, "sandbox"); v.Allow || v.Reason != watch.ReasonDuplicate {
		return fail("Watches", "duplicates are refused", "a second watch of a live entity was allowed", true)
	}
	// The master switch gates Ghost volunteering, never an owner's request.
	off := watch.Default()
	off.Sources = []string{"sandbox"}
	off.Enabled = false
	if v := off.Allow(good[0], nil, now, "sandbox"); v.Allow || v.Reason != watch.ReasonProactiveDisabled {
		return fail("Watches", "automatic creation obeys the switch", "a disabled proactive policy allowed a volunteer watch", true)
	}
	explicit := good[0]
	explicit.Explicit = true
	if v := off.Allow(explicit, nil, now, "sandbox"); !v.Allow {
		return fail("Watches", "explicit request bypasses the switch", string(v.Reason), true)
	}

	// Diff is deterministic: alphabetical, whitespace-blind, metadata-blind,
	// and a real change either way.
	base := map[string]string{"gate": "A", "status": "on time", "_probe": "1", "terminal": "B"}
	cur := map[string]string{"gate": "B", "status": "on  time", "_probe": "2", "terminal": "B"}
	ch := watch.Diff(base, cur)
	if len(ch) != 1 || ch[0].Field != "gate" || ch[0].From != "A" || ch[0].To != "B" {
		return fail("Watches", "change detection", "expected only the gate change, got a whitespace/metadata change", true)
	}
	ordered := watch.Diff(
		map[string]string{"z": "1", "a": "1", "m": "1"},
		map[string]string{"z": "2", "a": "2", "m": "2"})
	for i, want := range []string{"a", "m", "z"} {
		if len(ordered) != 3 || ordered[i].Field != want {
			return fail("Watches", "diff is deterministic", "changes are not in alphabetical order", true)
		}
	}
	// A reversal is a new fingerprint (it notifies again); a repeat is not.
	fwd := ch[0].Fingerprint(w.ID)
	rev := watch.Change{Field: "gate", From: "B", To: "A"}.Fingerprint(w.ID)
	if fwd == rev {
		return fail("Watches", "fingerprints distinguish reversal", "a reversal reused the original fingerprint", true)
	}
	want := (watch.Change{Field: "gate", From: "A", To: "B"}).Fingerprint(w.ID)
	if fwd != want {
		return fail("Watches", "fingerprints are stable", "the same change produced a different fingerprint", true)
	}

	// The store is the record of delivery: one fingerprint is sent once,
	// across restarts.
	sent, _, err := store.MarkNotified(w.ID, fwd)
	if err != nil || !sent {
		return fail("Watches", "notice recorded once", "the first delivery was not recorded", true)
	}
	if sent, _, err := store.MarkNotified(w.ID, fwd); err != nil || sent {
		return fail("Watches", "restart-safe dedupe", "an already-delivered notice was sent again", true)
	}

	// Honest failure: retries below the budget, StatusFailed after it.
	for i := 1; i < watch.DefaultMaxFailures; i++ {
		fw, err := store.RecordFailure(w.ID, "offline: no route to host", now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			return fail("Watches", "failure recorded", err.Error(), true)
		}
		if fw.Status != watch.StatusActive {
			return fail("Watches", "failure is retried first", "one failure killed the watch", true)
		}
	}
	fw, err := store.RecordFailure(w.ID, "offline: no route to host", now)
	if err != nil {
		return fail("Watches", "failure recorded", err.Error(), true)
	}
	if fw.Status != watch.StatusFailed {
		return fail("Watches", "exhausted failure says so", "a watch that could never be probed still claimed to watch", true)
	}

	// The daily probe budget is a hard ceiling.
	b, err := watch.OpenBudget(e.Workspace)
	if err != nil {
		return fail("Watches", "budget opens", err.Error(), true)
	}
	if !b.Allow(now, 2) || !b.Allow(now, 2) || b.Allow(now, 2) {
		return fail("Watches", "daily budget caps probes", "the budget did not stop the third probe of a two-probe day", true)
	}
	if used := b.Used(now); used != 2 {
		return fail("Watches", "budget accounting", fmt.Sprintf("budget counted %d, want 2", used), true)
	}

	return pass("Watches", "watch lifecycle (provenance, dedupe, guards, diff, policy, failure, budget)")
}
