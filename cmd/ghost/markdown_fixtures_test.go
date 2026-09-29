package main

import (
	"strings"
	"testing"
)

// Markdown rendering fixtures: realistic Ghost responses (not isolated
// syntax fragments) exercising the terminal renderer end to end —
// committed renderAssistantBody plus the progressive streamStyler.
// Content assertions run on stripANSI output: Bubble Tea v2 styles
// rune-by-rune, so raw styled text never contains plain substrings.

func renderPlain(t *testing.T, body string, width int) string {
	t.Helper()
	return stripANSI(renderAssistantBody(body, width))
}

func TestMarkdownBasicDocument(t *testing.T) {
	body := `# How Ghost works

Ghost first checks whether it can answer the request directly.

If it needs reasoning or external data, it **escalates** to tools.

## Key points

- *Memory* persists across turns.
- ~~Guessing~~ verification before claiming.
- See [the docs](https://example.com/docs) for setup.

> Evidence wins over eloquence.

---
`
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"How Ghost works", "answer the request directly", "escalates",
		"Key points", "Memory", "Guessing", "verification",
		"the docs", "Evidence wins", "Memory", "persists",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("basic doc lost %q:\n%s", want, plain)
		}
	}
	for _, leaked := range []string{"**", "~~", "[the docs](", "# How"} {
		if strings.Contains(plain, leaked) {
			t.Errorf("basic doc leaked marker %q:\n%s", leaked, plain)
		}
	}
}

func TestMarkdownCodeLanguages(t *testing.T) {
	body := "Here are examples:\n\n```go\nresult := ghost.Process(request)\n```\n\n```javascript\nconst x = await ghost.run(req);\n```\n\n```bash\nghost serve --api-only\n```\n\n```json\n{\"ok\": true}\n```\n\n```yaml\nkey: value\n```\n\n```sql\nSELECT id FROM watches;\n```\n\n```klingon\nnuqneH **not code**\n```\n"
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"result := ghost.Process(request)", "const x = await ghost.run(req)",
		"ghost serve --api-only", `"ok": true`, "key: value",
		"SELECT id FROM watches", "nuqneH **not code**",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("code block lost %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "```") {
		t.Errorf("fences must be concealed:\n%s", plain)
	}
	// Unknown language still renders as code (markers inside are literal).
	if strings.Contains(plain, "klingon") {
		t.Errorf("language tag must be concealed:\n%s", plain)
	}
}

func TestMarkdownNestedListsAndTasks(t *testing.T) {
	body := `- inbox
  - nested **bold** item
    1. deep ordered
    2. second
- [ ] open task
- [x] done task
- [X] also done
`
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"inbox", "nested bold item", "deep ordered", "second",
		"○ open task", "● done task", "● also done",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("lists lost %q:\n%s", want, plain)
		}
	}
}

func TestMarkdownNestedQuotes(t *testing.T) {
	body := `> Single level quote.

>> Nested quote collapses gracefully.

> Quote with a **bold** word and a [link](https://example.com/x).`
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"Single level quote", "Nested quote collapses gracefully",
		"bold", "link",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("quotes lost %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, ">>") {
		t.Errorf("nested markers must collapse:\n%s", plain)
	}
}

func TestMarkdownImagesAutolinksEmail(t *testing.T) {
	body := "See ![architecture diagram](https://example.com/arch.png) and ![](https://example.com/blank.png).\n\nVisit <https://example.com/long/path?q=1> or mail <ops@example.com>.\n\nRaw HTML <div>stays literal</div>."
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"◈ architecture diagram", "◈ image", "https://example.com/long/path?q=1",
		"ops@example.com", "<div>stays literal</div>",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("images/autolinks lost %q:\n%s", plain, want)
		}
	}
	if strings.Contains(plain, "![") || strings.Contains(plain, "arch.png") {
		t.Errorf("image syntax/URL must not leak:\n%s", plain)
	}
}

