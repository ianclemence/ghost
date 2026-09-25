# Tool ↔ Skill Map

Ghost's rule: **a capability has one governed tool, and the skill that covers
it drives that tool.** This is the same relationship connected apps have with
their skills — the app provides the account, the tool provides the governed
execution, and the skill teaches the model what to do with both.

## Why it matters

- **One execution path.** The tool carries evidence, approvals, redaction,
  provider fallback, and honest failures. A skill that shells out with `curl`
  bypasses all of that; its commands are only a fallback for when a tool
  reports it is unavailable.
- **Honest setup.** When a tool is unconfigured, the runtime returns the
  product message ("connect this in Ghost settings"), and the skill's
  readiness shows *needs configuration* instead of pretending it can work.
- **No drift.** `TestWorkspaceContract_SkillsDriveTheirTools` fails the build
  if a mapped skill stops naming its tool, so the two can't drift apart.

## The map

| Tool | Skill | Fallback the skill keeps |
|---|---|---|
| `weather_now` | `weather` | `curl` (wttr.in / open-meteo) |
| `aqi_now` | `aqi` | `curl` |
| `crypto_price` | `crypto` | `curl` |
| `currency_convert` | `currency` | `curl` |
| `flight_status` | `flight` | provider `curl` (AviationStack) |
| `places_nearby` | `find-nearby`, `maps` | OSM scripts (routing, distance, timezone) |
| `calendar` | `calendar` | gcalcli with the service token dir |
| `email_search`, `email_send` | `email` | himalaya CLI |
| `media_play` | `spotify` | — |
| `docs_search` | `notion` | — |
| `code_search` | `github` | `gh` CLI + workflow skills |
| `hass` / `device` | `homeassistant` | REST `curl` with `HASS_URL`/`HASS_TOKEN` |

## Capabilities with no user-facing skill (by design)

Some tools are primitives the model uses directly and no skill should wrap:
`read_file`/`write_file`/`list_dir`/`edit_file`, `exec`, `sandbox`,
`schedule`, `message`, `web_search`/`web_fetch`, `vision`, `video_frames`,
`tts`, `image_generate`, `canvas`, `todo`, `goal`, `spawn`/`subagent`,
`publish_artifact`, `memory_*`, `connections`, `session_search`,
`context_get`, `compact_context`, `clarify`, `oracle`, `switch_lane`,
`networking`, `update`, `skill_manage`, `batch_delegate`, `voice_wake`,
`doc_parser`, `browser_*`, `computer_*`.

These are always available, always governed, and never need a skill to
explain them — the model's tool descriptions are the interface.

## Adding a capability

1. Build the tool (governed: evidence, approvals, honest errors).
2. Write or update the skill and put **`> **Preferred path:**`** at the top,
   naming the tool.
3. Add the pair to the contract test's mapping.
4. If the capability needs credentials, register a connected app so the
   setup flow exists in Apps — the skill must never ask for a secret in chat.
