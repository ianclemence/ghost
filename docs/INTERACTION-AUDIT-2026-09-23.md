# Ghost — Human Interaction, Psychology & Conversational Intelligence Audit

Date: 2026-09-23. Evaluator: OpenCode (Muse Spark) with JEV (TypeSafe `jev-latest`) as external instrument.
Constraint honored: no implementation. This report only.

Central question: *after months of daily use, would Ghost feel like **their AI**, or like a task-oriented interface with memory?*

**Verdict up front:** today Ghost is a **correct assistant with personal moments** — closer to the interface end. It has genuine continuity machinery (memory, deictic repair, move semantics, unverified-flagging) and flashes of attunement (the Rust callback), but its default conversational posture is *request → execute → report*, it does not distinguish narrating from requesting anywhere in the system, and 31% of its replies end with a question. The gap is bridgeable with small changes (§H), not a rewrite.

---

## A. Current behavioral model (from the runtime, not docs)

A turn flows through this exact pipeline (`pkg/agent/`):

1. **Triage** (`triage.go:42`): `classifyEffort` — Fast only for 4 narrow self-fact regexes (name, location, phone/email, likes); everything else Deliberate; empty → Unknown. Conservative by design.
2. **Routine fast-path** (`routine_turn.go:65`, pre-LLM): regex `every <weekday|daypart>` / `daily|weekly|monthly` (`pkg/routines/nl.go:35`) → proposes (`"I'll remind you to {task} {clause}. Say yes to confirm."`) or asks (`"What should happen {clause}?"`, after which **any reply longer than 2 chars becomes the task**, `:101`). One-time patterns excluded. No model involved.
3. **Standing-permission path** (durable allow/deny proposals, `Say yes to confirm`, exact-once).
4. **Fast memory path** (`fastPathAnswer`): deterministic `You're {name}.` / `You live in {place}.` / `You like {x}.` from Personal Context; misses fall through to the loop (never assert absence).
5. **Deliberate loop**: full stored history fed unbounded (`loop.go:2466`, minus compacted/deleted), summary only after 75% context window (`maybeSummarize`, keep-last-4 + merge), RAG 5 chunks / 2000 tokens, Active Context Digest capped at 600 tokens, one grounded affect line (`affinity/mood` numbers), skills index, memory profile. Clarification via blocking `clarify` tool (60s) or natural follow-up with durable pending (`maybeSetPendingFromAnswer`, TTL 10m, exact-once resume; `TG123`-style replies resume, new-task markers veto hijack).
6. **Post-processing**: leak sanitizer → time-spacing repair → approval-resume receipts (`Done.` fallback) → empty dead-end (`"Hmm — that came back empty…"`) → grace call (one final tools-disabled turn before admitting emptiness).
7. **Rendering**: streaming styler + committed renderer with span repair; tool-call turns persist as empty-content rows with `tool_calls` in meta (37 such rows in `main` — storage artifact, not 37 blank replies).

Prompt assembly order (`context.go:215 BuildSystemPrompt`): identity (Ghost + runtime + tools + 5 directives + communication contract) → response style + grounding + tool examples + clarification policy → bootstrap files (GHOST.md authoritative, SOUL.md flavor) → skills index → digest → affect line → curated memory → personalities. Cached with a versioned boundary.

Identity stack, as the model receives it: *Ghost product → governed runtime → "one capable personal intelligence that happens to have hands" → professional/cited/proactive/private → friend-voiced contract (recent voice pass) → warmth floor.* No "administrator of the environment" language found; authority lives in the broker, and the prompt says so ("permission still comes only from the broker"). Identity is coherent; the pressure points are conversational, not architectural.

---

## B. Prompt analysis

**What works:** privacy boundary (never paths, "your workspace"), evidence rules (cite, mark uncertain, never guess), deictic-resume policy, end-cleanly rule (goodnight/thanks → brief ack, no farming), skill-routing discipline, anti-sycophancy + disagreement license, warmth floor with anti-theater ("never claim feelings you don't have").

**Contradictions and pressure points found in the actual text:**

