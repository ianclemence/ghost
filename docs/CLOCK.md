# Clock Trust

A stock Raspberry Pi has no battery-backed clock. After a power loss
without network it can boot believing it is 1970 — and every routine,
TTL, and cron comparison in Ghost runs on wall time. This document
states the policy; `pkg/clock` implements it.

## Time states

- **invalid** — provably wrong (before 2024-01-01). Schedulers hold all
  firing. Nothing executes "because 2026 finally arrived."
- **synced** — NTP reports synchronization. Normal operation.
- **unsynced** — sane time, NTP explicitly not synchronized (typical
  offline boot with a persisted clock). Normal operation: an
  unsynced-but-sane clock behaves fine, and blocking it would brick
  automation for every offline user.
- **unknown** — sync state indeterminable (no timedatectl). Treated like
  unsynced.

Only **invalid** blocks scheduler firing. Conversational and local
functions are never gated — Ghost talks and remembers with a wrong
clock; it just doesn't act on time.

## Mechanics

- One shared gate (`clock.NewGate`, 30 s cache) feeds both the
  scheduled-items service and the legacy cron service. Nil gate =
  always fire (tests, callers without a clock).
- A monotonic-vs-wall watcher logs loud warnings on steps over 2 min so
  "everything fired at once" mysteries are diagnosable; firing semantics
  themselves are unchanged (late reminders still catch up — deliberate).
- `ghost doctor` reports the clock: error when invalid, warning when
  unsynced/unknown, ok when synced.
- Time sync itself is the OS's job (systemd-timesyncd/chrony/NTP).
  Ghost only observes; it never implements NTP.

## Known limitation (deferred, documented)

One-time natural-language schedules ("at 9pm") are interpreted as UTC
regardless of device timezone; recurring rules honor timezones
correctly. Fixing the one-time path is tracked separately and does not
affect trust gating.
