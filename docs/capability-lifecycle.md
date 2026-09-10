# Capability Lifecycle and Ownership

Ghost has many long-lived and dynamically created resources. This document is
the single contract for how they are owned, bounded, and cleaned up. It is
descriptive of the real code, not an aspiration: each entry names the owner
and the cleanup path.

The invariant: **every resource has one owner, a bounded lifetime, and a
deterministic cleanup path. A resource whose owner is gone must not retain
authority.**

## The contract

Every resource answers six questions:

1. **Owner** — the component that creates and is responsible for it.
2. **Dependency** — what it needs to be usable.
3. **Lifecycle** — create → activate → terminal states.
4. **Cleanup** — who releases it and when.
5. **Restart** — what survives a process restart.
6. **Orphan prevention** — what stops it from outliving its owner.

## Current resources

| Resource | Owner | Dependency | Terminal / cleanup | Restart | Orphan prevention |
|---|---|---|---|---|---|
| Live surface (`pkg/live`) | `live.Registry` (per loop) | browser/computer gate | `CompleteTask` on turn settle; `Reconcile` idle expiry (30m); cap eviction (128) | Lost (memory-only) | User-held surfaces never evicted; `Resume` only from `paused` |
| Browser session (`pkg/browser`) | `browser.SessionStore` (SQLite) | profile dir | TTL 30m; `ExpireSweep(24h)` on gate use | Rows persist; re-minted on demand | TTL + per-call expiry |
| Computer lease (`pkg/computer`) | `LeaseStore` (SQLite) | computer provider | TTL 10m; explicit `Release`; **`RecoverStale` at boot** | Rows persist; stale expired at boot | `OwnedBy` re-check before acting; boot recovery |
| Routine (`pkg/routines`) | `routines.Service` | `scheduled.Store` | pause/resume/cancel/delete; `PruneOrphaned` at startup | Persists (SQLite) | Sidecar prune + execution-key idempotency |
| Scheduled run (`pkg/scheduled`) | `scheduled.Service` (1s tick) | clock gate | item lifecycle; retry/expire; **panic contained per run** | Persists (SQLite) | Panic recovery; missed items picked up on next tick |
| Permission request (`pkg/permissions`) | `Broker` (SQLite) | — | TTL 15m; `Resolve`; **`SweepExpires` at boot + every 15m** | Persists; stale swept | Fail-closed expiry; deny grants outrank allow |
| Agent turn (`pkg/turnlog`) | `turnlog.Store` (files) | — | terminal state immutable; `Recover` at boot | Persists | Crash-marked `interrupted`; stable trajectory id |
| Artifact (`pkg/artifacts`) | `artifacts.Store` (SQLite) | workspace file | `Get` downgrades to `unavailable` | Persists | Workspace confinement + protected prefixes |
| Durable job (`pkg/tasks`) | `tasks.Store` (SQLite) | — | validated transitions; terminal immutable | Persists | Generation guard; `MarkInterrupted` at boot |
| Canonical event (`pkg/cevents`) | `cevents.Stream` (SQLite) | — | type-based durability; `Prune` by age | Persists | Redaction at publish; seq monotonic |
| Credential | `config.Secrets` / `credentials.Vault` | — | atomic file write; vault is metadata-only | Persists | Secrets never enter config.json, events, or logs |

## Authorization ownership

The Permission Broker is the single authorization authority. Authorization is
**monotonic**: a decision is `allow < ask < deny`, and composition keeps the
most restrictive result (`permissions.Combine`). A deny can never be turned
into an allow by any later layer, including registered deny-only guards
(`Governance.AddGuard`). Providers, plugins, and the model can never grant
authority; they can only be subject to it.

## What is intentionally not here

Ghost does not use a plugin kernel or a generic lifecycle framework. Resources
extend their existing domain primitives. The contract above exists so the
question "who owns this, and who cleans it up?" has one answer per resource.
