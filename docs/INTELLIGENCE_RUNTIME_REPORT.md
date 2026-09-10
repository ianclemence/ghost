# Ghost Intelligence Runtime — Engineering Report (Phases 1–8)

Model target: `deepseek/deepseek-flash` (DeepSeek-V4.1-Flash). Baseline and
architecture map: `INTELLIGENCE_RUNTIME_PHASE0.md`. Phase 1 detail:
`INTELLIGENCE_RUNTIME_PHASE1.md`. DeepSeek cross-check:
`DEEPSEEK_V41_CROSSCHECK.md`.

Guiding principle (brief §1): Ghost never spends compute, memory, storage,
network, tokens, or reasoning unless the expected benefit justifies the cost.
Every phase extends an existing abstraction; nothing was rewritten.

## 1. Architecture — what changed and why

The runtime now moves through: reflex → effort → context → retrieval →
task state → model router → tools → verification → recovery → memory. The
changes are additive and wired at real integration points, not stand-ins.

| Concern | Package | Status |
|---|---|---|
| Trajectory identity | `pkg/turnlog`, `pkg/cevents` | per-turn `trj_` id, durable, replayable |
| Execution events | `pkg/cevents`, `pkg/agent/governance.go` | tool/verification/fallback/task/effort events |
| Task state machine | `pkg/tasks` | validated transitions, crash recovery, durable events |
| Memory lifetimes/provenance | `pkg/personalcontext` | lifetime, trust class, promotion gate |
| Retrieval scoring/budget | `pkg/retrieval`, `pkg/rag`, `pkg/session` | weighted rerank + token budget |
| Context cache | `pkg/contextcache`, `pkg/agent/context.go` | versioned, bounded, invalidating |
| Effort/routing | `pkg/effort`, `pkg/providers/capabilities.go` | Quick/Normal/Deep budget + descriptors |
| Tool observations/retry | `pkg/tools` | normalized observations, error classes, retry gate |
| Execution hardening | `pkg/tools/env_guard.go` | secret-free subprocess environment |
| Replay/eval | `pkg/cevents`, `pkg/failurecorpus`, `cmd/ghost/replay.go` | `ghost replay`, failure corpus |
| Resource pressure | `pkg/hardware/pressure.go`, `pkg/doctor` | live pressure + adaptive retrieval |

## 2. Database & migrations

- **v4** (`canonical_events.trajectory_id` + index) — idempotent, additive.
- No destructive migrations. Old event rows keep `NULL` trajectory and scan.
- Backups written before v4 restore with a NULL tail (`rehydrateWithNullTail`).
- `personalcontext` lifetime is a JSON field with load-time normalization — no
  log rewrite, no data loss, provenance never invented.
- Task events reuse `canonical_events` (no new table).

## 3. APIs / events

- `GET /v1/chat/turn` includes `turn.trajectory_id` (additive).
- New event types: `verification.started|completed|failed`, `fallback.started`,
  `model.escalated`, `effort.selected`, and the `task.*` lifecycle family.
- SSE/WS contracts unchanged; new events are internal-trace visibility by
  default so the mobile UI is not spammed.
- New CLI: `ghost replay <trajectory-id> [--json]` / `--list`.

## 4. Memory (tiers, provenance, promotion)

- **Lifetimes**: `durable` (default) and `reconstructable`. HOT/WARM live in
  the session store and RAG, not Personal Context; documented rather than
  forced into a durable store.
- **Provenance**: `ProvenanceClass` orders trust — canonical (user/manual) >
  observed (document/import/workflow) > inferred (model) > unknown (legacy).
- **Promotion**: `PromotionPolicy` gates model inference from becoming current
  truth; weak/no-provenance candidates are stored `uncertain`, not promoted.
  Enforced at the semantic-extraction path.
- **Contradiction**: existing `DeclareConflict`/`ResolveConflict` retire losers
  with `superseded_by`, preserving the full chain; no silent overwrite.
- **Versioning**: `Store.Version()` is a monotonic counter for cache keys.

## 5. Context & retrieval

- **Scoring** (`pkg/retrieval`): deterministic, inspectable weighted score —
  semantic + lexical + recency + confidence + source quality, minus staleness
  and contradiction — with a per-component breakdown.
- **Hierarchy**: RAG over-fetches a coarse vector pool, then reranks with the
  scorer and truncates (the sparse-indexer lesson applied to memory).
- **Budget**: a token budget keeps only the highest-value items.
- **Cache** (`pkg/contextcache`): bounded LRU, TTL, prefix invalidation, and
  hit/miss/invalidation stats; the compiled system prompt is keyed by memory
  version + bootstrap mtimes + tools + persona, so it rebuilds only when an
  input actually changes.

## 6. Effort & routing

- **Levels**: Quick/Normal/Deep with an explicit budget (output tokens, tool
  calls, execution ms, retries, verification strength, retrieval depth, cloud
  permission). Quick is a hard small tool-call cap.
- **Signals**: deterministic complexity heuristics (multi-step, code/debug,
  ambiguity, memory dependency, length, prior failures) — no model classifier
  until telemetry justifies it.
- **Escalation**: bounded Quick→Normal→Deep; never infinite.
- **Descriptors** (`providers.Describe`): local, tool calling, vision,
  reasoning, max context, RAM estimate — routing by ability, not name.

## 7. Tools: authorization, verification, retry

- Authorization unchanged: the permission broker + gates remain the security
  boundary; the model never enforces policy.
- **Observations**: every result is normalized (status, error class,
  retryable, reconstructable, bounded summary) and recorded on the trajectory.
- **Verification**: `VerifiableTool` outcomes are observed at the registry and
  recorded; `VerificationContract` declares intended world-state checks.
