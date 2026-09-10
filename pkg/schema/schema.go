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
const CurrentVersion = 3

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
	}
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
