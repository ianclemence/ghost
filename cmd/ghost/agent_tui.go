package main

// Ghost Agent CLI — the interactive terminal surface.
//
// Talk to Ghost, watch it work, approve what it asks, and see where each
// answer came from. Inspired by the pi coding agent's terminal UX, but built
// around Ghost's own semantics: provenance ("where did this run"), memory,
// routines, and inline approvals.
//
// See docs/AGENT-CLI.md for the behavior contract.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── messages ─────────────────────────────────────────────────────────────

// agentProgram is the running tea.Program, set at launch so the turn goroutine
// can push streamed events back into the UI.
var agentProgram *tea.Program

// streamChunkMsg carries one streamed token/segment from the agent.
type streamChunkMsg struct{ text string }

// toolCallMsg reports the model invoking a tool (label is product language).
type toolCallMsg struct{ tool, label string }

// toolProgressMsg is toolCallMsg for turns that run on the daemon: the
// server-side label arrives complete, so it bypasses local relabeling.
type toolProgressMsg struct{ tool, label string }

// clarifyRequestMsg surfaces an in-flight clarification question (the same
// event the mobile app renders as an interactive card) so the terminal can
// answer it in-band instead of hanging until the tool times out.
type clarifyRequestMsg struct {
	questionID string
	question   string
	choices    []string
}

// spinnerTickMsg advances the working spinner (pi-style activity pulse).
type spinnerTickMsg struct{}

// turnDoneMsg carries the final response of a turn.
type turnDoneMsg struct {
	text string
	err  error
}

// ─── transcript entries ──────────────────────────────────────────────────

type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryTool
	entryNotice
	entryError
)

type entry struct {
	kind entryKind
	text string
	dur  time.Duration // assistant turns only (opencode `· duration` footer)
	at   time.Time     // when the message landed; zero = unknown (no divider)
}

// ─── model ───────────────────────────────────────────────────────────────

// agentRuntime is the slice of the agent loop the TUI needs. Depending on an
// interface (not the concrete loop) keeps the UI testable without a full
// runtime and makes the coupling explicit. Both the embedded AgentLoop and
// the gateway client implement it, so the TUI is transport-blind: it cannot
// tell whether a turn ran in-process or on the daemon.
type agentRuntime interface {
	GetCurrentModel() string
	ModelPresets() []string
	SetModel(target string) error
	// RefreshModels busts any cached model state (the picker calls it on
	// open). Embedded runtimes read live and no-op.
	RefreshModels()
	// InjectSteering queues a follow-up into the running turn (Enter while
	// working); AbortTurn ends it immediately (Esc).
	InjectSteering(sessionKey, content string)
	AbortTurn(sessionKey string)
	// RespondClarify answers an in-flight clarification question,
	// reporting false when there is no such pending question.
	RespondClarify(questionID, response string) bool
	ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string, media []string, onChunk func(string), onToolCall func(string, string)) (string, error)
	// PendingApproval reports a durable permission request awaiting the owner.
	PendingApproval(sessionKey string) (id, title, risk string, ok bool)
	// Contexts: which topic space the session is in (Ghost's native answer to
	// keeping complex topics separate, without forking memory).
	CurrentContext(sessionKey string) string
	ListContexts() []string
	SwitchContext(sessionKey, contextID string) error
}

type agentTUI struct {
	loop    agentRuntime
	session string

	viewport viewport.Model
	input    textarea.Model
	entries  []entry

	width, height int
	ready         bool

	working   bool
	streaming string // in-progress assistant text (not yet committed)
	toolLine  string // current tool activity
	toolCount int

	showTools bool // expand tool detail (Ctrl+O)

	queued    []string // messages typed while working (steering)
	lastErr   string
	quitting  bool
	history   []string // sent messages, for ↑ recall
	histIndex int

	// pi/opencode-inspired chrome state (display only, never semantics).
	turnCount   int       // completed turns this session
	turnStart   time.Time // when the current turn started
	elapsed     time.Duration
	spinFrame   int
	toolHistory []toolStep // product-language tool steps for the current turn
	paletteSel  int        // selected index in the / palette popup

	// approval is set when a turn ends with a durable permission request;
	// the composer is replaced by Allow once / Always allow / Deny choices.
	approval *pendingApproval
	// approvalSel is the opencode-style left/right cursor over those
	// choices (1/2/3 still answer directly).
	approvalSel int
	// modal is an open centered dialog (opencode dialog.select), e.g. the
	// model picker. It owns the keyboard until Enter picks or Esc closes.
	modal *selectModal
	// clarify is set when the running turn asks a clarification question
	// (the event the mobile app renders as an interactive card). The next
	// Enter answers it in-band instead of starting a new turn.
	clarify *pendingClarify
}

type pendingClarify struct {
	questionID string
	question   string
	choices    []string
}

type pendingApproval struct {
	id    string
	title string
	risk  string
}

// toolStep is one collapsed tool row in the opencode-style activity trail.
// Only the active step shows a spinner; finished steps collapse to ✓ rows
// (raw tool JSON is never streamed into the transcript).
type toolStep struct {
	tool  string // machine tool name (for the opencode icon map)
	label string
	start time.Time
	done  bool
	dur   time.Duration
}

const agentPrompt = "› "

// promptExamples rotates the composer placeholder the opencode way
// (`Ask anything… "{example}"`) so the empty box teaches by example.
var promptExamples = []string{
	"What routines do you have for me?",
	"What do you remember about me?",
	"Remind me to stretch in 25 minutes",
	"Summarize what we discussed yesterday",
}

func promptPlaceholder() string {
	return fmt.Sprintf("Ask Ghost anything…  e.g. %q  (/ for commands)", promptExamples[time.Now().Second()%len(promptExamples)])
}

func newAgentTUI(loop agentRuntime, session string) *agentTUI {
	ta := textarea.New()
	// No ❯ prefix: like opencode's composer, the prompt is a bare
	// textarea in a left-bordered panel — the border is the chrome.
	ta.Placeholder = promptPlaceholder()
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Focus()

	return &agentTUI{
		loop:      loop,
		session:   session,
		input:     ta,
		histIndex: -1,
	}
}

// ─── bubbletea lifecycle ─────────────────────────────────────────────────

func (m *agentTUI) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, spinnerTick())
}

func spinnerTick() tea.Cmd {
	// opencode spins at 80ms; match it so activity reads identically.
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

type teaMsg = tea.Msg

func (m *agentTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		if !m.ready {
			m.ready = true
			m.renderTranscript()
		}
		return m, nil

	case spinnerTickMsg:
		if m.working {
			m.spinFrame++
			m.elapsed = time.Since(m.turnStart)
			m.renderTranscript()
			return m, spinnerTick()
		}
		return m, nil

	case streamChunkMsg:
		m.streaming += msg.text
		m.renderTranscript()
		return m, nil

	case toolCallMsg:
		m.pushToolStep(msg.tool, msg.label)
		return m, nil

	case toolProgressMsg:
		// Daemon turns arrive with complete server-side labels.
		m.pushToolStep(msg.tool, msg.label)
		return m, nil

	case clarifyRequestMsg:
		m.clarify = &pendingClarify{questionID: msg.questionID, question: msg.question, choices: msg.choices}
		m.append(entry{kind: entryNotice, text: renderClarifyPrompt(msg.question, msg.choices, m.contentWidth())})
		m.renderTranscript()
		return m, nil

	case clarifyAnswerMsg:
		if !msg.ok {
			m.clarify = nil
			m.append(entry{kind: entryError, text: "Ghost couldn't take that answer (question expired) — ask again"})
			m.renderTranscript()
		}
		return m, nil

	case turnDoneMsg:
		m.working = false
		m.clarify = nil
		now := time.Now()
		for i := range m.toolHistory {
			if !m.toolHistory[i].done {
				m.toolHistory[i].done = true
				m.toolHistory[i].dur = now.Sub(m.toolHistory[i].start)
			}
		}
		m.toolLine = ""
		m.turnCount++
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.append(entry{kind: entryError, text: "✗ " + friendlyAgentError(msg.err)})
		} else {
			// If the turn is blocked on a durable approval, show it as a card
			// rather than a wall of text. The paused call resumes through the
			// governed path when the owner answers.
			if id, title, risk, ok := m.loop.PendingApproval(m.session); ok {
				m.approval = &pendingApproval{id: id, title: title, risk: risk}
				m.append(entry{kind: entryNotice, text: "needs your approval"})
			} else {
				// opencode rule: the final response wins. The stream buffer
				// is a live preview only — never concatenated with the final.
				text := strings.TrimSpace(msg.text)
				if text == "" {
					text = strings.TrimSpace(m.streaming)
				}
				if text == "" {
					text = "(no response)"
				}
				m.append(entry{kind: entryAssistant, text: text, dur: time.Since(m.turnStart), at: time.Now()})
			}
		}
		m.streaming = ""
		m.toolCount = 0
		m.renderTranscript()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.clampPalette()
	m.layout()
	return m, cmd
}

