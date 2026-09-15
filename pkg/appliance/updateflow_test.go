package appliance

import (
	"errors"
	"strings"
	"testing"
)

type callLog struct {
	calls []string
}

func (l *callLog) fn(name string, err error) func() error {
	return func() error {
		l.calls = append(l.calls, name)
		return err
	}
}

func (l *callLog) stop(name string) func() {
	return func() { l.calls = append(l.calls, name) }
}

// Planning failure must abort before services are touched.
func TestRunUpdatePlanFailureLeavesServicesRunning(t *testing.T) {
	var log callLog
	err := RunUpdate(UpdateSteps{
		Pull:  log.fn("pull", nil),
		Plan:  log.fn("plan", errors.New("vault: no master key")),
		Stop:  log.stop("stop"),
		Apply: log.fn("apply", nil),
		Start: log.fn("start", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "services untouched") {
		t.Fatalf("plan failure must abort with untouched-services error, got: %v", err)
	}
	for _, c := range log.calls {
		if c == "stop" || c == "apply" || c == "start" {
			t.Fatalf("post-plan step %q ran after a planning failure: %v", c, log.calls)
		}
	}
}

// Apply failure must trigger a best-effort restart and report it.
func TestRunUpdateApplyFailureRestartsServices(t *testing.T) {
	var log callLog
	err := RunUpdate(UpdateSteps{
		Pull:  log.fn("pull", nil),
		Plan:  log.fn("plan", nil),
		Stop:  log.stop("stop"),
		Apply: log.fn("apply", errors.New("build broke")),
		Start: log.fn("start", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "services restarted") {
		t.Fatalf("apply failure must report the restart, got: %v", err)
	}
	if len(log.calls) != 5 || log.calls[4] != "start" {
		t.Fatalf("start must run last after apply failure: %v", log.calls)
	}
}

// A failed restart must surface both errors, not swallow the restart one.
func TestRunUpdateRestartFailureReportsBoth(t *testing.T) {
	var log callLog
	err := RunUpdate(UpdateSteps{
		Pull:  log.fn("pull", nil),
		Plan:  log.fn("plan", nil),
		Stop:  log.stop("stop"),
		Apply: log.fn("apply", errors.New("build broke")),
		Start: log.fn("start", errors.New("systemctl missing")),
	})
	if err == nil || !strings.Contains(err.Error(), "build broke") || !strings.Contains(err.Error(), "systemctl missing") {
		t.Fatalf("both errors must be reported, got: %v", err)
	}
	_ = log
}

// Success runs every stage exactly once and never restarts.
func TestRunUpdateSuccessOrder(t *testing.T) {
	var log callLog
	if err := RunUpdate(UpdateSteps{
		Pull:  log.fn("pull", nil),
		Plan:  log.fn("plan", nil),
		Stop:  log.stop("stop"),
		Apply: log.fn("apply", nil),
		Start: log.fn("start", nil),
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"pull", "plan", "stop", "apply"}
	if strings.Join(log.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", log.calls, want)
	}
}

// Pull failure aborts before planning.
func TestRunUpdatePullFailureAbortsFirst(t *testing.T) {
	var log callLog
	if err := RunUpdate(UpdateSteps{
		Pull:  log.fn("pull", errors.New("no network")),
		Plan:  log.fn("plan", nil),
		Stop:  log.stop("stop"),
		Apply: log.fn("apply", nil),
		Start: log.fn("start", nil),
	}); err == nil {
		t.Fatal("pull failure must abort")
	}
	if len(log.calls) != 1 || log.calls[0] != "pull" {
		t.Fatalf("only pull may run: %v", log.calls)
	}
}
