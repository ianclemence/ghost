# DeepSeek-V4.1-Flash Tech Report — Cross-Check vs Ghost

Reference: *DeepSeek-V4.1-Flash: Pushing the Limits of KV Cache Compression*
(DeepSeek-AI). PDF: `https://huggingface.co/deepseek-ai/DeepSeek-V4.1-Flash/
resolve/main/DeepSeek_V41_Tech_Report.pdf`. Reviewed 2026-09-10 against the
Ghost Intelligence Runtime brief's "DeepSeek concept → Ghost translation".

This is a systems cross-check, not a plan to reproduce the model. Per brief
§129 we do **not** implement CED/CSA2/MoE/FP4/Engram/DSpark as model features.

## 1. Model facts (for context only)

- DeepSeek-V4.1-Flash, multimodal MoE, 552B backbone + 196B Engram; activates
  **8B/token prefill, 16B/token decode**; 1M-token context; 45T multimodal tokens.
- Global KV **890 bytes/token** (~¼ of V4-Flash, ~437× vs V1); persistent KV
  (SSD/host) ~⅛. Decode FLOPs grow only ~¼ across 4K→1M context.
- These are architecture/precision properties; **not portable targets** for an
  external-API runtime like Ghost.

## 2. Core mechanisms and the transferable lesson

- **CED (Causal Encoder-Decoder):** decoder global KV is projected from the
  encoder's final hidden state; only the first half of layers runs at prefill.
  Lesson: asymmetry (full decode, half prefill) via projection, not recompute.
- **CSA2:** compress KV along entry/sequence/layer; decouple *what you cache*
  from *what you select*; hierarchical sparse indexer builds a coarse candidate
  pool (16,384) that deeper layers score. Lesson: narrow candidates once, then
  retrieve cheaply downstream.
- **FP4 KV cache:** QAT, one E4M3 scale per 16 channels, dequant before
  attention. Lesson: cache precision is a deployment axis; portability via
  dequant.
- **SWA Bounded Replay:** replay only the last window and **accept approximate**
  reconstruction; SWA KV moves to a short-TTL DRAM pool, global KV keeps ≥72h.
  Lesson: "storage vs bounded recompute" is legitimate — but the result is
  explicitly not exact.

## 3. Reasoning effort (report §5.1.4, App. C)

- Scalar effort `b∈{1..100}` prepended to the system prompt; API tiers
  `low=50 / high=75 / max=100`; **exponential, capped** length penalty.
- Effort 25→100: avg Pass@1 67.1→76.3, DeepSWE 66.0→74.2, Terminal-Bench
  82.4→90.6, at ~2.5× tokens. **60–80 recovers most accuracy at <½ the budget**;
  100 lengthens trajectories 1.6–1.8× for marginal gain.
- **Accuracy is not monotone in effort across scaffolds** (plateaus/dips at
  intermediate settings).

## 4. Agent/scaffold co-design (report §5.1, §5.3.4, App. B.1)

- Task = **(problem, environment, verification system)**, scored on difficulty +
  correctness. Post-training gains attributed to **data/environment scale**, not
  algorithmic novelty.
- 8 scaffolds, same checkpoint/tasks: DeepSWE spreads **65.5 (OpenCode) → 74.2
  (mini-SWE)**; Terminal-Bench **84.1 (Codex) → 90.6 (DSH Minimal)**.
- Conclusion: **scaffold choice matters at least as much as effort tier**; they
  commit to model–harness co-design.

## 5. Cross-check table