// clampPalette keeps the palette selection inside the filtered list.
func (m *agentTUI) clampPalette() {
	n := len(m.paletteMatches())
	if n == 0 {
		m.paletteSel = 0
		return
	}
	if m.paletteSel < 0 {
		m.paletteSel = 0
	}
	if m.paletteSel >= n {
		m.paletteSel = n - 1
	}
}

// pushToolStep appends one collapsed activity row. Dedupe: providers often
// re-report the same active tool, so repeats update the spinning row in
// place instead of appending.
func (m *agentTUI) pushToolStep(tool, label string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	if n := len(m.toolHistory); n > 0 && !m.toolHistory[n-1].done && m.toolHistory[n-1].label == label {
		m.toolLine = label
		m.renderTranscript()
		return
	}
	now := time.Now()
	if n := len(m.toolHistory); n > 0 && !m.toolHistory[n-1].done {
		m.toolHistory[n-1].done = true
		m.toolHistory[n-1].dur = now.Sub(m.toolHistory[n-1].start)
	}
	m.toolHistory = append(m.toolHistory, toolStep{tool: tool, label: label, start: now})
	m.toolCount = len(m.toolHistory)
	m.toolLine = label
	m.renderTranscript()
}

// renderClarifyPrompt formats the in-flight question the way the mobile
// app's interactive card reads: question first, numbered choices after.
func renderClarifyPrompt(question string, choices []string, width int) string {
	var b strings.Builder
	b.WriteString("Ghost asks: " + strings.TrimSpace(question))
	for i, c := range choices {
		b.WriteString(fmt.Sprintf("\n  [%d] %s", i+1, strings.TrimSpace(c)))
	}
	if len(choices) > 0 {
		b.WriteString("\nanswer with text, or pick a number")
	} else {
		b.WriteString("\ntype your answer and press Enter")
	}
	return b.String()
}