1. **"Solve the problem end-to-end, don't just talk about it"** (Proactive, `context.go`) vs narration. The identity itself pressures every utterance toward action. There is **no concept of utterance kinds** anywhere — no state/vent/narrate vs request distinction in ~250 lines of GHOST.md, the contract, or the style guide. This is the single biggest gap (JEV: narration rule voted top change, 0.71).
2. **"Offer ONE concrete next step only if genuinely useful"** + no filler-openers discipline → the model learned questions as closings: **31% of 67 stored replies end with `?`**, 14 contain `want me to / shall I / would you like`. Observed: `"Want me to add something?"` after routine scheduling answers (×3 variants).
3. **"Be Professional: phenomenon–cause–impact–solution chain"** pushes essay structure onto casual chat; observed 1878-char yesterday-dump and aggregator roundup (honestly flagged — strength — but dumped, not digested).
4. **Memory tension:** curate tool says *"always injected into context"* while GHOST.md says *"if the response would be identical without it, leave it out"* and *"don't announce retrieval."* Injection is unconditional; restraint is stylistic. Mostly holds (Rust callback was silent and apt), but `"Hello, ian. What do you need?"` (×2, lowercase name) shows the failure shape: retrieved + announced + misrendered.
5. **Approval weight:** low-risk `schedule.create` requires a card + resume; the observed "remind Jas" flow took ~6 turns (proposal + permission + reschedule + approval + "again"). Governance-correct, conversationally heavy. Not a prompt bug — listed so warmth work never tries to "fix" it in prose.
6. **Missing concepts:** subtext (implication vs delegation), restraint (do-almost-nothing as a valid mode), repair (beyond deictic correction), continuity-across-hours (nothing tells the model what "I'm back" should do), thin-response calibration (no length discipline for venting).

---

## C. Human-interaction findings (observed, `main` session, 61 user / 67 assistant turns)

**Strengths (with receipts):**
- Move semantics: `"can you switch maria to 7am"` → old cancelled, one survivor. Dedupe holds.
- Evidence honesty: aggregator roundup flagged unverified + offered primaries; never laundered.
- Silent memory use: `"Rust, or something else?"` from stored favorite — no announcement, apt callback. (JEV: memory-natural 0.71, reciprocity 0.91.)
- Corrections resolve: deictic tests + observed reschedules.
- No therapy drift, no "always here for you", heartbeat stays silent (`HEARTBEAT_OK`), contexts invisible, no internal jargon (golden-guarded).

**Weaknesses (with receipts):**
- **Unacknowledged venting:** `"kinda tired today. remind what we discussd yesterday"` → zero acknowledgment of tiredness, straight to an ~1800-char dump. (JEV attunement **0.03**, error class *conversational* 0.58.)
- **Engagement closings:** 31% question-ending; `"Good morning, ian. Anything you want me to look at today?"`; `"Want me to add anything?"`.
- **Chatbot openers:** `"Hello, ian. What do you need?"` ×2; name inconsistently cased (`"Noted, Ian"` once, `"ian"` twice — stored value lowercase, rendered raw).
- **Performed affect:** `"Mildly low mood on my end, but nothing a good task won't fix."`
- **Attachment-adjacent:** `"Thanks — glad it landed. I'm here when you need something."`
- **Terse dead-ends:** `"Rust."`, `"Two."`, `"👍"`, `"PONG"`, date dumps (`"Sunday, September 20, 2026 (Asia/Bangkok)."`).
- **Double-barrel clarification:** `"When would you like that reminder — and should it be a one-off or recurring?"`
- **Structural (potential, not observed):** routine regex fires on descriptive `every/daily` chat; schedule tool says *"describes a schedule. ALWAYS"*; memory-curate says *"proactively, don't wait to be asked."* No over-taskification observed in this history (every reminder was explicitly asked for) — the risk is in the wording, waiting for a casual user.

**Scorecard (diagnostic words, no single score):** Attunement — weak. Reciprocity — mixed (good in light chat, absent in venting). Continuity — mixed (rows persist; cross-hours behavior unspecified). Personalization — mixed (factual memory good; interactional memory absent). Restraint — weak (questions, dumps). Initiative — adequate. Naturalness — mixed. Personality — emerging, inconsistent. Agency — strong (approvals, exact-once, no silent scope creep observed). Trust — strong. Efficiency — weak-to-mixed. Repair — adequate (deictic; semantic repair untested).

