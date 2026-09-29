// Behavioral test for the web console's markdown renderer (GhostUI.md).
//
// The console ships plain browser scripts with no test harness, so this
// evaluates the real components.js in a sandbox that stubs just the DOM
// bits the IIFE touches at load time, then exercises md() directly.
//
// Run: bun cmd/ghost-web/web/js/markdown.test.mjs
// (cmd/ghost-web/markdown_test.go runs it automatically when bun exists.)

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, "components.js"), "utf8");

// Minimal DOM surface: the IIFE only defines functions until something is
// constructed, and md() touches no DOM at all.
globalThis.document = { createElement: () => ({ style: {}, appendChild() {}, setAttribute() {} }) };
globalThis.Node = class Node {};

let md;
try {
  // The script declares `const GhostUI = ...`; run it in this scope so the
  // binding is reachable, then pin it to globalThis for the assertions.
  const run = new Function(src + "; globalThis.__GhostUI = GhostUI;");
  run();
  md = globalThis.__GhostUI.md;
} catch (err) {
  console.error("could not load components.js:", err);
  process.exit(1);
}
if (typeof md !== "function") {
  console.error("GhostUI.md is not a function");
  process.exit(1);
}

const failures = [];
function check(name, cond, detail) {
  if (!cond) failures.push(`${name}${detail ? " — " + detail : ""}`);
}

// ── Safety ────────────────────────────────────────────────────────────────
const xss = md('# Hi\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>');
check("raw script escaped", !xss.includes("<script>"), xss);
check("raw img escaped", !xss.includes("<img"), xss);
check("escaped payload retained as text", xss.includes("&lt;script&gt;"), xss);

const jsUrl = md("[click](javascript:alert(1))");
check("javascript: never becomes an anchor", !jsUrl.includes("<a"), jsUrl);
check("javascript: label kept as text", jsUrl.includes("click"), jsUrl);

const evilHref = md('[x](https://a.com" onmouseover="alert(1))');
check("attribute breakout is impossible", !evilHref.includes("onmouseover"), evilHref);

// ── Inline correctness ────────────────────────────────────────────────────
check("inline code is literal", md("run `**not bold**` now").includes("<code>**not bold**</code>"), md("run `**not bold**` now"));
check("markdown link in code is literal", md("`[x](https://y.com)`").includes("<code>[x](https://y.com)</code>"), md("`[x](https://y.com)`"));
check("bold", md("a **b** c").includes("<strong>b</strong>"));
check("italic", md("a *b* c").includes("<em>b</em>"));
check("strikethrough", md("~~gone~~").includes("<del>gone</del>"));
check("explicit link", md("[docs](https://example.com)").includes('href="https://example.com"'));
check("bare url autolinked", md("see https://example.com/x now").includes('<a href="https://example.com/x"'), md("see https://example.com/x now"));
check("url not double-linked", (md("[a](https://x.com)").match(/<a /g) || []).length === 1, md("[a](https://x.com)"));

// ── Images never fetch ────────────────────────────────────────────────────
const img = md("![architecture](https://example.com/a.png)");
check("image renders as alt badge", img.includes("md-image") && img.includes("architecture"), img);
check("image url never emitted", !img.includes("a.png"), img);

// ── Block structure ───────────────────────────────────────────────────────
check("heading", md("## Title").includes("<h2>Title</h2>"));
check("unordered list", md("- one\n- two").includes("<ul><li>one</li><li>two</li></ul>"), md("- one\n- two"));
check("ordered list", md("1. one\n2. two").includes("<ol><li>one</li>"), md("1. one"));
check("task open", md("- [ ] todo").includes('class="task">☐ todo'), md("- [ ] todo"));
check("task done", md("- [x] done").includes('class="task done">☑ done'), md("- [x] done"));
check("nested quote collapses", md(">> deep").includes("<blockquote><p>deep</p></blockquote>"), md(">> deep"));
check("code fence", md("```go\nx := 1\n```").includes("<pre><code>x := 1"), md("```go\nx := 1\n```"));
check("table", md("| a | b |\n| --- | --- |\n| 1 | 2 |").includes("<table>"));
check("hr", md("---").includes("<hr />"));

// ── Formatting restraint is a model concern, not a renderer one: the
// renderer must not invent structure for plain prose. ──────────────────────
const plain = md("Yes, that works. The issue is the embedding call.");
check("plain prose stays a paragraph", plain === "<p>Yes, that works. The issue is the embedding call.</p>", plain);

if (failures.length) {
  console.error("web markdown failures:");
  for (const f of failures) console.error("  ✗ " + f);
  process.exit(1);
}
console.log("web markdown: all checks passed");