func (m *agentTUI) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A modal dialog owns the keyboard until picked or dismissed.
	if m.modal != nil {
		return m.handleModalKey(msg)
	}
	// While an approval is pending, the keyboard is the approval panel:
	// 1/2/3 (or a/A/d) answer directly; ←/→ (h/l) moves the opencode
	// cursor and Enter confirms it. A stray keystroke can never approve.
	if m.approval != nil {
		switch msg.String() {
		case "1", "a":
			return m.resolveApproval("allow once")
		case "2", "A":
			return m.resolveApproval("always allow")
		case "3", "d":
			return m.resolveApproval("deny")
		case "left", "h":
			if m.approvalSel > 0 {
				m.approvalSel--
			}
			return m, nil
		case "right", "l":
			if m.approvalSel < 2 {
				m.approvalSel++
			}
			return m, nil
		case "enter":
			return m.resolveApproval([]string{"allow once", "always allow", "deny"}[m.approvalSel])
		case "ctrl+c", "esc":
			// Dismissing is a deny-by-inaction: leave the request pending and
			// return to normal input without claiming anything ran.
			m.approval = nil
			m.append(entry{kind: entryNotice, text: "left pending — Ghost will wait"})
			m.renderTranscript()
			return m, nil
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEsc:
		// Escape is contextual cancel at every level (pi app.interrupt,
		// opencode session_interrupt): palette first, then the running
		// turn. Idle with an empty editor, it closes the TUI — the
		// session persists in the database, so nothing is lost.
		if m.paletteVisible() {
			m.input.Reset()
			m.paletteSel = 0
			m.renderTranscript()
			return m, nil
		}
		if m.working {
			// Abort the turn; return queued text to the editor. A
			// dropped clarification dies with the turn, so clear it.
			m.loop.AbortTurn(m.session)
			m.clarify = nil
			if len(m.queued) > 0 {
				m.input.SetValue(strings.Join(m.queued, "\n"))
				m.queued = nil
			}
			m.append(entry{kind: entryNotice, text: "aborted"})
			m.renderTranscript()
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case tea.KeyCtrlL:
		m.cycleModel()
		return m, nil

	case tea.KeyCtrlO:
		m.showTools = !m.showTools
		m.renderTranscript()
		return m, nil

	case tea.KeyPgUp:
		m.viewport.HalfViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		return m, nil

	case tea.KeyTab:
		if items := m.paletteMatches(); len(items) > 0 {
			if m.paletteSel < 0 || m.paletteSel >= len(items) {
				m.paletteSel = 0
			}
			m.completePalette(items[m.paletteSel])
			return m, nil
		}
		return m, nil

	case tea.KeyCtrlJ:
		// Universal newline key (opencode's input.newline family):
		// terminals that swallow Shift+Enter still send Ctrl+J
		// faithfully, and the trailing-\ escape keeps working too.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m.clampPalette()
		m.layout()
		return m, cmd

	case tea.KeyUp:
		if m.paletteVisible() {
			if m.paletteSel > 0 {
				m.paletteSel--
			}
			return m, nil
		}
		m.recallHistory(-1)
		return m, nil
	case tea.KeyDown:
		if m.paletteVisible() {
			if m.paletteSel < len(m.paletteMatches())-1 {
				m.paletteSel++
			}
			return m, nil
		}
		m.recallHistory(1)
		return m, nil

	case tea.KeyEnter:
		// An in-flight clarification owns Enter: the answer goes to the
		// blocked turn (like the mobile card), never a new turn.
		if m.clarify != nil {
			m.answerClarify()
			return m, nil
		}
		// With the palette open, Enter accepts the highlighted completion
		// AND runs it immediately — every slash command is valid with
		// zero args, so there is no dead complete-only state. (Tab is
		// the compose-first key: it completes without running, for
		// adding arguments.)
		if items := m.paletteMatches(); len(items) > 0 && m.paletteVisible() {
			sel := m.paletteSel
			if sel < 0 || sel >= len(items) {
				sel = 0
			}
			m.completePalette(items[sel])
		}
		line := strings.TrimSpace(m.input.Value())
		if msg.Alt || strings.HasSuffix(m.input.Value(), "\\") {
			// allow newline
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		m.input.Reset()
		if line == "" {
			return m, nil
		}
		if strings.HasPrefix(line, "/") {
			return m.runCommand(line)
		}
		m.send(line)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// recallHistory moves through sent messages with ↑/↓. delta -1 = older.
func (m *agentTUI) recallHistory(delta int) {
	if len(m.history) == 0 {
		return
	}
	if m.histIndex == -1 {
		m.histIndex = len(m.history)
	}
	m.histIndex += delta
	if m.histIndex < 0 {
		m.histIndex = 0
	}
	if m.histIndex >= len(m.history) {
		m.histIndex = len(m.history)
		m.input.Reset()
		return
	}
	m.input.SetValue(m.history[m.histIndex])
}

// ─── sending ─────────────────────────────────────────────────────────────

func (m *agentTUI) send(text string) {
	m.history = append(m.history, text)
	m.histIndex = -1
	m.paletteSel = 0

	if m.working {
		// Queue a steering message into the running turn.
		m.queued = append(m.queued, text)
		m.loop.InjectSteering(m.session, text)
		m.append(entry{kind: entryNotice, text: "↳ queued for the current turn: " + text})
		m.renderTranscript()
		return
	}

	m.append(entry{kind: entryUser, text: text, at: time.Now()})
	m.working = true
	m.toolCount = 0
	m.toolLine = ""
	m.toolHistory = nil
	m.streaming = ""
	m.turnStart = time.Now()
	m.elapsed = 0
	m.renderTranscript()

	go m.runTurn(text)
}

func (m *agentTUI) runTurn(text string) {
	send := func(msg tea.Msg) {
		if agentProgram != nil {
			agentProgram.Send(msg)
		}
	}
	chunk := func(s string) { send(streamChunkMsg{text: s}) }
	onTool := func(tool, args string) {
		send(toolCallMsg{tool: tool, label: toolStatusLabel(tool, args)})
	}
	resp, err := m.loop.ProcessDirectWithChannel(
		context.Background(), text, m.session, "cli", "direct", nil, chunk, onTool)
	send(turnDoneMsg{text: resp, err: err})
}

// answerClarify posts the editor text as the answer to the in-flight
// clarification question (mobile-card parity). A bare number picks that
// choice. The blocked turn resumes on its own; nothing new starts.
func (m *agentTUI) answerClarify() {
	text := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	if text == "" || m.clarify == nil {
		return
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(m.clarify.choices) {
		text = m.clarify.choices[n-1]
	}
	qid := m.clarify.questionID
	m.history = append(m.history, text)
	m.histIndex = -1
	m.append(entry{kind: entryUser, text: text})
	m.renderTranscript()
	go func() {
		ok := m.loop.RespondClarify(qid, text)
		if agentProgram != nil {
			agentProgram.Send(clarifyAnswerMsg{ok: ok})
		}
	}()
}

// clarifyAnswerMsg reports whether the clarify answer landed.
type clarifyAnswerMsg struct{ ok bool }

// resolveApproval answers a pending approval by sending the recognized grant
// phrase as a normal turn. This is deliberate: the reply travels through the
// SAME governed resume path the console and mobile use (CheckApprovalReply),
// so the CLI can never authorize around the broker. The card clears first so
// the phrase is sent as an ordinary message, not re-interpreted as a key.
func (m *agentTUI) resolveApproval(phrase string) (tea.Model, tea.Cmd) {
	m.approval = nil
	m.approvalSel = 0
	m.append(entry{kind: entryNotice, text: "you chose: " + phrase})
	m.renderTranscript()
	m.send(phrase)
	return m, nil
}

// handleContext shows or switches the session's context. Contexts scope
// memory and tools the way pi's branches scope a session — but without
// forking Ghost's single durable memory, so "what Ghost knows" stays one
// reconciled truth.
func (m *agentTUI) handleContext(args []string) {
	if len(args) == 0 {
		cur := m.loop.CurrentContext(m.session)
		all := m.loop.ListContexts()
		m.append(entry{kind: entryNotice, text: fmt.Sprintf("context: %s\navailable: %s\nswitch with /context <name>", cur, strings.Join(all, ", "))})
		m.renderTranscript()
		return
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if err := m.loop.SwitchContext(m.session, name); err != nil {
		m.append(entry{kind: entryError, text: "context: " + err.Error() + " (see /context for available)"})
	} else {
		m.append(entry{kind: entryNotice, text: "context → " + m.loop.CurrentContext(m.session) + " (memory and tools are scoped to it)"})
	}
	m.renderTranscript()
}

// ─── slash commands ──────────────────────────────────────────────────────

type paletteItem struct {
	name string
	desc string
}

var paletteCommands = []paletteItem{
	{"help", "list commands and keys"},
	{"model", "show or switch model"},
	{"details", "toggle tool step details"},
	{"new", "fresh conversation"},
	{"sessions", "session and turn count"},
	{"memory", "ask what Ghost remembers"},
	{"context", "topic space (scoped memory/tools)"},
	{"rewind", "edit and resend last message"},
	{"routines", "what Ghost does for you"},
	{"clear", "clear the screen"},
	{"quit", "exit"},
}

// paletteMatches filters commands by the current "/xyz" input.
func (m *agentTUI) paletteMatches() []paletteItem {
	v := strings.TrimSpace(m.input.Value())
	if !strings.HasPrefix(v, "/") {
		return nil
	}
	q := strings.ToLower(strings.TrimPrefix(strings.Fields(v)[0], "/"))
	if q == "" {
		return paletteCommands
	}
	var out []paletteItem
	for _, c := range paletteCommands {
		if strings.HasPrefix(c.name, q) {
			out = append(out, c)
		}
	}
	return out
}

// completePalette accepts a palette item the opencode way: the command
// name is completed in place (partial `/mod` → `/model `), preserving any
// already-typed arguments. It never submits — the next Enter runs it.
func (m *agentTUI) completePalette(it paletteItem) {
	v := m.input.Value()
	fields := strings.Fields(v)
	rest := ""
	if len(fields) > 1 {
		rest = " " + strings.Join(fields[1:], " ")
	} else if strings.HasSuffix(v, " ") {
		rest = " "
	}
	m.input.SetValue("/" + it.name + rest)
	if !strings.HasSuffix(m.input.Value(), " ") {
		m.input.SetValue(m.input.Value() + " ")
	}
	m.input.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m.paletteSel = 0
}

// ─── modal dialog (opencode dialog.select) ─────────────────────────────
// Centered overlay with title, live filter, grouped rows and footer hints:
// ↑↓/ctrl+p/ctrl+n move, pgup/pgdn jump, home/end, return selects,
// esc closes. Filter narrows as you type; empty shows everything.
type modalItem struct {
	label   string
	desc    string
	current bool
}

type selectModal struct {
	title  string
	items  []modalItem
	sel    int
	filter string
	pick   func(label string)
}

func (m *agentTUI) modalMatches() []modalItem {
	if m.modal == nil {
		return nil
	}
	q := strings.ToLower(m.modal.filter)
	if q == "" {
		return m.modal.items
	}
	var out []modalItem
	for _, it := range m.modal.items {
		if strings.Contains(strings.ToLower(it.label), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m *agentTUI) openModelModal() {
	m.loop.RefreshModels()
	presets := m.loop.ModelPresets()
	cur := m.loop.GetCurrentModel()
	items := make([]modalItem, 0, len(presets)+1)
	seen := map[string]bool{}
	for _, p := range presets {
		if seen[p] {
			continue
		}
		seen[p] = true
		items = append(items, modalItem{label: p, desc: providerLocality(p), current: p == cur})
	}
	if !seen[cur] {
		items = append(items, modalItem{label: cur, desc: providerLocality(cur) + " · active", current: true})
	}
	m.modal = &selectModal{title: "Models", items: items, pick: func(label string) { m.setModel(label) }}
	m.renderTranscript()
}

func (m *agentTUI) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.modalMatches())
	clamp := func() {
		if m.modal.sel < 0 {
			m.modal.sel = 0
		}
		if m.modal.sel >= n {
			m.modal.sel = n - 1
		}
		if n == 0 {
			m.modal.sel = 0
		}
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.modal = nil
		m.renderTranscript()
		return m, nil
	case tea.KeyEnter:
		items := m.modalMatches()
		clamp()
		if len(items) == 0 {
			return m, nil
		}
		pick := m.modal.pick
		label := items[m.modal.sel].label
		m.modal = nil
		pick(label)
		return m, nil
	case tea.KeyUp:
		m.modal.sel--
		clamp()
		return m, nil
	case tea.KeyDown:
		m.modal.sel++
		clamp()
		return m, nil
	case tea.KeyPgUp:
		m.modal.sel -= 5
		clamp()
		return m, nil
	case tea.KeyPgDown:
		m.modal.sel += 5
		clamp()
		return m, nil
	case tea.KeyHome:
		m.modal.sel = 0
		return m, nil
	case tea.KeyEnd:
		m.modal.sel = n - 1
		clamp()
		return m, nil
	case tea.KeyBackspace:
		r := []rune(m.modal.filter)
		if len(r) > 0 {
			m.modal.filter = string(r[:len(r)-1])
		}
		clamp()
		return m, nil
	case tea.KeyRunes:
		// ctrl+p / ctrl+n move like opencode; other runes filter.
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 0x10: // ctrl+p
				m.modal.sel--
				clamp()
				return m, nil
			case 0x0e: // ctrl+n
				m.modal.sel++
				clamp()
				return m, nil
			}
		}
		m.modal.filter += string(msg.Runes)
		clamp()
		return m, nil
	}
	s := msg.String()
	switch s {
	case "ctrl+p":
		m.modal.sel--
		clamp()
	case "ctrl+n":
		m.modal.sel++
		clamp()
	}
	return m, nil
}

// renderModal draws the dialog centered over the frame, opencode-style:
// title + esc hint, filter echo, current-marked rows, footer hints.
func (m *agentTUI) renderModal() string {
	items := m.modalMatches()
	maxRows := m.height - 10
	if maxRows < 3 {
		maxRows = 3
	}
	if maxRows > 10 {
		maxRows = 10
	}
	off := 0
	if m.modal.sel >= maxRows {
		off = m.modal.sel - maxRows + 1
	}
	end := off + maxRows
	if end > len(items) {
		end = len(items)
	}
	var b strings.Builder
	title := styleModalTitle.Render(m.modal.title)
	esc := styleNotice.Render("esc")
	gap := m.contentWidth() - lipgloss.Width(m.modal.title) - lipgloss.Width("esc")
	if gap < 1 {
		gap = 1
	}
	b.WriteString(title + strings.Repeat(" ", gap) + esc)
	b.WriteString("\n")
	filter := m.modal.filter
	if filter == "" {
		filter = "type to filter…"
	}
	b.WriteString(styleNotice.Render("  " + filter + "▍"))
	b.WriteString("\n")
	if len(items) == 0 {
		b.WriteString(styleNotice.Render("  No results found"))
		b.WriteString("\n")
	}
	for i := off; i < end; i++ {
		it := items[i]
		mark := "  "
		if it.current {
			mark = "● "
		}
		row := fmt.Sprintf("%s%-24s %s", mark, cellTruncate(it.label, 24), it.desc)
		if i == m.modal.sel {
			b.WriteString(stylePaletteSel.Render(" " + cellTruncate(row, m.contentWidth()-2) + " "))
		} else {
			b.WriteString(stylePaletteRow.Render(" " + row))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleNotice.Render("  ↑↓ move · enter select · esc close"))
	return styleModalBox.Width(m.contentWidth()).Render(strings.TrimRight(b.String(), "\n"))
}

// overlayCenter splices the dialog over the middle rows of the frame.
func overlayCenter(base, dialog string, w, h int) string {
	lines := strings.Split(base, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	dl := strings.Split(dialog, "\n")
	dw := 0
	for _, ln := range dl {
		if wd := lipgloss.Width(ln); wd > dw {
			dw = wd
		}
	}
	start := (h - len(dl)) / 2
	if start < 0 {
		start = 0
	}
	for i, dln := range dl {
		r := start + i
		if r < 0 || r >= len(lines) {
			continue
		}
		pad := (w - lipgloss.Width(dln)) / 2
		if pad < 0 {
			pad = 0
		}
		lines[r] = strings.Repeat(" ", pad) + dln
	}
	return strings.Join(lines, "\n")
}

func (m *agentTUI) paletteVisible() bool {
	// The approval dialog and modals own the keyboard while up.
	if m.approval != nil || m.modal != nil {
		return false
	}
	return len(m.paletteMatches()) > 0 && strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/")
}

func (m *agentTUI) runCommand(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(line)
	cmd := strings.TrimPrefix(fields[0], "/")
	args := fields[1:]

	switch cmd {
	case "help", "?":
		m.append(entry{kind: entryNotice, text: agentHelpText()})
	case "quit", "q", "exit":
		m.quitting = true
		return m, tea.Quit
	case "clear":
		m.entries = nil
		m.toolHistory = nil
		m.streaming = ""
		m.renderTranscript()
	case "new":
		m.session = "cli:" + fmt.Sprintf("%d", time.Now().UnixNano())
		m.entries = nil
		m.toolHistory = nil
		m.streaming = ""
		m.turnCount = 0
		m.append(entry{kind: entryNotice, text: "new conversation: " + m.session})
		m.renderTranscript()
	case "session", "sessions":
		m.append(entry{kind: entryNotice, text: fmt.Sprintf("session: %s · model: %s · %d turns", m.session, m.loop.GetCurrentModel(), m.turnCount)})
	case "model", "models":
		if len(args) == 0 {
			m.openModelModal()
		} else {
			m.setModel(strings.Join(args, " "))
		}
	case "details":
		m.showTools = !m.showTools
		m.append(entry{kind: entryNotice, text: fmt.Sprintf("tool details %s", onOff(m.showTools))})
	case "memory":
		m.showMemory(args)
	case "context":
		m.handleContext(args)
	case "rewind":
		m.rewind()
	case "routines":
		m.showRoutines()
	default:
		m.append(entry{kind: entryError, text: "unknown command: /" + cmd + " (try /help)"})
	}
	m.renderTranscript()
	return m, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m *agentTUI) cycleModel() {
	presets := m.loop.ModelPresets()
	if len(presets) == 0 {
		m.append(entry{kind: entryNotice, text: "no model presets configured"})
		m.renderTranscript()
		return
	}
	cur := m.loop.GetCurrentModel()
	next := presets[0]
	for i, p := range presets {
		if p == cur {
			next = presets[(i+1)%len(presets)]
			break
		}
	}
	m.setModel(next)
}

func (m *agentTUI) setModel(name string) {
	if err := m.loop.SetModel(name); err != nil {
		m.append(entry{kind: entryError, text: "model: " + err.Error()})
	} else {
		m.append(entry{kind: entryNotice, text: "model → " + m.loop.GetCurrentModel()})
	}
	m.renderTranscript()
}

func (m *agentTUI) showMemory(args []string) {
	// Delegate to the same read-only paths the console uses: ask Ghost in a
	// turn so memory retrieval stays governed and scoped.
	q := "What do you remember about me?"
	if len(args) > 0 {
		q = "From memory, tell me about: " + strings.Join(args, " ")
	}
	m.send(q)
}

func (m *agentTUI) showRoutines() {
	m.send("What routines do you have scheduled for me?")
}

// rewind puts the most recent user message back in the editor so it can be
// edited and resent. It is an editor convenience only: nothing in the stored
// conversation or memory is changed, so the session stays one continuous
// thread (no branching).
func (m *agentTUI) rewind() {
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].kind == entryUser {
			m.input.SetValue(m.entries[i].text)
			m.append(entry{kind: entryNotice, text: "rewound the last message into the editor"})
			m.renderTranscript()
			return
		}
	}
	m.append(entry{kind: entryNotice, text: "nothing to rewind"})
	m.renderTranscript()
}

// ─── transcript ──────────────────────────────────────────────────────────

func (m *agentTUI) append(e entry) { m.entries = append(m.entries, e) }

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *agentTUI) spinner() string {
	return spinnerFrames[m.spinFrame%len(spinnerFrames)]
}

// dayLabel groups the transcript the way chat apps do: Today,
// Yesterday, or the calendar date. Zero time means unknown — no divider.
func dayLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	y, mo, d := t.Date()
	ny, nmo, nd := time.Now().Date()
	if y == ny && mo == nmo && d == nd {
		return "Today"
	}
	yy, ymo, yd := time.Now().AddDate(0, 0, -1).Date()
	if y == yy && mo == ymo && d == yd {
		return "Yesterday"
	}
	return t.Format("2 January 2006")
}

func (m *agentTUI) renderDayDivider(label string) string {
	w := m.contentWidth()
	core := " " + label + " "
	fill := w - lipgloss.Width(core)
	if fill < 0 {
		return styleDayDivider.Render(cellTruncate(label, w))
	}
	left := fill / 2
	return styleDayDivider.Render(strings.Repeat("─", left) + core + strings.Repeat("─", fill-left))
}

func (m *agentTUI) renderTranscript() {
	if !m.ready {
		return
	}
	var b strings.Builder
	if len(m.entries) == 0 && m.streaming == "" && !m.working {
		b.WriteString(m.welcomeCard())
		b.WriteString("\n")
	}
	// iMessage/WhatsApp grouping: a day divider opens the transcript and
	// reappears wherever the calendar day flips. Entries without a
	// timestamp inherit the previous day so they never split a group.
	prevDay := ""
	for i, e := range m.entries {
		if d := dayLabel(e.at); d != "" && d != prevDay {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.renderDayDivider(d))
			b.WriteString("\n")
			prevDay = d
		} else if d != "" {
			prevDay = d
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderEntry(e))
		b.WriteString("\n")
	}
	if m.working {
		b.WriteString(m.workingBlock())
		b.WriteString("\n")
	}
	m.viewport.SetContent(strings.TrimRight(b.String(), "\n"))
	m.viewport.GotoBottom()
}

// workingBlock is the opencode-style live turn: in-place stream preview with
// a cursor, then one collapsed ✓ row per finished tool and a spinner row for
// the active one. Raw tool JSON never reaches the transcript.
func (m *agentTUI) workingBlock() string {
	var b strings.Builder
	w := m.contentWidth()
	if m.streaming != "" {
		b.WriteString(renderAssistantBody(m.streaming+"▍", w))
		b.WriteString("\n")
	}
	steps := m.toolHistory
	limit := len(steps)
	if !m.showTools && limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		s := steps[i]
		icon := toolIcon(s.tool)
		if !s.done {
			b.WriteString(styleToolActive.Render(fmt.Sprintf("  %s %s %s", m.spinner(), icon, cellTruncate(s.label, w-8))))
		} else if m.showTools {
			b.WriteString(styleTool.Render(fmt.Sprintf("  %s %s (%s)", icon, cellTruncate(s.label, w-12), formatElapsed(s.dur))))
		} else {
			b.WriteString(styleTool.Render(fmt.Sprintf("  %s %s", icon, cellTruncate(s.label, w-8))))
		}
		b.WriteString("\n")
	}
	if !m.showTools && len(steps) > limit {
		b.WriteString(styleNotice.Render(fmt.Sprintf("  · +%d more (ctrl+o for details)", len(steps)-limit)))
		b.WriteString("\n")
	}
	status := fmt.Sprintf("  %s working", m.spinner())
	if m.elapsed > 0 {
		status += fmt.Sprintf(" · %s", formatElapsed(m.elapsed))
	}
	if len(steps) > 0 {
		status += fmt.Sprintf(" · %d tool%s", len(steps), plural(len(steps)))
	}
	if len(m.queued) > 0 {
		status += fmt.Sprintf(" · %d queued", len(m.queued))
	}
	b.WriteString(styleWorking.Render(status + "…"))
	return b.String()
}

