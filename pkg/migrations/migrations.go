// Package migrations is Ghost's tiny deterministic schema-evolution
// engine. SQLite DDL is transactional, so each migration runs inside its
// own transaction and is recorded only after it commits: a crash or error
// mid-migration leaves the version untouched and the next startup retries
// it. There is deliberately no down-migration: forward-only, with restore
// from a pre-update backup as the rollback story (see the updater).
package migrations

import (
	"database/sql"
	"fmt"
	"sort"
)

// Migration is one numbered schema step. Exactly one of Up / UpDB must be
// set. Up runs inside a transaction. UpDB runs without one and exists only
// for steps that must reuse existing *sql.DB-based initializers (the v1
// baseline); such steps must be idempotent so a retry after a crash is
// safe, and this is enforced by review, not by machinery — keep UpDB steps
// to CREATE-IF-NOT-EXISTS-style DDL reuse.
type Migration struct {
	Version     int
	Description string
	Up          func(tx *sql.Tx) error
	UpDB        func(db *sql.DB) error
}

// validate checks ordering, numbering, and shape before anything runs.
// Registries must be strictly increasing with no duplicates; gaps are
// allowed (a removed-then-superseded version must never be reused).
func validate(ms []Migration) error {
	seen := map[int]bool{}
	prev := 0
	for _, m := range ms {
		if m.Version <= 0 {
			return fmt.Errorf("migration has non-positive version %d", m.Version)
		}
		if m.Version <= prev {
			return fmt.Errorf("migrations out of order: version %d after %d", m.Version, prev)
		}
		prev = m.Version
		if seen[m.Version] {
			return fmt.Errorf("duplicate migration version %d", m.Version)
		}
		seen[m.Version] = true
		if m.Description == "" {
			return fmt.Errorf("migration %d has no description", m.Version)
		}
		if (m.Up == nil) == (m.UpDB == nil) {
			return fmt.Errorf("migration %d must set exactly one of Up / UpDB", m.Version)
		}
	}
	return nil
}

func ensureVersionTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL DEFAULT '',
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`)
	return err
}

// appliedVersions returns the set of recorded migration versions.
func appliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CurrentVersion returns the highest recorded migration version (0 when
// nothing has run yet). Cheap enough for startup sanity checks.
func CurrentVersion(db *sql.DB) (int, error) {
	if err := ensureVersionTable(db); err != nil {
		return 0, err
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return 0, err
	}
	max := 0
	for v := range applied {
		if v > max {
			max = v
		}
	}
	return max, nil
}

// setUserVersion mirrors the recorded max into PRAGMA user_version so
// external tools can read schema currency without parsing the table.
func setUserVersion(db *sql.DB, v int) error {
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v))
	return err
}

// Migrate brings db to the head of ms, running each pending migration in
// registry order and recording it only after success. On the first error
// it stops immediately and reports the exact migration; nothing is marked
// applied for the failed step, so the next startup retries it. Returns the
// resulting schema version.
func Migrate(db *sql.DB, ms []Migration) (int, error) {
	ordered := append([]Migration(nil), ms...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })
	if err := validate(ordered); err != nil {
		return 0, err
	}
	if err := ensureVersionTable(db); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return 0, err
	}
	current := 0
	for v := range applied {
		if v > current {
			current = v
		}
	}
	for _, m := range ordered {
		if applied[m.Version] {
			continue
		}
		if m.UpDB != nil {
			if err := m.UpDB(db); err != nil {
				return current, fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Description, err)
			}
		} else {
			tx, err := db.Begin()
			if err != nil {
				return current, fmt.Errorf("migration %d (%s): begin: %w", m.Version, m.Description, err)
			}
			if err := m.Up(tx); err != nil {
				tx.Rollback()
				return current, fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Description, err)
			}
			if err := tx.Commit(); err != nil {
				return current, fmt.Errorf("migration %d (%s): commit: %w", m.Version, m.Description, err)
			}
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version, description) VALUES (?, ?)`, m.Version, m.Description); err != nil {
			return current, fmt.Errorf("migration %d (%s): record: %w", m.Version, m.Description, err)
		}
		applied[m.Version] = true
		current = m.Version
	}
	head := 0
	for _, m := range ordered {
		if m.Version > head {
			head = m.Version
		}
	}
	if err := setUserVersion(db, head); err != nil {
		return head, fmt.Errorf("set user_version: %w", err)
	}
	return head, nil
}
