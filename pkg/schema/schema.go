// Package schema owns Ghost's database versioning: the ordered migration
// registry plus the current schema version. The gateway runs
// MigrateToCurrent at startup before any subsystem touches the database;
// import paths use it too. Subsystem DDL stays owned by each subsystem
// (db, scheduled, permissions, routines, cevents, tools curator) — the
// baseline reuses their idempotent initializers instead of copying DDL,
// so the two can never drift apart.
//
// Migrations are forward-only. Rollback is restore-from-backup (see the
// updater); the framework never partially applies a version.
package schema

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/computer"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/migrations"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/tasks"
	"github.com/ianclemence/ghost/pkg/tools"
)

// CurrentVersion is the schema head this build understands.
const CurrentVersion = 8

// baseline builds the full v1 schema through the same initializers
// production startup has always used. Every step is CREATE-IF-NOT-EXISTS
// style, so running it over any existing database is a safe no-op for
// objects that already exist, and a retry after a crash simply completes.
func baseline(raw *sql.DB) error {
	if err := db.EnsureBaseSchema(raw); err != nil {
		return fmt.Errorf("base schema: %w", err)
	}
	store := scheduled.NewStore(raw)
	if err := store.InitSchema(); err != nil {
		return fmt.Errorf("scheduled schema: %w", err)
	}
	if _, err := permissions.Open(raw, permissions.ModeAsk, 0); err != nil {
		return fmt.Errorf("permissions schema: %w", err)
	}
	if _, err := routines.New(raw, store); err != nil {
		return fmt.Errorf("routines schema: %w", err)
	}
	logDir, err := os.MkdirTemp("", "ghost-schema-cevents-*")
	if err != nil {
		return fmt.Errorf("cevents staging dir: %w", err)
	}
	defer os.RemoveAll(logDir)
	if _, err := cevents.Open(raw, logDir); err != nil {
		return fmt.Errorf("events schema: %w", err)
	}
	if err := tools.NewCurator(raw, tools.CuratorConfig{}).EnsureSchema(); err != nil {
		return fmt.Errorf("tool usage schema: %w", err)
	}
	return nil
}

// registry is the ordered migration history. Append-only: never reorder,
// never reuse a version number.
func registry() []migrations.Migration {
	return []migrations.Migration{
		{
			Version:     1,
			Description: "baseline: full v1 schema via subsystem initializers",
			UpDB:        baseline,
		},
		{
			Version:     2,
			Description: "durable work v2: job scope/generation columns, computer leases, browser sessions",
			UpDB:        durableWorkV2,
		},
		{
			Version:     3,
			Description: "event consumers: durable checkpoints and exactly-once claims",
			UpDB:        eventConsumersV3,
		},
		{
			Version:     4,
			Description: "trajectory identity: trajectory_id column on canonical_events",
			UpDB:        trajectoryV4,
		},
		{
			Version:     5,
			Description: "job trajectory: trajectory_id column on jobs",
			UpDB:        jobTrajectoryV5,
		},
		{
			Version:     6,
			Description: "one conversation: fold mobile:default and cli:default into main",
			UpDB:        mainSessionV6,
		},
		{
			Version:     7,
			Description: "compacted messages stay visible to the owner (compacted flag)",
			UpDB:        compactedVisibleV7,
		},
		{
			Version:     8,
			Description: "restore conversations hidden by the old compaction behaviour",
			UpDB:        restoreCompactedV8,
		},
	}
}

// restoreCompactedV8 un-hides conversations the old compaction behaviour
// archived. That behaviour archived everything but the last few rows, which
// leaves a session with BOTH archived and unarchived rows — unlike a
// deliberate "clear session" (every row archived, no survivors) or a single
// message delete (one archived row). Only the compaction signature is
// restored; explicitly cleared sessions are left cleared. Idempotent.
func restoreCompactedV8(raw *sql.DB) error {
	// Very old databases may lack the archived/compacted columns; nothing
	// to restore then.
	cols := map[string]bool{}
	if rows, err := raw.Query(`SELECT name FROM pragma_table_info('messages')`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil {
				cols[name] = true
			}
		}
	}
	if !cols["archived"] || !cols["compacted"] {
		return nil
	}
	if _, err := raw.Exec(`
		UPDATE messages SET compacted = 1, archived = 0
		WHERE archived = 1
		  AND session_id IN (
		    SELECT session_id FROM messages
		    WHERE archived IS NULL OR archived = 0
		  )
	`); err != nil {
		return fmt.Errorf("restore compacted rows: %w", err)
	}
	return nil
}

// compactedVisibleV7 adds a `compacted` flag to messages. Context compaction
// marks old rows compacted so they drop out of the model's context, but they
// remain in the owner's transcript: archiving is for deletion, compacting is
// for context-window management. Before this, compaction archived rows and
// the owner's history silently shrank to the last few turns. Idempotent.
func compactedVisibleV7(raw *sql.DB) error {
	has := false
	if rows, err := raw.Query(`SELECT name FROM pragma_table_info('messages')`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil && name == "compacted" {
				has = true
			}
		}
	}
	if has {
		return nil
	}
	if _, err := raw.Exec(`ALTER TABLE messages ADD COLUMN compacted BOOLEAN DEFAULT FALSE`); err != nil {
		return fmt.Errorf("messages.compacted column: %w", err)
	}
	return nil
}

