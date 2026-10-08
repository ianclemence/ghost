package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// What Ghost did while it worked, as the terminal reads it.
//
// The Pod reports each tool call as it starts and as it ends (tool_start,
// tool_result), with what it was about, whether it worked and how long it took.
// The terminal prints one quiet row per finished step into the scrollback, the
// way the phone lists them in its run sheet, so a long task shows its work
// without a wall of output and a simple answer shows none at all:
//
//	✓ Searched the web  nairobi bar culture                    1.2s
//	✓ Ran a command     php -v                                 0.3s
//	✗ Ran a command     composer install                       4.1s
//	  ╰ exit status 1
//
// Nothing here is the model's reasoning: only what ran, what it was about,
// how it ended and how long it took.

// stepRowCap is how many step rows a turn prints before the rest are counted
// instead (and summarised once the turn ends). /details lifts it.
const stepRowCap = 8

// stepKindOf groups tools the way the phone does.
func stepKindOf(tool string) string {
	t := strings.ToLower(strings.TrimSpace(tool))
	switch {
	case t == "web_search":
		return "search"
	case t == "web_fetch":
		return "page"
	case strings.HasPrefix(t, "browser_") || t == "screenshot" || strings.HasPrefix(t, "computer_"):
		return "browser"
	case t == "exec" || t == "sandbox" || t == "spawn":
		return "command"
	case t == "remember" || strings.HasPrefix(t, "memory_") || t == "context_get" || t == "session_search" || t == "oracle":
		return "memory"
	case t == "read_file" || t == "write_file" || t == "edit_file" || t == "append_file" || t == "list_dir" || t == "doc_parser":
		return "files"
	}
	return "other"
}

var stepPast = map[string]string{
	"web_search": "Searched the web", "web_fetch": "Read a page",
	"browser_navigate": "Opened a page", "browser_click": "Clicked", "browser_type": "Typed",
	"browser_fill": "Filled a field", "browser_fill_form": "Filled in a form", "browser_press": "Pressed a key",
	"browser_scroll": "Scrolled", "browser_snapshot": "Read the page", "browser_screenshot": "Captured the page",
	"browser_submit": "Submitted",
	"exec":           "Ran a command", "sandbox": "Ran a command", "spawn": "Ran a command",
	"read_file": "Read a file", "write_file": "Wrote a file", "edit_file": "Edited a file", "append_file": "Wrote a file",
	"list_dir": "Looked through files", "schedule": "Set a reminder", "weather_now": "Checked the weather",
	"calendar": "Checked your calendar", "email_search": "Looked through your email",
	"vision": "Looked at an image", "image_generate": "Made an image", "canvas": "Built a page",
}

var stepPresent = map[string]string{
	"web_search": "Searching the web", "web_fetch": "Reading a page",
	"browser_navigate": "Opening a page", "browser_click": "Clicking", "browser_type": "Typing",
	"browser_fill": "Filling a field", "browser_fill_form": "Filling in a form", "browser_press": "Pressing a key",
	"browser_scroll": "Scrolling", "browser_snapshot": "Reading the page", "browser_screenshot": "Capturing the page",
	"browser_submit": "Submitting",
	"exec":           "Running a command", "sandbox": "Running a command", "spawn": "Running a command",
	"read_file": "Reading a file", "write_file": "Writing a file", "edit_file": "Editing a file", "append_file": "Writing a file",
	"list_dir": "Looking through files", "schedule": "Setting a reminder", "weather_now": "Checking the weather",
	"calendar": "Checking your calendar", "email_search": "Looking through your email",
	"vision": "Looking at an image", "image_generate": "Making an image", "canvas": "Building a page",
}

var kindPast = map[string]string{
	"search": "Searched the web", "page": "Read a page", "browser": "Used the browser", "command": "Ran a command",
	"memory": "Checked memory", "files": "Used your files", "other": "Used a tool",
}
var kindPresent = map[string]string{
	"search": "Searching the web", "page": "Reading a page", "browser": "Using the browser", "command": "Running a command",
	"memory": "Checking memory", "files": "Working with your files", "other": "Working on it",
}

// stepTitle is the step's own words: past tense once it ended, present while it runs.
func stepTitle(tool string, running bool) string {
	t := strings.ToLower(strings.TrimSpace(tool))
	if running {
		if s, ok := stepPresent[t]; ok {
			return s
		}
		return kindPresent[stepKindOf(t)]
	}
	if s, ok := stepPast[t]; ok {
		return s
	}
	return kindPast[stepKindOf(t)]
}

// fmtStepDur reads like a person would say it: "<1s", "3.2s", "14s", "1m 19s".
func fmtStepDur(d time.Duration) string {
	if d < 0 {
		return ""
	}
	if d < 950*time.Millisecond {
		return "<1s"
	}
	s := d.Seconds()
	switch {
	case s < 10:
		return fmt.Sprintf("%.1fs", s)
	case s < 60:
		return fmt.Sprintf("%.0fs", s)
	}
	m := int(s) / 60
	return fmt.Sprintf("%dm %02ds", m, int(s+0.5)-m*60)
}

