# Scheduler + Credential Closure — Phase 3.1

**Repository:** `/home/ianclemence/ghost`
**Prior phase:** `f6c6a20` (Close capability architecture seams)
**This phase:** final closure of the two remaining architectural seams
**Principle:** one authority · one scheduler · one credential boundary.

This report reflects the actual final state.

---

## Scheduler

### What migrated

| Path | Before | After |
|---|---|---|
| `/loop` | wrote to `pkg/cron` (JSON) | creates a `pkg/scheduled` item (`ScheduleEvery`, source `loop`) |
| `/remind` | wrote to `pkg/cron` (JSON) | creates a `pkg/scheduled` item (`ScheduleAt`, source `remind`) |
| model scheduling | `cron` tool → `pkg/cron` | `schedule` tool → `pkg/scheduled` (the `cron` tool was removed) |
| `/loops`, `/stoploop` | `cron` tool list/disable | `pkg/scheduled` `ListItems`/`CancelItem` |
| heartbeat (`HEARTBEAT.md`) | `pkg/cron` jobs | `pkg/scheduled` items (source `heartbeat`, idempotent IDs) |
| `/v1/cron/jobs` API | `pkg/cron` service | removed (the only consumer, `home.js`, fetched it but never used the result) |
| `ghost cron` CLI | `pkg/cron` | removed |

### `pkg/cron` retired

`pkg/cron` and `pkg/tools/cron.go` were deleted. No production code imports a
cron engine (enforced by `pkg/archtest` `TestArchitecture_NoActiveCronScheduler`).

### Legacy `cron/jobs.json` migration

`pkg/scheduled/legacy_migration.go` (wired at boot via
`migrateLegacyCron` in `cmd/ghost/main.go`):

- Reads the legacy JSON with its **own** structs (does not import `pkg/cron`).
- Maps each job to a `ScheduledItem` (`at`/`every`/`cron`; command → `ActionCommand`, message → `ActionAgentTurn`).
- **Idempotent**: item IDs are deterministic `legacy-cron-<fnv>` hashes of the
  normalized schedule + content. Re-running migrates nothing new.
- Missing/empty file is a no-op; malformed entries are surfaced per-entry while
  valid entries still migrate.
- The legacy file is left in place as a migrated artifact; it is not read by
  any runtime execution path.
- Tests: `TestMigrateLegacyCronIdempotent`, `TestMigrateLegacyCronMissingAndMalformed`.

### Scheduler authority

Scheduled execution resolves through the normal path: routine/agent turn →
capability → Permission Broker → resolver → execution → evidence. Scheduled
shell commands go through `executeScheduledCommand` → `AuthorizeScheduledCommand`
(broker) → registry default-deny exec. The scheduler never authorizes.

---

## Credentials

### Vault boundary

`pkg/credentials.Vault` is the single credential lifecycle authority
(`Store`/`Use`/`Validate`/`Disconnect`/`List`/`Configured`).

### Storage adapter moved

The low-level typed provider-key accessors (`AviationKey`, `AeroDataBoxKey`,
`OpenWeatherKey`, `HassEndpoint`, `HassConfigured`, `FlightConfigured`) moved
from `pkg/skills/integrations.go` (deleted) to
`pkg/credentials/provider_keys.go`. `pkg/skills` no longer owns credential
storage.

### No direct secret access outside the boundary

- Web Console (`cmd/ghost-web/admin.go`) writes via `Vault.Store`.
- Connections API (`cmd/ghost/connections_api.go`) uses `Vault.Store` /
  `Vault.Configured` / `Vault.Disconnect`.
- The calendar credential is sealed and removed by the Vault.
- Enforced by `pkg/archtest` `TestArchitecture_CredentialStorageOwnedByVault`
  (no `config.LoadSecrets`/`config.SaveSecrets` outside `pkg/config` and
  `pkg/credentials`).

### Package cycle

`pkg/credentials` no longer imports `pkg/skills`. The calendar integration
hooks (`CalendarConnected`, `CalendarWebDisconnectFn`, `CalendarDisconnectFn`)
are injected by `pkg/skills` at init (`pkg/skills/credentials_wire.go`),
keeping storage ownership in the credential package without a cycle.

### MCP OAuth

MCP OAuth tokens are held in memory only (`pkg/mcp`), i.e. a session cache, not
a persistent second credential store. No MCP server is enabled by default.
This is the documented remaining item (see limitations).

---

## Contract tests added

`pkg/archtest`:
- `TestArchitecture_NoActiveCronScheduler`
- `TestArchitecture_CredentialStorageOwnedByVault`
- `TestArchitecture_OnePermissionBroker`

`pkg/scheduled`:
- `TestMigrateLegacyCronIdempotent`
- `TestMigrateLegacyCronMissingAndMalformed`

---

## Verification

| Command | Result |
|---|---|
| `go test ./...` | pass |
| `go vet ./...` | clean |
| `go test ./pkg/archtest/` | 10 tests pass |
| `ghost golden` | 56/56 (deepseek-flash) |
| `ghost verify` | PASS (as root; non-root fails only the appliance `/var/lib/ghost/workspace` permission check) |
| `ghost benchmark` | 100.0 PASS |
| `ghost update` | success |

---

## Remaining limitations

- **MCP OAuth** is an in-memory session cache, not persisted through the Vault.
  No persistent MCP credential store exists; when MCP OAuth persistence is
  needed it must use `Vault.Store`/`Vault.Use`.
- **`ghost cron` CLI / `/v1/cron/jobs` API removed.** Scheduling is via
  `/remind`, `/loop`, the `schedule` tool, and the Web Console's scheduled
  items surface (`/v1/scheduled`).
- **`hass` alias** and the calendar `exec gcalcli` fallback remain as
  intentional transitional compatibility.
