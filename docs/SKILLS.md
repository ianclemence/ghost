> **Status note:** the runtime no longer relies on chat for every skill. Many
> network capabilities run through **deterministic provider-backed tools**
> (weather/aqi/flight) that return validated output, and skills declare
> capabilities/risk consumed by the permission broker and readiness layer. See
> [CAPABILITY_ARCHITECTURE.md](CAPABILITY_ARCHITECTURE.md). The tier framing
> below is retained as guidance for authors.

# Ghost Skills — Product Tiers

Ghost is **“Your AI. Your Memory. Your Machine.”** — a personal assistant a
normal person asks naturally (*“weather, add milk, remind me at 9”*) without
knowing what a skill, `gcalcli`, or `SKILL.md` is. Every live skill must meet:
*triggers naturally → bounded execution → clean failure → no leaks.*

## Tier 1 — Core default (live, tested via `/v1/chat`)

Zero-setup day-to-day: `weather, aqi, currency, crypto, recipe, daily-briefing,
find-nearby, travel, calendar (enabled, needs OAuth), reminders/schedule,
shopping, journal, quick-capture, knowledge-base, scraper, summarize,
organizer, healthcheck`. All pass happy/missing/config/failure/security.

## Tier 2 — Optional packs (amber only when they need YOUR action)

Badge rule: green when ready (binaries Ghost installs by default are
present), amber only when the user must do something (connect an account,
pair a device, grant access). Command-only tools never show amber for
being developer-oriented.

- *Ready out of the box (green):* `git, tmux, network/nmap, system,
  process-manager, skill-creator, speedtest, document-convert,
  internet-reading, ascii-art` — binaries ship with setup (`setup.sh`),
  no account needed.
- *Needs your action (amber → Connect):* `calendar` (Google OAuth),
  `flight` (API key), `homeassistant` (URL + token), `mobile` (pair
  Android device), `spotify` (running app), `camera` (no camera detected),
  `hardware` (no bus hardware), `email` (IMAP credentials).
- *Packs:* Hardware (camera, hardware, mobile, network, system, tmux),
  Media (spotify, homeassistant, flight), Dev (git, skill-creator,
  process-manager). All report product messages, never `command not found`.

## Tier 3 — Dev/docs only (never live, never in prompt)

Nested containers and templates in the repo but **never loaded** (loader is
1-level only, sync only seeds dirs with top-level `SKILL.md`):
`email/himalaya, github/* (6), productivity/* (4), research/* (5),
software-development/* (7), workflows/*.md (20), ascii-art*`.

- `software-development/*` → contributor workflows (see `.opencode/skills`), not runtime.
- `workflows/*.md` → automation templates for Automations UI, not `<skills>`.
- `research/duckduckgo-search` → duplicates `web_search` tool; do not ship as skill.
- `productivity/google-workspace` → merges into Calendar Google connection when OAuth lands.
- `email/himalaya` → hidden until IMAP/OAuth setup flow exists.
- `ascii-art` → fun easter egg, optional, lowest priority.

*Why:* every live skill costs prompt tokens, latency, test surface, and leak
risk on a Pi. Slim core + opt-in packs keeps Ghost fast, honest, and testable.
