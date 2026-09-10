# Ghost Intelligence Runtime — Phase 1 Report (Durable State + Trajectories)

Status: complete. Head before this phase: `eb09c42` (1.1), `2e3a636` (1.2);
1.3/1.4 in the follow-up commit. Model target: `deepseek/deepseek-flash`
(DeepSeek-V4.1-Flash). Baseline and audit: `docs/INTELLIGENCE_RUNTIME_PHASE0.md`.

## 1. Architecture — what changed and why

Phase 1 makes execution **observable, durable, and recoverable** before any
memory/context/effort work depends on it. Nothing was rewritten; every change
extends an existing abstraction:

| Concern | Existing seed extended | What Phase 1 added |
|---|---|---|
| Turn identity | `pkg/turnlog` | per-turn `trajectory_id` (minted on claim, stable across reconnects) |
| Execution trace | `pkg/cevents` | `trajectory_id` column + `ByTrajectory`; verification/fallback/task types |
| Tool world-state checks | `tools.VerifiableTool` | `SetVerifySink` observes outcomes where they happen |
| Task state machine | `pkg/tasks` jobs | explicit transition errors; generation rotation on recovery; durable task events |
| Doctor | `pkg/doctor` | `checkIntelligence` (memory/tasks/recovery/retrieval latency) |
| Retrieval cost | — | measured at RAG assembly and memory-note search |

## 2. Files

**1.1 Trajectory identity**
- `pkg/turnlog/turnlog.go` — `Turn.TrajectoryID`, `NewTrajectoryID`,
  `WithTrajectoryID`/`TrajectoryIDFromContext`.
- `cmd/ghost/internal_api.go` — captures the claim's trajectory and carries it
  into the turn context; `/v1/chat/turn` exposes it via the Turn record.
- `pkg/cevents/cevents.go` — `trajectory_id` column, index, `Filter.TrajectoryID`,
  `ByTrajectory`; `EnsureTrajectoryColumn` for idempotent convergence.
- `pkg/schema/schema.go` — migration v4.
- `pkg/ghoststate/dbsnapshot.go` — snapshot shape + NULL-tail restore for
  pre-trajectory archives.

**1.2 Execution events**
- `pkg/cevents/cevents.go` — `verification.started/completed/failed`,
  `fallback.started`, `model.escalated`.
- `pkg/agent/governance.go` — turn/tool/capability publishes take a trajectory;
  new `VerificationRan`, `FallbackRan`.
- `pkg/tools/registry.go` — `SetVerifySink`.
- `pkg/agent/loop.go` — sink bridge; fallback emission in `callLLM`.

**1.3 Task state + recovery**
- `pkg/tasks/tasks.go` — `TransitionError`, terminal-transition rejection,
  `noopOrReject`, generation rotation in `MarkInterrupted`, durable task events
  (`publishTaskEvent`), `SetEventStream`, `EventCheckpointed`.
- `pkg/agent/loop.go` — task-event mapping (interrupted/checkpointed are not
  failures); `SetGovernance` attaches the stream.

**1.4 Doctor intelligence**
- `pkg/agent/retrieval_stats.go` — concurrency-safe latency aggregates.
- `pkg/tools/memory_recall.go` — `LatencyObserver`.
- `pkg/doctor/doctor.go` — `checkIntelligence`, `SetRetrievalSource`.

## 3. Database

- **v4 migration**: `canonical_events.trajectory_id TEXT` + index
  `idx_cevents_trajectory`. Idempotent via `PRAGMA table_info` check; fresh
  installs get the column from the v1 baseline (`cevents.Open`).
- **No destructive change.** Old event rows keep `NULL` trajectory and scan
  fine. Backup archives written before v4 restore with a NULL tail.
- Task events add no schema (they reuse `canonical_events`).

## 4. APIs / events

- `GET /v1/chat/turn` now includes `turn.trajectory_id` (additive).
- New durable event types: `verification.started|completed|failed`,
  `fallback.started`, `model.escalated`, `task.created|started|checkpointed|
  resumed|retrying|waiting|paused|completed|failed|cancelled|expired|interrupted`.
- SSE/WS contracts unchanged (additive-only). Verification/fallback/task events
  are internal-trace visibility by default, so the mobile UI is not spammed.

## 5. Tasks — checkpoint / resume / recovery

- **Invalid transitions rejected**: `start/resume/retry/pause/wait/expire/
  progress/finish` on a terminal job return `*TransitionError` instead of
  silently no-op-ing. Same-outcome completion stays idempotent.
- **Crash recovery**: `MarkInterrupted` rotates each recovered job's generation,
  so a zombie worker's late completion fails `CheckGeneration`.
- **Durable evidence**: every transition publishes to `canonical_events` with
  job identity (`job_id`, `kind`, `owner`, `context`, attempts, progress).
  Bare progress heartbeats stay out of the warehouse; checkpoints land.

## 6. Verification

- `VerifiableTool` outcomes are observed at the registry and recorded on the
  turn trajectory (`verification.completed/failed`, tool + latency + detail).
- A failed verification is durable evidence and still surfaces as a tool error,
  preserving "real world state wins".

## 7. Doctor

`checkIntelligence` reports: durable facts (current personal-context entries),
embeddings (`memory_chunks`), active/completed/recovered tasks, 24h
verification failures, and observed retrieval latency (RAG + note search).
Warning (not silent) when a verification failure occurred in the last 24h.

## 8. Tests

- `pkg/turnlog` — stable trajectory across reconnect, uniqueness, context
  round-trip, legacy-turn load.
- `pkg/cevents/trajectory_test.go` — trace linkage, filters, NULL legacy rows,
  column convergence.
- `pkg/agent/governance_trace_test.go` — events carry trajectory; failed
  verification durable; nil-safe.
- `pkg/agent/trace_wiring_test.go` — a full `processMessage` turn lands
  `agent.started → tool.completed → agent.completed` on one trajectory
  (mock provider, no model).
- `pkg/tools/verify_sink_test.go` — sink fires on pass/fail, silent for
  non-verifiable tools, sees session + trajectory.
- `pkg/tasks/transitions_test.go` — terminal rejection, idempotent same-outcome,
  generation rotation, durable task events, nil-stream safety.
- `pkg/doctor/intelligence_test.go` — real-state counts, warning on
  verification failure, missing-store info, entry counting.
- `pkg/schema/staged_test.go` — v3→v4 upgrade with live rows.

## 9. Performance / compatibility

- No hot-path serialization added. Trajectory is a string on the turn context;
  Doctor queries are bounded `COUNT(*)`/24h windows.
- `go build`, `go vet`, full `go test ./...` green.
- Golden: **52/52 PASS, 0 hard fails** on `deepseek/deepseek-flash`.
- Existing API/SSE/WS/session contracts preserved; schema changes are additive
  and backward-compatible.

## 10. Known limitations

- **Trajectory on permission events**: the permission broker emits
  `permission.*` with `request_id` only (no trajectory); correlation rides the
  request. Threading trajectory into the broker is future work.
- **Task events have no trajectory**: background/cron-originated jobs are
  honest-empty. The turn-driven executor (Phase 5) will stamp the originating
  trajectory.
- **Doctor intelligence is count-based**, not a resource-pressure model
  (RAM/disk thresholds are Phase 8).
- **Retrieval latency is observed, not yet acted on** (scoring/reranking is
  Phase 3).

## 11. Follow-up

- Phase 2: memory lifetimes + provenance + promotion.
- Phase 3: context compiler, hierarchical retrieval, budgets, cache.
- Wire trajectory into the permission broker when the task engine owns
  permission waits.
