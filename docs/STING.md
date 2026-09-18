# Sting offline tool-router

Sting is Ghost's own **offline intent router** — never a brain, never
an authority. It began as a fork-compatible layer over
[Needle](https://github.com/cactus-compute/needle) (Cactus Compute,
Apache-2.0; see `THIRD-PARTY-NOTICES.md`) and is being grown into an
independent implementation under Ghost's MIT license.

## What it is

A 14MB tool-calling model (~28MB RAM, 256-token window) that turns
natural language into structured tool calls: text in, JSON out. No
chat, no prose, no memory. Off-topic input returns no call.

## Where it sits

```
user text → Sting sidecar (127.0.0.1:11436) → proposed calls
  → confidence gate + strict grounding validation
  → Permission Broker (allow/ask/deny, unchanged)
  → Capability execution → runtime evidence → events → answer
```

Two integration points:

1. **Fast-path** (`pkg/agent/sting_fastpath.go`): read-only tools only
   (`web_search`, `weather_now`, `memory_recall`, …), zero LLM calls,
   runs after deterministic dispatch. Anything unroutable falls
   through to the normal loop. Actuation NEVER fast-paths.
2. **Provider** (`pkg/providers/sting_provider.go`): `sting/base` as a
   first-class `LLMProvider`, usable in the fallback chain and golden
   runs. Inside the normal loop every call still passes
   `AuthorizeTool`, so governed actuation works there.

The model proposes. Ghost decides, executes, verifies, records.

## Why read-only for the fast-path

Measured on the Pi (`smart_home` 32 cases: 25–26/32, 3 critical fails):
the base router hallucinates missing slots (`turn on the lights` → bedroom),
duplicates parallel calls, and drops valid ones. DeepSeek Flash scored
6/6 on the same probe. So the fast-path answers offline info questions
and escalates everything else — the failure mode is "ask the bigger
model", never "run the wrong tool".

## Install (Pi)

```bash
sudo make install-sting
```

This creates `/var/ghost/sting-venv`, prefetches the engine + base
weights once (inference itself never touches the network), installs
`ghost-sting.service` (loopback only, `NEEDLE_TELEMETRY=0`), and
starts it on `127.0.0.1:11436`.

Enable routing:

```bash
GHOST_STING_ENABLED=true ghost serve
```

or set `"sting": {"enabled": true}` in `config.json`.

## Configuration

| Key | Env | Default | Meaning |
|-----|-----|---------|---------|
| `enabled` | `GHOST_STING_ENABLED` | `false` | master switch |
| `sidecar_url` | `GHOST_STING_SIDECAR_URL` | `http://127.0.0.1:11436` | loopback only |
| `confidence_threshold` | `GHOST_STING_CONFIDENCE_THRESHOLD` | `0.5` | act at/above, escalate below |
| `timeout_secs` | `GHOST_STING_TIMEOUT_SECS` | `15` | per-turn sidecar budget |
| `weights` | `GHOST_STING_WEIGHTS` | `""` | tuned `.cact` path on the sidecar host |
| `rollout_log` | `GHOST_STING_ROLLOUT_LOG` | `<workspace>/state/sting-rollouts.jsonl` | rollout-evidence log (`off` disables) |
| `ledger` | `GHOST_STING_LEDGER` | `<workspace>/state/sting-ledger.json` | reliability ledger **that gates routing**; missing file falls back to shipped priors (`off` = confidence-only) |

Pre-fork `GHOST_NEEDLE_*` envs are still honoured when the `GHOST_STING_*`
equivalent is unset.

No API keys: local engines need none. `HasCredential` is always true;
`PresetAvailable` treats sting as local.

## The ledger is the gate

Ghost does not trust the engine's confidence score blindly, and it does
not trust tuned weights at all until they have earned it. `pkg/sting`
keeps an empirical reliability ledger — measured precision per routing
scope — and `GateWithLedger` consults it on **every** turn. The ledger is
not a report; it is the named gate.

Serving scopes are the shape of the validated call set
(`positive-single`, `parallel`), so live traffic and the harness measure
the same thing. The gate decides:

- **Scored turn (base weights).** The ledger sets the threshold. Thin
  data (<10 samples) raises the bar by 0.2; a scope at or above 0.90
  precision uses the configured base; below it the threshold is 1.01 —
  auto-act off until measured data improves.
- **Unscored turn (tuned weights).** Fine-tuning freezes the confidence
  head, so there is no score to compare. The turn acts **only** for a
  scope the ledger has measured at or above target; every other scope
  escalates to the normal provider path. This is what makes a tuned
  `.cact` shippable without a tuned head.
- Anything unexpected escalates. The failure mode is "ask the bigger
  model", never "run the wrong tool".

```bash
ghost sting calibrate
# ledger: priors
# scope               n precision  threshold  verdict
# missing-slot        4      0.25       0.70  thin-data
# positive-single    19      0.84       1.01  below-target:0.84
# suite:productivity 32      0.91       0.50  calibrated
```

Priors ship from our Pi spike on the home-control surface and are
labeled as such. T0 ratchets them from real traffic:

```bash
ghost sting learn       # priors + rollout log -> workspace ledger
ghost sting calibrate   # show the resulting thresholds
```

`learn` rebuilds from priors plus the whole (rotating) rollout log each
run, so it is idempotent — running it twice does not double-count. Note
that rollout precision is an upper bound: false abstentions are
invisible in the log by construction, which is why the harness measures
those. `ledger: "off"` restores the old confidence-only gate for
comparison; it is not the shipping default.

Consequence worth stating plainly: with the current priors,
`positive-single` measures 0.84, below the 0.90 auto-act bar, so the
generative path escalates single-call turns and defers to the normal
provider. That is the doctrine working — the embedding selector
(`selection_acc` 0.971) and the T3 tuned artifact plus ledger priors are
the path that earns auto-act back.

## Dataset flywheel (lab pipeline — not an on-device loop)

Ghost's labeling moat: the teacher proposes, Ghost disposes. Labels are
grounded by execution evidence and strict validation — a label is kept
only if every argument is evidenced by the query text.

**This pipeline is lab-only (T1/T2).** Training never runs as a product
behaviour on a user's Ghost: it runs T0 (ledger + rollout
log) and nothing else. It consumes versioned T3 artifacts — a `.cact`
plus its ledger priors — and never a `needle finetune` job. The commands
below are for a build/QA host with the box to itself; see
`STING_IMPROVE.md` for the tier rules and why.

```bash
ghost sting dump-tools > tools.json   # real registry schemas, bounded to 5
STING_TEACHER_KEY=... STING_TEACHER_MODEL=deepseek-chat \
  python3 sting-sidecar/flywheel/synthesize.py \
    --tools tools.json --out sting_data.jsonl
# kept=12 dropped=0  (off-topic/negated/missing correctly emit [])
needle finetune sting_data.jsonl --epochs 10 --out adapter.safetensors
needle build --lora adapter.safetensors --out sting_ghost.cact
```

Validator self-test (no teacher, no network):

```bash
python3 sting-sidecar/flywheel/synthesize.py --self-test  # 5/5
```

Scale path on this device class (Pi 5, 4 cores, 7GB, no GPU): the JAX
LoRA pipeline runs on CPU. Measured on-device lessons (09/2026 run,
391 rows, batch 8, 10 epochs, 440 steps):
- Batch 16 OOMs the 7GB box at 1024 seqlen (XLA compile peaked 6.8GB).
  Batch 8 compiles at ~5.5GB and survives — watch `dmesg` for oom-kill.
- XLA compile takes ~35 min on 4×A76; training then runs ~100s/step
  (~12h for 440 steps). Loss 1.03 → 0.73 over the first 16 steps:
  judge by trend, not level (starts near 1.0 per upstream docs).
- Rendered rows MUST fit `--max-len` (default 1024): longer rows are
  silently truncated, corrupting labels. Our first 391-row set hit
  1113 tokens max (260 rows over) because registry descriptions are
  verbose — fixed by tightening the five router schemas to one line
  each (also faster prefill at runtime), re-measuring max 749, zero
  over. Always re-measure with the bundled tokenizer after schema
  changes.
