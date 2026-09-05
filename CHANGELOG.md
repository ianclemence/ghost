# Changelog

All notable changes to Ghost are tracked here. This project uses
[Semantic Versioning](https://semver.org) with tags of the form `vX.Y.Z`.

## [v0.22.0] - 2026-09-05

> The "capability substrate & appliance" release. Ghost is now a local-first
> personal AI runtime whose **runtime evidence — not model claims — is the
> authority** for what happened. It ships a permission broker, canonical event
> evidence, durable memory + RAG with context isolation, routines, contexts,
> voice/device interfaces, appliance verification, benchmarking, and a
> model-agnostic Golden Conversation Suite. Backend status: **READY FOR MOBILE**.

### Reliability (core principle)

- Runtime execution evidence is authoritative: deterministic network dispatch
  (weather/AQI/flight) executes provider-backed tools and returns validated
  output; forecast/unsupported asks are answered honestly, never fabricated.
- Duplicate protection: routine creation, scheduled executions, and approval
  continuations are idempotent; repeating a routine request no longer stacks
  duplicates.
- Durable clarification continuations survive process restart; resumed turns
  are not re-asked and approvals are not confused with clarifications.
- Provider failure classification, bounded retries/backoff/jitter, circuit
  breaking, and honest unavailability.

### Product / governance

- Ghost identity + primary-agent model; one persistent entity per appliance.
- Permission broker: ALLOW/ASK/DENY with durable, scoped grants; allow-once
  exact-once consumption; revocation; expiry; model cannot self-grant.
- Canonical event stream (redacted, ordered, persisted) and user-safe Activity
  projection; events are evidence, replay never re-executes.
- Memory + RAG: durable structured memory, natural corrections (supersede),
  context scoping enforced on every model-reachable read; RAG always enabled.
- Contexts (personal/work/project) with memory + capability isolation;
  cross-user and cross-context isolation proven.
- Routines: natural-language creation, durable scheduling, timezone handling,
  duplicate prevention, restart persistence, evidence-based execution.
- Voice as a channel (push-to-talk) into the same runtime; Home Assistant as a
  governed device capability (no unrestricted shell).
- Credential vault: write-only from the UI, presence-only in events, redacted
  everywhere, excluded from backups.

### Appliance / operations

- Idempotent provisioning/setup and canonical health model.
- Hardware-aware defaults (Pi 5, RK1-class, x86 mini-PC, GPU).
- Automatic bundled-skill seeding on first CLI run; self-healing capabilities.
- Storage/retention discipline for small-SD appliances.
- `ghost verify` (infrastructure + Ghost quality + hard gates) and
  `ghost benchmark` (Ghost Core Score + governance hard-fails + history).

### Evaluation

- **Golden Conversation Suite** (`ghost golden`): model-agnostic natural-language
  evaluation, 34 conversations across 14 categories, semantic assertions,
  truthfulness hard-gates, model-vs-runtime failure classification, JSON output,
  history/comparison, suite versioning. Detects models claiming actions with no
  execution evidence. Qwen is a supported target but was intentionally NOT run
  (too slow on the development appliance).

### Security

- Context isolation enforced on retrieval, not by prompts.
- Deterministic rejection of broad account-grant attempts; no unintended grants.
- Secret redaction at event boundaries; backups verified secret-free.

### Bug fixes (from real-user + golden testing)

- Fabricated live data (models answering weather without a tool) fixed via
  deterministic dispatch.
- Durable clarification lost across processes/restarts fixed.
- Natural-language standing permissions ("You can always add calendar events
  for me") recognized and runtime-validated.
- Fresh CLI workspaces now auto-seed bundled skills.
- Location parsing and memory-value extraction edge cases fixed.
- RAG pinned enabled; the console cannot silently disable it.

[//]: # (v0.21.0 history is represented by the git tag v0.21.0)