| DeepSeek concept | Ghost analogue | Verdict |
|---|---|---|
| Global vs SWA KV pools (media/TTL) | HOT/WARM/DURABLE/RECONSTRUCTABLE tiers | **FAITHFUL in spirit**; DeepSeek has ~3 pools + fixed TTL/LRU, no promotion — Ghost must define its own. |
| Bounded replay (approximate) | Reconstructable task/context state | **FAITHFUL only if context is approximate.** Treating reconstructed *task* state as exact would be a misreading. |
| CSA2 KV/index reuse (cross-layer) | Shared/reusable context + retrieval state | **FAITHFUL** as reuse; **OVERSTATED** if read as cross-session semantic sharing. |
| Hierarchical sparse indexer | Hierarchical retrieval / candidate generation | **FAITHFUL** — strong structural match (digest → RAG over-fetch → scope filter → top-N). |
| CED | Context checkpointing / delta-aware compilation | **OVERSTATED.** CED is layer projection, not delta-based recomputation. Do not cite it for incremental compilation. |
| Reasoning effort | Quick/Normal/Deep + adaptive budgets | **FAITHFUL**; but DeepSeek conditions on explicit request, and accuracy is non-monotone across harnesses. |
| DSpark | Cheap predict → expensive verify | **FAITHFUL as pattern**; DSpark verifies with the same model, not an independent critic. |
| Engram | External structured memory | **OVERSTATED / leaning misreading.** Engram is parametric hashed memory; the lesson is decouple storage from compute + prefetch. |
| task = problem+environment+verifier | Task + environment + verifier | **FAITHFUL** — near-exact. |
| Trajectory/replay infra | Trajectories + replay | **FAITHFUL**; note DeepSeek "replay" = rollout/routing resumption, Ghost cevents replay is read-only and never re-executes. |
| Sandboxed agent environments | Capability/security boundaries | **FAITHFUL**; DeepSeek uses OS/VM isolation, Ghost uses a policy broker. |
| Scaffold co-design | Agent Protocol / model×harness benchmarking | **FAITHFUL and strongly supported** — direct evidence for the position. |

## 6. Warnings for implementers

1. **"Reconstructable" ≠ exact.** SWA bounded replay is deliberately
   approximate. Context may be approximate; **task/verification state must stay
   exact** via its own state machine.
2. **CED is not delta-aware compilation.** It always runs the full encoder half.
3. **CSA2 reuse is cross-layer, not cross-session.** Session reuse is the
   persistent-KV mechanism (LRU + TTL), a separate thing.
4. **Engram is not a knowledge base.** Don't justify external memory by it.
5. **DSpark is not a verifier.** Same-model speculative acceptance.
6. **Effort is not monotone in accuracy**, and scaffold can dominate effort.
   Validate Quick/Normal/Deep per harness; don't assume "Deep ⇒ better".
7. **The tier model is under-specified** (two pools + fixed TTL). Promotion/
   demotion/contradiction is Ghost's own design work (Phase 2).
8. **Eval infra is hackable** (agents found answer leaks, deleted binaries).
   Ghost verifiers/golden must assume hackability and strip solution leakage.

## 7. Ideas the brief missed (worth adopting)

1. **Tune under the deployed approximation** — evaluate the context compiler /
   retrieval at the real budget, not ideal full context.
2. **Exponential, capped, continuous budget knob** rather than discrete tiers.
3. **Decoupled long-lived sandbox vs schedulable worker**, full state offload on
   preemption — maps directly onto Ghost durable tasks/routines.
4. **Derived-latency reward** (critical path from token counts + measured tool
   time) for multi-step cost accounting.
5. **Latency-sensitive execution class** so background work can't distort
   interactive turns.
6. **Crash = failed trajectory + "repercussion" signal** as a first-class
   negative signal.
7. **Per-type quotas** (DeepSeek balances MoE load per modality) — analogue:
   separate accounting/quotas per memory/context type.

## 8. Model-name update (official DeepSeek API docs, 2026-09)

Per `https://api-docs.deepseek.com/quick_start/pricing`:

- Current model: **`deepseek-flash`** (DeepSeek-V4.1-Flash), multimodal,
  1M context, thinking by default.
- Legacy `deepseek-v4-flash` and `deepseek-v4-flash-vision-exp` are retired
  aliases (requests still served by V4.1-Flash). `deepseek-v4-pro` is being
  retired and routed to V4.1-Flash.
- Ghost now uses `deepseek-flash` as the DeepSeek model, and because it is
  multimodal, `visionModelFor` no longer remaps it (non-vision DeepSeek models
  route to `deepseek-flash`).
