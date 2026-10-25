# Anthropic agent loops and topology — research notes (current as of Oct 2026)

*Source status: the briefed URL `claude.com/resources/articles/the-evolution-of-agentic-surfaces` returns 404; its content is now published as "The evolution of agentic surfaces: building with Claude Managed Agents" at `claude.com/resources/articles/building-with-claude-managed-agents`. `anthropic.com/engineering/building-effective-agents` still resolves, but now carries a banner saying the tooling landscape has changed and pointing readers to the Managed Agents post/docs.*

---

## Q1. What is Anthropic's current default topology, and when is a subagent justified?

### Takeaway
As of October 2026 Anthropic's Applied AI team explicitly recommends one model, one context window, one open-ended tool loop as the target design, and treats subagents as a targeted exception for three specific conditions. The direction of travel is fewer subagents plus more progressive disclosure.

### Cited Findings
- Anthropic states that "a single agent with skills frequently outperformed both the one-prompt-for-everything design and the subagent design on quality, and often at a lower cost and latency per task" across enterprise deployments — [Building agents: When to fold subagents into your main agent loop](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop) (Oct 22, 2026).
- Subagents remain justified only when: "A single agent loop hits measurable performance degradation from context bloat"; "Multiple tasks need to be tackled in parallel with fresh context windows that can be discarded once the task is done"; or "A single agent struggles to select appropriate tools or there are conflicting instructions in the system prompt" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- The anti-pattern named is organizational: giving each team or problem type its own subagent, so "the agent architecture becoming a copy of their org chart (Conway's law at work)" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Multi-agent systems "typically use 3-10x more tokens than single-agent approaches for equivalent tasks," and Anthropic has "seen teams invest months building elaborate multi-agent architectures only to discover that improved prompting on a single agent achieved equivalent results" — [Building multi-agent systems: When and how to use them](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them) (Jan 23, 2026).
- The same article frames multi-agent as justified by three conditions only: context pollution, parallelization, and specialization (tool-set or conflicting-prompt specialization) — [same](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- The older canonical framing still stands as vocabulary: distinguish **workflows** (LLMs orchestrated through predefined code paths) from **agents** (LLMs that "dynamically direct their own processes and tool usage") — [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) (Dec 19, 2024, with a 2026 update banner).
- That post also states agents are "typically just LLMs using tools based on environmental feedback in a loop," that agents "trade latency and cost for better task performance," and that autonomy brings "higher costs, and the potential for compounding errors" — [same](https://www.anthropic.com/engineering/building-effective-agents).
- Counter-evidence within Anthropic: for breadth-first research, a multi-agent system (Opus 4 lead, Sonnet 4 subagents) "outperformed single-agent Claude Opus 4 by 90.2% on our internal research eval" — [How we built our multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system) (Jun 13, 2025).

### Inferences
- The reconciled rule is task-class dependent, not ideological: parallel, breadth-first, context-heavy exploration is the proven multi-agent win; sequential, stateful, conversational work is the proven single-loop win. Research's own authors concede "most coding tasks involve fewer truly parallelizable tasks than research," and that agents are "not yet great at coordinating and delegating to other agents in real time."
- The 90.2% research result and the "single agent wins" enterprise result are not a clean contradiction: they differ in task shape (independent facets vs. chained workflow), model generation (2025 vs. 2026), and metric (answer accuracy vs. quality + cost + latency per task). Teams should treat the research number as an upper bound for embarrassingly parallel search, not as a general architecture argument.
- Topology should be an eval outcome, not an upfront design commitment.

### Gaps
- No public Anthropic number quantifies when context bloat becomes "measurable degradation" (no token threshold or quality-drop threshold is given).
- No public guidance on the right subagent count ceiling for a personal/single-user agent; the research-system scaling rules (1 agent / 2–4 / >10) are specific to research queries.

---

## Q2. What mechanisms implement progressive disclosure of tools and context?

### Takeaway
Anthropic implements disclosure at three layers: tool definitions (tool search + `defer_loading`), instructions/procedure (skills), and data (just-in-time retrieval via filesystem/search). The reported effects are large token reductions at equal or better accuracy.

### Cited Findings
- The single-loop target design moves "ordering and domain procedure" into **skills**, hard limits (validation, approval gates) and mid-conversation lookups into **tools**, and keeps code only for "anything cheap and almost always relevant" plus mechanical work inside tools — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Anthropic reports it "reduced Claude Code's system prompt for more advanced models by over 80%" with no measurable loss on coding evals, including cuts from progressively disclosing skills and tools — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop); the primary account is [The new rules of context engineering for Claude 5 generation models](https://claude.dev/blog/the-new-rules-of-context-engineering-for-claude-5-generation-models/) (Jul 24, 2026).
- Tool search: "A typical multiserver setup (GitHub, Slack, Sentry, Grafana, and Splunk) can consume ~55k tokens in definitions before Claude does any work. Tool search typically reduces this by over 85 percent, loading only the 3–5 tools Claude needs for a given request" — [Tool search tool docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
- Tool search exists because "Claude's ability to pick the right tool degrades once you exceed 30–50 available tools"; the mechanism is server-side search over "tool names, descriptions, argument names, and argument descriptions," returning `tool_reference` blocks (default 5) that the API expands into full definitions — [same](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
- Caching interaction: deferred tools "are excluded from the system-prompt prefix," the reference is appended "inline in the conversation," and "the prefix is untouched, so prompt caching is preserved" — [same](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
- Operating rules: keep "your 3–5 most frequently used tools non-deferred"; at least one tool must be non-deferred; max 10,000 deferred tools; use tool search at "10 or more tools," ">10k tokens" of definitions, or when selection accuracy drops — [same](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
- Measured accuracy effects: Opus 4 improved 49% → 74% and Opus 4.5 79.5% → 88.1% on MCP evaluations with tool search enabled; definitions overhead has been observed at "134K tokens before optimization" — [Introducing advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use) (Nov 24, 2025).
- A stronger form is code-execution disclosure: presenting MCP servers as a file tree of typed functions so the agent lists/reads only what it needs reduced definitions from "150,000 tokens to 2,000 tokens—a time and cost saving of 98.7%" — [Code execution with MCP](https://www.anthropic.com/engineering/code-execution-with-mcp) (Nov 4, 2025).
- Context-layer disclosure: keep "lightweight identifiers (file paths, stored queries, web links, etc.)" and load at runtime; Claude Code is a hybrid (CLAUDE.md up front, glob/grep just-in-time) — [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) (Sep 29, 2025).
- Documented progressive-disclosure shifts in Claude Code: "give Claude rules → let Claude use judgement," "give Claude examples → design interfaces," "put it all upfront → use progressive disclosure," "repeat yourself → simple tool descriptions" — [new rules of context engineering](https://claude.dev/blog/the-new-rules-of-context-engineering-for-claude-5-generation-models/).
- Concrete example of interface-over-examples: the TodoWrite description went from ~9,100 characters of when-to-use lists and worked examples to a short description plus a `pending/in_progress/completed` enum and one rule — [same](https://claude.dev/blog/the-new-rules-of-context-engineering-for-claude-5-generation-models/).

### Inferences
- Disclosure has a cost: a search/read step before use, and therefore added latency and an extra failure mode (search misses). Anthropic only recommends it past ~10 tools / ~10k tokens of definitions, which implies small local tool sets should stay eagerly loaded.
- The claim that examples "constrain them to a certain exploration space" is model-generation-specific (Claude 5-era). It is evidence for simplifying, not a universal prohibition on examples.
- For a personal agent, the highest-leverage disclosure layer is probably skills (procedure), not tool search, because local toolsets are usually well under the 30–50 threshold.

### Gaps
- No published latency cost for a tool-search round trip (the docs say it "adds a search step before tool invocation" without a number).
- No guidance on how many skills can be listed before skill *selection* degrades; the 30–50 figure is stated for tools only.

---

## Q3. When and how should a subagent be folded back into the main loop?

### Takeaway
Anthropic prescribes an incremental, eval-gated migration: one shared end-to-end eval, subagents-as-tools as a baseline, then fold one subagent at a time into skills + MCP tools and keep the subagent only if the folded version scores worse.

### Cited Findings
- The target loop is: "One model and one context window running an open-ended tool loop, with limits (turn and cost budgets, write gates) set in the harness"; "Each turn, the model emits a batch of tool calls. The harness runs them in parallel and returns raw results to the same model"; "The loop stops when the model answers or hits a budget. A goal or verifier check can optionally run after the answer" — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Planning changes materially: "The model writes its plan in context on the first turn and revises it as results arrive… the model that writes the plan is also the one carrying it out, so it can change the plan mid-turn." A code step that rewrites the plan from a rule catalog is described as "no longer needed" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Eval scaffolding: one shared end-to-end set owned by a central team, plus smaller eval sets shipped with each skill; "Even a set of 10 golden examples is enough to start"; grade the final message against a rubric; while an orchestrator exists also track routing accuracy, and after folding "check instead whether the agent loaded the right skill or called the right tool" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Merge order: start with the most-used subagents, and merge "specialists that chain on each other's output, touch the same data, or are often called together"; if a significant share of traffic is specialist A then specialist B, "that is a tightly coupled pair" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Subagents-as-tools is the recommended intermediate step because it needs no rewrite, defines a shared response schema, lets the main agent verify conclusions against real data, and establishes a baseline; residual failure modes are separate prompts/owners/evals, "each call still hands back a lossy summary," and "every call is a nested model loop that adds latency" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Full handoff (subagent takes over the conversation) should be reserved for when "it needs to talk to the user directly"; "Everything else can be a skill plus tools in the main loop" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Folding is a prompts→skills, API-calls→MCP-tools translation, one subagent at a time, comparing eval performance with and without — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Reported migration hazard: "folding one subagent into another lowered latency, but also broke a process that had been passing. The main agent was holding the order lookup tool and answering on its own, without asking the shipping specialist for the delivery date." The break "only showed up in one run out of two," so tasks must be run more than once, and the team must inspect "which tasks flipped," not just the final score — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Decomposition rule if subagents are kept: "divide by context: give each subagent a piece of work that needs little from the rest of the system and have it hand back a compact result" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- The complementary multi-agent article names good boundaries (independent research paths, clean interfaces, black-box verification) and bad ones (sequential phases of the same work, tightly coupled components, shared state) — [multi-agent systems](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- Organizational prerequisite: one small central team owns end-to-end quality and the system prompt, vetting every new tool, skill, and eval — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Anthropic ships a migration kit (`anthropics/single-agent-loop-kit`) plus skills `/claude-api build-eval`, `prompt-audit`, `hillclimb`, `cost-optimize`, and Claude Code `/skill-creator`, `/skill-doctor` — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).

### Inferences
- The only defensible fold criterion in the sources is per-task eval deltas plus cost/latency, not architecture aesthetics. "Keep what holds up" is the stated principle.
- The order-lookup regression is a specific warning for fold-ins: when a specialist's value is a *check* (asking for a second data point) rather than a *capability*, folding removes the check and the main agent silently substitutes its own judgment. Evals need tasks that assert the check happened.
- Ablating one component at a time is the only reliable way to know what is load-bearing; Anthropic reports that a radical cut of its harness failed to replicate performance and made load-bearing pieces impossible to identify — [Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps) (Mar 24, 2026).

### Gaps
- No public rule for how many folds are too many, or when to stop folding (i.e., the point at which a monolithic loop becomes unmanageable). "Measure" is the only stated answer.

---

## Q4. What are the current mechanics and tradeoffs of compaction, context editing, and context resets?

### Takeaway
Anthropic now treats server-side compaction as the default long-context strategy, tool-result clearing as the cheap safe lever, and context *resets* as a model-specific workaround that should be removed once models stop exhibiting "context anxiety." Compaction buys continuity but not a clean slate.

### Cited Findings
- "For most use cases, server-side compaction is the primary strategy for managing context in long-running conversations"; context editing is for "specific scenarios where you need more fine-grained control over what content is cleared" — [Context editing docs](https://platform.claude.com/docs/en/build-with-claude/context-editing).
- Compaction "replaces the older turns of a conversation with a summary that Claude writes on the server"; rationale is both window fit and quality: "it keeps the active context small, because response quality degrades as a conversation grows" — [Compaction overview](https://platform.claude.com/docs/en/build-with-claude/compaction).
- On-demand compaction returns a single signed `compaction` block, generates no reply (`stop_reason: "compaction"`), replaces the summarized messages, must be sent first in `messages`, and must be sent back byte-identical (signature checked) — [Compaction on demand](https://platform.claude.com/docs/en/build-with-claude/compaction-on-demand).
- Practical constraints: you must compact before the conversation outgrows the window ("The conversation must still fit the model's context window, so compact before you outgrow it, not after"); you cannot compact when the last assistant turn ends in a tool call with no result; custom `instructions` replace the summarizer prompt and are capped at 16,384 characters — [same](https://platform.claude.com/docs/en/build-with-claude/compaction-on-demand).
- Lossy by construction: "Images, documents, `container_upload` blocks, and fetched URLs inside the summarized messages are gone once the block replaces them. Restate or re-upload anything a later turn still needs." Mid-conversation `role: "system"` messages inside the summarized range are also summarized, so "their text instructions stop applying" — [same](https://platform.claude.com/docs/en/build-with-claude/compaction-on-demand).
- Summarization can fail while returning HTTP 200 with empty content; `stop_reason` distinguishes `max_tokens`, `model_context_window_exceeded`, `tool_use`, `refusal`, `end_turn`; in every case "you can continue without a summary and compact later" — [same](https://platform.claude.com/docs/en/build-with-claude/compaction-on-demand).
- Tool-result clearing (`clear_tool_uses_20250919`) clears "the oldest tool results in chronological order," replacing each with placeholder text; it supports `trigger`, `keep` (e.g. keep 3 tool uses), `clear_at_least` (minimum tokens cleared), `exclude_tools`, and optional `clear_tool_inputs` — [Context editing docs](https://platform.claude.com/docs/en/build-with-claude/context-editing).
- Cache tradeoff, stated plainly: tool-result clearing "Invalidates cached prompt prefixes when content is cleared. To account for this, clear enough tokens to make the cache invalidation worthwhile." Thinking-block clearing preserves cache only while thinking is kept; "Configure the `keep` parameter based on whether you want to prioritize cache performance or context window availability" — [same](https://platform.claude.com/docs/en/build-with-claude/context-editing).
- What to keep in a summary, per the older playbook: preserve "architectural decisions, unresolved bugs, and implementation details while discarding redundant tool outputs or messages," plus "the five most recently accessed files"; tune for recall first, then precision — [Effective context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents).
- Compaction vs reset is a named tradeoff: "While compaction preserves continuity, it doesn't give the agent a clean slate, which means context anxiety can still persist. A reset provides a clean slate, at the cost of the handoff artifact having enough state for the next agent to pick up the work cleanly" — [Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps).
- Context anxiety: on Sonnet 4.5 "an agent would rush to finish as it neared the end of its context, cutting work short"; Anthropic's fix was context resets, but "on Claude Opus 4.5, the behavior was gone, and the resets we'd added were just overhead" — [Managed Agents / evolution of agentic surfaces](https://claude.com/resources/articles/building-with-claude-managed-agents) (Jun 10, 2026).
- The follow-up harness dropped resets entirely and relied on "the Claude Agent SDK's automatic compaction handling context growth along the way" — [Harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps).
- Anchoring caution: waiting for bigger windows is not a strategy, because "context windows of all sizes will be subject to context pollution and information relevance concerns" — [Effective context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents).

### Inferences
- Compaction is a *continuity* mechanism, not a *quality* mechanism. If the failure is premature wrap-up or drift, resets/restarts with artifact handoff address a different problem than summarization does.
- Any harness that compacts must also re-assert durable instructions (system-level constraints, active tool policy) after a summary, because in-range system messages do not survive.
- For a local agent, tool-result clearing is the cheapest first lever (no summarizer call, no latency, no model dependency); compaction should be reserved for genuinely long sessions.

### Gaps
- No published quality metric for compaction fidelity, and no recommended trigger fraction of the context window (docs say "below the model's context window" without a number).
- No current (2026) public data on whether context anxiety persists in the newest models other than the qualitative Opus 4.5/4.6 statements.

---

## Q5. How should tool results, tool interfaces, and tool errors be designed?

### Takeaway
Tools are an interface for a non-deterministic caller: consolidate around user-intent workflows, return high-signal semantic fields, cap and paginate aggressively, and make errors instructional rather than diagnostic dumps.

### Cited Findings
- "More tools don't always lead to better outcomes"; wrapping every API endpoint is a named common error. Preferred shapes: `schedule_event` instead of `list_users`/`list_events`/`create_event`; `search_logs` instead of `read_logs`; `get_customer_context` instead of three getters — [Writing effective tools for agents](https://www.anthropic.com/engineering/writing-tools-for-agents) (Sep 11, 2025).
- Tools should "return only high signal information" and "eschew low-level technical identifiers (for example: `uuid`, `256px_image_url`, `mime_type`)"; resolving UUIDs to semantic names "significantly improves Claude's precision in retrieval tasks by reducing hallucinations" — [same](https://www.anthropic.com/engineering/writing-tools-for-agents).
- A `response_format` enum (`concise`/`detailed`) lets the agent choose verbosity; the documented example cut 206 tokens to 72 (~⅓) — [same](https://www.anthropic.com/engineering/writing-tools-for-agents).
- "We suggest implementing some combination of pagination, range selection, filtering, and/or truncation with sensible default parameter values"; "For Claude Code, we restrict tool responses to 25,000 tokens by default" — [same](https://www.anthropic.com/engineering/writing-tools-for-agents).
- On errors: "if a tool call raises an error (for example, during input validation), you can prompt-engineer your error responses to clearly communicate specific and actionable improvements, rather than opaque error codes or tracebacks" — [same](https://www.anthropic.com/engineering/writing-tools-for-agents).
- Namespacing (by service and/or resource) is recommended; "We have found selecting between prefix- and suffix-based namespacing to have non-trivial effects on our tool-use evaluations" — [same](https://www.anthropic.com/engineering/writing-tools-for-agents).
- Interface design beats examples on current models: "Instead of using examples, think more about the design of your tools, scripts and files—what parameters does Claude have and how can they be more expressive?" — [new rules of context engineering](https://claude.dev/blog/the-new-rules-of-context-engineering-for-claude-5-generation-models/).
- Poka-yoke and ACI: Anthropic "spent more time optimizing our tools than the overall prompt" on SWE-bench; requiring absolute filepaths fixed a recurring error because "the model used this method flawlessly" — [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents).
- Format guidance: give the model tokens to think before writing; keep formats close to naturally occurring text; avoid "formatting overhead" like line counts or string escaping — [same](https://www.anthropic.com/engineering/building-effective-agents).
- Tool descriptions can move out of the system prompt: "We found we could delete these repeat examples and put instructions on how to use tools in the tool descriptions rather than the system prompt" — [new rules](https://claude.dev/blog/the-new-rules-of-context-engineering-for-claude-5-generation-models/).
- Where an error/refusal belongs: "if Y needs X's output, Y's tool checks for it and returns an instruction the model can act on. Per-user permissions work the same way… returns a plain refusal the model can read" — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Tool-search failures are typed rather than thrown: `invalid_tool_input`, `unavailable`, `too_many_requests`, `execution_time_exceeded`; a search matching nothing returns an empty `tool_references` array, "not an error" — [Tool search docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
- Tool changes made by tools, not orchestration: Programmatic Tool Calling keeps intermediate results out of context (200KB of expense rows → ~1KB of results) but requires documenting return formats so the model can write correct parsing logic — [Advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use).

### Inferences
- The strongest recurring pattern is *moving control flow into the tool*: prerequisites, permissions, retries, pagination, and consolidation all reduce the number of decisions the model must make and the tokens it must hold.
- Typed, machine-readable failure classes (retryable vs. invalid input vs. unavailable) are more useful than prose for letting a model choose between retry, re-search, or escalate.
- Error text is a prompt surface; it should be evaluated like one. Anthropic's only quantified claim here is indirect (a rewritten tool description cut task completion time 40% — [multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system)).

### Gaps
- No published standard for error payload schema (no equivalent of RFC 9457 for agent tools), and no measured effect of error-message quality on recovery rate.
- The 25,000-token tool-response cap is stated for Claude Code only; no general recommendation for local/self-hosted agents.

---

## Q6. How should the loop terminate, and how should budgets be set?

### Takeaway
Terminate on either a model-declared answer or an explicit budget, and make the budget advisory to the model while keeping a hard per-request cap. Anthropic distinguishes turn budgets (breadth) from effort (depth) and warns that too-small budgets cause refusal-like behavior.

### Cited Findings
- "The loop stops when the model answers or hits a budget. A goal or verifier check can optionally run after the answer" — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Harness-level limits named: "turn and cost budgets, write gates" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- "The task often terminates upon completion, but it's also common to include stopping conditions (such as a maximum number of iterations) to maintain control" — [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents).
- Task budgets "let you tell Claude how many tokens it has for a full agentic loop, including thinking, tool calls, tool results, and output. The model receives a running countdown and uses it to prioritize work and finish gracefully as the budget is consumed" — [Task budgets docs](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- "Task budgets are a **soft hint, not a hard cap**"; "The enforced limit on total output tokens is still `max_tokens`, which truncates the response with `stop_reason: "max_tokens"`." Recommended pairing: task budget as target, `max_tokens` as absolute ceiling — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Failure mode of undersizing: "A budget that is too small for the task can cause refusal-like behavior… it might decline to attempt the task at all, scope it down aggressively, or stop early with a partial result." Minimum accepted value is 20,000 tokens — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Sizing method: run a representative sample without budgets, "Start with the p99 of your per-task token spend," then tune — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Budget accounting is about what the model sees, not payload size: resent history is not re-counted; tool results and generated tokens are. "Set a generous budget and let the model self-regulate against the countdown rather than trying to mirror it client-side" because client-side decrementing under-reports and makes Claude finish early — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Turn boundary definition: "A user message that carries no tool results starts a new turn with a fresh budget"; a message containing `tool_result` blocks continues the current turn — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Compaction does not refund budget: "Server-side compaction during a turn does not reset the budget: tokens the turn consumed before the compaction still count against it" — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Depth/breadth split: "effort controls how thoroughly Claude reasons about each step, whereas task budgets cap the total work Claude can do across an agentic loop" — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Cache interaction: the budget marker is injected server-side each request; mutating `remaining` "invalidates any cache prefix that contains it," so set once and let the model self-regulate — [same](https://platform.claude.com/docs/en/build-with-claude/task-budgets).
- Product-level precedent for hard stops: a run that hits its spending cap "pauses instead of failing," with stop reason `budget_reached`; caps should start "at three to five times the cost of a normal run" — [Building effective agent automations](https://claude.dev/blog/building-effective-agent-automations/) (Oct 8, 2026).
- Research-system effort scaling shows termination policy is also a *quality* lever: "Simple fact-finding requires just 1 agent with 3-10 tool calls, direct comparisons might need 2-4 subagents with 10-15 calls each, and complex research might use more than 10 subagents" — [multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system).

### Inferences
- Two different termination semantics are needed: *graceful* (advisory countdown → partial summary) and *hard* (per-request or per-run ceiling → stop). Conflating them produces either runaway loops or truncated answers.
- Budgets are also a scoping signal: a too-small budget is read as "this task is not worth attempting," which is a useful capability if intentional and a bug if accidental.
- Because compaction and new user turns interact differently with budgets, the loop's notion of "a unit of work" must be explicit in code; otherwise cost accounting drifts from what the model is pacing against.

### Gaps
- No guidance on wall-clock/time budgets (only tokens and a product-level money cap), nor on how to set turn-count limits for open-ended personal-assistant work.
- No public evaluation of how often models exceed advisory budgets, or by how much.

---

## Q7. How should an agent recover from tool failures, partial failures, and uncertain side effects?

### Takeaway
Anthropic's pattern is: make failure legible to the model, let the model adapt, and wrap that adaptivity in deterministic safeguards (resume points, idempotency checks, explicit unknown states). The most damaging failure is not an error — it is a failure that looks like an empty success.

### Cited Findings
- "We also use the model's intelligence to handle issues gracefully: for instance, letting the agent know when a tool is failing and letting it adapt works surprisingly well. We combine the adaptability of AI agents built on Claude with deterministic safeguards like retry logic and regular checkpoints" — [multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system).
- Restart-vs-resume: "When errors occur, we can't just restart from the beginning: restarts are expensive and frustrating for users. Instead, we built systems that can resume from where the agent was when the errors occurred" — [same](https://www.anthropic.com/engineering/multi-agent-research-system).
- Error messages should be actionable rather than tracebacks (see Q5) — [Writing effective tools](https://www.anthropic.com/engineering/writing-tools-for-agents).
- The silent-failure rule, stated as a design constraint: "If an MCP server is down or its token has expired, the run still starts, just without that server's tools… the agent sees nothing from that source and reports 'nothing new.'" The fix: when a source fails, "keep the source's bookmark where it is, write the brief from the other sources, and end the brief with one line naming what it couldn't read" — [agent automations](https://claude.dev/blog/building-effective-agent-automations/).
- Summary rule from that reference implementation: "Report a failed read as unreadable, never as a quiet day" — [same](https://claude.dev/blog/building-effective-agent-automations/).
- Side-effect discipline: "Count a post as sent only when Slack confirms it, then update bookmarks and ledger"; "If the result is unclear, it marks the run 'maybe posted' and changes nothing else, so nothing is lost"; it also checks for an existing post before posting, to avoid duplicates — [same](https://claude.dev/blog/building-effective-agent-automations/).
- Staleness re-check before acting: "For every item you will report, re-check its live source just before posting… One stale 'still waiting on you' costs more trust than ten missing items, so never hedge an item's status: assert it or drop it" — [same](https://claude.dev/blog/building-effective-agent-automations/).
- Run-state as an explicit state machine: the agent writes `runs/<date>.md` marking "posting" before the post, then "posted" with message ID, or "maybe posted" — [same](https://claude.dev/blog/building-effective-agent-automations/).
- Verification loops need bounded retries with feedback: Anthropic's example retries up to `max_attempts=3`, appending "Previous attempt failed: {issues}" to the requirements each time — [multi-agent systems](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- Early-victory is the dominant verification failure: verifiers "run one or two tests, observe them pass, and declare success"; mitigations are concrete criteria, negative tests, and explicit instructions ("You MUST run the complete test suite before marking as passed") — [same](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- Self-grading bias: "When asked to evaluate work they've produced, agents tend to respond by confidently praising the work"; a separate evaluator is "far more tractable" to tune skeptical than a generator is to make self-critical — [Harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps).
- Structured failure handling in the loop itself: when compaction fails, the contract is to continue without a summary and retry later, keyed on `stop_reason` — [Compaction on demand](https://platform.claude.com/docs/en/build-with-claude/compaction-on-demand).
- Tool search failures are returned in-band with typed codes rather than aborting the request — [Tool search docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).

### Inferences
- Three recovery classes deserve distinct handling: (a) *transient* (retry, backoff), (b) *invalid request* (feed the validation message back and let the model correct), (c) *unavailable/partial* (proceed with degradation, but record and surface the gap). Treating (c) as (a) creates loops; treating (c) as success creates silent data loss.
- "Unreadable ≠ empty" and "unconfirmed ≠ done" are the two invariants that generalize beyond the automation example, including to local file and network tools.
- Idempotency keys or pre-action existence checks are cheaper than reconciliation, and the "maybe posted" state is a practical alternative when the side effect is not idempotent.

### Gaps
- No public Anthropic guidance on circuit-breaker thresholds, backoff policy, or how many consecutive tool failures should terminate a loop.
- No guidance on user-facing escalation: when the agent should stop and ask versus degrade silently (beyond the automations post's "name what it couldn't read").

---

## Q8. What does Anthropic report about latency, cost, and evaluation?

### Takeaway
The consistent quantitative story is: multi-agent and heavy harnesses buy quality with roughly 3–15× tokens and often more wall-clock time; single-loop + progressive disclosure is the cost/latency play; and architecture decisions should be settled by end-to-end task-completion evals, not component-level scores.

### Cited Findings
- Multi-agent token multipliers: "agents typically use about 4× more tokens than chat interactions, and multi-agent systems use about 15× more tokens than chats"; "multi-agent implementations typically use 3-10x more tokens than single-agent approaches for equivalent tasks" — [research system](https://www.anthropic.com/engineering/multi-agent-research-system); [multi-agent systems](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- Parallelism's benefit is coverage, not speed: "The primary benefit of parallelization is thoroughness, not speed… multi-agent systems often take longer overall than single-agent systems because of the sheer increase in total computation" — [multi-agent systems](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- Parallel tool calling did cut research time "by up to 90% for complex queries" (lead agent 3–5 subagents in parallel; subagents 3+ tools in parallel) — [research system](https://www.anthropic.com/engineering/multi-agent-research-system).
- Enterprise latency anecdote: "One customer that evaluated their multi-agent harness against a single agent loop saw a roughly 2x faster runtime with the latter" — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Harness cost is superlinear in quality: the same one-sentence game-maker prompt took "20 min / $9" solo vs "6 hr / $200" with the full planner/generator/evaluator harness — "over 20x more expensive," with visibly better output; a later DAW run took 3 hr 50 min / $124.70 — [Harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps).
- Infrastructure latency: decoupling the harness from the sandbox "cut the time-to-first-token by roughly 60% in the median case (p50) and by over 90% in the slowest cases (p95)" — [Managed Agents / evolution of agentic surfaces](https://claude.com/resources/articles/building-with-claude-managed-agents).
- Token-usage is the dominant performance variable in search: "token usage by itself explains 80% of the variance" in BrowseComp, with tool-call count and model choice as the other two factors (95% combined) — [research system](https://www.anthropic.com/engineering/multi-agent-research-system).
- Isolation pays only when the ratio is favorable: context isolation is "most effective when subtasks generate high context volume (more than 1000 tokens) but most of that information is irrelevant," returning ~50–100 tokens — [multi-agent systems](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them).
- Eval design: start with "20-50 simple tasks drawn from real failures" (evals post) and "about 20 queries" (research post); "Even a set of 10 golden examples is enough to start" (fold post) — [Demystifying evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents); [research system](https://www.anthropic.com/engineering/multi-agent-research-system); [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Non-determinism metrics: `pass@k` (at least one success in k) vs `pass^k` (all k succeed); a 75% per-trial agent has (0.75)³ ≈ 42% chance of passing three trials — [Demystifying evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents).
- Grade outcomes, not paths: "there is a common instinct to check that agents followed very specific steps… We've found this approach too rigid"; "it's often better to grade what the agent produced, not the path it took" — [same](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents).
- Capability evals should start below 100%; regression evals should be ~100%; saturation (100% pass) means the eval no longer produces signal — [same](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents).
- Diagnostic discipline: "we do not take eval scores at face value until someone digs into the details of the eval and reads some transcripts"; a 0% pass@100 across many trials "is most often a signal of a broken task" — [same](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents).
- Metrics to track per task: latency (TTFT, output tokens/sec, time to last token), turns, tool calls, total tokens, error rates, cost per task — [same](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents); [Writing effective tools](https://www.anthropic.com/engineering/writing-tools-for-agents).
- Architecture comparison method: same eval, one change at a time; "When you compare, don't just examine the final score, take a look at which tasks flipped"; run each task more than once — [fold-subagents](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Replay approach for hard-to-simulate environments: "record timestamps on important events, so they can be replayed later to run as targeted evals" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- Cost tooling: `/claude-api cost-optimize` profiles "where your calls go" and proposes savings from prompt caching, input/output trimming, effort, and model choice, "one change at a time" — [same](https://claude.com/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop).
- LLM-as-judge detail: a single judge call with one rubric and a 0.0–1.0 score plus pass/fail "was the most consistent and aligned with human judgements," across dimensions like factual accuracy, citation accuracy, completeness, source quality, tool efficiency — [research system](https://www.anthropic.com/engineering/multi-agent-research-system).

### Inferences
- For a single-user local agent, the multi-agent cost multipliers are disqualifying for routine work; the relevant comparisons are single-loop vs. one verifier, not orchestrator vs. swarm.
- `pass^k` is the right primary metric for a personal assistant, because "reliable every time" matters more than "succeeds eventually" when a human is waiting.
- Because token spend explains most of the variance in hard search, cost controls (disclosure, truncation, consolidation) can improve *and* cheapen simultaneously — but only where the task is retrieval-shaped. Elsewhere they trade quality away.

### Gaps
- No public benchmark of latency/quality for skill-based progressive disclosure vs. subagents on identical hardware/model.
- No published cost-per-task figures for a single-user assistant workload, and no guidance on local-model (non-Claude) behavior for any of these mechanisms.

---

## Q9. What should Ghost change? (synthesis — inferences only)

### Takeaway
The evidence points to a small number of high-confidence moves: keep one loop as the default, make skill/tool descriptions do the routing work, treat compaction as continuity rather than quality, make failures legible, and gate any topology change on a small repeated-task eval.

### Inferences (each traced to the findings above)
- **Default to one loop with no router/planner stage.** Ghost's skills directory suggests procedure already lives outside the prompt; Anthropic's evidence says the plan should be written and revised by the executing model, and that a code step rewriting plans from a rule catalog is no longer needed (Q1, Q3).
- **Do not add subagents for domain separation.** Only three triggers justify them (context bloat, discardable parallel work, tool-selection/instruction conflict). A personal assistant's domains are usually chained and stateful — the case where Anthropic measured coordination costing more than work (Q1).
- **If a verifier is kept, keep exactly one, and make it black-box.** It is the one pattern the sources call consistently effective, and its value is bounded by whether the task sits beyond what the model does reliably solo. Tune it to be skeptical rather than hoping the main agent self-corrects (Q3, Q7, Q8).
- **Invest in descriptions before mechanisms.** Tool and skill descriptions are the routing layer; Anthropic reports >80% system-prompt reduction with no measurable loss once instructions moved into descriptions and interfaces. Thresholds (10+ tools / >10k tokens) suggest tool search is unnecessary for a small local toolset (Q2, Q5).
- **Prefer consolidation over proliferation.** Group tools by user intent, not by API; keep the count well under 30–50. For Ghost this means workflow-shaped tools (locate and open a thing in one call) rather than CRUD wrappers (Q5).
- **Cap and truncate every tool result, and make the cap visible.** A default cap plus "results truncated, narrow your query" guidance is the cheapest context protection available, and it composes with a `concise`/`detailed` choice where the agent needs IDs for follow-up calls (Q5).
- **Design the error surface as three classes.** Retryable, invalid-input-with-fix-instructions, and unavailable/partial. Only the first two should loop; the third must degrade visibly and preserve state (Q7).
- **Adopt "unreadable ≠ empty" and "unconfirmed ≠ done" as invariants.** These are the two failure modes with the highest trust cost in the sources: a silent empty result and a recorded-but-unlanded side effect (Q7).
- **Use tool-result clearing first, compaction second, resets only if measured.** Clearing is the lightest lever and does not need a summarizer; compaction preserves continuity but loses artifacts and in-range system instructions; resets are a workaround for a model behavior that Anthropic itself removed as models improved (Q4).
- **Terminate on answer-or-budget, with both a soft and a hard ceiling.** Mirror the advisory countdown + hard `max_tokens` split; size the soft budget from measured p99, not a default, and never set it so low that the agent declines the task (Q6).
- **Build the eval before changing topology.** 10–20 real tasks, run more than once, compare per-task flips, track tokens/latency/tool errors alongside pass rate. Grade outcomes, not tool-call sequences (Q3, Q8).
- **Re-audit the harness on every model upgrade.** Every harness component "encodes an assumption about what the model can't do on its own," and Anthropic's own resets, sprint decomposition, and 80% of its system prompt were removed when models changed (Q2, Q4, Q8).

### Gaps
- Whether Ghost's runtime can express server-side compaction/context-editing equivalents is unknown from these sources; the docs assume the Claude API. Local implementations must reimplement the semantics (placeholder clearing, summary-block-first, signature-free equivalents) and should be evaluated for the same failure modes.
- No source addresses single-user privacy constraints on memory/state files, which matter for a local personal agent more than for the enterprise deployments described.
- The sources give no guidance on multi-provider or local-model fallback behavior, which is a likely Ghost requirement and a material gap in the evidence base.
