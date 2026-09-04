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

## Tier 2 — Optional packs (installed, `needs_setup`, opt-in via Skills settings)

Need a binary, hardware, or service: `camera, hardware, mobile/adb,
network/nmap, system, tmux, git, homeassistant, flight, spotify, speedtest,
document-convert, internet-reading, skill-creator, process-manager`. They
report product messages (never `command not found`) and stay out of
diagnostics warnings on clean installs (`IsOptionalSkill`).

- *Hardware pack:* camera, hardware, mobile, network, system, tmux
- *Media pack:* spotify, homeassistant, flight
- *Dev pack:* git, skill-creator, process-manager

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
