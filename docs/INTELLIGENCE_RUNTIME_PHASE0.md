# Ghost Intelligence Runtime — Phase 0: Audit, Baseline, Architecture Map

Date: 2026-09-10. Head: `b860efe`. Model-test target: `deepseek/deepseek-v4-flash`
(stored key via `GHOST_CONFIG_DIR`). Hardware target: Raspberry Pi 5, 8 GB RAM.

This note is the Phase 0 deliverable (§0, §131): where responsibilities live
today, what already works, what is duplicated, the measured baseline, and the
concrete Phase 1 slice. It does not change behavior.

## 1. Architecture map (as built, not as briefed)

```
entrypoints (cmd/ghost, ghost-relay, ghost-web, ghost-service)
  ↓
HTTP API (cmd/ghost/internal_api.go + *_api.go, std ServeMux, /v1/*)
  SSE: POST /v1/chat, GET /v1/activity/stream, live surface streams
  WS:  GET /v1/ws (server→client fan-out only)
  ↓
turnlog.Claim (idempotency: replay 200 / 409 if live)
  ↓
AgentLoop.ProcessDirectWithChannel (pkg/agent/loop.go)
  fast paths: /commands, clarify-resume, deterministic skills,
              classifyEffort, tryRoutineTurn, approval-resume
  slow path:  runAgentLoop — history+summary+RAG+context,
              router.SelectModel, provider stream, tool loop,
              curator, steering injection
  ↓
providers (pkg/providers + pkg/provider strategy/breaker/fallback)
  single OpenAI-compatible HTTP path + native Anthropic/CLI variants
  ↓
tools (pkg/tools registry: schema validate → channel/session gate →
      capability check → governance authorize → execute → verify?)
  permissions broker (pkg/permissions) is the security boundary, not the model
  ↓
memory (4 overlapping systems — see §3)
persistence (ghost.db WAL via pkg/db + pkg/schema v3 + pkg/migrations)
```

Cross-cutting, already durable: `pkg/cevents` (canonical event stream,
exactly-once consumers, replay-describes-never-re-executes), `pkg/activity`
(human projection), `pkg/turnlog` (crash-marked `interrupted` turns),
`pkg/tasks` (durable jobs + checkpoints + waiting/paused/retrying states),
`pkg/doctor` (8 read-only checks), `pkg/golden` (NL eval harness, 52 cases).

## 2. What the brief asks for vs what already exists

| Brief concept | Existing seed (extend, don't duplicate) | Gap |
|---|---|---|
| Task state machine | `pkg/tasks` jobs + states + checkpoints + `resume_state` | no transition validation; resume path untested end-to-end |
| Trajectory IDs/events | `cevents` seq + taxonomy; `pkg/trajectory` is a *training-feed compressor*, different thing (naming collision — do not merge) | no per-turn trajectory id linking model→tool→verify |
| Structured execution events | `bus` (transient) + `cevents` (durable, redacted) | agent loop + tool registry don't emit task/tool/verify events |
| Effort control | `classifyEffort` fast path + `router.SelectModel` + `FallbackChain` | no Quick/Normal/Deep budgets driving retrieval/tools/verify |
| Context compiler | `pkg/agent/context.go` BuildSystemPrompt/BuildMessages | concatenates; no budget, deltas, cache, trust levels |
| Memory tiers/provenance | personal-context JSONL (scoped, authoritative) + digest; RAG over memory_chunks | no lifetimes, provenance, confidence, promotion, contradiction |
| Hierarchical retrieval | digest (cheap) → RAG over-fetch 5x → scope filter → top-N | no candidate staging, scoring is similarity-only, no rerank |
| Verifier contracts | `VerifiableTool` (filesystem Write/Append only) | pattern not extended to calendar/cron/scheduler |
| Golden tasks | `pkg/golden` 52 cases, world-state assertions exist | no task+environment+verifier record format, no replay cmd |
| Doctor intelligence | DB/schema/provider/tool/skill checks | no memory/task/retrieval/latency section |

## 3. Duplication to resolve (incrementally, none this phase)

1. Schedulers: `pkg/cron` (JSON file, legacy) vs `pkg/scheduled` (SQLite, canonical). Direction: canonicalize on `scheduled`; retire `cron` via migration.
2. Memory: `MEMORY.md` files (legacy, no longer injected) vs `memory_chunks`+RAG vs `personal-context` JSONL (authoritative) vs `memory_curate` notes vs Honcho (opt-in). Direction: tiers over personal-context + chunks; MEMORY.md becomes derived/export only.
3. Events: `bus` + `events` (both transient in-mem) vs `cevents` (durable). Direction: keep transient bus + durable cevents; audit `events` pkg for folding.
4. Types: `providers.{Message,ToolCall,…}` ≡ `tools.{same}`; `FallbackChain` vs `provider.Strategy`. Direction: single alias source, no behavior change.
5. Session stores: SQLite (live) vs JSONL (retained). Direction: SQLite canonical, JSONL export/restore only.

Out of scope for Phase 1: sandboxing beyond current gates, NVMe tiers,
multimodal, multi-agent (abstraction only when Phase 4+ needs it).

## 4. Baseline (measured, not assumed)

- `go build ./...` — PASS. `go vet ./...` — PASS.
- `gofmt -l` — one pre-existing offender: `cmd/ghost-updater/main.go` (untouched).
- `go test ./...` — ALL PASS, zero failures at HEAD.
- Golden (deepseek/deepseek-v4-flash, stored key): **52/52 PASS, 0 hard fails**,
  ~288 s. Artifact: `/tmp/golden_baseline_head.json`.
- Pre-existing risks noted, not caused by us: systemd traps SIGINT only
  (SIGTERM relies on Restart=always); `pkg/schema` is DB migrations, not
  JSON-schema; repo `workspace/` contains a committed `ghost.db` distinct
  from runtime `/var/lib/ghost/workspace/ghost.db`.

## 5. Compatibility constraints (mobile contract, docs/mobile-contract.md)

Additive-only API/events; `request_id` idempotency; terminal outcome vocab
(success/failed/waiting_for_user/waiting_for_permission); reconnect-by-observe
(`/v1/chat/turn`), never resend; no client-inferred success. Every phase must
keep the 52 golden cases green on deepseek before merge.

## 6. Phase 1 slice (durable state + trajectories) — proposed, in order

1. **Trajectory ID**: generate per turn in `turnlog.Claim`, propagate via
   context through agent loop → provider calls → tool executions →
   verification; attach to every `cevents` payload for the turn.
2. **Execution events**: emit `tool_started/tool_completed` (normalized,
   redacted observation), `verification_*`, `task_checkpointed`,
   `fallback_started/model_escalated` from loop + registry + broker.
3. **Task transitions**: validate transitions in `pkg/tasks`
   (e.g. COMPLETED→RUNNING rejected without new execution); wire
   crash recovery: `turnlog.Recover` (interrupted) → jobs `interrupted` →
   validate world state → resume from checkpoint; test restart-resume.
4. **Doctor intelligence (minimal)**: memory counts by store, active task
   counts, retrieval latency (adds the first retrieval timers, reused by
   Phase 3 scoring), last recovery events from cevents.

Tests per phase-1 item (brief §121/§149 style): transition rejection,
checkpoint→restart→resume, verification-failure recovery, fallback path,
event redaction (no secrets in cevents payloads).

Planned ADRs (written as phases land): task state machine; trajectory event
schema; memory lifetimes; context cache invalidation; model routing;
verification contracts; sandbox boundary.
