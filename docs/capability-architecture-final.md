# Capability Architecture — Final State

**Repository:** `/home/ianclemence/ghost`
**Previous migration:** `5c846ec` (consolidate ghost capability architecture)
**This migration:** capability resolution + model-surface migration
**Governing principle:** *The model is allowed to be wrong; the system is not allowed to blindly believe it.*
**Architectural principle:** *Everything that varies is replaceable. Everything that defines trust is owned by Ghost.*

This report is honest about what is complete and what remains a documented
dual path. Sections that are not complete are marked **INCOMPLETE**.

---

## 1. What changed from the previous migration

The previous migration made the capability registry *metadata* alongside the
tool system. This migration makes capability identity, resolution, and
connected-app state part of the real execution path:

- **Capability resolver** (`pkg/capability/resolve.go`): deterministic
  `capability → implementation` selection with credential/connection-aware
  availability and provider replacement. Wired into governance so every
  consequential execution event records the runtime-selected capability and
  provider.
- **One Permission Broker**: the runtime now opens a single authoritative
  broker (`InitAuthoritativeBroker`) and passes it to the scheduler, HTTP API,
  and agent governance. The scheduler no longer opens its own.
- **Model surface**: Home Assistant's provider-named `hass` tool is now the
  semantic `device` surface (`hass` remains a governed alias). A new semantic
  `calendar` tool replaces the model's need to run `gcalcli` via `exec`, with
  action-dependent capability identity (`calendar.read` vs `calendar.modify`)
  and evidence on modify.
- **Skill authority**: the 6-message commitment window was replaced by
  **turn-scoped** commitment, closing the mid-turn expiry hole.
- **MCP mapping**: well-known read-oriented MCP tools resolve to a semantic
  capability; everything unknown stays `mcp.execute` (high impact).
- **Connected App foundation** (`pkg/connectedapp`): a runtime entity with
  identity, provider, credential reference, scopes, capabilities, status,
  health, and revocation — consulted live by the resolver.
- **Credential boundary**: `connections_api.go` now writes through the single
  `credentials.Vault`; nothing writes the secrets file directly.

## 2. How capability resolution works

`Resolver.Resolve(capability, preferLocal)`:

1. Lists implementations registered for the capability.
2. Filters to those whose availability predicate returns true (credential
   present / connected app usable).
3. Prefers a local implementation when `preferLocal` is set.
4. Otherwise picks the highest `Priority`; ties break deterministically on
   provider name.
5. Returns `(Implementation, ok)`. `ok=false` means **no implementation is
   available** — the caller reports a capability-specific outcome, never a
   provider error.

Adding a provider is a registration change in `RegisterDefaults`, never a
model-facing change. Tests prove that changing provider (Open-Meteo →
OpenWeather by credential) does not change capability identity or the tool.

## 3. How the model requests capabilities

The model still invokes a **bounded semantic tool surface** (the spec forbids
one generic `execute(capability, args)` escape hatch). The semantic surfaces
are: `calendar`, `device`, `message`, `artifact`, `weather_now`, `web_search`,
`browser_*`, `computer_*`, memory tools, and so on. Provider/integration
names no longer appear where they would leak infrastructure (`hass` →
`device`; calendar no longer requires `exec gcalcli`).

**INCOMPLETE:** tool *names* are still the model-facing vocabulary for most
capabilities; the capability registry governs identity underneath. Fully
renaming the surface to capability IDs is the next phase.

## 4. How capabilities resolve to implementations

```
capability (calendar.modify)
   ↓  Resolver (runtime-owned)
implementation (provider=google-calendar, tool=calendar)
   ↓  tool executes
integration (Google Calendar via gcalcli)
```

The model never selects Open-Meteo/OpenWeather/Google/Home Assistant/MCP.
Selection is deterministic and runtime-owned.

## 5. How permissions map to capabilities

The broker authorizes **capability identity**, not tool names:
`message.send`, `device.control`, `calendar.modify`, `mcp.execute`,
`exec.shell`, etc. Scope/resource context further constrains the decision.

Action-dependent capabilities are handled explicitly: the `calendar` tool
resolves to `calendar.read` (read-only, no approval) for `list` and
`calendar.modify` (consequential, evidence required) for `create`/`delete`.
`governance.authorize` and the browser/computer gates all key actions as
`tool:action`, so grants are consistent across paths.

## 6. How credentials are accessed

`pkg/credentials.Vault` is the single boundary (`Store`/`Use`/`Validate`/
`Disconnect`/`List`). `connections_api.go` routes through it. Calendar tokens
are sealed with the config vault. Secrets are never returned to the model,
events, memory, artifacts, UI, or backup.

**INCOMPLETE:** MCP OAuth tokens remain in-memory only (not yet persisted
through the vault); the vault is not yet the *only* writer for every secret
(e.g. provider keys saved via `.secrets.json` by the web console). The dead
duplicate `pkg/credential` was removed in the prior migration.

## 7. How evidence is validated

