# Personal Intelligence

The scaling era — bigger models, larger datasets, more centralized compute —
is not Ghost's strategy. Ghost's advantage compounds per individual: unique
data, persistent context, and intelligence that lives as close to the person
as technically and economically possible.

The loop Ghost is built around:

More time with Ghost → more unique personal context → better personalization
→ better decisions → more useful actions → deeper integration into daily
life → more time with Ghost.

## The data advantage is per person, not per population

Every Ghost is embedded in a different environment and exposed to a different
life: one person's conversations, preferences, routines, decisions,
corrections, workflows, devices, and recurring behavior. That experience
cannot be reproduced by a generic cloud model, and it is the asset Ghost
compounds: month one it knows little; by year two it holds a history of
interactions, decisions, corrections, and workflows no fresh assistant can
match. Two Ghosts diverge because two lives diverge.

The store of that advantage is `pkg/personalcontext` (beliefs with
provenance: owner-stated beats Ghost-inferred), the attention layer's
learned per-source quieting (`pkg/attention`), and the durable record
(sessions, routines, artifacts, canonical events). It belongs to the user:
it lives on their machine, and it must remain exportable, portable, and
recoverable — valuable because the user owns an increasingly capable
system, not because a company owns a profile of them.

## Intelligence lives in layers

Ghost reasons in layers, escalating only when the task earns it:

```
You → Local Reflex → Local Brain → Cloud only when necessary
```

- **Local Reflex** — fast, cheap, continuous: deterministic gates (the
  noticer, attention decisions), routing, embeddings, triggers. No model
  call is spent where a rule decides.
- **Local Brain** — memory, context, tools, routines, proactive reasoning,
  everyday tasks. The relationship lives here, on the owner's hardware,
  working offline where the runtime permits.
- **Cloud Brain** — heavy reasoning, difficult tasks, specialized
  capabilities. Used when it creates meaningful value, never by default.

The per-computation question is "why should this leave the user's device?",
not "how do we send more of the user's life to the cloud?" Local embeddings
are preferred over provider embeddings precisely so memory learning never
depends on a remote service. The model is a replaceable reasoning
participant behind a fallback chain; the personal context layer is the
durable intelligence, and it may ultimately matter more than which model
serves a given turn.

## Learning, honestly marked

Personal intelligence compounds along a spectrum:

memory → retrieval → preference learning → behavioral adaptation →
structured user model → local fine-tuning → continual learning

Ghost today stands on the first three: governed memory, scoped retrieval,
and behavioral adaptation (corrections supersede, ignored proactive sources
go quiet). Further stages are investigated against hardware constraints as
they earn their place — never chased for their own sake, never claimed
before they exist.

## The five-year question

"What happens if Ghost is installed in someone's life for five years?" is
the review every architectural decision must survive: what does it know, how
much better has it become, what can it do locally, and what is possible
after years of compounding context that is impossible when every
interaction starts from scratch?

## Implementation

The layering lives in `pkg/agent` (light-model routing, fallback chain,
deferred background extraction that yields to interactive turns),
`pkg/personalcontext` and `pkg/rag` (local-first memory and embeddings),
`pkg/proactive` and `pkg/attention` (continuous local noticing batched
into one message), and the Pod packaging that keeps all of it on
owner-controlled hardware with cloud escalation optional.

## Related concepts

See [Ghost](GHOST.md) for user alignment (who Ghost works for) and the
control plane, and [Memory, Activity, Routines & Artifacts](
MEMORY-ACTIVITY-ROUTINES-ARTIFACTS.md) for the systems that compound.
