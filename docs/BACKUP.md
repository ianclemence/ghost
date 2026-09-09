# Backup & Restore Contract

This document is the authoritative statement of what a Ghost backup
contains, what it does not, and what restore guarantees. If behavior ever
disagrees with this file, the behavior is the bug.

## Formats

There is exactly one backup format: the **encrypted Ghost State archive**
(`.ghost`), produced by `ghost state export` and by console
Download backup (which prompts for a passphrase). Console restore accepts
only this format. There is no supported restore path for ad-hoc tarballs.

Archives are passphrase-encrypted (minimum 8 characters at the console;
the CLI requires a non-empty passphrase). A forgotten passphrase means an
unrestorable backup — there is no recovery path, by design.

## What survives (preserved)

- Ghost identity (`state/identity.json`) — the restored Ghost is the same Ghost
- Conversations (sessions + messages, as versioned JSONL; database rehydrated)
- Memory files (`memory/`, `knowledge/`) and RAG embeddings (`memory_chunks`)
- Routines (`scheduled_items` rows with `source=routine` + `routine_meta`)
- Scheduled automations/reminders (`scheduled_items`)
- Standing permission grants AND denials (`permission_grants`)
- Agent background jobs (`jobs`), execution history, canonical events
- Key-value store, tool usage/pins
- Skills, workflows (`kanban.json`), USER.md/SOUL.md/docs
- Configuration (`config/config.json`, sanitized)

## What rebinds (not exported, must be redone)

- Device pairings (`paired_devices`) — re-pair every phone/device
- Pairing handshakes in flight (`pending_pairings`, `permission_requests`)
- Provider API bases/proxies, gateway host/port (target machine wins)
- The ghost.db binary itself (rebuilt from portable artifacts on import)

## What is never restored

- Secrets: `.secrets.json`, `.env`, OAuth tokens, credential stores —
  unless exported explicitly with `ghost state export --include-secrets`
  (console backups never include secrets)
- The owner password is NOT in the backup and does not change on restore
- Ephemeral runtime state: WAL sidecars, sockets, locks, PIDs, temp files

## Restore semantics

- Import refuses a non-fresh workspace unless forced; the console always
  validates first (decrypt + manifest + content inventory), shows a
  summary, and requires explicit confirmation before replacing state.
- Validation is fail-closed: unknown tables, column drift, bad digests,
  path traversal, and schema-version mismatch all abort with the reason.
- The gateway is stopped during apply and restarted after, success or
  failure — a failed restore never leaves the machine down.
- After restore: re-pair devices, reconnect integrations that need fresh
  credentials. Owner password is unchanged.

## Operational notes

- Backups exclude nothing silently: every exclusion is recorded in the
  manifest (`rebound`, `secrets_excluded`) and shown before confirmation.
- `ghost state inspect <archive>` shows the manifest without importing.
- Test restores against a scratch workspace before trusting an archive
  with the only copy of a Ghost.
