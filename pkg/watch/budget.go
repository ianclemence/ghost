package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Budget is the daily probe ceiling — max_watch_checks_per_day. It is the
// outer bound on watch work regardless of how many watches exist or how
// often the cadence would allow a check: when the budget is spent, polls
// defer to tomorrow rather than running unbounded background traffic.
//
// The count is persisted (watches/budget.json) so a restart cannot reset
// the ceiling — otherwise crash-looping would become a way around it.
type Budget struct {
	path string
	mu   sync.Mutex
	day  string
	used int
}

// BudgetFile is the workspace-relative counter file.
const BudgetFile = "budget.json"

// OpenBudget loads (creating if needed) the daily budget for a workspace.
func OpenBudget(workspace string) (*Budget, error) {
	if workspace == "" {
		return nil, os.ErrInvalid
	}
	dir := filepath.Join(workspace, Dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	b := &Budget{path: filepath.Join(dir, BudgetFile)}
	if err := b.load(); err != nil {
		return nil, err
	}
	return b, nil
}

func dayKey(now time.Time) string { return now.UTC().Format("2006-01-02") }

func (b *Budget) load() error {
	raw, err := os.ReadFile(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var rec struct {
		Day  string `json:"day"`
		Used int    `json:"used"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		// A corrupt counter must not break polling: it resets the count,
		// which fails OPEN (allows checks), never closed.
		return nil
	}
	b.day, b.used = rec.Day, rec.Used
	return nil
}

func (b *Budget) saveLocked() error {
	raw, err := json.MarshalIndent(map[string]interface{}{"day": b.day, "used": b.used}, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

// rollLocked moves to a new day when the stored day has passed.
func (b *Budget) rollLocked(now time.Time) {
	if k := dayKey(now); k != b.day {
		b.day, b.used = k, 0
	}
}

// Allow reserves one probe against today's budget. It returns false when
// the ceiling is already spent — the caller defers, honestly, and the
// counters record the deferral.
func (b *Budget) Allow(now time.Time, max int) bool {
	if b == nil {
		return true
	}
	if max <= 0 {
		max = DefaultBudgetPerDay
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked(now)
	if b.used >= max {
		return false
	}
	b.used++
	_ = b.saveLocked()
	return true
}

// Used reports today's probe count (for status surfaces).
func (b *Budget) Used(now time.Time) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked(now)
	return b.used
}