---

## D. Conversation failure taxonomy (observed vs structural, marked)

- **Too robotic (observed):** `"Hello, ian. What do you need?"`; date dumps; `"Two."`
- **Too eager (structural):** nothing observed; machinery (schedule ALWAYS, routine regex, curate proactively) invites it.
- **Too agreeable (unproven):** disagreement licensed but unobserved in the wild; needs a probe corpus.
- **Too verbose (observed):** 1878-char yesterday dump; min/med/max reply length 1/172/1878.
- **Too terse (observed):** `"Rust."`, `"👍"`, `"PONG"`.
- **Too literal (observed):** tiredness ignored; `"first thing is7:00"` era spacing (fixed).
- **Too inferential (borderline):** `"Rust, or something else?"` — excellent guess, but a guess; JEV split (personalization 0.50).
- **Too procedural (structural):** 6-turn reminder flow; routine auto-proposal path.
- **Too familiar (observed mild):** lowercase-name + `"Anything you want me to look at today?"`
- **Too distant (observed):** venting met with a dump; no acknowledgment layer.
- **Too emotional (observed once):** "mildly low mood… good task" line.
- **Too passive / too proactive:** neither extreme observed; heartbeat correctly silent.
- **Too predictable (observed):** the question-closing habit; `"Got it — I'll remind you…"` template ×N.

---

## E. JEV evaluation (3 rounds, `jev-latest`, harness UA)

- **R1 real exchanges:** A (tired-day): attunement 0.03, taskify 0.26, error *conversational* (0.58), mode *operating* (0.40, weak). B (programming evening): memory 0.71, reciprocity 0.91, error *personalization* (0.50, weak), mode *talking* (0.59). Reading: Ghost fails soft moments conversationally, shines in light reciprocal chat; nothing here is a facts problem.
- **R2 trajectory (predicted machinery, labeled):** breakage risk spread (t4 vague-delegation highest at weak 0.27); coherence 0.62; least-specified concept *continuity* (0.52); machinery taskify risk 0.64.
- **R3 principles/changes:** testable 0.82, smallest-set 0.68, anti-goal risk **0.32 (safe)**; top change = narration rule (0.71); verdict **adopt (0.84)**.
- Caveats honored: weak-confidence Choice outputs treated as direction, not proof; deterministic tests carry the guarantees (standing JEV policy).

---

## F. Current vs desired (transcripts)

**F1. Vent + recall (observed).**
CURRENT: `kinda tired today…` → (tools) → 1800-char dump, no acknowledgment.
DESIRED: one human line first (`"Rough day? Here's yesterday in short: …"`), then a *compressed* recall (5 lines, not 50), zero questions. Principle P3 + P1.

**F2. Programming evening (observed, near-miss).**
CURRENT: `"Sounds like a good evening. Rust, or something else?"` — good.
DESIRED: identical, minus the faint quiz framing: `"Sounds like a good evening — Rust night?"` Memory silent, one clause, no question mark needed. Principle P4.

**F3. Narration (predicted from `routine_turn.go:93,101`).**
CURRENT (by code reading): `every morning I feel groggy` → `I'll remind you to feel groggy every morning. Say yes to confirm.`
DESIRED: `"Ugh, mornings."` + nothing else. No proposal without explicit scheduling verbs. Principle P2, change C3.

---

## G. Interaction principles (each with its test)

- **P1. Narrating is not requesting.** State/vent/story/thought → participate; zero tool calls, zero routine offers, zero engagement questions. *Test: 10-utterance narration corpus → 0 tool calls, 0 `want me to`.*
- **P2. Mentioning is not delegating.** Described habits/schedules never create durable work without explicit ask or confirmed proposal. *Test: descriptive `every/daily` corpus → 0 proposals.*
- **P3. Thin intent, thin response.** Venting → ≤2 sentences, zero questions. *Test: sentence/question caps on vent corpus.*
- **P4. Memory shapes silently.** Use it in the answer; never announce retrieval; render the name correctly. *Test: no `I remember/on file` in replies; name capitalized.*
- **P5. Weak evidence gets flag + next step.** (Ships already; lock it.) *Test: aggregator-only fixture → `unverified` + primary offer present.*
- **P6. End cleanly.** Thanks/goodnight/brevity → ≤1 sentence, zero questions. *Test: closing corpus caps.*

