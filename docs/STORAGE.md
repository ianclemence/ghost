# Storage & SD Durability

Measured on the reference appliance (Pi 5, 8 GB, 32 GB microSD,
Debian 13). Numbers are observed, not modeled.

## Measured write profile

- **Idle (45 min window, services running, no interaction):** database
  byte-identical, ~157 bytes appended to heartbeat.log (one tick line).
  Effective idle write rate: ~0 B/s from Ghost itself.
- **Per conversation message:** ~7 KB all-in (row + FTS index + WAL
  overhead), measured as 3.4 MB for ~470 messages plus all other state.
- **Normal use (~50 msgs/day):** ~350 KB/day → ~10 MB/month.
- **Heavy use (~500 msgs/day + routines):** ~3.5 MB/day → ~100 MB/month.
- **Heartbeat log:** ~10 KB/day, hard-capped at 2000 lines / 1 MB.

Even aggressive use is three orders of magnitude below what would
threaten a 32 GB card (typical endurance is tens of TB written).
Ghost's own storage profile is a non-issue; no tmpfs moves were made,
because every durable path is durable by design (see below) and the
ephemeral paths are already capped.

## Retention (all enforced by the daily maintenance loop)

- Canonical events: transient types 7 days, durable history 180 days.
- NDJSON event debug trail: 30 days.
- heartbeat.log: 2000 lines / 1 MB cap.
- TTS spill / delegation files (`tmp/`): 7 days.
- User data (conversations, memory, routines, permissions, skills) is
  **never** pruned for space. Ever.

## SQLite posture (deliberate, do not "optimize")

- `journal_mode=WAL`, `synchronous=FULL`, `busy_timeout=5000`.
  Durability beats benchmark speed; a power cut must never corrupt the
  database (proven by the kill--9-mid-transaction regression test in
  `pkg/db`).
- `auto_vacuum` off: free pages (~2 MB observed) are reused in place;
  no surprise full-file rewrites on a small disk.
- `page_size` 4096 (default, SD-friendly).

## Recommendations

- Any 32 GB+ card from a reputable vendor is fine; high-endurance cards
  buy margin, not necessity.
- Keep journald on defaults (bounded); keep Ghost logs inside the
  workspace caps above.
- Re-measure if message volume grows 10× or new always-on writers land.

## Persistence invariants

Ghost uses SQLite (WAL, `synchronous=FULL`) as its durable store and does not
require a separate flush barrier: a `Publish`/`Exec` that returns committed is
durable. The invariants Ghost guarantees:

- **Append-only history.** Committed canonical events are never rewritten in
  place; `canonical_events.seq` is monotonic (`AUTOINCREMENT`). Redaction
  happens before persistence.
- **Committed-prefix immutability.** Once an event or state transition is
  committed and visible, later operations never change its historical meaning.
- **Replay is read-only.** `cevents` replay and `ghost replay` observe history
  and never re-execute side effects.
- **Fail-closed compatibility.** Migrations are forward-only and
  version-tracked (`schema_migrations` + `PRAGMA user_version`); a database
  from a newer schema is refused, not silently downgraded.
- **Atomicity.** Secret and config writes use temp-file + rename; snapshots
  validate before restore. SQLite transactions/WAL provide event atomicity.

Retention (daily `maintenance.Run`): transient events 7d, durable events 180d,
NDJSON 30d, temp files 7d, terminal turns 7d, finished jobs 30d, dangling file
artifacts. Live, waiting, interrupted, and canonical user data are never
pruned.