// toolIcon maps Ghost tools to opencode's collapsed-row icon language:
// → read, ← write, ✱ search, % fetch, ◈ web search, $ shell, ⚙ generic.
func toolIcon(name string) string {
	switch name {
	case "read_file", "list_dir", "screenshot", "vision":
		return "→"
	case "write_file", "edit_file", "canvas", "image_generate":
		return "←"
	case "web_search", "oracle":
		return "◈"
	case "web_fetch", "browser":
		return "%"
	case "exec", "sandbox":
		return "$"
	case "remember", "spawn", "subagent":
		return "✱"
	default:
		return "⚙"
	}
}

func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}

func (m *agentTUI) renderEntry(e entry) string {
	w := m.contentWidth()
	switch e.kind {
	case entryUser:
		// opencode user bubble: name line, then a left-bar panel with
		// the raw text (never markdown-rendered), paragraphs preserved.
		var lines []string
		lines = append(lines, styleUserName.Render("You"))
		for _, para := range strings.Split(e.text, "\n") {
			if strings.TrimSpace(para) == "" {
				lines = append(lines, styleUserPanel.Render(styleUserBar.Render("┃")))
				continue
			}
			for _, wl := range wrapText(para, w-4) {
				lines = append(lines, styleUserPanel.Render(styleUserBar.Render("┃")+" "+styleUserText.Render(wl)))
			}
		}
		return strings.Join(lines, "\n")
	case entryAssistant:
		head := styleAssistantName.Render(logo + " Ghost · " + m.loop.GetCurrentModel())
		if e.dur > 0 {
			head += styleAssistantMeta.Render(" · " + formatElapsed(e.dur))
		}
		return head + "\n" + renderAssistantBody(e.text, w)
	case entryTool:
		return styleTool.Render("  ✓ " + cellTruncate(e.text, w-6))
	case entryNotice:
		return styleNotice.Render("  · " + e.text)
	case entryError:
		var lines []string
		for _, wl := range wrapText(e.text, w-4) {
			lines = append(lines, "  "+wl)
		}
		return styleErrorCard.Render(strings.Join(lines, "\n"))
	}
	return e.text
}

