# Markdown & diagrams

How Ghost renders model responses. One canonical Markdown response flows
from the runtime to every surface; each surface interprets it its own way.

    model → canonical Markdown response → terminal renderer
                                        → mobile renderer
                                        → web console renderer

The model output is never rewritten per platform.

## Response style contract

Ghost is instructed to use **the simplest formatting that materially
improves the answer** (`pkg/agent/context.go`, "Response Style"):

- A one-sentence answer stays one sentence — no headings, bullets, bold,
  tables, dividers, or code fences.
- Tables only for genuine comparisons across shared columns.
- Headings only for multi-part answers with real sections.
- Mermaid diagrams only when a visual relationship genuinely helps (or the
  owner asks for one).

This is the product rule that keeps the renderer's capability from turning
into decoration.

## Supported Markdown

| Construct | Terminal | Mobile | Web console |
|---|---|---|---|
| Paragraphs, soft breaks | ✓ | ✓ | ✓ |
| Headings H1–H6 | ✓ | ✓ | H1–H4 |
| Bold / italic / bold+italic | ✓ (`**`, `*`) | ✓ | ✓ (H1–H4 blocks) |
| Strikethrough | ✓ | ✓ | ✓ |
| Inline code | ✓ | ✓ | ✓ |
| Fenced code + language label | ✓ (label concealed) | ✓ (label + copy) | ✓ |
| Indented code | ✓ | ✓ | ✓ |
| Unordered / ordered lists | ✓ | ✓ | ✓ |
| Nested lists | ✓ (indent preserved) | ✓ | ✓ (flattened) |
| Task lists (`- [ ]`, `- [x]`) | ✓ (`○` / `●`) | ✓ (`☐` / `☑`) | ✓ |
| Blockquotes (+ nested) | ✓ (collapses depth) | ✓ | ✓ (collapses depth) |
| Tables | ✓ (grid, degrades when narrow) | ✓ (horizontal scroll) | ✓ (horizontal scroll) |
| Horizontal rules | ✓ | ✓ | ✓ |
| Links | label only, URL concealed | tappable, in-app sheet | ✓ `rel=noopener` |
| Autolinks `<url>`, `<email>` | ✓ | ✓ (linkify) | ✓ bare URLs |
| Images | alt-text badge (never fetched) | ✓ https-only, tap-to-view | alt-text badge |
| Inline HTML | left literal | not executed (`html: false`) | escaped |
| Mermaid | text panel (see below) | real diagram | code block |

## Streaming

Responses stream token by token, so a construct is usually incomplete when
it first arrives. Every surface holds incomplete constructs instead of
flashing raw syntax:

- **Terminal** (`streamStyler`, `cmd/ghost/agent_tui.go`): completed lines
  render immediately; an unclosed inline span is held and joined to its
  continuation; table rows buffer until the block resolves; a mermaid
  fence buffers entirely and prints one panel when it closes.
- **Mobile** (`lib/streaming.ts`): unclosed fences, tables without a
  separator, unclosed `$$` math, and dangling links are held back;
  unclosed inline markers are auto-closed. A closed fence's own ` ``` `
  line is never mistaken for an unclosed code span.
- **Web console**: content is rendered after completion only.

## Mermaid

Diagram source is untrusted model output.

**Mobile** renders real diagrams: a WebView loads a pinned Mermaid build
(`mermaid@11.4.1`) with `securityLevel: 'strict'`, `htmlLabels: false`, and
no clickable links; the WebView cannot navigate anywhere (every load is
denied, file access and DOM storage off) and posts its height back. Any
failure — parse error, CDN unreachable, 15s timeout — falls back to the
code block. Sources over 20,000 characters never reach the renderer.

**Terminal** cannot render SVG. A `mermaid` fence becomes a bordered text
panel that preserves the information the diagram carries:

    ┌─ Diagram · flowchart ────────────┐
    │ User → Ghost Reflex              │
    │ Ghost Reflex → Memory            │
    │ Ghost Reflex → Tools             │
    │ Tools → Evidence                 │
    └──────────────────────────────────┘

Flowcharts and sequence diagrams collapse to `A → B` relationship rows
(sequence participants resolve through `participant X as Name` aliases).
Any other diagram type — and any malformed or hostile block — shows capped
source lines (12) so nothing disappears. Diagrams are capped at 30 rows.
This is a deliberate, documented limitation: no fragile pseudo-graphics.

Supported diagram types: `flowchart`/`graph`, `sequenceDiagram`. Others
degrade to readable source. Unknown languages always render as code.

## Safety

Model output is untrusted everywhere.

- **Terminal**: no network access, no execution; URLs are concealed and
  never opened; HTML is literal text; huge tables/code/quotes are bounded.
- **Mobile**: `html: false`; only `https:` links open (validated in
  `lib/link-policy.ts`), in an in-app browser sheet, so `javascript:`,
  `data:`, `file:`, `intent:`, `tel:` are refused; only `https:` images
  load; Mermaid is sandboxed as above.
- **Web console**: escape-first with rendered fragments stashed behind
  placeholders, so markup can never be re-matched into an attribute;
  quotes are escaped and URLs are cut at whitespace/quotes and must be
  `http(s)`; links carry `target="_blank" rel="noopener"`.

## Fixtures and tests

| Suite | Location | What it covers |
|---|---|---|
| Terminal fixtures | `cmd/ghost/markdown_fixtures_test.go` | realistic documents, code languages, lists/tasks, nested quotes, images/autolinks, wide tables, Mermaid (valid, sequence, invalid, hostile, streaming), bounded hostile input |
| Terminal span/wrap | `cmd/ghost/tui_span_wrap_test.go` | marker concealment across wrap boundaries |
| Terminal benchmarks | `cmd/ghost/markdown_fixtures_test.go` | large document, stream line, Mermaid panel |
| Web console | `cmd/ghost-web/web/js/markdown.test.mjs` + `cmd/ghost-web/markdown_test.go` | executable renderer checks + source invariants |
| Mobile helpers | `ghost-app/lib/{mermaid,link-policy,markdown,streaming}.test.ts` | diagram safety, URL policy, task markers, streaming hold-back |
| Mobile cost | `ghost-app/scripts/bench-markdown.ts` | parse cost per response size |
| Formatting choices | `pkg/golden/formatting.go` (fmt-01..fmt-06) | restraint and appropriateness |
| Response calibration | `scripts/jev/fixtures.json` | formatting/omission/diagram honesty |

### Adding a Markdown case

- **Terminal**: add a realistic document to `TestMarkdown*` in
  `cmd/ghost/markdown_fixtures_test.go`; assert on `stripANSI` output
  (styling splits text rune-by-rune).
- **Mobile**: add pure-logic cases next to the helper you exercise; if the
  behaviour lives in a component, move the decision into `lib/` first.
- **Web**: extend `web/js/markdown.test.mjs` (it evaluates the real
  `components.js`).
- **Formatting choice**: add a case to `pkg/golden/formatting.go` and bump
  `SuiteVersion`.
