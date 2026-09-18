package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/hardware"
)

// Health checks: disk pressure with remedies, vault openability,
// last golden score, eval spend, and failing routines. Each follows the
// checkFoo pattern (start/latency, ok/warning/error/info) and carries a
// next action in the message — health output is read by owners, not
// engineers.

// SetConfigPath binds the Ghost config for the vault check.
// Empty means unbound: the check reports info instead of guessing
// layout (the config dir is not derivable from the workspace).
func (d *Doctor) SetConfigPath(path string) {
	if d == nil {
		return
	}
	d.configPath = path
}

// checkDiskPressure promotes hardware thresholds to actionable health:
// warn/error with free-space numbers and the prune remedy.
func (d *Doctor) checkDiskPressure(ctx context.Context) CheckResult {
	start := time.Now()
	s := hardware.Snapshot(d.workspace)
	msg := fmt.Sprintf("disk %d GB free (%d%%)", s.DiskFreeGB, s.DiskFreePct)
	status := "ok"
	switch s.Worst() {
	case hardware.PressureCritical:
		status = "error"
		msg += " — critical: free space now or run `ghost state prune` to drop old snapshots"
	case hardware.PressureWarning:
		status = "warning"
		msg += " — running low: snapshots and derived caches will be trimmed first"
	}
	_ = ctx
	return CheckResult{Name: "disk_pressure", Label: "Disk", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
}

// checkVault proves the sealed secrets file opens with the resolved key
// — without minting anything, and without touching credential storage
// directly (the Vault authority owns that boundary). A stray .master-key
// beside a live .master-env is a warning: it means a read path once
// generated instead of resolving, and a future rotation could misread
// the room.
func (d *Doctor) checkVault(ctx context.Context) CheckResult {
	start := time.Now()
	done := func(status, msg string) CheckResult {
		return CheckResult{Name: "vault", Label: "Vault", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
	}
	_ = ctx
	if d.configPath == "" {
		return done("info", "no config bound; vault check skipped")
	}
	vault := credentials.New(filepath.Dir(d.configPath))
	path, ok, err := vault.Sealed()
	if err != nil {
		return done("error", fmt.Sprintf("sealed secrets do not open: %v (restore with the master key)", err))
	}
	if !ok {
		return done("ok", "no sealed secrets file yet (first boot)")
	}
	dir := filepath.Dir(path)
	if _, err := os.Stat(filepath.Join(dir, ".master-env")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, ".master-key")); err == nil {
			return done("warning", "sealed secrets open, but a stray .master-key sits beside the live .master-env — remove it so rotation reads one key")
		}
	}
	return done("ok", "sealed secrets open with the resolved key")
}