// welcomeCard is the opencode-style empty state. It is rendered directly,
// never stored as an entry, so it can never duplicate.
func (m *agentTUI) welcomeCard() string {
	w := m.contentWidth()
	art := styleGhostArt.Render("▓▒░  G H O S T  ░▒▓")
	title := styleWelcomeTitle.Render(logo + " Ghost")
	sub := styleNotice.Render(wrapFirst("Your AI on your machine — it remembers, acts with approval, and shows where it ran.", w))
	cmds := styleWelcomeCmds.Render("  /help      commands & keys\n  /model     switch thinking engine\n  /memory    what Ghost remembers\n  /routines  recurring work")
	return art + "\n" + title + "\n" + sub + "\n" + cmds
}

// ─── width helpers ─────────────────────────────────────────────────────
// contentWidth is the usable transcript width: terminal minus the viewport
// margin. Every renderer must wrap/truncate to it — nothing may assume the
// full terminal width, which is what caused mid-word truncation.
func (m *agentTUI) contentWidth() int {
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	return w
}

// truncate shortens s to at most w cells, adding … when cut.
func cellTruncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi) / 2
		if lipgloss.Width(string(runes[:mid])) < w-1 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo > 1 {
		return string(runes[:lo-1]) + "…"
	}
	return "…"
}

// wrapText greedily wraps s to lines of at most w cells.
func wrapText(s string, w int) []string {
	if w < 10 {
		w = 10
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if lipgloss.Width(para) <= w {
			out = append(out, para)
			continue
		}
		var cur strings.Builder
		curW := 0
		for _, word := range strings.Fields(para) {
			ww := lipgloss.Width(word)
			if curW == 0 {
				cur.WriteString(word)
				curW = ww
				continue
			}
			if curW+1+ww > w {
				out = append(out, cur.String())
				cur.Reset()
				cur.WriteString(word)
				curW = ww
				continue
			}
			cur.WriteString(" " + word)
			curW += 1 + ww
		}
		out = append(out, cur.String())
	}
	return out
}

func wrapFirst(s string, w int) string {
	return strings.Join(wrapText(s, w), "\n")
}

// ─── markdown (opencode spec, no new deps) ─────────────────────────────────
// Follows OpenCode's TUI markdown roles on the dark theme: markers are
// concealed (no ``` fences, no #/backtick/bracket noise, URLs hidden
// behind cyan underlined labels), headings bold violet (h1 underlined),
// **bold** orange, *italic*/quotes sand italic, code green with no
// background, bullets peach, ordered numbers cyan, checked green.
func renderAssistantBody(text string, width int) string {
	if width < 20 {
		width = 20
	}
	var out []string
	inCode := false
	for _, ln := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode // conceal fences and language tags entirely
			continue
		}
		if inCode {
			for _, wl := range wrapText(ln, width) {
				out = append(out, styleMDCodeBlock.Render(wl))
			}
			continue
		}
		if level, rest, ok := parseHeading(trim); ok {
			body := cellTruncate(rest, width)
			if level == 1 {
				out = append(out, styleMDHead1.Render(body))
			} else {
				out = append(out, styleMDHead.Render(body))
			}
			continue
		}
		switch {
		case trim == "---" || trim == "***" || trim == "___":
			out = append(out, styleMDHR.Render(strings.Repeat("─", width)))
		case strings.HasPrefix(trim, "> "):
			for _, wl := range wrapText(strings.TrimPrefix(trim, "> "), width-4) {
				out = append(out, styleMDQuoteMark.Render("> ")+styleMDQuote.Render(renderInline(wl)))
			}
		case strings.HasPrefix(trim, "- [ ] ") || strings.HasPrefix(trim, "* [ ] "):
			rest := strings.TrimSpace(trim[6:])
			for _, wl := range wrapText(rest, width-6) {
				out = append(out, styleMDUncheck.Render("  ○ "+renderInline(wl)))
			}
		case strings.HasPrefix(trim, "- [x] ") || strings.HasPrefix(trim, "- [X] ") ||
			strings.HasPrefix(trim, "* [x] ") || strings.HasPrefix(trim, "* [X] "):
			rest := trim[6:]
			for _, wl := range wrapText(rest, width-6) {
				out = append(out, styleMDCheck.Render("  ● "+renderInline(wl)))
			}
		case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ "):
			body := strings.TrimSpace(trim[2:])
			parts := wrapText(body, width-4)
			for i, wl := range parts {
				if i == 0 {
					out = append(out, styleMDList.Render(trim[:1]+" ")+styleAssistant.Render(renderInline(wl)))
				} else {
					out = append(out, "  "+styleAssistant.Render(renderInline(wl)))
				}
			}
		case isOrderedList(trim):
			dot := strings.Index(trim, ".")
			parts := wrapText(strings.TrimSpace(trim[dot+1:]), width-6)
			for i, wl := range parts {
				if i == 0 {
					out = append(out, styleMDEnum.Render(trim[:dot+1]+" ")+styleAssistant.Render(renderInline(wl)))
				} else {
					out = append(out, "  "+styleAssistant.Render(renderInline(wl)))
				}
			}
		case trim == "":
			out = append(out, "")
		default:
			for _, wl := range wrapText(ln, width) {
				out = append(out, styleAssistant.Render(renderInline(wl)))
			}
		}
	}
	return strings.Join(out, "\n")
}

// parseHeading returns the level and concealed text of an ATX heading.
func parseHeading(s string) (int, string, bool) {
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(s) || s[i] != ' ' {
		return 0, "", false
	}
	return i, strings.TrimSpace(s[i+1:]), true
}

func isOrderedList(s string) bool {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] == ' '
}

// renderInline applies opencode's inline roles in conceal-safe order:
// code spans first (literals win), then links (label cyan underlined,
// URL hidden; bare URLs peach underlined), **bold** orange,
// *italic* sand, ~~strikethrough~~ muted.
func renderInline(s string) string {
	s = renderSpan(s, "`", styleMDCode.Render)
	s = renderLinks(s)
	s = renderSpan(s, "**", styleMDStrong.Render)
	s = renderEmphasis(s)
	s = renderSpan(s, "~~", styleMDStrike.Render)
	return s
}

var (
	mdLinkRe = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdURLRe  = regexp.MustCompile(`https?://[^\s)>\]]+`)
)

