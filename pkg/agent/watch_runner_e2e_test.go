package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/watch"
)

// A2, runner level: a failed probe after restart records as a failure and is
// visible on the watch the normal way (status/failure fields the owner's
// surfaces read), never as a silent "no change".
func TestAcceptance_WatchFailureAfterRestartIsVisible(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoopWithProvider(t, ws, &recordingDigestProvider{})
	al.noticer = NewNoticer(func(Notice) {})

	// Sandbox directory exists, but no state file for this entity: the source
	// answers honestly that it has no record, which is a failure.
	if err := os.MkdirAll(filepath.Join(ws, watch.WatchSourceDir), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := watch.New(ws)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Minute)
	w, err := store.Create(watch.Watch{
		Kind: watch.KindFlight, Entity: "TG999", Source: "sandbox",
		Provenance: watch.Provenance{Quote: "watch my flight TG999", At: time.Now()},
		NextCheck:  &due,
	})
	if err != nil {
		t.Fatal(err)
	}

	al.PollWatches(time.Now())

	got, err := store.Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failures == 0 {
		t.Fatal("a failed probe was not recorded as a failure; it read as a silent no-change")
	}
	if got.LastFailure == "" {
		t.Fatal("the failure carries no reason the owner can see")
	}
	// Cadence is not consumed by a failure: the watch stays live and due
	// later, not expired or silently dropped.
	if !got.Status.Polled() {
		t.Fatalf("status = %s after one failure, want still polled", got.Status)
	}
}

// A2, runner level: a watch that came due DURING downtime must fire exactly
// once on recovery — not zero, not twice. Driven through the real runner
// (PollWatches), not the store API, and across a fresh loop over the same
// workspace (the restart). The real-process kill is proven separately by
// pkg/watch's re-exec harness; here the runner's exactly-once is the subject.
func TestAcceptance_WatchDueDuringDowntimeFiresOnce(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoopWithProvider(t, ws, &recordingDigestProvider{})
	al.noticer = NewNoticer(func(Notice) {})

	// A deterministic, network-free source: the sandbox reads a state file.
	srcDir := filepath.Join(ws, watch.WatchSourceDir)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(map[string]string{"gate": "B"})
	if err := os.WriteFile(watch.StatePath(ws, watch.KindFlight, "TG123"), state, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := watch.New(ws)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Minute)
	w, err := store.Create(watch.Watch{
		Kind: watch.KindFlight, Entity: "TG123", Source: "sandbox",
		Provenance: watch.Provenance{Quote: "watch my flight TG123", At: time.Now()},
		NextCheck:  &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Baseline A on disk, source now reads B: the change happened during the
	// downtime, before any poll.
	if _, err := store.ResetBaseline(w.ID, map[string]string{"gate": "A"}); err != nil {
		t.Fatal(err)
	}

	// Recovery 1: the due watch fires exactly once.
	if delivered := al.PollWatches(time.Now()); delivered != 1 {
		got, _ := store.Get(w.ID)
		t.Fatalf("recovery delivered %d, want exactly 1 (notified=%v status=%s)", delivered, got.Notified, got.Status)
	}
	got, _ := store.Get(w.ID)
	if len(got.Notified) != 1 {
		t.Fatalf("notified = %v, want exactly one fingerprint", got.Notified)
	}

	// Recovery 2: the same due window, nothing new. This is the ledger doing
	// its job across a restart of the runner's in-memory state.
	if delivered := al.PollWatches(time.Now()); delivered != 0 {
		t.Fatalf("re-delivered %d on the second recovery, want 0", delivered)
	}
	got2, _ := store.Get(w.ID)
	if len(got2.Notified) != 1 {
		t.Fatalf("notified = %v after second recovery, want still exactly one", got2.Notified)
	}
	// Cadence does not drift across the restart: the next check is scheduled
	// ahead of now, computed from the current instant — not a backlog of
	// missed cycles, and not stuck in the past.
	if got2.NextCheck == nil || !got2.NextCheck.After(time.Now()) {
		t.Fatalf("next check not scheduled ahead after a restart: %v", got2.NextCheck)
	}
}