func stepPhrase(kind string, n int) string {
	switch kind {
	case "search":
		if n == 1 {
			return "searched the web"
		}
		return fmt.Sprintf("searched the web %d times", n)
	case "page":
		if n == 1 {
			return "read a page"
		}
		return fmt.Sprintf("read %d pages", n)
	case "browser":
		return "browsed the web"
	case "command":
		if n == 1 {
			return "ran a command"
		}
		return fmt.Sprintf("ran %d commands", n)
	case "memory":
		return "checked memory"
	case "files":
		if n == 1 {
			return "used a file"
		}
		return fmt.Sprintf("used %d files", n)
	}
	if n == 1 {
		return "used a tool"
	}
	return fmt.Sprintf("used %d tools", n)
}

// summarizeSteps is the one line for a run: "Searched the web, ran 2 commands
// (1 failed)". Kinds appear in the order they first happened; a failure is
// named on the kind that failed. Capped at three phrases.
func summarizeSteps(steps []toolStep) string {
	var order []string
	count := map[string]int{}
	failed := map[string]int{}
	for _, s := range steps {
		if s.id == "" && s.detail == "" && s.tool == "phase" {
			continue // a phase word is not a step
		}
		k := stepKindOf(s.tool)
		if count[k] == 0 {
			order = append(order, k)
		}
		count[k]++
		if s.failed {
			failed[k]++
		}
	}
	if len(order) == 0 {
		return ""
	}
	var parts []string
	for i, k := range order {
		if i >= 3 {
			break
		}
		text := stepPhrase(k, count[k])
		if f := failed[k]; f > 0 {
			if f == count[k] && f > 1 {
				text += " (all failed)"
			} else {
				text += fmt.Sprintf(" (%d failed)", f)
			}
		}
		parts = append(parts, text)
	}
	line := strings.Join(parts, ", ")
	if rest := len(order) - len(parts); rest > 0 {
		line += fmt.Sprintf(", and %d more", rest)
	}
	return strings.ToUpper(line[:1]) + line[1:]
}

var (
	styleStepOK     = lipgloss.NewStyle().Foreground(cFaint)
	styleStepFail   = lipgloss.NewStyle().Foreground(cErr)
	styleStepTitle  = lipgloss.NewStyle().Foreground(cMuted)
	styleStepDetail = lipgloss.NewStyle().Foreground(cFaint)
)

// stepRow draws one finished step: status, title, what it was about, and how
// long it took, with the time flush right. A failure adds a second line saying
// why. expanded (/details) wraps the whole detail instead of cutting it, so a
// long command can be read in full.
func stepRow(s toolStep, width int, expanded bool) string {
	const lead = "  "
	glyph, gs := "✓", styleStepOK
	titleStyle := styleStepTitle
	if s.failed {
		glyph, gs = "✗", styleStepFail
		titleStyle = styleStepFail
	}
	title := stepTitle(s.tool, false)
	dur := fmtStepDur(s.dur)
	// Column for the title keeps the details aligned down the scrollback.
	const titleCol = 20
	head := lead + gs.Render(glyph) + " " + titleStyle.Render(padCells(title, titleCol))
	headW := lipgloss.Width(lead) + 1 + 1 + titleCol
	right := dur
	room := width - headW - 2 - lipgloss.Width(right) - 2
	if room < 8 {
		room = 8
	}
	var b strings.Builder
	detail := strings.TrimSpace(s.detail)
	if detail == "" || expanded && lipgloss.Width(detail) > room {
		// Nothing to say, or the whole detail goes beneath in /details.
		b.WriteString(head)
		b.WriteString(strings.Repeat(" ", maxInt(1, width-headW-lipgloss.Width(right))))
		b.WriteString(styleStepDetail.Render(right))
		if detail != "" {
			for _, ln := range wrapText(detail, width-len(lead)-4) {
				b.WriteString("\n" + lead + "  " + styleStepDetail.Render(ln))
			}
		}
	} else {
		shown := cellTruncate(detail, room)
		pad := width - headW - lipgloss.Width(shown) - lipgloss.Width(right)
		b.WriteString(head + styleStepDetail.Render(shown) + strings.Repeat(" ", maxInt(1, pad)) + styleStepDetail.Render(right))
	}
	if s.failed && strings.TrimSpace(s.note) != "" {
		b.WriteString("\n" + lead + "  " + styleStepFail.Render("╰ "+cellTruncate(strings.TrimSpace(s.note), width-len(lead)-6)))
	}
	return b.String()
}

