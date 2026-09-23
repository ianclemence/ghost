# Verification — Ghost 0.24.11

2026-09-23 · commit `c46c6af` · dynamic suites against `deepseek/deepseek-flash`

What ran, what passed, what failed, and what was not exercised. Every number below comes from Ghost's own checks; nothing here is asserted from prose.

## Verdict: PASS

All suites that ran passed: verify PASS; benchmark PASS (100.0); golden 59/59 pass.

## Verification (`ghost verify`)

Overall PASS — 48 pass, 0 fail, 1 skip/not-run.

Ran as: `ghost verify (attached /tmp/opencode/verify_02411.json)`

## Benchmark (`ghost eval benchmark`)

Ghost Core Score 100.0 — overall PASS.

| Dimension | Pass rate |
|---|---|
| agent | 100% |
| automation | 100% |
| capability | 100% |
| governance | 100% |
| memory | 100% |
| privacy | 100% |
| responsiveness | 100% |


Ran as: `ghost eval benchmark (attached /tmp/opencode/bench_02411.json)`

## Golden Conversation Suite (`ghost eval golden`)

59/59 conversations pass (suite v1, model `deepseek/deepseek-flash`).

| Category | Pass | Fail | Skipped |
|---|---|---|---|
| ambiguity | 3 | 0 | 0 |
| companion | 4 | 0 | 0 |
| context_isolation | 3 | 0 | 0 |
| contradiction | 3 | 0 | 0 |
| conversation | 6 | 0 | 0 |
| correction | 2 | 0 | 0 |
| cross_user | 2 | 0 | 0 |
| denial | 2 | 0 | 0 |
| goals | 2 | 0 | 0 |
| memory | 5 | 0 | 0 |
| offline | 3 | 0 | 0 |
| permission | 7 | 0 | 0 |
| provider | 2 | 0 | 0 |
| routines | 3 | 0 | 0 |
| tool_failure | 2 | 0 | 0 |
| truthfulness | 10 | 0 | 0 |


Ran as: `ghost eval golden (attached /tmp/opencode/golden_v2.json)`

## What's new

Trust you can read, delight you can feel.

- **Verification pages.** `ghost eval release-notes` assembles
  `docs/VERIFICATION-<version>.md` from Ghost's own checks — verify,
  benchmark, golden results, and limits — committed per release.
- **Tasks surface.** `ghost tasks`, `/tasks`, and `/task` show and manage
  durable routines (pause, resume, cancel) through the same routines
  service the gateway and mobile app use.
- **Ideas with evidence.** `ghost ideas`, `/ideas`, and `/idea` surface
  suggestions that cite their sources, with accept/dismiss receipts.
  `refresh` derives them deterministically; `draft` asks the model and
  verifies every citation, marking failures unverified.

## Limits — what this page does not establish

- Live-vendor checks did not run (no network credentials in the evaluation environment).
- Qwen-class local models are supported but intentionally not run on the reference device; selection reports supported/not-run.
- Results describe the cited commit and model only; a different provider, model, or workspace may behave differently.

## Reproduce

```
ghost verify (attached /tmp/opencode/verify_02411.json)
ghost eval benchmark (attached /tmp/opencode/bench_02411.json)
ghost eval golden (attached /tmp/opencode/golden_v2.json)
```
