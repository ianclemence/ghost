package setup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testSteps(dir string, failLocalAI bool) []Step {
	ready := map[StepID]bool{}
	mk := func(id StepID, required bool) Step {
		return Step{
			ID: id, Human: string(id), Required: required,
			Check: func() (bool, string) {
				if ready[id] {
					return true, "already provisioned"
				}
				// Disk-backed check for storage: dir exists.
				if id == StepStorage {
					if _, err := os.Stat(filepath.Join(dir, "data")); err == nil {
						return true, "data dir exists"
					}
				}
				return false, ""
			},
			Apply: func() error {
				if id == StepLocalAI && failLocalAI {
					return errors.New("model download failed (network loss)")
				}
				if id == StepStorage {
					if err := os.MkdirAll(filepath.Join(dir, "data"), 0700); err != nil {
						return err
					}
				}
				ready[id] = true
				return nil
			},
			Validate: func() error {
				if !ready[id] {
					return errors.New("not applied")
				}
				return nil
			},
		}
	}
	return []Step{mk(StepStorage, true), mk(StepLocalAI, true), mk(StepBackup, false)}
}

func TestIdempotentDoubleRun(t *testing.T) {
	dir := t.TempDir()
	o, err := New(dir, testSteps(dir, false))
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Status(); got != NotInitialized {
		t.Fatalf("fresh must be not_initialized, got %s", got)
	}
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}
	if got := o.Status(); got != Ready {
		t.Fatalf("expected ready, got %s", got)
	}
	// Second run must not destroy state.
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}
	if got := o.Status(); got != Ready {
		t.Fatalf("second run must stay ready, got %s", got)
	}
}

func TestInterruptedResume(t *testing.T) {
	dir := t.TempDir()
	o, _ := New(dir, testSteps(dir, true))
	if err := o.Run(); err == nil {
		t.Fatal("expected failure on local_ai")
	}
	if got := o.Status(); got != ActionRequired {
		t.Fatalf("failed required step -> action_required, got %s", got)
	}
	if got := o.StepState(StepStorage); got != StepDone {
		t.Fatalf("completed step must keep checkpoint, got %s", got)
	}
	// Simulate reboot: new orchestrator on same dir, now healthy.
	o2, _ := New(dir, testSteps(dir, false))
	if got := o2.StepState(StepStorage); got != StepDone {
		t.Fatalf("checkpoint must survive restart, got %s", got)
	}
	// Storage check passes via disk even though in-memory map is fresh.
	if err := o2.Run(); err != nil {
		t.Fatal(err)
	}
	if got := o2.Status(); got != Ready {
		t.Fatalf("resume must reach ready, got %s", got)
	}
}

func TestOptionalFailureDegrades(t *testing.T) {
	dir := t.TempDir()
	steps := []Step{{
		ID: StepBackup, Human: "Backup", Required: false,
		Check:    func() (bool, string) { return false, "" },
		Apply:    func() error { return errors.New("backup target unreachable") },
		Validate: func() error { return nil },
	}}
	o, _ := New(dir, steps)
	_ = o.Run()
	if got := o.Status(); got != Degraded && got != ActionRequired {
		t.Fatalf("optional failure must not read ready, got %s", got)
	}
}

func TestResetStep(t *testing.T) {
	dir := t.TempDir()
	o, _ := New(dir, testSteps(dir, false))
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}
	o.ResetStep(StepBackup)
	if got := o.StepState(StepBackup); got != StepPending {
		t.Fatalf("reset must clear checkpoint, got %s", got)
	}
	if got := o.StepState(StepStorage); got != StepDone {
		t.Fatal("reset must not touch other steps")
	}
}
