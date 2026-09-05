# Ghost Reference Appliance

The canonical definition of a correctly configured Ghost. Not a demo,
not an edition, not a special build — the behavior and composition
every change is judged against:

> Does this make the reference Ghost better, or merely more complicated?

## Composition

```
Ghost
├── one canonical identity (ghost-id minted once, owner linked)
├── one primary agent (agent-main; future kinds attach, never orchestrate)
├── local AI first (Ollama; cloud only in hybrid/cloud modes, never for governance)
├── SQLite-backed state (ghost.db + append-only logs; no other database)
├── memory (personalcontext: write → validate → dedupe → evolve; scope-tagged)
├── canonical capabilities (runtime-registered, risk-declared, readiness-gated)
├── permission broker (ALLOW/ASK/DENY; durable requests; scoped grants; expiry)
├── canonical event stream (redacted at publish, ordered, SQLite + NDJSON)
├── activity (chips from safe events only; three detail layers)
├── credential vault (references + states; secrets never leave the boundary)
├── routines (scheduler-backed; same pipeline as chat; waiting, not hanging)
├── calendar integration (OAuth web flow; narrowest scopes; relay callback)
├── optional cloud reasoning (mode-gated fallback, never authority)
├── optional device capability (trust lattice; declared capabilities only)
└── channels (web/mobile/voice/telegram → one Request envelope → one runtime)
```

## Behavior contract

- Plug in → Pair → Name → Talk → remembers → asks when necessary → acts → reports.
- No fake success: runtime evidence determines outcomes; the model explains.
- Offline: local works; cloud reports honest unavailability with product language.
- Restart: identity, memory, permissions, routines, events, and sessions recover.
- Replay: describes, never executes. Idempotency keys prevent doubles.
- Secrets: absent from events, activity, SSE, APIs, logs, backups, model context.

## Complexity model (the one control)

The owner chooses **intelligence mode** — local, hybrid, or cloud
(`/v1/mode`, `GHOST_INTELLIGENCE_MODE`, or derived). Everything else
(model tiers, concurrency, context sizes, routing, retries, thresholds)
derives from the hardware profile + mode automatically. Advanced and
diagnostic surfaces exist separately; the product surface stays calm.

## Hardware classes

| Class | Example | Model tier | Concurrency | Context | Voice local |
|---|---|---|---|---|---|
| raspberry-pi-5 | Pi 5 8GB | small | 2 | 8k | no |
| rk1-class | 12–16GB ARM SBC | medium | 4 | 16k | yes |
| x86-mini-pc | N100-class | medium | 4 | 32k | yes |
| gpu-server | CUDA/ROCm/32GB+ | large | 8 | 64k | yes |
| generic | unknown | small | 2 | 8k | no |

Detection is best-effort (`pkg/hardware`); unknown machines get safe
generic defaults. Non-local classes are labeled NOT_TESTED without
hardware — never invented numbers.

## Memory origins and scope (honest audit)

| Origin | Write tags scope | Retrieval scoped |
|---|---|---|
| Regex extractor (chat) | yes (`context:<id>`, personal stays global) | yes (digest, fast-path, semantic dedup) |
| Semantic extractor (chat) | yes | yes |
| `context_get` tool | n/a (read) | yes (session-bound, server-side) |
| Curator notes (`memory_curate` tool) | yes (per-context files; personal writes the global file) | yes (global + own-context merged) |
| CLI-imported memories (`--context`) | yes (untagged gain scope; tagged keep theirs) | yes |
| CLI-imported memories (no flag) | global (documented legacy default) | global |
| `/v1/memory/self` UI | n/a | owner view: all by default, `?context=` filters |
| `context`/`forget` CLI commands | n/a | owner-operated, global by design |
| Compaction | preserves tags | n/a (maintenance, not Q&A) |

Rule: **model-reachable retrieval is scope-filtered on every path**
(prompt digest, fast-path recall, semantic dedup, `context_get`,
curator notes). No silent globals remain in model-reachable code.

## Global semantics

Global is deliberate, never a leftover. A memory is global when it is:

- written from the personal context (the V1 default scope), or
- imported without `--context` (legacy archives), or
- unscoped by an explicit owner action.

Global means **deliberately cross-context**: genuinely universal facts
(the owner's name, language, home city) that every context may use.
Precedence is explicit and total:

1. Context-scoped facts win over global facts on the same predicate.
2. Global facts never override a context-scoped fact.
3. A context never sees another context's scoped facts, no matter how
   semantically similar.
4. Owner surfaces (`/v1/memory/self` default, CLI commands, console
   forget-everywhere) intentionally see everything: isolation constrains
   the *model*, not the owner.

## Deployment assumptions (current)

- Raspberry Pi 5 · 8GB RAM · 32GB SD card. NVMe arrives later as a
  **storage migration, not a redesign**: identity, SQLite schema, and
  paths stay identical; only the mount backing them changes.
- SD discipline: retention runs at daemon startup + every 24h
  (`pkg/maintenance`): transient events >7d, user history >180d,
  NDJSON logs >30d, heartbeat.log capped at 1MB/2000 lines, tmp/ >7d,
  bench history keeps 20 runs. `ghost verify` checks disk headroom
  (fail ≥85%, hard fail ≥95%) and retention health.
- No component assumes NVMe latency or capacity.

## Verification & benchmark

- `ghost verify [--json] [--live]`: INFRASTRUCTURE (structurally
  healthy?) + GHOST QUALITY (behaves correctly?) + OVERALL. Hard fails
  (unauthorized execution, leaks, replay side effects, duplicates,
  cross-owner/context leaks, false success, bypass, backup secrets)
  force FAIL.
- `ghost benchmark [--json] [--no-save]`: latencies by layer, honest
  capability grading, 10 governance invariants, deterministic memory
  eval + context matrix, routine idempotency, 0-leak privacy, restart
  recovery → Ghost Core Score (15/20/15/20/10/10/10) with hard-gate
  override + local history for change→compare.
- Live-model prose and non-local hardware are labeled NOT_TESTED,
  never scored.

## Engineering philosophy

Ghost is: local-first · capability-oriented · evidence-driven ·
permission-governed · measurable · hardware-aware · minimal in concepts ·
easy to extend · easy to diagnose.

Ghost is not: a generic agent framework · a cloud orchestration platform ·
a configurable AI laboratory · a collection of managers · microservices ·
an LLM wrapper delegating authority to the model.

**Complexity belongs in the runtime, not in the owner's head.**
