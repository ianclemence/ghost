# Capability Architecture Closure — Phase 3

**Repository:** `/home/ianclemence/ghost`
**Prior phase:** `2f57b16` (make capabilities the execution boundary)
**This phase:** capability architecture closure
**Principles:** *The model is allowed to be wrong; the system isn't allowed to blindly believe it.* · *Everything that safely varies is pluggable; the things that define trust are not.*

This report reflects the actual final state. Sections that remain incomplete
are marked **INCOMPLETE** and explained.

---

## Completed

### 1. Evidence validation (capability-specific, runtime-only)
`capability.ValidateEvidence(kind, evidence)` now validates structure, not
mere presence: the evidence type must match the capability's contract and the
required fields must be present. The registry refuses success when validation
fails. Kinds and required fields:

| Kind | Required |
|---|---|
| `acknowledgement` | `timestamp` + operation identity (`operation`/`message_id`/`recipient`/`provider`) |
| `state_transition` | `entity`, `requested`, `timestamp` |
| `artifact` | `artifact_id`, `timestamp` |
| `file_write` | `path`, `timestamp` |
| `action` (browser/computer) | `outcome`, `timestamp`, `op`/`operation` |

Evidence is produced by the runtime (tool execution) and cannot be asserted by
the model: the model supplies arguments, never `ToolResult.Evidence`. Browser
and computer evidence is enforced by their dedicated gates (the registry
defers to them so their product message is authoritative).

### 2. Capability-based subagent scopes (deny-by-default)
`capability.InScope(scope, tool, args)` resolves the tool's capability
(action-aware) and checks membership. `ToolLoopConfig.AllowedCapabilities`
enforces it before any execution path is reached; `SubagentManager` exposes
`SetAllowedCapabilities`. The agent loop delegates a conservative default
scope (read/write files, web, memory, artifacts, read-only information
capabilities, browser) and explicitly excludes exec, device/computer control,
messaging, calendar mutation, skill management, and MCP. The actuator
blocklist in `filteredTools` remains as defense-in-depth.

### 3. MCP semantic mapping + scoped high-impact fallback
`mcpCapabilityFor` maps only well-known, read-shaped tools to a semantic
capability (`mcp_github_search` → `repository.search`, `mcp_weather_get` →
`weather.get`, `mcp_web_search` → `web.search`). Any mutating verb
(delete/remove/create/update/write/push/merge/close/set_/…) or unknown shape
stays `mcp.execute` with `high_impact` risk. The authorization action identity
is the specific tool name, so a grant for one MCP tool does not authorize
another.

### 4. Implicit skill schedules removed
`schedule:` frontmatter is now **declarative metadata only**. Installing or
reading a skill can no longer create a cron job. The startup auto-discovery
that created jobs from skill schedules is gone; the schedule is logged as a
suggestion that requires an explicit routine.

### 5. Capability-based skill enforcement
Skill commitment is **turn-scoped** (the fixed 6-message window is gone and is
not replaced by another conversational-distance heuristic). A SKILL.md read
in the current turn stays committed for the whole turn and does not leak into
the next. `AllowedTools` remains a surface-narrowing mechanism only; the
Permission Broker remains the authority. `skills.HasCapability` now recognizes
canonical capability IDs.

### 6. One credential authority (partial)
The web console (`cmd/ghost-web/admin.go`) now writes credentials through
`credentials.Vault` instead of `.secrets.json` directly; `connections_api.go`
already routed through the Vault. Calendar credentials are sealed. Dead
`pkg/credential` was removed in the prior phase.

**INCOMPLETE:** `pkg/skills/integrations.go` remains the low-level typed
provider-key accessor that the Vault itself calls (`Vault.secretValue` →
`skills.*Key` → `config.LoadSecrets`). This is the storage-access layer used
by the Vault, not an independent lifecycle authority, but it is still a direct
`config.LoadSecrets` reader. Fully inverting it (move typed accessors into the
Vault) is the remaining work.

### 7. Capability Contract Test Suite
`pkg/archtest/contracts_test.go` (`TestCapabilityArchitectureContracts_*`) is a
fast, deterministic suite covering: resolution (available/unavailable,
provider replacement, determinism), authority (broker is the only authority;
connected app and resolver do not grant), evidence (missing/wrong-type/malformed/
valid), delegation scope, MCP mapping (semantic vs high-impact), broker
isolation, and end-to-end registry evidence enforcement.

### 8. Dead legacy paths removed
- Skill auto-cron removed.
- Fixed 6-message skill window removed.
- Phantom `message_write` removed (prior phase).
- 13 dead packages / 8 dead tools removed (prior phase).

---

## Verification

| Command | Result |
|---|---|
| `go test ./...` | pass |
| `go vet ./...` | clean |
| `pkg/archtest` contract suite | pass |
| `ghost golden` | see final response |
| `ghost verify` | see final response |
| `ghost benchmark` | see final response |
| `ghost update` | see final response |

---

## Remaining limitations

### Code limitations

1. **Two schedulers remain (INCOMPLETE).** `pkg/scheduled` (SQLite) is the
   intended authoritative scheduler, but `pkg/cron` (JSON) is still an active
   engine used by (a) the `cron` tool — reached by `/loop`, `/remind`, and the
   model — and (b) the heartbeat service's `ParseAndSchedule` (HEARTBEAT.md).
   This is a genuine dual path. The migration plan:
   1. Make `/loop` and `/remind` create items via the `schedule` tool
      (`scheduled.ParseNaturalLanguage` already handles "every N minutes" and
      "in N minutes").
   2. Make the heartbeat service create scheduled items instead of cron jobs.
   3. Add an idempotent first-boot migration from `cron/jobs.json` into
      `scheduled_items` (dedupe by normalized content + schedule).
   4. Remove `pkg/cron` once no callers remain.

   This was not attempted in this pass because it touches two core features
   (user scheduling and heartbeats) and a rushed migration risks data loss;
   the responsible move is a dedicated, tested migration rather than a partial
   swap.

2. **Credential accessor inversion (see §6).** `pkg/skills/integrations.go`
   still reads `config.LoadSecrets` directly as the Vault's storage layer.

3. **Model surface still uses semantic tool names** (`calendar`, `device`,
   `message`, …). Capability identity governs authorization, evidence, and
   events underneath. This is intentional per the Phase 3 directive ("do not
   mass-rename semantic model tools"); the tool name is not the permission
   identity.

4. **MCP OAuth tokens remain in-memory** (`pkg/mcp`), not persisted through the
   Vault. No MCP server is enabled by default.

### Environmental limitations

- `ghost verify` run as a non-root user fails the appliance "state writable"
  check (`/var/lib/ghost/workspace` is root-only, mode 2700). Run as root it
  passes. This is an environment condition, not a code regression.

### Intentional transitional compatibility

- The `cron` tool remains registered and functional while the scheduler merge
  is pending.
- The `hass` tool remains a governed alias of the semantic `device` surface.
- The calendar SKILL.md `exec gcalcli` fallback remains for pre-existing
  gcalcli setups alongside the semantic `calendar` tool.