func renderLinks(s string) string {
	s = mdLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := mdLinkRe.FindStringSubmatch(m)
		if len(parts) != 2 {
			return m
		}
		return styleMDLinkText.Render(parts[1]) + " "
	})
	return mdURLRe.ReplaceAllStringFunc(s, func(m string) string {
		return styleMDLinkURL.Render(m)
	})
}

// renderEmphasis handles single-star *italic* with boundary guards so
// multiplication (2 * 3) and list markers never render as emphasis.
func renderEmphasis(s string) string {
	var b strings.Builder
	for {
		a := strings.Index(s, "*")
		if a < 0 {
			b.WriteString(s)
			return b.String()
		}
		if a+1 < len(s) && (s[a+1] == '*' || s[a+1] == ' ') {
			b.WriteString(s[:a+1])
			s = s[a+1:]
			continue
		}
		rest := s[a+1:]
		c := strings.Index(rest, "*")
		if c < 0 {
			b.WriteString(s)
			return b.String()
		}
		inner := rest[:c]
		if inner == "" || strings.HasSuffix(inner, " ") || strings.Contains(inner, "*") {
			b.WriteString(s[:a+1])
			s = s[a+1:]
			continue
		}
		b.WriteString(s[:a])
		b.WriteString(styleMDEmph.Render(inner))
		s = rest[c+1:]
	}
}

func renderSpan(s, delim string, fn func(...string) string) string {
	var b strings.Builder
	for {
		a := strings.Index(s, delim)
		if a < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:a])
		rest := s[a+len(delim):]
		c := strings.Index(rest, delim)
		if c < 0 {
			b.WriteString(s[a:])
			return b.String()
		}
		b.WriteString(fn(rest[:c]))
		s = rest[c+len(delim):]
	}
}

// layout recomputes frozen geometry. It runs only on resize and on input
// edits (via Update), never inside View: View must stay pure or the
// viewport height oscillates and chrome duplicates.
//
// pi layout: no top header — the transcript owns the full height. The
// bottom stack is palette popup + prompt box + 2-line footer.
func (m *agentTUI) layout() {
	const footerH = 3 // session, stats/model, shortcuts
	paletteH := m.paletteHeight()
	inputH := m.estimatedInputHeight()
	if m.approval != nil {
		inputH = m.estimatedApprovalHeight()
	}
	vpH := m.height - footerH - paletteH - inputH - 1
	if vpH < 1 {
		vpH = 1
	}
	if m.viewport.Width == 0 {
		m.viewport = viewport.New(m.width, vpH)
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpH
	}
	m.input.SetWidth(m.inputWidth())
	m.syncInputHeight()
}

// syncInputHeight grows the composer with the text between
// composerMinLines and maxPromptLines (opencode caps its composer height
// the same way) so long input scrolls inside the box instead of pushing
// the transcript away.
func (m *agentTUI) syncInputHeight() {
	lines := 0
	inner := m.inputWidth()
	for _, ln := range strings.Split(m.input.Value(), "\n") {
		w := lipgloss.Width(ln)
		if w <= 0 {
			lines++
			continue
		}
		lines += (w + inner - 1) / inner
	}
	if lines < composerMinLines {
		lines = composerMinLines
	}
	if lines > maxPromptLines {
		lines = maxPromptLines
	}
	m.input.SetHeight(lines)
}

// The composer idles at composerMinLines rows (presence, not a sliver)
// and never exceeds maxPromptLines.
const (
	composerMinLines = 3
	maxPromptLines   = 5
)

// estimatedInputHeight mirrors promptBox without rendering it: border (2)
// + textarea visual lines clamped to the box. Visual lines, not physical
// ones: a long line wraps in the editor (tuicomp-measure-element), so the
// box estimate must wrap too or the viewport drifts. No title row — pi
// has no label above the prompt box, and neither do we.
func (m *agentTUI) estimatedInputHeight() int {
	inner := m.inputWidth()
	lines := 0
	for _, ln := range strings.Split(m.input.Value(), "\n") {
		w := lipgloss.Width(ln)
		if w <= 0 {
			lines++
			continue
		}
		lines += (w + inner - 1) / inner
	}
	if lines < composerMinLines {
		lines = composerMinLines
	}
	if lines > maxPromptLines {
		lines = maxPromptLines
	}
	return lines // bare panel: no border rows (opencode composer)
}

func (m *agentTUI) estimatedApprovalHeight() int {
	// Title + subject + risk note + options, no border (inline panel).
	return 5
}

func (m *agentTUI) inputWidth() int {
	// Bare panel: "┃ " gutter only.
	w := m.width - 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m *agentTUI) paletteHeight() int {
	if !m.paletteVisible() {
		return 0
	}
	items, _, moreAbove, moreBelow := m.paletteWindow()
	n := len(items)
	if moreAbove {
		n++
	}
	if moreBelow {
		n++
	}
	return n // bare rows + edge indicators, no border
}

// paletteOffset returns the first visible row so the selection stays in
// a maxRows window.
func (m *agentTUI) paletteOffset(total, maxRows int) int {
	if total <= maxRows || m.paletteSel < maxRows {
		return 0
	}
	off := m.paletteSel - maxRows + 1
	if off+maxRows > total {
		off = total - maxRows
	}
	return off
}

// ─── view ────────────────────────────────────────────────────────────────
// pi layout: no top header — the transcript owns the full height. Bottom
// stack is palette popup + prompt box + 3-line footer (session, stats/model,
// shortcuts). Pure composition: each region renders exactly once; geometry
// was frozen in layout() and rendering must not mutate it.
func (m *agentTUI) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "starting Ghost…"
	}
	var b strings.Builder
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.paletteVisible() {
		b.WriteString(m.paletteView())
		b.WriteString("\n")
	}
	if m.approval != nil {
		b.WriteString(m.approvalCard())
	} else {
		b.WriteString(m.promptBox())
	}
	b.WriteString("\n")
	for i, ln := range m.footerLines() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ln)
	}
	if m.modal != nil {
		return overlayCenter(b.String(), m.renderModal(), m.width, m.height)
	}
	return b.String()
}

// ─── footer (pi-faithful) ──────────────────────────────────────────────
// pi's footer is two dim lines under the prompt box:
// line 1: `~/cwd (branch) • session` — Ghost's equivalent is
// `session • context` (Ghost has no cwd/branch; memory is one truth).
// line 2: left usage stats, right `(provider) model`, right-aligned.
// Model, session and turn state live here — nowhere else.
// ─── footer (pi-faithful) ──────────────────────────────────────────────
// Three dim lines under the prompt box (ux-color-semantics: hints dim,
// model in its locality color, never decorative):
// line 1: `session • context` (pi's `pwd (branch) • session`; Ghost has
// no cwd/branch — session + context is the honest equivalent).
// line 2: activity left, `(locality) model` right-aligned (pi's stats line).
// line 3: contextual shortcuts (pi shows key hints alongside the editor).
// Model, session and turn state live here — nowhere else.
func (m *agentTUI) footerLines() []string {
	return []string{m.footerSessionLine(), m.footerStatsLine(), m.footerKeysLine()}
}

func (m *agentTUI) currentCtx() string {
	ctx := ""
	func() {
		defer func() { _ = recover() }()
		if m.loop != nil {
			ctx = m.loop.CurrentContext(m.session)
		}
	}()
	return ctx
}

// footerSessionLine is pi's `pwd (branch) • session` line.
func (m *agentTUI) footerSessionLine() string {
	line := shortSession(m.session)
	if ctx := m.currentCtx(); ctx != "" {
		line += " • " + ctx
	}
	return styleFooter.Render(cellTruncate(line, m.width))
}

// footerStatsLine is pi's `↑in ↓out … ctx% │ (provider) model` line,
// right-aligned with a 2-space minimum gap, truncating gracefully.
// The model carries its locality color (ux-color-semantics: green local,
// blue cloud, muted pod) so "where it ran" reads at a glance.
func (m *agentTUI) footerStatsLine() string {
	model := m.loop.GetCurrentModel()
	local := providerLocality(model)
	left := m.activityWord()
	plainRight := fmt.Sprintf("(%s) %s", local, shortModel(model))
	lw, rw := lipgloss.Width(left), lipgloss.Width(plainRight)
	const minGap = 2
	right := modelLocalityStyle(local).Render(plainRight)
	if lw+minGap+rw <= m.width {
		return styleFooter.Render(left+strings.Repeat(" ", m.width-lw-rw)) + right
	}
	if lw+minGap < m.width {
		right = modelLocalityStyle(local).Render(cellTruncate(plainRight, m.width-lw-minGap))
		rw = lipgloss.Width(cellTruncate(plainRight, m.width-lw-minGap))
		return styleFooter.Render(left+strings.Repeat(" ", m.width-lw-rw)) + right
	}
	return styleFooter.Render(cellTruncate(left, m.width))
}

