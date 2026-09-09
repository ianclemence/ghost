# Database Migrations

Ghost's database schema is versioned explicitly. There is no silent
schema drift: every change is a numbered migration, applied in order,
recorded durably, and fatal on failure.

## How it works

- `pkg/migrations`: tiny runner, no dependencies. Each migration runs in
  its own transaction and is recorded in `schema_migrations` only after
  it commits. A crash or error mid-migration leaves the version
  untouched, so the next startup retries it. `PRAGMA user_version`
  mirrors the head for cheap external checks.
- `pkg/schema`: the ordered registry. v1 ("baseline") builds the full
  schema by calling each subsystem's own idempotent initializer — no
  copied DDL, so the baseline and the live code can never disagree.
- Registries are append-only: never reorder, never reuse a version.
  Validation rejects duplicates, gaps in application, and missing
  upgrade functions before anything runs.

## Startup contract

The gateway runs `MigrateToCurrent` before any subsystem touches the
database. A migration failure stops startup with the exact version
named; Ghost never runs services against a partially migrated schema.
Import paths use the same canonical migration call.

The old pattern (bare `CREATE TABLE IF NOT EXISTS` at open time) remains
as a harmless idempotent safety net, but version tracking and ordering
live in the framework — that is the canonical path.

## OTA relationship

Migrations are forward-only; there is no automatic downgrade. Rollback
is restore-from-backup (see [BACKUP.md](BACKUP.md)): the updater keeps
the previous binary, and a Ghost State archive restores user state.
An update must never ship a migration that cannot be preceded by a
working backup, and archives record the schema version they were taken
at — imports refuse mismatched versions loudly instead of guessing.

## Adding a migration

1. Append the next version to `pkg/schema`'s registry with a
   description, using `Up` (transactional SQL) wherever possible.
2. Cover it with tests: fresh DB reaches head, old DB migrates with
   data preserved, failure stops and retries cleanly.
3. If it changes any table covered by backup snapshots, update the
   snapshot column lists in `pkg/ghoststate` in the same commit —
   otherwise exports will (correctly) refuse the unknown shape.