// goldenScore is the minimal history-file shape the health check reads.
// Parsed locally (not via pkg/golden) because golden imports the agent
// loop, which transitively imports doctor — a direct import would cycle.
// The file format is stable JSON; unknown fields are ignored.
type goldenScore struct {
	At       string `json:"at"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Summary  struct {
		Total     int `json:"total"`
		Passed    int `json:"passed"`
		Failed    int `json:"failed"`
		HardFails int `json:"hard_fails"`
	} `json:"summary"`
}

// checkLastGolden reports the newest golden score for the workspace, or
// info when no run exists yet. A score with hard fails is a warning even
// at 100%: hard fails are never clean.
func (d *Doctor) checkLastGolden(ctx context.Context) CheckResult {
	start := time.Now()
	done := func(status, msg string) CheckResult {
		return CheckResult{Name: "last_golden", Label: "Golden", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
	}
	_ = ctx
	raw, err := os.ReadFile(filepath.Join(d.workspace, "state", "golden-history.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return done("info", "no golden runs recorded yet")
		}
		return done("warning", fmt.Sprintf("cannot read golden history: %v", err))
	}
	var entries []goldenScore
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return done("warning", "golden history unreadable")
	}
	last := entries[len(entries)-1]
	if last.Summary.Total == 0 {
		return done("info", "golden history holds no completed runs")
	}
	if last.Summary.HardFails > 0 {
		return done("warning", fmt.Sprintf("last golden: %d/%d with %d HARD FAILS (%s)", last.Summary.Passed, last.Summary.Total, last.Summary.HardFails, last.At))
	}
	if last.Summary.Passed < last.Summary.Total {
		return done("warning", fmt.Sprintf("last golden: %d/%d (%s, %s) — some cases failing, see `ghost golden --compare`", last.Summary.Passed, last.Summary.Total, last.Model, last.At))
	}
	return done("ok", fmt.Sprintf("last golden: %d/%d (%s, %s)", last.Summary.Passed, last.Summary.Total, last.Model, last.At))
}

// checkEvalSpend sums recorded turn costs from canonical events. Empty
// means uninstrumented-or-idle, reported as info — never as spend.
func (d *Doctor) checkEvalSpend(ctx context.Context) CheckResult {
	start := time.Now()
	done := func(status, msg string) CheckResult {
		return CheckResult{Name: "eval_spend", Label: "Spend", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
	}
	_ = ctx
	if d.db == nil {
		return done("info", "no database bound; spend unknown")
	}
	var total float64
	var turns int64
	err := d.db.QueryRow(`SELECT COALESCE(SUM(CAST(json_extract(payload,'$.cost_usd') AS REAL)),0), COUNT(*) FROM canonical_events WHERE type='usage.recorded'`).Scan(&total, &turns)
	if err != nil {
		return done("info", "usage not recorded yet (cost accounting lands with turn persistence)")
	}
	if turns == 0 {
		return done("info", "no metered turns yet")
	}
	return done("ok", fmt.Sprintf("$%.4f across %d metered turns", total, turns))
}

// checkRoutinesFailing lists scheduled items in failed state with the
// newest failure's name and error. Direct SQL: the routines service has
// no failed-only query, and health must not pay List+N×history.
func (d *Doctor) checkRoutinesFailing(ctx context.Context) CheckResult {
	start := time.Now()
	done := func(status, msg string) CheckResult {
		return CheckResult{Name: "routines_failing", Label: "Routines", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
	}
	_ = ctx
	if d.db == nil {
		return done("info", "no database bound; routines unknown")
	}
	var count int64
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM scheduled_items WHERE state='failed'`).Scan(&count); err != nil {
		// Table may not exist on old schemas: not a health failure.
		return done("info", "routine store unavailable")
	}
	if count == 0 {
		return done("ok", "no failing routines")
	}
	var title, lastErr, lastRun string
	_ = d.db.QueryRow(`SELECT COALESCE(title,''), COALESCE(last_error,''), COALESCE(last_run_at,'') FROM scheduled_items WHERE state='failed' ORDER BY last_run_at DESC LIMIT 1`).Scan(&title, &lastErr, &lastRun)
	msg := fmt.Sprintf("%d failing (newest: %q", count, title)
	if lastErr != "" {
		msg += fmt.Sprintf(": %s", lastErr)
	}
	msg += ") — inspect, fix, or pause the routine"
	return done("error", msg)
}

// LastTurnCost reports the most recent metered turn for /status and
// one-line surfaces. Empty when nothing is metered yet.
func (d *Doctor) LastTurnCost() (line string, ok bool) {
	if d == nil || d.db == nil {
		return "", false
	}
	var model string
	var cost float64
	var unknown int64
	err := d.db.QueryRow(`SELECT COALESCE(json_extract(payload,'$.model'),''), COALESCE(CAST(json_extract(payload,'$.cost_usd') AS REAL),0), COALESCE(CAST(json_extract(payload,'$.cost_unknown') AS INTEGER),0) FROM canonical_events WHERE type='usage.recorded' ORDER BY seq DESC LIMIT 1`).Scan(&model, &cost, &unknown)
	if err != nil {
		return "", false
	}
	suffix := ""
	if model != "" {
		suffix = " (" + model + ")"
	}
	if unknown != 0 {
		return "last metered turn: cost unknown" + suffix, true
	}
	return "last metered turn: $" + trimCost(cost) + suffix, true
}

func trimCost(cost float64) string {
	s := fmt.Sprintf("%.4f", cost)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
