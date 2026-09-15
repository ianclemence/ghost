package appliance

import "fmt"

// UpdateSteps are the injectable stages of an appliance update. Splitting
// planning from mutation is what makes updates crash-safe: Plan must be
// read-only (config load, migration dry-check), while Apply performs the
// workspace move and the rebuild/reinstall.
type UpdateSteps struct {
	Pull     func() error // fetch latest changes (no service impact)
	Plan     func() error // read-only validation; must not stop services or write disk
	Snapshot func() error // recovery snapshot; nil skips (tests only — production always wires it)
	Stop     func()       // quiesce services before mutation
	Apply    func() error // migrate workspace + build/install (services restart on success path)
	Start    func() error // best-effort service restart after a failed Apply
}

// RunUpdate executes pull → read-only plan → snapshot → stop → apply.
//
// Planning and snapshotting run while services are still up: a planning
// failure or a failed snapshot aborts before anything stops, so a broken
// update changes nothing and every applied update is recoverable. Once
// services are stopped, any Apply failure triggers a best-effort Start
// so an update can never leave the appliance dark; both errors reported.
func RunUpdate(s UpdateSteps) error {
	if s.Pull == nil || s.Plan == nil || s.Apply == nil {
		return fmt.Errorf("update: pull, plan, and apply steps are required")
	}
	stop := s.Stop
	if stop == nil {
		stop = func() {}
	}
	start := s.Start
	if start == nil {
		start = func() error { return nil }
	}
	if err := s.Pull(); err != nil {
		return fmt.Errorf("update pull: %w", err)
	}
	if err := s.Plan(); err != nil {
		return fmt.Errorf("update plan: %w (services untouched)", err)
	}
	if s.Snapshot != nil {
		if err := s.Snapshot(); err != nil {
			return fmt.Errorf("update snapshot: %w (services untouched; fix backups before updating)", err)
		}
	}
	stop()
	if err := s.Apply(); err != nil {
		if serr := start(); serr != nil {
			return fmt.Errorf("update apply: %v; service restart also failed: %v", err, serr)
		}
		return fmt.Errorf("update apply: %v (services restarted)", err)
	}
	return nil
}

// CheckWorkspaceMigration is the read-only half of the workspace
// migration: it loads the appliance config (vault included) and reports
// whether a move is needed, without moving anything. Update planning
// calls this before services stop so a sealed-config failure aborts
// early instead of stranding stopped services.
func CheckWorkspaceMigration(ghostDir string) error {
	plan, err := PlanWorkspaceMigrationFromDisk(ghostDir, "")
	if err != nil {
		return err
	}
	if !plan.Needed {
		fmt.Printf("  Workspace layout OK: %s\n", plan.Reason)
		return nil
	}
	fmt.Printf("  Workspace migration pending: %s (applies after services stop)\n", plan.Reason)
	return nil
}