func TestMarkdownWideTableDegrades(t *testing.T) {
	body := "| Name | Role | Notes |\n| --- | --- | --- |\n| Ghost Reflex | router | picks the **best** tool for the job |\n| Memory | store | durable facts with [provenance](https://example.com) |\n"
	// Full width: every phrase intact, markers concealed.
	plain := renderPlain(t, body, 76)
	for _, want := range []string{"Ghost Reflex", "Memory", "best", "provenance", "picks the", "tool for the job"} {
		if !strings.Contains(plain, want) {
			t.Errorf("width 76 table lost %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "**") || strings.Contains(plain, "[provenance](") {
		t.Errorf("width 76 table leaked markers:\n%s", plain)
	}
	// Narrow: cells wrap mid-phrase (graceful), but no word is lost and
	// no markers leak.
	for _, width := range []int{40, 24} {
		plain := renderPlain(t, body, width)
		for _, want := range []string{"Ghost", "Reflex", "Memory", "best", "provenance", "router", "store"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width %d table lost %q:\n%s", width, want, plain)
			}
		}
		if strings.Contains(plain, "**") || strings.Contains(plain, "[provenance](") {
			t.Errorf("width %d table leaked markers:\n%s", width, plain)
		}
	}
}

func TestMarkdownMermaidFlowchart(t *testing.T) {
	body := "## How Ghost works\n\nGhost first checks whether it can answer the request directly.\n\nIf it needs reasoning or external data, it escalates.\n\n```go\nresult := ghost.Process(request)\n```\n\n```mermaid\nflowchart TD\n    U[User] --> R[Ghost Reflex]\n    R --> M[Memory]\n    R --> T[Tools]\n    T --> E[Evidence]\n```\n\nThe runtime remains authoritative over actions."
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"How Ghost works", "result := ghost.Process(request)",
		"Diagram", "User → Ghost Reflex", "Ghost Reflex → Memory",
		"Ghost Reflex → Tools", "Tools → Evidence",
		"authoritative over actions",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("mermaid doc lost %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "```") || strings.Contains(plain, "-->") || strings.Contains(plain, "[User]") {
		t.Errorf("mermaid syntax must not leak:\n%s", plain)
	}
}

func TestMarkdownMermaidSequence(t *testing.T) {
	body := "```mermaid\nsequenceDiagram\n    participant U as User\n    participant G as Ghost\n    U->>G: watch flight BA123\n    G-->>U: Watching Flight BA123\n    Note over G: polls in background\n```"
	plain := renderPlain(t, body, 76)
	for _, want := range []string{
		"User", "Ghost", "User → Ghost: watch flight BA123",
		"Ghost → User: Watching Flight BA123", "Note: polls in background",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("sequence lost %q:\n%s", want, plain)
		}
	}
}

func TestMarkdownMermaidInvalidAndMalicious(t *testing.T) {
	cases := []struct {
		name string
		body string
		keep []string
	}{
		{
			name: "unknown type shows source",
			body: "```mermaid\nmindmap\n  root((Ghost))\n    child one\n```",
			keep: []string{"Diagram", "mindmap", "Ghost"},
		},
		{
			name: "unclosed fence degrades to code",
			body: "```mermaid\nflowchart TD\n    A --> B",
			keep: []string{"flowchart TD", "A --> B"},
		},
		{
			name: "javascript URL never executes",
			body: "```mermaid\nflowchart TD\n    A[Click](javascript:alert(1)) --> B[ok]\n```",
			keep: []string{"Diagram", "Click", "ok"},
		},
		{
			name: "script tag stays inert",
			body: "```mermaid\nflowchart TD\n    A[<script>alert(1)</script>] --> B[x]\n```",
			keep: []string{"Diagram"},
		},
		{
			name: "empty block",
			body: "```mermaid\n```",
			keep: []string{"Diagram"},
		},
	}
	for _, tc := range cases {
		plain := renderPlain(t, tc.body, 76)
		for _, want := range tc.keep {
			if !strings.Contains(plain, want) {
				t.Errorf("%s lost %q:\n%s", tc.name, want, plain)
			}
		}
		if strings.Contains(plain, "```") {
			t.Errorf("%s leaked fences:\n%s", tc.name, plain)
		}
	}
}

func TestMarkdownStreamingMermaidStable(t *testing.T) {
	// While the fence is open, nothing diagram-shaped may print: no
	// half-panel, no flicker. When it closes, exactly one panel prints.
	sty := streamStyler{width: 76}
	var shown []string
	feed := func(raw string) {
		for _, ln := range sty.line(raw) {
			shown = append(shown, ln)
		}
	}
	feed("```mermaid")
	feed("flowchart TD")
	feed("    A[User] --> B[Ghost]")
	if len(shown) != 0 {
		t.Fatalf("open mermaid fence must buffer silently, got %q", shown)
	}
	feed("```")
	joined := strings.Join(shown, "\n")
	plain := stripANSI(joined)
	if !strings.Contains(plain, "User → Ghost") {
		t.Errorf("closed diagram must render the panel, got %q", plain)
	}
	if n := strings.Count(plain, "Diagram"); n != 1 {
		t.Errorf("panel must print exactly once, got %d in %q", n, plain)
	}
}

func TestMarkdownStreamingMermaidUnclosed(t *testing.T) {
	// Turn ends mid-diagram: buffered source shows as code, nothing lost.
	sty := streamStyler{width: 76}
	for _, ln := range []string{"```mermaid", "flowchart TD", "    A --> B"} {
		sty.line(ln)
	}
	tail := stripANSI(strings.Join(sty.flush(), "\n"))
	if !strings.Contains(tail, "A --> B") {
		t.Errorf("unclosed diagram must flush as code, got %q", tail)
	}
}

func TestMarkdownHostileInputs(t *testing.T) {
	// javascript: URLs are concealed like any link (terminal never fetches).
	plain := renderPlain(t, "[click](javascript:alert(1))", 76)
	if !strings.Contains(plain, "click") {
		t.Errorf("link label must survive:\n%s", plain)
	}
	if strings.Contains(plain, "javascript:") {
		t.Errorf("URL must stay concealed:\n%s", plain)
	}
	// Deep quote nesting collapses without runaway output.
	deep := strings.Repeat(">", 60) + " deep"
	plain = renderPlain(t, deep, 76)
	if !strings.Contains(plain, "deep") {
		t.Errorf("deep quote lost content:\n%s", plain)
	}
	if len(plain) > 400 {
		t.Errorf("deep quote must stay bounded, got %d chars", len(plain))
	}
	// Enormous table renders bounded (grid or fallback, never a hang).
	var sb strings.Builder
	sb.WriteString("| a | b |\n| --- | --- |\n")
	for i := 0; i < 500; i++ {
		sb.WriteString("| row content here | more content |\n")
	}
	plain = renderPlain(t, sb.String(), 76)
	if !strings.Contains(plain, "row content here") {
		t.Errorf("huge table lost rows")
	}
	// Enormous code block stays intact and bounded.
	big := "```\n" + strings.Repeat("x = 1  # padding to force wrapping across many lines here\n", 300) + "```"
	plain = renderPlain(t, big, 76)
	if !strings.Contains(plain, "x = 1") {
		t.Errorf("huge code lost content")
	}
}

// Performance: rendering must stay cheap on large responses. The
// streaming path must never re-render the whole message per token —
// streamStyler.line only ever handles one completed line.

func largeMarkdownDoc() string {
	var sb strings.Builder
	sb.WriteString("# Field report\n\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("## Section with **bold** and `code` and a [link](https://example.com/x)\n\n")
		sb.WriteString("Prose with *emphasis* and ~~struck~~ words that wrap across the width.\n\n")
		sb.WriteString("- item one\n- item two with **bold**\n\n")
		sb.WriteString("```go\nresult := ghost.Process(request)\n```\n\n")
	}
	sb.WriteString("| a | b |\n| --- | --- |\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("| cell content | more **styled** content |\n")
	}
	return sb.String()
}

func BenchmarkRenderAssistantBodyLarge(b *testing.B) {
	doc := largeMarkdownDoc()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderAssistantBody(doc, 76)
	}
}

func BenchmarkStreamStylerLine(b *testing.B) {
	sty := streamStyler{width: 76}
	line := "Prose with **bold** and `code` and a [link](https://example.com/x) here."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sty.line(line)
	}
}

func BenchmarkRenderMermaidBlock(b *testing.B) {
	lines := []string{"flowchart TD"}
	for i := 0; i < 100; i++ {
		lines = append(lines, "A[Node] --> B[Other node here]")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderMermaidBlock(lines, 76)
	}
}