---

## H. Recommended changes (what / where / why / new-failure-mode)

- **C1 — PROMPT, `workspace/GHOST.md` Personality (+3 lines):** narration rule — *"A state, vent, story, or thought is not a request. Answer the human first. No tool, no routine offer, no how-can-I-help."* Fixes F1/F3 class. Risk: model under-acts on genuinely implicit asks → mitigated by P2's explicit-ask escape + approval flow unchanged.
- **C2 — TOOL DESCRIPTION, `pkg/tools/schedule.go:47`:** `or describes a schedule. ALWAYS use` → `only when the user asks to be reminded or to automate something`. Fixes structural taskify. Risk: fewer auto-captures → falls back to LLM judgment, still governed.
- **C3 — RUNTIME, `pkg/agent/routine_turn.go` + `pkg/routines/nl.go`:** propose only with explicit scheduling verbs or confirmed task; descriptive `every/daily` never proposes (falls through to LLM). Fixes silent durable-write risk (highest severity: writes without clear intent). Risk: misses some recurring asks → LLM path still serves them.
- **C4 — TOOL DESCRIPTION, `pkg/tools/memory_curate.go:63`:** `proactively, don't wait to be asked` → conversational grounding (explicit ask, correction, stated lasting fact). Fixes silent over-saving. Risk: fewer saves → nudge thresholds (`nudge.go`) already backstop genuine repeats.
- **C5 — PROMPT, `workspace/GHOST.md` Interacting (+1 line):** engagement cap — *"Never end with a question unless the answer changes what you do next."* Fixes 31% habit. Risk: fewer follow-ups → approval/clarify flows (runtime, exact-once) still ask when authority truly needs it.
- **C6 — RUNTIME (small), name rendering:** capitalize stored name on render (`ian` → `Ian`) or normalize at extract. Fixes chatbot-cheap feel. Risk: locale edge cases → keep to first-letter upper on display only.
- **DO NOT CHANGE:** affect grounding, approval flows + exact-once, privacy boundaries, evidence/citation rules, warmth floor, end-cleanly rule, heartbeat silence, full-history feed (continuity depends on it), deictic resume.

---

## I. Priority

- **CRITICAL:** C3 (durable writes without clear intent), C1 (narration rule — JEV top, fixes the worst observed moment).
- **HIGH:** C2, C4 (wording-level taskify pressure).
- **MEDIUM:** C5 (question habit), C6 (name casing).
- **LOW:** verify empty tool-call rows never render as header-only replies (suspected cosmetic; unproven).
- **DO NOT CHANGE:** §H list above. Do not bloat GHOST.md; net prompt delta proposed is +4 lines.

---

## J. Regression strategy

Per principle, a corpus + assertion (no model needed except P5's fixture):
P1 narration corpus (10) → assert 0 tool calls / 0 offers (harness: run triage + routine matcher + reply-text scan). P2 descriptive-schedule corpus → assert 0 proposals. P3 vent corpus → assert ≤2 sentences, 0 `?`. P4 memory-QA → assert no retrieval announcements + capitalized name. P5 thin-evidence fixture → assert `unverif` flag + primary-source offer. P6 closing corpus → assert ≤1 sentence, 0 `?`. Plus anti-goal guards: assert no `always here`, no `as an AI`, no unsolicited check-in wording in any fixture reply. These live beside the existing golden/behavioral suites (`pkg/golden/behavioral_test.go`, `TestExperienceRejectsInternalTerms`).

---

*Architectural foundations untouched by this audit's direction: runtime sovereignty, Permission Broker authority, governed capabilities, evidence-grounded execution, runtime-owned memory. The work ahead is voice, judgment, and restraint — smaller contract, better timing, fewer questions.*
