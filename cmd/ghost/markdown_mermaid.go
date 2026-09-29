package main

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// Terminal Mermaid fallback.
//
// A normal terminal cannot render SVG, so a fenced ```mermaid block is
// rendered as a deterministic text panel instead of raw diagram syntax.
// The mobile app renders the real diagram; the terminal shows the same
// semantic content (nodes and relationships) in readable form.
//
// Deliberately not a pseudo-graphic renderer: flowcharts and sequence
// diagrams collapse to `A → B` relationship rows (the information the
// diagram carries), and anything else shows the capped source lines so
// nothing disappears. Malicious or malformed input degrades to the same
// capped source — the terminal never fetches, executes, or interprets
// anything in the block.

// maxMermaidRows bounds the panel: a 500-line generated diagram must not
// flood the transcript.
const maxMermaidRows = 30

// maxMermaidSourceLines caps how many raw source lines an unparseable
// block may show.
const maxMermaidSourceLines = 12

// fenceLanguage extracts the info string of a ``` fence line
// ("```mermaid" → "mermaid", "```" → "").
func fenceLanguage(trim string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(trim, "```"))
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

func isMermaidFence(lang string) bool { return lang == "mermaid" }

// mermaidDiagramType names the diagram from its first meaningful line.
func mermaidDiagramType(lines []string) string {
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) == 0 {
			continue
		}
		return strings.ToLower(fields[0])
	}
	return ""
}