// footerKeysLine is the contextual shortcut hint. It names the escape
// routes (input-escape-routes) for the current state and truncates from
// the right so it never wraps.
func (m *agentTUI) footerKeysLine() string {
	var keys string
	switch {
	case m.clarify != nil:
		keys = "type your answer · enter sends · esc aborts the question"
	case m.approval != nil:
		keys = "1 allow once · 2 always allow · 3 deny · esc leaves pending"
	case m.working:
		keys = "enter queues steering · esc aborts · ctrl+o details · / commands"
	case strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/"):
		keys = "↑↓ pick · tab/enter complete · esc dismiss"
	default:
		keys = "enter send · ctrl+j newline · esc quit · ctrl+l model · / commands · tab complete"
	}
	return styleFooterHint.Render(cellTruncate(keys, m.width))
}

func modelLocalityStyle(local string) lipgloss.Style {
	switch local {
	case "local":
		return styleModelLocal
	case "cloud":
		return styleModelCloud
	default:
		return styleModelPod
	}
}

// activityWord is the left half of the stats line.
func (m *agentTUI) activityWord() string {
	if m.approval != nil {
		return "waiting for you"
	}
	if m.working {
		s := fmt.Sprintf("%s working", m.spinner())
		if m.elapsed > 0 {
			s += fmt.Sprintf(" · %s", formatElapsed(m.elapsed))
		}
		if n := len(m.toolHistory); n > 0 {
			s += fmt.Sprintf(" · %d tool%s", n, plural(n))
		}
		if len(m.queued) > 0 {
			s += fmt.Sprintf(" · %d queued", len(m.queued))
		}
		return s
	}
	if m.turnCount == 0 {
		return "ready"
	}
	return fmt.Sprintf("%d turn%s", m.turnCount, plural(m.turnCount))
}

func shortModel(s string) string {
	if len(s) > 28 {
		return s[:27] + "…"
	}
	return s
}

func shortSession(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 && i+1 < len(s) {
		tail := s[i+1:]
		if len(tail) > 8 {
			return "cli:" + tail[:6] + "…"
		}
		return s
	}
	if len(s) > 18 {
		return s[:17] + "…"
	}
	return s
}

// ─── prompt composer (opencode-faithful) ───────────────────────────────
// opencode's composer is NOT a full box: it is a panel with a single left
// `┃` border tinted with the agent color (Ghost violet, faint at idle),
// no `>`/`❯` prefix, and a rotating `Ask anything… "{example}"`
// placeholder. Rows sit on the panel background so the composer reads as
// one defined area. The working status lives in the below-box status row.
func (m *agentTUI) promptBox() string {
	bar := stylePromptBar
	if m.working {
		bar = stylePromptBarActive
	}
	innerW := m.inputWidth()
	var b strings.Builder
	for i, ln := range strings.Split(m.input.View(), "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		pad := innerW - lipgloss.Width(ln)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(stylePromptPanel.Render(bar.Render("┃") + " " + ln + strings.Repeat(" ", pad)))
	}
	return b.String()
}

// paletteView is opencode's autocomplete popup: absolute above the
// composer, left `┃` split-border, menu background, selected row
// highlighted. Return accepts, Tab completes, Esc hides.
const paletteMaxRows = 6

// paletteWindow returns the visible slice plus edge flags. paletteHeight
// renders from this same window, so geometry can never drift from paint.
func (m *agentTUI) paletteWindow() (items []paletteItem, off int, moreAbove, moreBelow bool) {
	all := m.paletteMatches()
	if len(all) <= paletteMaxRows {
		return all, 0, false, false
	}
	off = m.paletteOffset(len(all), paletteMaxRows)
	end := off + paletteMaxRows
	if end > len(all) {
		end = len(all)
	}
	return all[off:end], off, off > 0, end < len(all)
}

func (m *agentTUI) paletteView() string {
	items, off, moreAbove, moreBelow := m.paletteWindow()
	fieldW := m.contentWidth() - 2 // bar + space
	if fieldW < 10 {
		fieldW = 10
	}
	var b strings.Builder
	put := func(bar, row string) {
		pad := fieldW - lipgloss.Width(row)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(bar + " " + row + strings.Repeat(" ", pad))
		b.WriteString("\n")
	}
	if moreAbove {
		put(styleMenuBar.Render("┃"), stylePaletteRow.Render("↑ more"))
	}
	for i, it := range items {
		row := cellTruncate(fmt.Sprintf("/%-10s %s", it.name, it.desc), fieldW-2)
		if off+i == m.paletteSel {
			put(styleMenuBar.Render("┃"), stylePaletteSel.Render(" "+row+" "))
		} else {
			put(styleMenuBar.Render("┃"), stylePaletteRow.Render(row))
		}
	}
	if moreBelow {
		put(styleMenuBar.Render("┃"), stylePaletteRow.Render("↓ more"))
	}
	return styleMenu.Render(strings.TrimRight(b.String(), "\n"))
}

// approvalCard is opencode's inline permission block: a left-bordered
// panel in place of the composer (not a modal) — title, risk note, and
// the three governed choices resolved through the broker resume path.
func (m *agentTUI) approvalCard() string {
	title := m.approval.title
	if title == "" {
		title = "Ghost needs your approval"
	}
	risk := strings.ToLower(m.approval.risk)
	badge := styleRiskDefault.Render(" " + risk + " ")
	switch risk {
	case "high_impact":
		badge = styleRiskHigh.Render(" ◆ high impact ")
	case "consequential":
		badge = styleRiskMid.Render(" ◆ consequential ")
	case "low_risk":
		badge = styleRiskLow.Render(" ◆ low risk ")
	}
	bar := styleApprovalBar
	var b strings.Builder
	b.WriteString(bar.Render("┃") + " " + styleApprovalTitle.Render("△ Permission required") + "  " + badge)
	b.WriteString("\n")
	b.WriteString(bar.Render("┃") + " " + styleAssistant.Render(cellTruncate(title, m.contentWidth()-4)))
	b.WriteString("\n")
	if note := approvalRiskNote(m.approval.risk); note != "" {
		for _, wl := range wrapText(note, m.contentWidth()-4) {
			b.WriteString(bar.Render("┃") + " " + styleNotice.Render(wl))
			b.WriteString("\n")
		}
	}
	labels := []string{"[1] allow once", "[2] always allow", "[3] deny"}
	hints := "←→ select · enter confirm · esc leaves pending"
	// Lay out the options: one row when it fits, stacked rows when narrow.
	oneLine := "  " + strings.Join(labels, "    ") + "  " + hints
	var row strings.Builder
	if lipgloss.Width(oneLine)+2 <= m.contentWidth() {
		row.WriteString(bar.Render("┃") + " ")
		for i, l := range labels {
			if i == m.approvalSel {
				row.WriteString(styleApprovalSel.Render(" " + l + " "))
			} else {
				row.WriteString(styleApprovalKeys.Render(" " + l + " "))
			}
			row.WriteString("  ")
		}
		row.WriteString(styleNotice.Render(hints))
	} else {
		for i, l := range labels {
			if i > 0 {
				row.WriteString("\n")
			}
			if i == m.approvalSel {
				row.WriteString(bar.Render("┃") + " " + styleApprovalSel.Render(" "+l+" "))
			} else {
				row.WriteString(bar.Render("┃") + " " + styleApprovalKeys.Render(" "+l+" "))
			}
		}
		row.WriteString("\n")
		row.WriteString(bar.Render("┃") + " " + styleNotice.Render("←→ select · enter confirm · esc leaves pending"))
	}
	b.WriteString(row.String())
	return b.String()
}

// approvalRiskNote mirrors the mobile permission card's risk language so the
// CLI and the phone explain the stakes identically.
func approvalRiskNote(risk string) string {
	switch strings.ToLower(risk) {
	case "high_impact":
		return "This can be hard to undo, so Ghost stops for you every time."
	case "consequential":
		return "This acts on your behalf, so Ghost asks before doing it."
	case "low_risk":
		return "Ghost asks the first time; you can let it always do this."
	default:
		return ""
	}
}

