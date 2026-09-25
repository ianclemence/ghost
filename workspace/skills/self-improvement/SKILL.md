---
name: self-improvement
description: "Turn verified mistakes and non-obvious fixes into durable learning. Use after a failure, a correction from the user, a recurring procedure, or when asked to learn from something and improve. Keeps a learnings ledger and promotes proven workflows into skills."
version: 1.0.0
author: Ghost
license: MIT
platforms: [linux, macos, windows]
---

# Self-Improvement

Ghost gets better by remembering what actually worked. This skill is the loop:
notice → verify → generalize → promote → never repeat the same failure.

Use it after a mistake, a user correction, a non-obvious fix, or a repeated
procedure. Skill promotion is delegated to the `skill-gardener` skill; this
skill owns the learning ledger and the verification discipline.

## The loop

1. **Notice.** Capture the trigger in one line: what was being attempted, what
   went wrong or turned out to be non-obvious.
2. **Verify.** Only record learnings backed by evidence: a command that now
   succeeds, test output, a user confirmation. A hunch is not a learning. If
   verification is missing, say so and stop.
3. **Generalize.** Strip incidental detail (filenames, IDs, dates). Keep the
   durable rule. If the rule is only true for one version, record the range.
4. **Record.** Append to the ledger: `workspace/learning/learnings.md`.
   One entry, five fields:
   `date · trigger · what failed · what works · evidence`
   Keep it short. Never record secrets, tokens, personal data, or transcripts.
5. **Promote.** If the learning is repeatable, stable, specific, verified, and
   safe, hand it to `skill-gardener` to create or repair a skill. Do not
   promote generic advice or one-off facts.
6. **Apply.** Before a similar task, read the ledger for that area and follow
   the recorded procedure instead of rediscovering it.

## Verification before consequential actions

Before acting on something that changes user state (sending, deleting,
paying, controlling devices, editing shared documents), run the three-question
check and answer honestly:

- **Goal** — what outcome did the user actually ask for?
- **Falsify** — what would make this the wrong action?
- **Reversible** — if this is wrong, can it be undone?

If any answer is unclear, ask one short question instead of guessing. This is
not permission theater: security and irreversible actions still need a real
check, but routine, reversible reads and lookups should just proceed.

## Memory tiers

- **Core** — durable facts about the user and their setup. Lives in memory and
  personal context, not here.
- **Learned** — reusable procedures and pitfalls: the ledger above, promoted to
  skills when proven.
- **Ephemeral** — scratch for the current task. Discard when done; never let it
  drift into the ledger.

## Boundaries

- Never edit governance, approval rules, or security settings as part of
  learning. A learning that would weaken a safeguard is rejected, not recorded.
- Never modify skills you don't own (bundled or third-party) in place; propose a
  local replacement or repair a user-owned skill only.
- Behavior changes that affect the user (new automations, scheduled actions,
  outbound messages) need the user's go-ahead once, not per step.
- If two learnings conflict, the newer verified one wins; delete the stale one
  rather than keeping both.

## Maintenance

Re-read the ledger at the start of a related task. Prune entries that a
promoted skill now covers, so the ledger stays a working memory, not an archive.
