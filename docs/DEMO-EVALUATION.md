# Demo Evaluation — Ghost end-to-end (deepseek-flash)

A realistic multi-turn demo driven through the **real gateway** over the
SSE `/v1/chat` surface, exactly as the mobile app and web console do. Owner
persona "Maya". Every finding was reproduced against the canonical data
(`personal-context/entries.jsonl`, `scheduled_items`, `permissions`), not
inferred from the model's prose.

Environment: isolated workspace, gateway on `127.0.0.1:8877`, clean
`GHOST_DIR`. The live production Ghost on 8766 was never touched.

## Result

The user-visible surfaces are now correct and consistent:

**Memory** (`/v1/memory/self`) — what Ghost knows, as the owner sees it:
```
[constraint ] Work     | Does not take meetings before 10am
[fact       ] Location | Lives in Bangkok
[fact       ] Work     | Works as a product designer
[identity   ] Name     | The user's name is Maya
[preference ] Food     | Always orders oat milk lattes
```

**Things Ghost does** (`/v1/things`) — what Ghost runs for you:
```
• Prepare my weekly design review brief  —  Every Monday at 9:00 AM
```

**Recall** is conversational: "Where do I live?" → "You live in Bangkok.";
"What's my name?" → "You're Maya."

## Defects found and fixed

### 1. Memory surface read a different workspace than the agent wrote (HIGH)
`workspaceDir` in `cmd/ghost/internal_api.go` was resolved from
`GHOST_WORKSPACE_DIR` or `$HOME/ghost/workspace`, ignoring the configured
`agents.defaults.workspace`. The agent wrote memories to the configured
workspace; `/v1/memory/self` and `/v1/memory/files` read the default. On a
real Pod (workspace `/var/lib/ghost/workspace`) the owner's Memory screen
showed a different, empty memory. **Fixed**: `resolveApiWorkspace` prefers
the loaded config; `memoryDir` derives from it. Regression tests added.

### 2. Compound memories stored verbatim and fused (MED)
"Remember that I never take meetings before 10am, and I always order oat
milk lattes." was stored as ONE entry whose value was the raw sentence
(including "Remember that"). **Fixed**: the extractor now returns discrete,
model-authored summaries (`memories[]`); the value comes from the summary,
never scraped raw text; the predicate derives from domain.

### 3. `constraint` and `project` kinds were silently rejected (HIGH)
`entry.ValidKind` omitted `KindConstraint` and `KindProject`, though the
classifier emitted them. Every constraint ("no meetings before 10am") — the
class of memory a personal AI most needs — was **dropped at persist time**
with only a log warning. **Fixed**; a vocabulary-agreement test now prevents
the two lists from ever diverging again.

### 4. Multi-clause messages lost all but one fact (HIGH)
"I'm Maya. I live in Bangkok and work as a product designer" produced only
`fact/location`: the deterministic grammar captured one fact and suppressed
the model extractor entirely. The name and role were never stored, so "What's
my name?" honestly — but **wrongly** — answered "I don't have that stored
yet." **Fixed**: when the grammar captures exactly one fact from a
multi-clause message, the model's complementary memories are merged in, with
dedup against current memory.

### 5. Raw cron shown to the owner (MED)
The Things feed rendered `schedule: "0 9 * * 1"`. The humanizer was a tiny
lookup table and echoed any expression it didn't hardcode. **Fixed**: a real
cron humanizer ("Every Monday at 9:00 AM"), with tests; the old test that
asserted raw-cron output encoded the bug and was corrected.

### 6. Scheduled item titled with its own schedule (MED)
A routine's title was "Every monday at 9 AM" (the schedule) instead of the
action ("Prepare my weekly design review brief"). **Fixed**: the title
derives from the action content, falling back to the schedule only when there
is no content.

### 7. Routine task carried stale connective text (MED)
"Also remind me every Friday at 4pm to send my team a status update" produced
the task `"Also remind me   to send my team a status update"`. **Fixed**: the
parser peels leading connectives ("also", "and", "then", "plus") and collapses
whitespace.

### 8. A stale routine proposal hijacked the next request (MED)
An abandoned proposal stayed open for the session; the next routine request
was treated as its continuation and its text was appended to the old intent
("prepare my brief. Please create it now Every Monday"). **Fixed**: a fresh,
complete routine intent supersedes a stale open proposal.

### 9. Streamed assistant segments were glued together (LOW/MED)
When the model spoke before a tool call and again after, the client
concatenated both with no separator: "I'll remember that.Got it, Maya — …".
**Fixed** in the mobile client: a `tool_status` boundary after text inserts a
paragraph break. Tests added.

### 10. Fast-path recall returned third-person notes (LOW)
Stored values were returned verbatim ("The user's name is Maya", "Lives in
Bangkok"). **Fixed**: answers are re-voiced conversationally ("You're Maya.",
"You live in Bangkok.") with a fallback that never regresses unknown
predicates.

### 11. A harmless file read was gated as a consequential "Send email" (HIGH)
Under the email skill (which lists `read_file` in its AllowedTools so the
skill can read its own SKILL.md), reading `skills/email/SKILL.md` was
authorized as `email.read` with the skill's `consequential` risk — producing a
"Send this email?" approval card for a read-only file read. A false consent
prompt is worse than no prompt: it teaches owners to approve reflexively and
mislabels what is actually happening. **Fixed**: `authorizedToolRisk`
classifies read-only tools (`read_file`, `list_dir`, `search_files`,
`grep_search`, `web_fetch`, `web_search`, memory recall) as `read_only`
regardless of the surrounding skill's declared risk — mirroring the existing
rule that `exec`/`sandbox` are always `high_impact`. Genuinely consequential
tools (`email_send`) still inherit the skill's risk.

## What already worked well (verified, not claimed)

- Permission discipline: routine creation asks before acting and creates a
  durable, resumable approval card with allow-once / always-allow / deny.
- Cross-referencing: Ghost flagged a 09:00 reminder inside Maya's
  "no meetings before 10am" window.
- Honest unavailability: "I can't read your calendar right now — blocked by
  the runtime's execution policy."
- Memory recall, routine creation, the unified Things feed, and the
  permission broker all function end to end after the fixes.

## Verification after fixes

- `go test ./pkg/... ./cmd/...` — all pass.
- `bun test` (mobile) — 199 pass.
- Golden Conversation Suite (deepseek-flash), two clean runs: **59/59 PASS,
  0 fail, 0 hard-fail**. A later run hit a transient DNS outage
  (`dial tcp: lookup api.deepseek.com`); the four resulting failures
  (`cc-01`, `goal-01`, `goal-02`, `mem-05`) all pass in isolation and the
  runner labeled them `environment`. This is the harness correctly
  distinguishing a product failure from an infrastructure one.

## Method note

Every defect above was reproduced against canonical data or the live
permission broker — not inferred from the model's prose. The demo drove the
real gateway over SSE exactly as the mobile app does; the production Ghost on
port 8766 was never touched (the demo used an isolated workspace on 8877).
