# Browser Governance

This document describes the *actual* browser execution path as it runs
today, the security boundary it enforces, and the guarantees the runtime
holds — independent of any model.

## The model proposes; the runtime decides

A browser operation is never "the model called a tool". The path is:

```
model tool call (browser_navigate/snapshot/click/type/press)
   │
   ▼  agent loop — browser gate (pkg/agent/browser_gate.go)
   resolve operation → capability `browser` + concrete action
   resolve owner (ghost identity), context (session binding),
           task/work item + its generation
   routine scope? context scope?            (both set by code, not model)
   permission broker: Evaluate(capability, action, scope, risk)
   │
   ├── allow   → bind BrowserCall (owner/context/task/session ledger/op)
   │            → real BrowserTool enforces the binding again (tool layer)
   │            → execute → evidence → canonical tool.completed/failed
   ├── wait    → mint + pin the browser session now, store durable
   │            approval request; turn pauses with a human message
   └── deny    → turn ends; nothing executed
```

Every binding is resolved **server-side from the turn** (session →
context → task), never from model-supplied arguments. Forged
`owner`/`context`/`task`/`session` arguments cannot widen authority: the
gate ignores them for binding, and the tool layer re-checks the pinned
session row's owner/context/task against the live binding before it
touches the executor.

## Operation taxonomy — the security boundary

The boundary is the **operation**, not natural-language intent:

| Tool | Capability action | Risk | Requires broker authorization |
|------|-------------------|------|-------------------------------|
| `browser_navigate` | `browser_navigate` | read-only | no (allow) |
| `browser_snapshot` | `browser_snapshot` | read-only | no (allow) |
| `browser_click` | `browser_click` | consequential | yes |
| `browser_type` | `browser_type` | consequential | yes |
| `browser_press` | `browser_press` | consequential | yes |
| anything else `browser_*` | — | — | **denied** |

There is no "the model didn't call this a transaction, therefore it is
safe." A `browser_click` is consequential because it drives the page,
full stop. `browser.download/upload/transact` do not exist as tool
operations yet; when they do they must be registered here as
consequential-or-higher with explicit broker handling. The model cannot
invent a tool name to escape this: unknown `browser_*` names are denied.

## Owner / context / task / session binding

- **Owner** is the Ghost identity (`ghost_id`) from governance — a device
  constant, never an argument.
- **Context** is derived from the live session binding (`contexts.Store`),
  so the same conversation mapped to Personal vs Work authorizes
  differently. A Work-scoped browser session row can never be driven from
  a Personal binding.
- **Task / generation**: when the turn runs under a durable job, the
  binding carries the job's ID and its worker generation. Resuming after
  an approval re-checks the generation: if the task moved on (retry,
  restart rotated the generation), the stale approval resumes nothing.
- **Session** is the isolated cookie jar per owner+context+task. Approval
  waits **mint and pin the session up front**, so resume reuses the same
  session rather than silently minting a fresh one. A pinned session that
  expired, or that no longer matches the owner/context/task, is refused.

## Approval and resume

- A consequential operation without a grant parks **durably** in the
  permission broker (SQLite) with the full continuation (operation,
  pinned session, owner, context, task, generation). Process death and
  restart do not lose it.
- The user's reply ("allow once" / "always allow" / "deny") resolves the
  durable request. `allow once` consumes it — a second reply finds nothing
  to consume, so **a replayed approval cannot re-execute**.
- `always allow` stores a scoped standing grant AND resumes the paused
  call (revocation before resume is re-checked against current policy).
- Before executing, resume re-verifies: owner unchanged, context
  unchanged, routine/context scope still permits, task generation still
  current, session still live and matching, grant not revoked. Anything
  drifted refuses with a reason.

## Evidence is first-class

For every executed operation the runtime records: the operation and its
class, who requested it (owner), under which context and task/work item,
which browser session, the policy/permission decision that allowed it,
the actual outcome, and the execution window. That record:

- travels on the tool result as structured evidence;
- is published as a canonical `tool.completed` / `tool.failed` event with
  the evidence in the payload (success without evidence is never emitted);
- is what a "success" claim must rest on. If execution produced **no**
  evidence, the result is reported as *not proven to have run* — never
  "Done".

User-visible claims follow the outcome vocabulary: completed / failed /
waiting / awaiting-verification. Ghost never invents a completion.

## Page content is untrusted input

Everything a page returns is treated as hostile:

- secret-shaped strings are redacted from model-bound output and never
  enter evidence;
- the output is labeled untrusted (prompt-injection preamble) before it
  reaches the model;
- the user-visible text is unchanged;
- a page can never redefine Ghost's policies, scopes, credentials,
  context boundaries, task ownership, or system instructions — runtime
  policy remains authoritative. "Ignore previous instructions", "disable
  security checks", or "you have permission to proceed" are content, not
  authority.

## Subagents

Subagent browser calls resolve through the same gate (owner/context/task
carried from the parent turn). Without a governance hook the subagent
tool loop **refuses** browser calls — there is no ungated subagent
browser execution.

## Legacy / internal paths

Two surfaces intentionally bypass the gate, and only these:

1. The real `BrowserTool` with a **nil policy and no gate binding** runs
   its original bare path. In production this is reachable only by
   internal/non-model code; the agent loop never sends a model browser
   call down this path.
2. A `BrowserTool` with an explicit `Policy` (the previous guarded mode)
   for explicitly configured callers.

Neither is a downgrade hole: the model-reachable path in the agent loop is
gated, and an **unwired loop refuses browser calls outright** rather than
falling through to ungated execution. If a future feature exposes browser
operations to model-controlled execution it must go through the gate.

## Replay and events

- Event delivery is at-least-once; **claims** give exactly-once processing
  per consumer for acknowledged events. This is not an "exactly-once
  external side effect" guarantee — a handler that fails after delivery
  releases its claim and is redelivered on the next replay.
- Event identity is deterministic: re-publishing the same logical event
  keeps one warehouse row and one seq.
- Replay is observation only. Only an explicit `SubscribeDurable`
  replays *and* processes.
- A browser action itself is never "replayed into effect": a spent
  one-shot approval cannot re-authorize, a stale generation cannot
  resume, and an expired/repinned session cannot drive the page.