- **Retry**: classification gate — only timeout/network/transient retry, even
  for opted-in tools; validation/permission/not-found/internal do not.

## 8. Security

- Subprocess environment is sanitized (`EnvGuard`): secret-shaped vars and the
  `GHOST_*` namespace are never inherited; HOME is confined to the workspace.
- Existing boundaries retained: filesystem confinement + `ScopeGuard`, URL
  SSRF/metadata blocking, command guard, browser/computer gates, secret
  redaction at the event boundary.
- Memory scoping retained and extended to the daily-note journal (Phase 1.1).

## 9. Recovery & replay

- Task transitions are validated; terminal jobs cannot be resurrected.
- Crash recovery rotates generations so zombie completions are refused.
- `ghost replay` reconstructs a full execution trace from durable events;
  replay describes, never re-executes.
- Failures are captured locally in the failure corpus (redacted) for
  regression conversion.

## 10. Resource pressure

- Live memory/disk snapshot with normal/warning/critical classification.
- Under pressure, retrieval budget scales down (1.0/0.5/0.25); canonical memory
  is never scaled away.
- Doctor reports RAM/disk headroom alongside memory/task/retrieval counts.

## 11. Evaluation & tests

- Full `go test ./...` green across every phase.
- Golden suite: **52/52 PASS, 0 hard fails** on `deepseek/deepseek-flash`
  (artifact `/tmp/golden_final_allphases.json`).
- New unit suites: turnlog trajectories, cevents trajectory/replay, task
  transitions, memory lifetime/promotion, retrieval scoring/budget,
  context cache, effort policy, tool observation/retry, env guard, failure
  corpus, hardware pressure, doctor intelligence/resources.

## 12. Performance

- No hot-path serialization added; scoring and budgets are O(candidates).
- Resource snapshots cached 30s; Doctor queries are bounded `COUNT(*)`/24h.
- Prompt cache avoids rebuilding the system prompt on every internal turn.
- Before/after latency benchmarks on real hardware remain Phase 8 follow-up;
  no optimization was claimed without measurement.

## 13. Compatibility

- Existing chat/SSE/WS/session/clarification/steering/doctor/model APIs
  preserved; new fields and events are additive.
- Old databases migrate forward (v4) and old backups restore.
- Existing memory, sessions, tasks, and secrets survive.

## 14. Known limitations (explicit)

- Permission events carry `request_id` only, not trajectory (documented).
- Background/cron task events have no trajectory until the turn-driven
  executor stamps one.
- Retrieval latency is observed, not yet fed back into scoring.
- Model routing is descriptor-driven but not yet hardware-scheduling.
- Failure corpus capture is wired to golden failures, not yet every runtime
  verification failure.
- No OS-level namespace/seccomp isolation for `exec` beyond env sanitization
  and existing guards.
- DeepSeek V4.1 report review flagged: "reconstructable" must stay approximate
  for context but exact for task state (respected); CED/CSA2/Engram/DSpark are
  not used as implementation mandates.

## 15. Follow-up

- Feed observed retrieval latency into scoring weights.
- Stamp trajectories onto cron/background tasks and permission events.
- Convert failure-corpus entries into golden regression cases.
- Hardware-realistic latency/RAM benchmarks on the Pi.
- Optional `exec` namespace isolation where the OS permits.

---

## Appendix — Architecture Decision Records (condensed)

**ADR-1 Memory lifetimes.** *Problem:* all context treated as equivalent.
*Decision:* explicit `durable`/`reconstructable` on durable memory; HOT/WARM
stay in session/RAG. *Why:* lets derived data be reclaimed without touching
canonical facts. *Trade-off:* two more concepts; avoided forcing tiers onto a
store that is durable by design.

**ADR-2 Trajectory IDs.** *Problem:* failures were unattributable.
*Decision:* one `trj_` id per claimed turn, stable across reconnects, on
`canonical_events`. *Why:* connects model/tool/verification/recovery into one
replayable trace. *Trade-off:* one column + index.

**ADR-3 Task state machine.** *Problem:* terminal jobs could silently no-op.
*Decision:* explicit `TransitionError`; same-outcome idempotent; generation
rotation on recovery. *Why:* no dead work revived, no zombie completions.
*Trade-off:* callers must handle the new error.

**ADR-4 Promotion policy.** *Problem:* model inference could become truth.
*Decision:* trust-class + confidence gate; weak inference held `uncertain`.
*Why:* breaks self-reinforcing hallucination. *Trade-off:* tunable threshold.

**ADR-5 Context cache.** *Problem:* rebuilding identical context each turn.
*Decision:* versioned bounded LRU keyed by memory version + file mtimes +
tools + persona. *Why:* avoids repeated work; invalidation is explicit.
*Trade-off:* a stale prompt if the version is incomplete (mitigated by
mtimes).

**ADR-6 Effort control.** *Problem:* every request paid deep-loop cost.
*Decision:* Quick/Normal/Deep budgets governing tools/retrieval/verification/
cloud. *Why:* cheap work first. *Trade-off:* heuristic classification; tuned
conservatively to avoid starving complex turns.

**ADR-7 Verification & retry.** *Problem:* success claims vs world state;
wasted retries. *Decision:* observation + error classes; only transient retry.
*Why:* real world state wins; no duplicate intent. *Trade-off:* new error
classes need upkeep.

**ADR-8 Sandbox boundary.** *Problem:* subprocesses inherited secrets.
*Decision:* minimal allowlist environment, HOME confined, extras screened.
*Why:* closes the direct exfiltration path on the Pi. *Trade-off:* not full
namespace isolation (deferred).