func (m *agentTUI) statusLine() string {
	state := "ready"
	if m.approval != nil {
		state = "waiting for you"
	}
	if m.working {
		state = "working"
		if m.toolCount > 0 {
			state = fmt.Sprintf("working · %d tool%s", m.toolCount, plural(m.toolCount))
		}
	}
	if len(m.queued) > 0 {
		state += fmt.Sprintf(" · %d queued", len(m.queued))
	}
	parts := []string{m.loop.GetCurrentModel(), providerLocality(m.loop.GetCurrentModel()), m.session, state}
	line := " " + strings.Join(parts, " · ") + " "
	if lipgloss.Width(line) > m.width {
		line = " " + strings.Join(parts[:2], " · ") + " "
	}
	return styleStatus.Width(m.width).Render(line)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// providerLocality reports where a model runs, in owner language. It mirrors
// the mobile app's routing labels so the CLI and the phone never disagree
// about "where did this run".
func providerLocality(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "ollama"), strings.Contains(m, "qwen"), strings.Contains(m, "llama"):
		return "local"
	case strings.Contains(m, "deepseek"), strings.Contains(m, "gpt"), strings.Contains(m, "claude"),
		strings.Contains(m, "gemini"), strings.Contains(m, "kimi"), strings.Contains(m, "glm"):
		return "cloud"
	default:
		return "pod"
	}
}

// ─── styles ──────────────────────────────────────────────────────────────

var (
	cInk     = lipgloss.Color("#d8d4cc")
	cMuted   = lipgloss.Color("#8a857c")
	cFaint   = lipgloss.Color("#5c574f")
	cAccent  = lipgloss.Color("#8a86b8")
	cTool    = lipgloss.Color("#6f9c86")
	cErr     = lipgloss.Color("#c86a5c")
	cGold    = lipgloss.Color("#e8c06a")
	cBgBar   = lipgloss.Color("#141210")
	cBgPanel = lipgloss.Color("#1b1815")
	cBorder  = lipgloss.Color("#3a352f")
	cGreen   = lipgloss.Color("#7fb08a")
	cBlue    = lipgloss.Color("#7fa8c9")
	cViolet  = lipgloss.Color("#a89bc7")
	cCodeBg  = lipgloss.Color("#201c18")
	cSelBg   = lipgloss.Color("#2a251f")

	styleUser      = lipgloss.NewStyle().Foreground(cInk).Bold(true)
	styleAssistant = lipgloss.NewStyle().Foreground(cInk)
	styleTool      = lipgloss.NewStyle().Foreground(cTool)
	styleNotice    = lipgloss.NewStyle().Foreground(cMuted)
	styleError     = lipgloss.NewStyle().Foreground(cErr)
	styleWorking   = lipgloss.NewStyle().Foreground(cAccent)
	styleApproval  = lipgloss.NewStyle().Foreground(cGold).Bold(true)
	styleStatus    = lipgloss.NewStyle().Foreground(cMuted).Background(cBgBar)

	styleUserName      = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styleAssistantName = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	styleAssistantMeta = lipgloss.NewStyle().Foreground(cFaint)
	styleErrorCard     = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	styleToolActive    = lipgloss.NewStyle().Foreground(cGreen)
	// opencode user bubble: agent-color bar, panel background, plain text.
	styleUserBar   = lipgloss.NewStyle().Foreground(cAccent)
	styleUserText  = lipgloss.NewStyle().Foreground(cInk)
	styleUserPanel = lipgloss.NewStyle().Background(cBgPanel).Padding(0, 1)

	// opencode autocomplete menu: left split-border, menu background,
	// selected row highlighted (dialog.select pattern).
	styleMenu       = lipgloss.NewStyle().Background(cBgPanel)
	styleMenuBar    = lipgloss.NewStyle().Foreground(cBorder).Background(cBgPanel)
	stylePaletteRow = lipgloss.NewStyle().Foreground(cMuted).Background(cBgPanel)
	// ─── Ghost palette ───────────────────────────────────────────────
	// One semantic scheme across composer, menus, modal, and approvals
	// (terminal-ui skill: ux-color-semantics), reverse-engineered from
	// pi's DynamicBorder selectors and opencode's dialog.select:
	// violet = brand/selection, gold = approvals/warnings only,
	// green = success/done, red = errors, blue = info/links,
	// dim = hints/meta. Selected rows are dark-on-violet blocks, the
	// same language as the approval cursor.
	stylePaletteSel = lipgloss.NewStyle().Foreground(lipgloss.Color("#141210")).Background(cAccent).Bold(true)

	// Footer is transparent dim text (opencode muted footer) — no bar
	// background, so it sits on the terminal instead of a solid block.
	styleFooter     = lipgloss.NewStyle().Foreground(cFaint)
	styleFooterHint = lipgloss.NewStyle().Foreground(cFaint).Italic(true)

	// ux-color-semantics: the footer model carries its locality color so
	// "where it ran" reads at a glance.
	styleModelLocal = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
	styleModelCloud = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	styleModelPod   = lipgloss.NewStyle().Foreground(cMuted).Bold(true)

	// Composer + approval panels: single left `┃` bar (opencode composer),
	// agent-accent while working, gold for approvals.
	stylePromptBar       = lipgloss.NewStyle().Foreground(cBorder)
	stylePromptBarActive = lipgloss.NewStyle().Foreground(cAccent)
	stylePromptPanel     = lipgloss.NewStyle().Background(cBgPanel)
	styleApprovalBar     = lipgloss.NewStyle().Foreground(cGold)
	styleModalTitle      = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styleModalBox        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Background(cBgPanel).Padding(0, 1)
	styleApprovalTitle   = lipgloss.NewStyle().Foreground(cGold).Bold(true)
	styleApprovalKeys    = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc"))
	styleApprovalSel     = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGold).Bold(true)
	styleRiskHigh        = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(lipgloss.Color("#c86a5c")).Bold(true)
	styleRiskMid         = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGold).Bold(true)
	styleRiskLow         = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGreen).Bold(true)
	styleRiskDefault     = lipgloss.NewStyle().Foreground(cMuted).Background(cSelBg)

	styleWelcomeTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleGhostArt     = lipgloss.NewStyle().Foreground(cViolet).Bold(true)
	styleWelcomeCmds  = lipgloss.NewStyle().Foreground(cMuted)
	styleDayDivider   = lipgloss.NewStyle().Foreground(cFaint)

	// opencode markdown roles (dark default): headings violet bold (h1
	// underlined), strong orange, emphasis/quotes sand italic, code green
	// with no background, bullets peach, ordered numbers cyan, checked
	// green, links cyan underlined with the URL concealed.
	styleMDHead      = lipgloss.NewStyle().Foreground(lipgloss.Color("#9d7cd8")).Bold(true)
	styleMDHead1     = lipgloss.NewStyle().Foreground(lipgloss.Color("#9d7cd8")).Bold(true).Underline(true)
	styleMDStrong    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5a742")).Bold(true)
	styleMDEmph      = lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b")).Italic(true)
	styleMDQuote     = lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b")).Italic(true)
	styleMDQuoteMark = lipgloss.NewStyle().Foreground(cMuted)
	styleMDCode      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7fd88f"))
	styleMDCodeBlock = lipgloss.NewStyle().Foreground(cInk)
	styleMDList      = lipgloss.NewStyle().Foreground(lipgloss.Color("#fab283"))
	styleMDEnum      = lipgloss.NewStyle().Foreground(lipgloss.Color("#56b6c2"))
	styleMDCheck     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7fd88f"))
	styleMDUncheck   = lipgloss.NewStyle().Foreground(cMuted)
	styleMDLinkText  = lipgloss.NewStyle().Foreground(lipgloss.Color("#56b6c2")).Underline(true)
	styleMDLinkURL   = lipgloss.NewStyle().Foreground(lipgloss.Color("#fab283")).Underline(true)
	styleMDStrike    = lipgloss.NewStyle().Foreground(cMuted).Strikethrough(true)
	styleMDHR        = lipgloss.NewStyle().Foreground(cMuted)
)

func agentHelpText() string {
	return strings.Join([]string{
		"Commands  (/ + Tab completes, ↑/↓ picks)",
		"  /help              this help",
		"  /model [name]      show or switch the active model",
		"  /details           toggle tool step details",
		"  /new               start a fresh conversation",
		"  /sessions          show session and turn count",
		"  /memory [query]    ask what Ghost remembers",
		"  /context [name]    show or switch topic context (scoped memory/tools)",
		"  /rewind            put the last message back in the editor",
		"  /routines          what Ghost does for you",
		"  /clear             clear the screen",
		"  /quit              exit",
		"",
		"Keys",
		"  Enter              send (while working: queue a steering message)",
		"  Enter (question)   answer Ghost's in-flight question in the running turn",
		"  Tab                complete /command",
		"  Esc                abort the turn; queued text returns to the editor",
		"  Ctrl+C             clear editor; twice to quit",
		"  Ctrl+L             cycle model presets",
		"  Ctrl+O             toggle tool detail",
		"  PgUp/PgDn          scroll transcript",
	}, "\n")
}