The capability contract declares an evidence kind
(`acknowledgement`/`state_transition`/`artifact`/`file_write`). The registry
refuses to report success for a capability that requires evidence when the
result carries none. Migrated consequential capabilities emit evidence:
`message` (channel/recipient/hash), file writes (path/size/hash), artifacts
(id/kind/title), `device` (entity/requested/observed), `calendar` modify
(operation/summary).

**INCOMPLETE:** evidence is not yet attached with `TrajectoryID` in every
path; browser/computer evidence already carries correlation, and the
capability/provider annotation is added to the canonical `ToolCompleted`
payload.

## 8. How connected apps fit underneath capabilities

`pkg/connectedapp.Registry` holds `App{ID, Provider, CredentialID, Scopes,
Capabilities, Status, LastValidated, Revoked}`. An app is **not** a
capability: it is the authenticated identity through which capabilities are
fulfilled. `Registry.ForCapability` maps capability → usable apps; the
resolver's availability predicates consult it live. Revocation is real
(`Revoke` → not usable → fulfils nothing).

## 9. How skills relate to capabilities

A skill declares capability requirements (`requires: [calendar.read,
web.search]`) — a **request**, never a grant. Ghost's normal capability/
permission system still authorizes each. Skill commitment is now turn-scoped:
a SKILL.md read in the current turn remains committed for the whole turn and
does not leak into the next.

**INCOMPLETE:** `AllowedTools` still narrows the tool surface (defense in
depth) and the broker is still the authority; converting `AllowedTools`
entirely into declared capability requirements is the next phase.

## 10. How MCP relates to capabilities

MCP is an integration/protocol mechanism. Known read-oriented shapes map to a
semantic capability (`mcp_github_search` → `repository.search`); everything
else is governed as `mcp.execute` with `high_impact` risk. Unknown third-party
tools can never inherit a low-risk classification. MCP cannot mint authority:
it resolves through the broker like any other consequential tool.

## 11. How routines/scheduler relate to capabilities

Routines are the product primitive; the scheduler is the execution mechanism.
Scheduled shell commands resolve through the broker-backed authorizer
(prior migration); the scheduler never mints authority.

**INCOMPLETE:** two scheduler engines still coexist (`pkg/cron` JSON and
`pkg/scheduled` SQLite). See §15.

## 12. How subagent authority is constrained

Subagents cannot inherit actuator primitives (`hass`/`device`, `i2c`, `spi`,
`update`, `networking`, `computer_*`, `mcp_*`) merely because a generic
surface exists. **INCOMPLETE:** delegation is not yet expressed as explicit
capability scopes; the block-list is the current mechanism.

## 13. Legacy tool paths removed

- `hass` is no longer the primary model surface (semantic `device`).
- Calendar no longer requires the model to run `exec gcalcli`.
- MCP no longer bypasses the broker.
- The fixed 6-message skill window is gone.
- 13 dead packages and 8 dead tools were removed in the prior migration.

## 14. Compatibility paths that remain

- `hass` remains a registered governed alias for `device`.
- The calendar SKILL.md `exec gcalcli` fallback remains for users whose
  gcalcli setup predates the semantic tool.
- `AllowedTools` remains as a narrowing mechanism (not an authority).
- Two schedulers remain (documented, §15).

## 15. Final architecture diagram

```
                         PERSON
                           │
                           ▼
                         GHOST
                           │
                    intent / reasoning
                           │
                           ▼
                      CAPABILITY  (pkg/capability)
                           │
                           ▼
                  PERMISSION BROKER  (one, runtime-owned)
                           │
                           ▼
               CAPABILITY RESOLVER  (capability → implementation)
                           │
                 ┌─────────┴─────────┐
                 ▼                   ▼
               LOCAL            INTEGRATION
             (tool)            provider / connected app
                                     │
                                     ▼
                                  EXECUTE
                                     │
                                     ▼
                                  EVIDENCE
                                     │
                                     ▼
                              CANONICAL EVENT
                                     │
                      ┌──────────────┼─────────────┐
                      ▼              ▼             ▼
                   MEMORY         ACTIVITY      ROUTINES

SKILL → knowledge/procedure → capability requirements → Ghost capability system
CONNECTED APP → authenticated system → integration → capability implementation
TOOL → internal invocation mechanism
```

## 16. Known limitations

1. **Two schedulers** (`pkg/cron`, `pkg/scheduled`). Documented dual path with
   a deterministic migration plan: move `/loop` and `/remind` to the SQLite
   scheduler, migrate `cron/jobs.json` on first boot, retire `pkg/cron`.
2. **Model surface still uses tool names** for most capabilities. The registry
   governs identity; the next phase renames the surface.
3. **`AllowedTools` still narrows**; not yet fully translated to declared
   capability requirements.
4. **MCP OAuth tokens are in-memory only**; not yet persisted via the vault.
5. **The resolver's calendar availability** depends on gcalcli token state;
   the first-party Google API client remains a future option.
6. **Subagent delegation** is a block-list, not explicit capability scopes.
7. **Two credential write paths** remain (vault for connections; direct
   `.secrets.json` for some web-console flows).

---

## Verification

- `go test ./...` — pass
- `go vet ./...` — clean
- `ghost golden` — see final response
- `ghost verify` — see final response
- `ghost benchmark` — see final response
