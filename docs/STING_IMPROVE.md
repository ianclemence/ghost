# Sting improvement doctrine (minimum-spec hardware)

The floor is this Pi: 4 cores, ~7GB RAM, ~2.5GB free disk on a 29GB
card, no GPU, 69–70°C sustained under load, possibly air-gapped. Some
Ghost users have exactly this. Every improvement loop below is designed
to fit it — what doesn't fit doesn't ship as an on-device loop.

## Measured budgets (09/2026 run, 391 rows, LoRA r16, batch 8, 10 epochs)

| Fact | Value | Consequence |
|------|-------|-------------|
| XLA compile | ~35 min, 4 cores | One-time per shape; keep shapes stable to reuse |
| Step time | ~100 s/step (batch 8 × seqlen 1024) | 440 steps ≈ 12 h; overnight job, never interactive |
| Batch 16 | OOM-killed at 6.8 GB | Batch 8 peaks ~5.5 GB and survives |
| Rendered rows | max 749 tokens after schema trim (was 1113) | Rows over `--max-len` are SILENTLY truncated = corrupted labels; re-measure with the bundled tokenizer after every schema change |
| Sustained temp | ~70°C at 244% load | Safe (<80°C throttle), but watch it on passively cooled units |
| Disk floor | 1 GB free, hard stop | Reclaimed 1.2 GB via `go clean -cache`; apt cache (1 GB) needs root, don't count it |

## Tier system

The split is by **where it runs**, not how strong it is. A personal AI
runs exactly one loop, ever: T0. Training is a lab activity that
produces versioned artifacts; it is never a product behaviour.

- **T0 — ledger (seconds, always on; per-device).** Rollout log +
  `ghost sting learn` (fold evidence into the ledger) +
  `ghost sting calibrate` (inspect thresholds). Zero training, zero
  risk. The **only** loop that runs on every device, including
  air-gapped ones. The ledger is the gate: it decides what auto-acts
  and what escalates.
- **T1 — filtered SFT / RFT (hours, overnight; lab-only).** Roll out
  the current head on seed queries, keep only rollouts with reward +1
  (see below), fine-tune on those with the existing LoRA pipeline. No
  trainer changes. Runs on a build/QA host, **never on a user's
  Ghost**, and produces a candidate T3 artifact.
- **T2 — full LoRA (overnight+; lab-only).** Curated flywheel datasets
  like this run (hundreds of rows, 10+ epochs). Same box, exclusive
  access. Still lab-only.
- **T3 — off-device GPU (releases; the shipping path).** Thousands of
  rows, rank-32, head surgery. Ships as a versioned `.cact` **plus its
  ledger priors**. This is the only training output that reaches a
  Ghost.

Rationale: training on the serving box forces a choice between serving
and improving, and this document already records a run killed at step
~48 by concurrent load. Making T1/T2 lab-only removes that conflict from
the product entirely. Ghost's improvement loop is T0 — it
accumulates rollout evidence that ratchets the ledger and becomes the
training input for the next lab run.

## Why RFT and not PPO-on-Pi (the honest RL analysis)

The reward problem is already solved: executions return evidence, so
every rollout grades itself — reinforcement learning with verifiable
rewards, no human labels. What is NOT solved is the machinery: PPO/GRPO
needs rollout batches plus reference/value machinery inside a JAX stack
built for SFT, driving a grammar-constrained sampler the standard RL
libs cannot touch. On 4 CPU cores the online step count such training
needs translates to days, with the box unusable for serving meanwhile.
So the sequence is RFT now (uses 100% existing infra), DPO-style
preference learning later (needs trainer mods, still offline), full
online RL only on bigger iron — if the data ever says SFT plateaued.
Do not reach for PPO because it sounds stronger; reach for the strongest
method that fits the floor.

## Reward spec (implemented in `pkg/sting/rollout.go`)

Asymmetric by design: on-device safety — a wrong act costs far more than a miss.

| Outcome | Reward | Trains on it? |
|---------|--------|---------------|
| Grounded act, success evidence | +1 | yes |
| Grounded act, world failed (timeout/provider) | +1 | yes — routing was right; punishing it teaches superstition |
| Fabrication (ungrounded/unknown/missing-required) | −1 | never; mine for gate tests |
| Safe abstain (`[]`) | 0 | n/a (coverage measured only with labels, in harness) |

Misses (false abstention) are invisible at runtime by construction —
the harness (golden, adversarial negatives) measures those.

## Anti-regression gates (a tuned router must pass all three)

1. **Generic suites**: Needle's own `smart_home`/`productivity` frozen
   suites — fine-tuning on Ghost tools must not destroy general
   tool-calling (catastrophic forgetting check). Keep ~10% generic rows
   in every training mix.
2. **Ghost golden** with `sting/base` vs tuned: tooling suites only.
3. **Adversarial negatives**: hallucination rate on missing/negated/
   invalid probes must not rise; ledger priors ratchet forward.

## Operating rules (learned the hard way, this run)

- **Training owns the box.** Proven 09/2026: with JAX eating 5.5 GB,
  `TestBuildCandidatesFallbackWhenUnpinned` fails — the hardware-aware
  scheduler correctly sheds the qwen3:1.5b fallback at 1410 MB
  available. Never run serving evals concurrently with training; never
  "fix" a load-induced failure without checking `MemAvailableMB` first.
- **Protect the run from the operator too.** Second data point,
  same day: a long verification session (full `go build`, test
  suites, 43 live embedding calls with Ollama loaded) alongside the
  5.5 GB training job ended with the trainer dead at step ~48/440 —
  no adapter, no checkpoints (the trainer only writes at the end),
  ~3 h of compute lost. During training: light log reads only, no
  builds, no test suites, no model loads. Verification waits for the
  box to be free.
- **Launch detached**: `setsid nohup … &` — tool timeouts kill process
  groups; setsid survives. Logs are block-buffered under nohup: step
  lines arrive in bursts; silence ≠ stuck (verify via CPU + `/proc/io`).
- **Pin the trainer**: v3-only trainers refuse v2 checkpoints — check
  the package major version and pass `--checkpoint` explicitly.
- **Disk floor 1 GB**: check before every run; `go clean -cache` is
  the safe reclaim.
- **Weights versioning**: ledger `weights` tags (`base` vs
  `tuned:<name>`) keep populations from mixing; never compare
  cross-generation scores as if they were one population.
- **Personalization lives in schemas, not weights.** Rooms, contacts,
  routine names are per-deployment Literals. One shipped `.cact`
  serves every Ghost; the user's world arrives at the schema
  layer. Multi-turn slot-filling stays in Ghost sessions — the router
  stays stateless, refusal stays clean, the loop turns refusal into a
  question.
- **Actuation data invariant**: the router must never learn to
  self-authorize. Train actuation shapes only with broker-governed
  execution traces; escalation stays the ask mechanism.