var (
	// sequenceMsg matches A->>B: text, A-->B: text, A->B: text.
	sequenceMsgRe  = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*-{1,2}>+\s*([A-Za-z0-9_]+)\s*:?\s*(.*)$`)
	sequenceNote   = regexp.MustCompile(`^(?i)note\s+(.*)$`)
	sequenceNoteOn = regexp.MustCompile(`^(?i)(?:over|right of|left of)\s+\S+:\s*(.*)$`)
	sequencePart   = regexp.MustCompile(`^(?i)(?:participant|actor)\s+(\S+)(?:\s+as\s+(.+))?$`)
)

// splitFlowEdge splits a flowchart relationship line around its arrow
// operator (`-->`, `---`, `==>`, `-.->`, `A-- label -->B`). Node tokens
// may carry bracketed labels with spaces (`R[Ghost Reflex]`), so this is
// a small parser rather than a single regex.
func splitFlowEdge(t string) (from, to string, ok bool) {
	for _, op := range []string{"-->", "==>", "-.->", "---"} {
		i := strings.Index(t, op)
		if i < 0 {
			continue
		}
		from = strings.TrimSpace(t[:i])
		to = strings.TrimSpace(t[i+len(op):])
		// `A-- label -->B`: the label rides on the left half; drop it.
		if j := strings.LastIndex(from, "--"); j >= 0 {
			from = strings.TrimSpace(from[:j])
		}
		// Trailing `|label|` on the target.
		if k := strings.Index(to, "|"); k >= 0 {
			if end := strings.LastIndex(to, "|"); end > k {
				to = strings.TrimSpace(to[:k] + to[end+1:])
			}
		}
		if from == "" || to == "" {
			return "", "", false
		}
		return from, to, true
	}
	return "", "", false
}

// mermaidNodeLabel resolves a flowchart node token to its display label:
// `A[User]` → "User", `B` → "B". Labels are truncated so a hostile block
// cannot stretch the panel.
func mermaidNodeLabel(tok string, labels map[string]string) string {
	tok = strings.TrimSpace(tok)
	// Strip edge decorations: `A[Label]`, `B(Label)`, `C{Label}`,
	// `D[[Label]]`, `E>Label]`, `F[/Label/]`, `G[\Label\]`, `H((Label))`.
	if i := strings.IndexAny(tok, "[({>"); i > 0 {
		id := tok[:i]
		rest := tok[i:]
		label := strings.Trim(rest, "[{()}>]/\\ ")
		label = strings.TrimSuffix(label, "]")
		label = strings.TrimSuffix(label, ")")
		label = strings.TrimSuffix(label, "}")
		label = strings.TrimSpace(label)
		if label == "" {
			label = id
		}
		if _, ok := labels[id]; !ok {
			labels[id] = label
		}
		return truncateCell(label, 40)
	}
	if label, ok := labels[tok]; ok {
		return truncateCell(label, 40)
	}
	return truncateCell(tok, 40)
}

// truncateCell shortens s to at most w cells for panel content.
func truncateCell(s string, w int) string { return cellTruncate(s, w) }

// mermaidFlowchartRows collapses a flowchart to relationship rows.
func mermaidFlowchartRows(lines []string) []string {
	labels := map[string]string{}
	var rows []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		lower := strings.ToLower(t)
		if strings.HasPrefix(lower, "flowchart") || strings.HasPrefix(lower, "graph") {
			continue
		}
		// Style/class directives carry no relationships.
		if strings.HasPrefix(lower, "style ") || strings.HasPrefix(lower, "class") ||
			strings.HasPrefix(lower, "click ") || strings.HasPrefix(lower, "subgraph") ||
			t == "end" {
			continue
		}
		from, to, ok := splitFlowEdge(t)
		if !ok {
			continue
		}
		rows = append(rows, mermaidNodeLabel(from, labels)+" → "+mermaidNodeLabel(to, labels))
		if len(rows) >= maxMermaidRows {
			break
		}
	}
	return rows
}

// mermaidSequenceRows collapses a sequence diagram to message rows.
// Participant aliases (`participant U as User`) resolve in messages so
// rows read as display names, not single-letter IDs.
func mermaidSequenceRows(lines []string) []string {
	names := map[string]string{}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if m := sequencePart.FindStringSubmatch(t); m != nil {
			name := m[1]
			if m[2] != "" {
				name = strings.TrimSpace(m[2])
			}
			names[strings.ToUpper(m[1])] = name
		}
	}
	disp := func(id string) string {
		if n, ok := names[strings.ToUpper(id)]; ok {
			return truncateCell(n, 30)
		}
		return truncateCell(id, 30)
	}
	var rows []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		lower := strings.ToLower(t)
		if strings.HasPrefix(lower, "sequencediagram") {
			continue
		}
		if m := sequencePart.FindStringSubmatch(t); m != nil {
			name := m[1]
			if m[2] != "" {
				name = strings.TrimSpace(m[2])
			}
			rows = append(rows, truncateCell(name, 40))
			continue
		}
		if m := sequenceNote.FindStringSubmatch(t); m != nil {
			note := strings.TrimSpace(m[1])
			// `Note over G: text` → `Note: text` (the placement is a
			// visual concern the text panel cannot show).
			if on := sequenceNoteOn.FindStringSubmatch(note); on != nil {
				note = strings.TrimSpace(on[1])
			}
			rows = append(rows, truncateCell("Note: "+note, 60))
			continue
		}
		if strings.HasPrefix(lower, "loop ") || strings.HasPrefix(lower, "alt ") ||
			strings.HasPrefix(lower, "else") || t == "end" ||
			strings.HasPrefix(lower, "opt ") || strings.HasPrefix(lower, "par ") ||
			strings.HasPrefix(lower, "rect ") || strings.HasPrefix(lower, "autonumber") {
			continue
		}
		m := sequenceMsgRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		row := disp(m[1]) + " → " + disp(m[2])
		if msg := strings.TrimSpace(m[3]); msg != "" {
			row += ": " + truncateCell(msg, 60)
		}
		rows = append(rows, row)
		if len(rows) >= maxMermaidRows {
			break
		}
	}
	return rows
}

// renderMermaidBlock renders a complete ```mermaid block as a bordered
// text panel. Returns lines without the reply inset (callers add it).
func renderMermaidBlock(lines []string, width int) []string {
	typ := mermaidDiagramType(lines)
	var rows []string
	switch typ {
	case "flowchart", "graph":
		rows = mermaidFlowchartRows(lines)
	case "sequencediagram":
		rows = mermaidSequenceRows(lines)
	}
	if len(rows) == 0 {
		// Unknown type, unparseable content, or hostile input: show the
		// capped source so the block stays readable instead of vanishing.
		for _, ln := range lines {
			t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "%%"))
			if t == "" {
				continue
			}
			rows = append(rows, truncateCell(t, width-4))
			if len(rows) >= maxMermaidSourceLines {
				break
			}
		}
	}
	if len(rows) == 0 {
		rows = []string{"(empty diagram)"}
	}
	title := "Diagram"
	if typ != "" {
		title = "Diagram · " + typ
	}
	return renderPanel(title, rows, width)
}

// renderPanel renders a titled bordered panel, width-aware.
func renderPanel(title string, rows []string, width int) []string {
	if width < 20 {
		width = 20
	}
	inner := width - 4 // "│ " + " │"
	if inner < 10 {
		inner = 10
	}
	var out []string
	head := "┌─ " + title + " "
	if lipgloss.Width(head)+1 > width {
		head = "┌─ " + truncateCell(title, width-6) + " "
	}
	fill := width - lipgloss.Width(head) - 1
	if fill < 0 {
		fill = 0
	}
	out = append(out, styleMDTableBorder.Render(head+strings.Repeat("─", fill)+"┐"))
	for _, r := range rows {
		for _, wl := range wrapText(r, inner) {
			pad := inner - lipgloss.Width(wl)
			if pad < 0 {
				pad = 0
			}
			out = append(out, styleMDTableBorder.Render("│")+" "+styleAssistant.Render(wl+strings.Repeat(" ", pad))+" "+styleMDTableBorder.Render("│"))
		}
	}
	out = append(out, styleMDTableBorder.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return out
}
