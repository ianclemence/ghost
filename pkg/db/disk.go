package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Disk-full handling. SQLite reports a full filesystem as SQLITE_FULL
// ("database or disk is full") or a disk I/O error mid-commit; both
// arrive as opaque driver errors. Translate them once, at the boundary,
// into a typed error carrying the remedy — raw SQLite text must never
// reach an owner wondering why Ghost stopped.

// ErrDiskFull signals a full filesystem. Compare with errors.Is.
var ErrDiskFull = errors.New("disk full")

// IsDiskFullError reports whether err is a full-filesystem failure.
func IsDiskFullError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDiskFull) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database or disk is full",
		"disk i/o error",
		"sqlite_full",
		"no space left on device",
		"enospc",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// DiskFullError wraps a write failure with the recovery path: prune old
// snapshots first, then retry. Pruning is the cheapest reclaim on an
// appliance (archives are large and retention-bounded).
func DiskFullError(op string, err error) error {
	return fmt.Errorf("%w during %s: free space now or run `ghost state prune` to drop old snapshots (original error: %v)", ErrDiskFull, op, err)
}

// TranslateError maps full-filesystem failures to ErrDiskFull,
// preserving the remedy. Call it on write-path errors where a raw
// driver error would otherwise surface.
func TranslateError(op string, err error) error {
	if IsDiskFullError(err) {
		return DiskFullError(op, err)
	}
	return err
}

// CheckIntegrity verifies the database opens clean: PRAGMA
// integrity_check must return exactly one row reading "ok". Anything
// else means corruption the daemon must not serve. Call after
// migration at gateway startup; failure refuses boot with the restore
// runbook (matching the migration-failure message pattern).
func CheckIntegrity(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("integrity check: no database handle")
	}
	rows, err := db.Query(`PRAGMA integrity_check`)
	if err != nil {
		if IsDiskFullError(err) {
			return DiskFullError("integrity check", err)
		}
		return fmt.Errorf("integrity check failed to run (%v): restore from a backup with `ghost state import` and try again", err)
	}
	defer rows.Close()
	var first string
	n := 0
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			return fmt.Errorf("integrity check unreadable: %w", err)
		}
		if n == 0 {
			first = msg
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return TranslateError("integrity check", err)
	}
	if n != 1 || first != "ok" {
		return fmt.Errorf("database integrity check failed (%d problems, first: %q): restore from a backup with `ghost state import` and try again", n, first)
	}
	return nil
}