// padCells pads or cuts s to exactly w cells.
func padCells(s string, w int) string {
	s = cellTruncate(s, w)
	if n := w - lipgloss.Width(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// hiddenStepsLine is what a turn says, once, about the rows it did not print.
// It never runs past the terminal: the summary is cut before the hint is.
func hiddenStepsLine(steps []toolStep, hidden int, total time.Duration, width int) string {
	if hidden <= 0 {
		return ""
	}
	line := fmt.Sprintf("  … %d more step", hidden)
	if hidden != 1 {
		line += "s"
	}
	if total > 0 {
		line += " · " + fmtStepDur(total)
	}
	const hint = "  /details shows all"
	if s := summarizeSteps(steps); s != "" {
		line += " · " + s
	}
	if lipgloss.Width(line)+lipgloss.Width(hint) <= width {
		line += hint
	}
	return styleStepDetail.Render(cellTruncate(line, width))
}

// ─── the model's side of it ──────────────────────────────────────────────

// beginStep records a call that has started. The step is not printed yet: a
// row says how it ended and how long it took, so it waits for its result.
func (m *agentTUI) beginStep(e toolStartMsg) {
	m.stepEvents = true
	m.podReportsPickup = true
	for _, s := range m.toolHistory {
		if s.id == e.id {
			return
		}
	}
	// The older bare-label steps (a Pod that also sends them, or a phase word)
	// are closed: this call is what Ghost is doing now.
	now := time.Now()
	for i := range m.toolHistory {
		if !m.toolHistory[i].done && m.toolHistory[i].id == "" {
			m.toolHistory[i].done = true
			m.toolHistory[i].dur = now.Sub(m.toolHistory[i].start)
		}
	}
	m.toolHistory = append(m.toolHistory, toolStep{id: e.id, tool: e.tool, detail: e.detail, label: stepTitle(e.tool, true), start: now})
	m.toolCount = len(m.toolHistory)
	m.toolLine = stepTitle(e.tool, true)
}

// finishStep closes a call with how it ended, and prints its row.
func (m *agentTUI) finishStep(e toolResultMsg) tea.Cmd {
	for i := range m.toolHistory {
		if m.toolHistory[i].id != e.id {
			continue
		}
		s := &m.toolHistory[i]
		if s.printed {
			return nil // a repeated result for a step already shown
		}
		s.done = true
		s.failed = !e.ok
		s.note = e.note
		s.dur = e.dur
		if s.dur <= 0 {
			s.dur = time.Since(s.start)
		}
		return m.printStep(i)
	}
	return nil
}

// printStep puts one step's row in the scrollback, once. Past stepRowCap a turn
// counts the rest instead (a long task would otherwise scroll the answer away);
// /details prints every one.
func (m *agentTUI) printStep(i int) tea.Cmd {
	s := &m.toolHistory[i]
	if s.printed || s.id == "" {
		return nil
	}
	s.printed = true
	if !m.showTools && m.stepsPrinted >= stepRowCap {
		m.stepsHidden++
		return nil
	}
	m.stepsPrinted++
	return printCmd(stepRow(*s, m.textWidth(), m.showTools))
}

// noteSteer says what became of messages sent into the running turn.
func (m *agentTUI) noteSteer(e steerMsg) {
	m.podReportsPickup = true
	if !e.picked {
		return // handed back unread: they stay queued and go out next
	}
	for _, c := range e.contents {
		for i, q := range m.queued {
			if strings.TrimSpace(q) == strings.TrimSpace(c) {
				m.queued = append(m.queued[:i], m.queued[i+1:]...)
				m.append(entry{kind: entryNotice, text: "✓ picked up: " + cellTruncate(strings.Join(strings.Fields(c), " "), 60)})
				break
			}
		}
	}
}

// settleQueue decides what happens to messages typed while the turn ran, once
// it is over: those Ghost read are done with; the rest go out next, one per
// turn, in the order they were typed. On a Pod that never reports pickup (an
// older one) a message it accepted was read, as before. It returns the message
// to send now, or "".
func (m *agentTUI) settleQueue(clean bool) string {
	if len(m.queued) == 0 {
		return ""
	}
	if !m.podReportsPickup {
		if clean {
			m.queued = nil
		}
		return ""
	}
	next := m.queued[0]
	m.queued = m.queued[1:]
	return next
}

// turnEventMsg turns a turn event from the in-process agent into a message the
// terminal understands. nil for anything it does not draw.
func turnEventMsg(ev map[string]interface{}) tea.Msg {
	str := func(k string) string { s, _ := ev[k].(string); return s }
	switch str("type") {
	case "tool_start":
		return toolStartMsg{id: str("id"), tool: str("tool"), detail: str("detail")}
	case "tool_result":
		ok, _ := ev["ok"].(bool)
		var ms int64
		switch v := ev["ms"].(type) {
		case int64:
			ms = v
		case int:
			ms = int64(v)
		case float64:
			ms = int64(v)
		}
		return toolResultMsg{id: str("id"), ok: ok, dur: time.Duration(ms) * time.Millisecond, note: str("note")}
	case "steer_picked":
		contents, _ := ev["contents"].([]string)
		return steerMsg{contents: contents, picked: true}
	}
	return nil
}