- Needle-3 artifacts are currently unreachable (gated repo, 401), so
  train the Needle-2 line: `pip install 'cactus-needle[train]<3'` and
  pass `--checkpoint` explicitly (the v3-only trainer refuses v2
  checkpoints — check `needle` major version first).
Run it detached (`setsid nohup … &`) and poll the log: stdout is
block-buffered under nohup, so step lines arrive in bursts. GPU /
Apple-silicon machines use the same commands with the `gpu`/`metal`
extras. Tuned archives stay `uncalibrated` at the engine layer by
upstream design — the ledger above is what makes them shippable.

## Tuned weights

Fine-tuning does NOT update the confidence head, so tuned `.cact`
agents report `confidence: null`. The ledger is what makes them
shippable: an unscored turn acts only for a scope the ledger has
measured at or above target, and is marked `uncalibrated` so callers
prefer read-only execution and log the fact. Validated calls still reach
the Broker (which governs as usual). See the upstream `doc/finetuning.md`
for dataset sizing; export with `needle build` and point `weights` at
the archive. Training our own calibrated head is the tracked T3 step
after the ledger + flywheel prove out at scale.

## Selection plane (from Jev, without Jev)

Closed-set decisions don't need generation. Sting has two deterministic
selection mechanisms around the generative call:

**Triage** (`pkg/sting/triage.go`): {act, ask, refuse} before any
inference. Negation → refuse, missing required input → ask (the
readiness layer asks the precise question), everything else → act.
Deliberately biased to Act: off-topic refusal belongs to the engine
(empty call), and false-act costs one cheap gated call while
false-refuse would push the turn to a bigger model. Ask/Refuse return
the turn to the loop — triage can only save a wasted round trip, never
drop a turn.

**Embedding pre-selection** (`pkg/sting/select.go`): embed the query
with the on-device nomic model, cosine-match tool descriptions,
rank top-k. Zero training, fully offline, no new artifacts:

```bash
ghost sting select "check the weather in berlin right now"
# query_embed_ms: ~600 (warm; ~5s cold model load)
#   weather_now      0.6743
#   web_search       0.5443 ...
```

Measured on heldout (43 rows): **selection accuracy 0.971** at
~0.6s/query warm. The 8 negative rows always pick a tool — abstention
stays with the generative gate, reported honestly. The hybrid this
proves: embed-select (fast) → generate-args (only when selected) →
gate → Broker. That path is what voice latency needs (~0.6s vs ~1s
autoregressive turns; keep the embedder warm via Ollama keep-alive).

Selector benchmark arm lives in the harness:

```bash
python3 sting-sidecar/flywheel/eval.py --heldout heldout.jsonl --selector embed
```

Same heldout, three future columns: generative-tuned vs
embedding-preselected vs external selectors. Decisions by measurement.

## Golden

```bash
ghost golden --model sting/base --suite tooling
```

`sting/base` is a supported target with no credential requirement. It
is expected to lose conversational/memory suites by design (no-call is
the correct answer for non-tool turns) — compare tooling suites only.

## Files

- `pkg/sting/` — client, gate, tool-subset export, calibration ledger,
  triage, embedding selector, rollout log (stdlib only)
- `pkg/providers/sting_provider.go` — `LLMProvider` adapter
- `pkg/agent/sting_fastpath.go` — read-only fast-path + loop hook
- `sting-sidecar/` — daemon (`sting_sidecar.py`, stdlib HTTP)
- `sting-sidecar/flywheel/synthesize.py` — teacher→validated JSONL dataset flywheel
- `sting-sidecar/flywheel/grow.py` — deterministic + paraphrase dataset growth
- `sting-sidecar/flywheel/eval.py` — generative + selector benchmark arms
- `sting-sidecar/vendor/` — pinned tokenizer (independence row 2)
- `ghost-sting.service.template` — systemd unit (loopback, hardened)
- `cmd/ghost/sting.go` — `ghost sting status|dump-tools|calibrate|learn|select`
