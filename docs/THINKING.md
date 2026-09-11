# Thinking defaults — per provider, verified against official docs

Ghost keeps thinking OFF unless the user opts in (`/think`, `thinking: true`,
or a non-`off` thinking level). The loop sends `thinking_level: "off"` by
default. What each provider does with that, per its own documented API:

| Provider | Default request | Why |
|---|---|---|
| DeepSeek (+flash) | `{"thinking":{"type":"disabled"}}` explicit | Server default is effort-high thinking; disabled requests carrying historical `reasoning_content` 400, so history is stripped on the wire copy (durable history untouched). Effort via `reasoning_effort: low/high/max` on opt-in. |
| Moonshot Kimi k2.5/k2.6 | `{"thinking":{"type":"disabled"}}` explicit | Server default is enabled. k2.7-code rejects `disabled` (always thinks) — param omitted there. |
| Ollama native | `"think": false` | Thinking models think by default server-side; explicit false required. GPT-OSS ignores bools — levels (`low/medium/high`) pass through on opt-in. |
| Anthropic 4.x and earlier | param absent | Server default is off; absent never 400s. Opt-in routes adaptive (4.6+) or budget (≤4.5) per official migration table. |
| Anthropic Sonnet 5 / Opus 5 | `{"type":"disabled"}` explicit | Server default is adaptive-ON; absent would think. |
| Anthropic Fable/Mythos 5 | param absent | `disabled` is rejected; thinking cannot be turned off at all. |
| OpenAI/Codex | `none` where supported, else absent | Pre-5.1 models default to medium and don't support `none`; reasoning-native models reason by design — selecting one IS the opt-in. |
| Generic OpenAI-compat (groq/openrouter/...) | zero thinking footprint | Nothing sent; vendor default applies. |
| Gemini (compat endpoint) | nothing sent | No compat param exists to disable server-side thinking (native API uses `thinkingBudget: 0`, unsupported over the compat endpoint). Known limitation, stated not assumed. |
| Claude CLI / Copilot | nothing sent | No thinking params in these transports. |

Rule: absent never 400s; explicit-disabled only where the vendor documents
it; reasoning-native models are opt-in by selection.