// mainSessionV6 unifies the pre-unification home-conversation names
// (mobile:default from the app era, cli:default from the terminal era)
// into the surface-neutral main session. Rows interleave chronologically
// by created_at, which is exactly the product promise: one conversation.
// Idempotent: re-running finds no legacy rows and changes nothing.
// Defensive like the other migrations: very old databases may lack the
// sessions title/timestamp columns, in which case only the message fold
// runs (titles fall back to first-user-message downstream anyway).
func mainSessionV6(raw *sql.DB) error {
	cols := map[string]bool{}
	if rows, err := raw.Query(`SELECT name FROM pragma_table_info('sessions')`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil {
				cols[name] = true
			}
		}
	}
	if cols["title"] {
		titleCols, titleVals := "id, title", "'main', s.title"
		if cols["summary"] {
			titleCols += ", summary"
			titleVals += ", s.summary"
		}
		// Freshest title wins; rowid order proxies recency when the
		// updated_at column predates this database.
		tsOrder := "s.rowid"
		if cols["created_at"] {
			titleCols += ", created_at"
			titleVals += ", s.created_at"
		}
		if cols["updated_at"] {
			titleCols += ", updated_at"
			titleVals += ", s.updated_at"
			tsOrder = "s.updated_at"
		}
		q := fmt.Sprintf(`
			INSERT INTO sessions (%s)
			SELECT %s FROM sessions s
			WHERE s.id IN ('mobile:default', 'cli:default')
			  AND s.title IS NOT NULL AND TRIM(s.title) != ''
			ORDER BY %s DESC LIMIT 1
			ON CONFLICT(id) DO NOTHING`, titleCols, titleVals, tsOrder)
		if _, err := raw.Exec(q); err != nil {
			return fmt.Errorf("main session title carry-over: %w", err)
		}
	}
	if _, err := raw.Exec(`UPDATE messages SET session_id = 'main' WHERE session_id IN ('mobile:default', 'cli:default')`); err != nil {
		return fmt.Errorf("main session message fold: %w", err)
	}
	if _, err := raw.Exec(`DELETE FROM sessions WHERE id IN ('mobile:default', 'cli:default')`); err != nil {
		return fmt.Errorf("main session ledger cleanup: %w", err)
	}
	return nil
}

// jobTrajectoryV5 adds trajectory_id to jobs on databases that predate it.
// The PRAGMA-guarded ALTER makes it idempotent; fresh installs carry it from
// the v1 baseline DDL.
func jobTrajectoryV5(raw *sql.DB) error {
	if err := tasks.EnsureTrajectoryColumn(raw); err != nil {
		return fmt.Errorf("jobs trajectory column: %w", err)
	}
	return nil
}

// trajectoryV4 converges existing databases to the trajectory shape. The
// column check inside cevents keeps this idempotent; fresh installs already
// carry the column from the v1 baseline (cevents.Open).
func trajectoryV4(raw *sql.DB) error {
	if err := cevents.EnsureTrajectoryColumn(raw); err != nil {
		return fmt.Errorf("trajectory column: %w", err)
	}
	return nil
}

// eventConsumersV3 adds the checkpoint/claim tables to databases that
// migrated before they existed. Fresh installs already get them from the
// v1 baseline (cevents.Open); the statements are idempotent, so this is a
// no-op there and a completion on older devices.
func eventConsumersV3(raw *sql.DB) error {
	if err := cevents.EnsureConsumerSchema(raw); err != nil {
		return fmt.Errorf("event consumers: %w", err)
	}
	return nil
}

// durableWorkV2 converges existing databases to the durable-work shape:
// owner/context/generation/evidence/resume columns on jobs (added only
// where missing, checked explicitly — never swallowed), plus the lease
// and browser-session ledgers. Every step is idempotent, so a retry after
// a crash simply completes instead of failing on half-applied state.
func durableWorkV2(raw *sql.DB) error {
	if err := tasks.EnsureV2Columns(raw); err != nil {
		return fmt.Errorf("jobs v2 columns: %w", err)
	}
	if err := computer.EnsureSchema(raw); err != nil {
		return fmt.Errorf("computer leases: %w", err)
	}
	if err := browser.EnsureSchema(raw); err != nil {
		return fmt.Errorf("browser sessions: %w", err)
	}
	if err := artifacts.EnsureSchema(raw); err != nil {
		return fmt.Errorf("artifacts: %w", err)
	}
	return nil
}

// MigrateToCurrent brings an open database to the schema head, running
// each pending migration in order. On failure it returns the exact
// migration and the version actually reached; the caller must not start
// services against a partially migrated database.
func MigrateToCurrent(raw *sql.DB) (int, error) {
	return migrations.Migrate(raw, registry())
}

// CheckCurrent reports whether db is already at the schema head without
// changing anything.
func CheckCurrent(raw *sql.DB) (bool, int, error) {
	v, err := migrations.CurrentVersion(raw)
	if err != nil {
		return false, 0, err
	}
	return v == CurrentVersion, v, nil
}
