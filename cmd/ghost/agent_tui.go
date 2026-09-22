package main

// Ghost Agent CLI — the interactive terminal surface.
//
// Talk to Ghost, watch it work, approve what it asks, and see where each
// answer came from. The chrome is Ghost's own: a full-bleed composer ruled
// top and bottom, a command palette and pickers that open below the box,
// provenance ("where did this run"), memory, routines, and inline
// approvals.
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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ianclemence/ghost/pkg/providers"
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

// spinnerTickMsg advances the activity spinner (the Ghost thinking pulse).
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
	dur  time.Duration // assistant turns only (`· duration` trailing meta)
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
	// LoadHistory backfills one conversation so the terminal can switch
	// threads (e.g. back to the shared main conversation) and show the
	// same rows every other surface sees.
	LoadHistory(sessionKey string) ([]historyEntry, error)
}

type agentTUI struct {
	loop    agentRuntime
	session string

	input   textarea.Model
	entries []entry
	// printed is how many entries have been flushed to the terminal's own
	// scrollback (main screen). The transcript lives in the scrollback, not
	// in an app-owned viewport, so the terminal's native scrolling reaches
	// every previous message — the same model the opencode CLI uses.
	printed        int
	lastPrintedDay string

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

	// chrome state (display only, never semantics).
	turnCount   int       // completed turns this session
	turnStart   time.Time // when the current turn started
	elapsed     time.Duration
	spinFrame   int
	toolHistory []toolStep // product-language tool steps for the current turn
	paletteSel  int        // selected index in the / palette popup

	// approval is set when a turn ends with a durable permission request;
	// the composer is replaced by Allow once / Always allow / Deny choices.
	approval *pendingApproval
	// approvalSel is the left/right cursor over those
	// choices (1/2/3 still answer directly).
	approvalSel int
	// modal is the inline model picker below the composer. It owns the
	// keyboard until Enter picks or Esc closes.
	modal *selectModal
	// modelCycleIdx is our own position in the preset rotation, so Ctrl+L
	// advances even when the canonical active model matches no preset.
	modelCycleIdx int
	// scoped is the owner's enabled/ordered cycling set (nil = all enabled).
	// Loaded from the runtime on first use and persisted on Ctrl+S.
	scoped    providers.ScopedModels
	scopedSet bool // whether scoped has been loaded from the runtime
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

// toolStep is one collapsed tool row in the activity trail. Only the
// active step shows a spinner; finished steps collapse to ✓ rows (raw
// tool JSON is never streamed into the transcript).
type toolStep struct {
	tool  string // machine tool name (for the icon map)
	label string
	start time.Time
	done  bool
	dur   time.Duration
}

const agentPrompt = "› "

// mainConversationKey is the one shared conversation every surface talks
// into. It aliases MainSessionID (internal_api.go) so the two can never
// drift: the TUI, the gateway, and every channel resolve to the same key.
const mainConversationKey = MainSessionID

func newAgentTUI(loop agentRuntime, session string) *agentTUI {
	ta := textarea.New()
	// The composer has no placeholder and no prefix: two bare horizontal
	// rules with the text between them. Hints live in the footer and the
	// welcome card, not in the box.
	ta.Placeholder = ""
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.SetHeight(composerMinRows) // grows to fit content; cap applied in layout()
	ta.ShowLineNumbers = false
	// Transparent composer: the cursor is the only affordance. The stock
	// textarea paints the cursor line with a background and tints placeholder
	// grammar; both are stripped here so Ghost text sits on the terminal
	// untouched, per the "only the cursor, transparent background" rule.
	focused, blurred := textarea.DefaultStyles()
	for _, s := range []*textarea.Style{&focused, &blurred} {
		s.Base = lipgloss.NewStyle()
		s.CursorLine = lipgloss.NewStyle()
		s.CursorLineNumber = lipgloss.NewStyle()
		s.EndOfBuffer = lipgloss.NewStyle()
		s.LineNumber = lipgloss.NewStyle()
		s.Placeholder = lipgloss.NewStyle().Foreground(cFaint)
		s.Prompt = lipgloss.NewStyle()
		s.Text = lipgloss.NewStyle().Foreground(cInk)
	}
	ta.FocusedStyle = focused
	ta.BlurredStyle = blurred
	ta.Focus()

	return &agentTUI{
		loop:          loop,
		session:       session,
		input:         ta,
		histIndex:     -1,
		modelCycleIdx: -1,
	}
}

// ─── bubbletea lifecycle ─────────────────────────────────────────────────

func (m *agentTUI) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, spinnerTick())
}

func spinnerTick() tea.Cmd {
	// The activity pulse ticks at 80ms so motion reads evenly.
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

type teaMsg = tea.Msg

// Update handles one message and then flushes any newly-committed entries
// into the terminal scrollback, so transcript lines reach the terminal's
// own buffer where native scrolling can reach them.
func (m *agentTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.updateInner(msg)
	if flush := m.flushScrollback(); flush != nil {
		cmd = tea.Batch(cmd, flush)
	}
	return model, cmd
}

func (m *agentTUI) updateInner(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		if !m.ready {
			m.ready = true
			// A genuinely new conversation opens with the welcome card in
			// the scrollback; backfilled history prints instead (flush).
			return m, m.welcomeScrollback()
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
				// The final response wins. The stream buffer is a live
				// preview only — never concatenated with the final.
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
	// 1/2/3 (or a/A/d) answer directly; ←/→ (h/l) moves the
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
		// Escape is contextual cancel at every level: palette first, then
		// the running
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
		// Ctrl+L opens the picker; Ctrl+P cycles the enabled scope (the
		// model-picker convention).
		m.openModelModal()
		return m, nil

	case tea.KeyCtrlP:
		m.cycleModel()
		return m, nil

	case tea.KeyCtrlO:
		m.showTools = !m.showTools
		m.renderTranscript()
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

	case tea.KeyUp:
		if m.paletteVisible() {
			// The select list wraps top↔bottom.
			if n := len(m.paletteMatches()); n > 0 {
				m.paletteSel = (m.paletteSel - 1 + n) % n
			}
			return m, nil
		}
		m.recallHistory(-1)
		return m, nil
	case tea.KeyDown:
		if m.paletteVisible() {
			if n := len(m.paletteMatches()); n > 0 {
				m.paletteSel = (m.paletteSel + 1) % n
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
		m.input.Reset()
		if line == "" {
			return m, nil
		}
		if strings.HasPrefix(line, "/") {
			return m.runCommand(line)
		}
		return m, m.send(line)
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

// send starts a turn (or queues a steering message while one runs). It
// returns the command that keeps the activity spinner moving, so the cube
// before "thinking"/"searching" is always animating — the tick loop stops
// when a turn ends and must be restarted on the next turn.
func (m *agentTUI) send(text string) tea.Cmd {
	m.history = append(m.history, text)
	m.histIndex = -1
	m.paletteSel = 0

	if m.working {
		// Queue a steering message into the running turn.
		m.queued = append(m.queued, text)
		m.loop.InjectSteering(m.session, text)
		m.append(entry{kind: entryNotice, text: "↳ queued for the current turn: " + text})
		m.renderTranscript()
		return nil
	}

	m.append(entry{kind: entryUser, text: text, at: time.Now()})
	m.working = true
	m.toolCount = 0
	m.toolLine = ""
	m.toolHistory = nil
	m.streaming = ""
	m.turnStart = time.Now()
	m.elapsed = 0
	// A new turn always follows: the owner just acted. The user message is
	// flushed to the scrollback by Update's flush; the terminal shows it at
	// the bottom.
	go m.runTurn(text)
	return spinnerTick()
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
	return m, m.send(phrase)
}

// handleContext shows or switches the session's context. Contexts scope
// memory and tools — scoping a session without forking memory.
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

// paletteCommands is ordered the way the help lists them: discovery and
// state first, then memory and behavior, then conversation navigation,
// then display, with housekeeping and exit last.
var paletteCommands = []paletteItem{
	{"help", "list commands and keys"},
	{"session", "where this terminal is + model"},
	{"model", "show or switch model"},
	{"scoped-models", "pick models to cycle (ctrl+p)"},
	{"context", "topic space (scoped memory/tools)"},
	{"memory", "ask what Ghost remembers"},
	{"routines", "ask what Ghost has scheduled"},
	{"thread", "open a side thread"},
	{"main", "return to the shared conversation"},
	{"rewind", "edit and resend last message"},
	{"details", "toggle tool step details"},
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

// completePalette accepts a palette item: the command
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

// ─── model picker state ─────────────────────────────────────────────────
// Inline below-box selector with a title, a live filter, grouped rows and
// footer hints: ↑↓/ctrl+p/ctrl+n move, pgup/pgdn jump, home/end, return
// selects, esc closes. The filter narrows as you type; empty shows all.
type modalItem struct {
	label   string
	desc    string
	current bool
	// target is what pick receives (preset/connection name or
	// provider:model); usable false blocks picking with an explanation.
	target string
	usable bool
}

// modalMode selects the interaction semantics a modal uses. The default
// (modalSelect) confirms one row; modalScoped edits the enabled cycling set.
type modalMode int

const (
	modalSelect modalMode = iota // Enter picks the row (model picker, etc.)
	modalScoped                  // Enter toggles; ctrl+a/x all|clear; alt+↑↓ reorder
)

// pickerScope is the model picker's catalog scope: all usable models, or the
// owner's enabled subset. It mirrors Pi's "all" / "scoped" toggle.
type pickerScope int

const (
	scopeAll pickerScope = iota
	scopeScoped
)

type selectModal struct {
	title  string
	items  []modalItem
	sel    int
	filter string
	pick   func(label string)

	mode  modalMode
	scope pickerScope // model picker only
	dirty bool        // scoped mode: unsaved changes
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

// scopedModels returns the owner's enabled/ordered cycling set, loading it
// from the runtime once. A runtime without persistence keeps it in memory.
func (m *agentTUI) scopedModels() providers.ScopedModels {
	if !m.scopedSet {
		if ss, ok := m.loop.(scopedStore); ok {
			m.scoped = ss.GetScopedModels()
		}
		m.scopedSet = true
	}
	return m.scoped
}

// allModelOptions is the full catalog (unusable entries included) when the
// runtime exposes it, else the switchable set.
func (m *agentTUI) allModelOptions() []providers.ModelOption {
	if po, ok := m.loop.(allModelOptionProvider); ok {
		return po.AllModelOptions()
	}
	return m.modelOptions()
}

// modelItem builds one picker row from an option.
func (m *agentTUI) modelItem(o providers.ModelOption, cur string) modalItem {
	desc := o.Provider
	if o.Model != "" && o.Model != o.Provider {
		desc += " · " + shortModel(o.Model)
	}
	if loc := providerLocality(o.Provider + ":" + o.Model); loc != "" {
		desc += " · " + loc
	}
	if !o.Available {
		if o.Reason != "" {
			desc += " · unavailable: " + o.Reason
		} else {
			desc += " · unavailable"
		}
	}
	current := o.Target == cur || o.Name == cur ||
		modelBase(o.Target) == modelBase(cur) || modelBase(o.Model) == modelBase(cur)
	return modalItem{label: o.Name, desc: desc, current: current, target: o.Target, usable: o.Available}
}

// openModelModal opens the model picker in Pi style: it offers only models
// from configured providers, and Tab switches between that set (all) and the
// owner's enabled subset (scoped). Enter switches the active model.
func (m *agentTUI) openModelModal() {
	m.loop.RefreshModels()
	available := m.modelOptions()
	if len(available) == 0 {
		m.append(entry{kind: entryNotice, text: "no models configured — /login a provider or start a local engine"})
		m.renderTranscript()
		return
	}
	scope := scopeAll
	opts := available
	if sc := m.scopedModels(); !sc.AllEnabled() {
		if scopedOpts := providers.FilterScoped(available, sc); len(scopedOpts) > 0 && len(scopedOpts) < len(available) {
			scope = scopeScoped
			opts = scopedOpts
		}
	}
	m.modal = m.modelModal(scope, opts)
	m.renderTranscript()
}

// modelModal builds the picker modal for a given scope.
func (m *agentTUI) modelModal(scope pickerScope, opts []providers.ModelOption) *selectModal {
	cur := m.loop.GetCurrentModel()
	items := make([]modalItem, 0, len(opts))
	seen := map[string]bool{}
	for _, o := range opts {
		if o.Name == "" || seen[o.Name+o.Target] {
			continue
		}
		seen[o.Name+o.Target] = true
		items = append(items, m.modelItem(o, cur))
	}
	return &selectModal{
		title: "Models",
		items: items,
		scope: scope,
		pick:  func(target string) { m.setModel(target) },
	}
}

// applyModelScope rebuilds the open model modal for the other scope.
func (m *agentTUI) applyModelScope(scope pickerScope) {
	available := m.modelOptions()
	opts := available
	if scope == scopeScoped {
		opts = providers.FilterScoped(available, m.scopedModels())
		if len(opts) == 0 {
			opts = available
			scope = scopeAll
		}
	}
	m.modal = m.modelModal(scope, opts)
	m.renderTranscript()
}

// openScopedModal opens the enable/disable + reorder selector for the cycling
// set. It lists the full catalog so a model can be enabled before its provider
// is configured. Enter toggles, ctrl+a/ctrl+x all|clear, alt+↑/↓ reorder, and
// ctrl+s saves.
func (m *agentTUI) openScopedModal() {
	all := m.allModelOptions()
	if len(all) == 0 {
		m.append(entry{kind: entryNotice, text: "no models known — configure a provider first"})
		m.renderTranscript()
		return
	}
	m.modal = m.scopedModal(all)
	m.renderTranscript()
}

// scopedModal builds the scoped-models modal from the full catalog.
func (m *agentTUI) scopedModal(all []providers.ModelOption) *selectModal {
	sc := m.scopedModels()
	items := make([]modalItem, 0, len(all))
	for _, o := range all {
		enabled := sc.AllEnabled() || sc.IsEnabled(o.Target)
		desc := o.Provider
		if o.Model != "" && o.Model != o.Provider {
			desc += " · " + shortModel(o.Model)
		}
		if !o.Available {
			desc += " · unavailable"
		}
		items = append(items, modalItem{
			label:   o.Name,
			desc:    desc,
			target:  o.Target,
			usable:  o.Available,
			current: enabled,
		})
	}
	return &selectModal{
		title: "Models to cycle (ctrl+s saves)",
		items: items,
		mode:  modalScoped,
	}
}

// modelTargets maps the current scoped items (enabled order) to targets.
func scopedTargets(items []modalItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.current { // current marks "enabled" in scoped mode
			t := it.target
			if t == "" {
				t = it.label
			}
			out = append(out, t)
		}
	}
	return out
}

// toggleScoped flips one entry's enabled flag in the open scoped modal.
func (m *agentTUI) toggleScoped() {
	items := m.modalMatches()
	if len(items) == 0 {
		return
	}
	sel := items[m.modal.sel]
	// Find the row in the base slice and flip it.
	for i := range m.modal.items {
		if m.modal.items[i].target == sel.target && m.modal.items[i].label == sel.label {
			m.modal.items[i].current = !m.modal.items[i].current
			break
		}
	}
	m.modal.dirty = true
}

// saveScoped persists the scoped set (nil when every model is enabled).
func (m *agentTUI) saveScoped() {
	all := m.allModelOptions()
	ids := providers.NormalizeScoped(scopedTargets(m.modal.items), all)
	if ss, ok := m.loop.(scopedStore); ok {
		if err := ss.SetScopedModels(ids); err != nil {
			m.append(entry{kind: entryError, text: "could not save model scope: " + err.Error()})
			m.renderTranscript()
			return
		}
	}
	m.scoped.Set(ids)
	m.scopedSet = true
	if m.modal != nil {
		m.modal.dirty = false
	}
	n := len(ids)
	msg := fmt.Sprintf("model scope saved: %d enabled", n)
	if ids == nil {
		msg = "model scope saved: all enabled"
	}
	m.append(entry{kind: entryNotice, text: msg})
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
		// Scoped edits are session-local until saved (ctrl+s), matching the
		// model picker's scope toggle; leaving discards unsaved changes.
		m.modal = nil
		m.renderTranscript()
		return m, nil
	case tea.KeyTab:
		if m.modal.mode == modalSelect {
			// The scope toggle only exists once a scoped set is saved (Pi
			// gates it the same way); otherwise there is nothing to toggle.
			if m.scopedModels().AllEnabled() {
				m.append(entry{kind: entryNotice, text: "no scoped set yet — /scoped-models to enable a subset"})
				m.renderTranscript()
				return m, nil
			}
			next := scopeAll
			if m.modal.scope == scopeAll {
				next = scopeScoped
			}
			m.applyModelScope(next)
		}
		return m, nil
	case tea.KeyEnter:
		items := m.modalMatches()
		clamp()
		if len(items) == 0 {
			return m, nil
		}
		chosen := items[m.modal.sel]
		if m.modal.mode == modalScoped {
			// Toggle enable state; nothing persists until ctrl+s.
			m.toggleScoped()
			m.renderTranscript()
			return m, nil
		}
		if !chosen.usable {
			m.append(entry{kind: entryNotice, text: "that model isn't usable right now — " + chosen.desc})
			m.modal = nil
			m.renderTranscript()
			return m, nil
		}
		pick := m.modal.pick
		target := chosen.target
		if target == "" {
			target = chosen.label
		}
		m.modal = nil
		pick(target)
		return m, nil
	case tea.KeyUp:
		if msg.Alt && m.modal.mode == modalScoped {
			m.moveScoped(-1)
			return m, nil
		}
		m.modal.sel--
		clamp()
		return m, nil
	case tea.KeyDown:
		if msg.Alt && m.modal.mode == modalScoped {
			m.moveScoped(1)
			return m, nil
		}
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
		// ctrl+p / ctrl+n move; other runes filter.
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
	case "ctrl+s":
		if m.modal.mode == modalScoped {
			m.saveScoped()
		}
	case "ctrl+a":
		if m.modal.mode == modalScoped {
			m.setScopedAll(true)
		}
	case "ctrl+x":
		if m.modal.mode == modalScoped {
			m.setScopedAll(false)
		}
	}
	return m, nil
}

// moveScoped reorders the selected enabled row within the base item order.
func (m *agentTUI) moveScoped(delta int) {
	items := m.modalMatches()
	if len(items) == 0 {
		return
	}
	sel := items[m.modal.sel]
	idx := -1
	for i := range m.modal.items {
		if m.modal.items[i].target == sel.target && m.modal.items[i].label == sel.label {
			idx = i
			break
		}
	}
	if idx < 0 || !m.modal.items[idx].current {
		return
	}
	// Find the neighbouring enabled row in the same direction and swap.
	for j := idx + delta; j >= 0 && j < len(m.modal.items); j += delta {
		if !m.modal.items[j].current {
			continue
		}
		m.modal.items[idx], m.modal.items[j] = m.modal.items[j], m.modal.items[idx]
		m.modal.sel += delta
		m.modal.dirty = true
		return
	}
}

// setScopedAll enables or clears every row (scoped to the filter when one is
// active, matching the model picker's filtered all/clear).
func (m *agentTUI) setScopedAll(on bool) {
	filtered := map[string]bool{}
	if strings.TrimSpace(m.modal.filter) != "" {
		for _, it := range m.modalMatches() {
			filtered[it.target+"\x00"+it.label] = true
		}
	}
	for i := range m.modal.items {
		if len(filtered) > 0 {
			if !filtered[m.modal.items[i].target+"\x00"+m.modal.items[i].label] {
				continue
			}
		}
		m.modal.items[i].current = on
	}
	m.modal.dirty = true
	m.renderTranscript()
}

// ─── inline selector (model picker) ──────────────────────────────────────
// No floating modal: like the slash palette, the picker opens BELOW the
// prompt box and shares its list language (bare rows, `→ ` selection, a
// padded primary column, a dim description, a `(n/total)` scroll footer).
// A thin header line names the surface and the esc route; a filter line
// echoes typing. Nothing floats over the transcript, so context is never
// hidden.
const modalMaxRows = 6

func (m *agentTUI) modalHeight() int {
	if m.modal == nil {
		return 0
	}
	n := 3 // header + status + filter
	items := m.modalMatches()
	if len(items) == 0 {
		n++
	} else {
		n += minInt(len(items), modalMaxRows)
		if len(items) > modalMaxRows {
			n++ // scroll footer
		}
	}
	return n
}

func (m *agentTUI) modalWindow() (items []modalItem, off int) {
	all := m.modalMatches()
	if len(all) <= modalMaxRows {
		return all, 0
	}
	off = m.paletteOffset(m.modal.sel, modalMaxRows)
	end := off + modalMaxRows
	if end > len(all) {
		end = len(all)
	}
	return all[off:end], off
}

// modalStatus is the honest status line under a modal's header. It states the
// model picker's scope (and that it is configured-only), or the scoped
// editor's save state.
func (m *agentTUI) modalStatus() string {
	switch {
	case m.modal.mode == modalScoped:
		enabled := 0
		for _, it := range m.modal.items {
			if it.current {
				enabled++
			}
		}
		s := fmt.Sprintf("enter toggle · ctrl+a all · ctrl+x clear · alt+↑↓ reorder · ctrl+s save · %d/%d enabled", enabled, len(m.modal.items))
		if m.modal.dirty {
			s += " (unsaved)"
		}
		return s
	case m.modal.scope == scopeScoped:
		return "scope: scoped (your enabled set) · tab to switch · enter selects"
	default:
		return "scope: all · showing models from configured providers · tab to switch · enter selects"
	}
}

func (m *agentTUI) modalView() string {
	items, off := m.modalWindow()
	w := m.contentWidth()
	var b strings.Builder
	put := func(row string) {
		pad := w - lipgloss.Width(row)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(row + strings.Repeat(" ", pad))
		b.WriteString("\n")
	}
	header := styleModalTitle.Render(m.modal.title)
	esc := stylePaletteScroll.Render("esc")
	gap := w - lipgloss.Width(m.modal.title) - lipgloss.Width("esc")
	if gap < 1 {
		gap = 1
	}
	put(header + strings.Repeat(" ", gap) + esc)
	// Status line: the model picker names its scope honestly; the scoped
	// editor names the save affordance and flags unsaved changes.
	if hint := m.modalStatus(); hint != "" {
		put(stylePaletteNoMatch.Render("  " + hint))
	}
	filter := m.modal.filter
	if filter == "" {
		filter = "type to filter…"
	}
	put(stylePaletteNoMatch.Render("  " + filter + "▍"))
	if len(items) == 0 {
		put(stylePaletteNoMatch.Render("  No results found"))
		return strings.TrimRight(b.String(), "\n")
	}
	for i, it := range items {
		selected := off+i == m.modal.sel
		prefix := "  "
		if selected {
			prefix = "→ "
		}
		// Scoped mode: current means enabled (✓). Model picker: current means
		// the active model (●); unavailable rows are dimmed.
		mark := "  "
		if m.modal.mode == modalScoped {
			if it.current {
				mark = "✓ "
			}
		} else if it.current {
			mark = "● "
		}
		primary := cellTruncate(it.label, 26)
		spacing := strings.Repeat(" ", maxInt(1, 28-lipgloss.Width(primary)))
		remaining := w - len(prefix) - len(mark) - lipgloss.Width(primary) - len(spacing) - 2
		row := prefix + mark + primary + spacing
		if remaining > 10 && it.desc != "" {
			row += cellTruncate(it.desc, remaining)
		}
		switch {
		case selected:
			put(stylePaletteSel.Render(row))
		case m.modal.mode == modalSelect && !it.usable:
			put(stylePaletteNoMatch.Render(row))
		default:
			put(row)
		}
	}
	if total := len(m.modalMatches()); total > modalMaxRows {
		put(stylePaletteScroll.Render(fmt.Sprintf("  (%d/%d)", m.modal.sel+1, total)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *agentTUI) paletteVisible() bool {
	// The approval dialog and modals own the keyboard while up.
	if m.approval != nil || m.modal != nil {
		return false
	}
	// The list opens on the trigger and stays open to say "No matching
	// commands" — it never silently vanishes mid-typing.
	return strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/")
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
		m.resetTranscript()
		m.renderTranscript()
	case "thread":
		// Ghost is one conversation. /thread does not wipe it — it opens a
		// side thread so a tangent never pollutes the main thread.
		m.session = "cli:" + fmt.Sprintf("%d", time.Now().UnixNano())
		m.resetTranscript()
		m.turnCount = 0
		m.append(entry{kind: entryNotice, text: "side thread opened: " + m.session + "\nthis is a separate thread; /main returns to the shared conversation"})
		m.renderTranscript()
	case "main":
		// Return to the one shared conversation (the default every surface
		// talks into).
		m.session = mainConversationKey
		m.resetTranscript()
		m.turnCount = 0
		m.append(entry{kind: entryNotice, text: "back to the shared conversation: " + m.session})
		m.loadHistory()
		m.renderTranscript()
	case "session", "sessions":
		m.showSession()
	case "model", "models":
		if len(args) == 0 {
			m.openModelModal()
		} else {
			m.setModel(strings.Join(args, " "))
		}
	case "scoped-models":
		m.openScopedModal()
	case "details":
		m.showTools = !m.showTools
		m.append(entry{kind: entryNotice, text: fmt.Sprintf("tool details %s", onOff(m.showTools))})
	case "memory":
		return m, m.showMemory(args)
	case "context":
		m.handleContext(args)
	case "rewind":
		m.rewind()
	case "routines":
		return m, m.showRoutines()
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
	opts := m.modelOptions()
	if len(opts) == 0 {
		m.append(entry{kind: entryNotice, text: "no models configured"})
		m.renderTranscript()
		return
	}
	// Cycle within the owner's enabled subset (Pi's scoped rotation). When no
	// scope is saved, every usable model is eligible.
	sc := m.scopedModels()
	eligible := providers.FilterScoped(opts, sc)
	if len(eligible) == 0 {
		eligible = opts
	}
	cur := m.loop.GetCurrentModel()
	next, ok := providers.CycleScoped(eligible, providers.ScopedModels{}, cur, 1)
	if !ok {
		msg := "no usable model configured"
		if !sc.AllEnabled() && len(eligible) < 2 {
			msg = "only one model in scope — /scoped-models to enable more"
		}
		m.append(entry{kind: entryNotice, text: msg})
		m.renderTranscript()
		return
	}
	for i, o := range eligible {
		if o.Target == next.Target {
			m.modelCycleIdx = i
			break
		}
	}
	m.setModel(next.Target)
}

// modelOptionProvider is the optional full switchable set (presets +
// connections + keyed providers). Runtimes without it fall back to bare
// preset names.
type modelOptionProvider interface {
	ModelOptions() []providers.ModelOption
}

// allModelOptionProvider exposes the complete catalog (including unusable
// entries) so /scoped-models can pre-enable models for providers that will be
// configured later. Runtimes without it use the switchable set instead.
type allModelOptionProvider interface {
	AllModelOptions() []providers.ModelOption
}

// scopedStore persists the enabled/ordered cycling set. A nil slice means all
// enabled. Runtimes without it keep the scope in memory for the session.
type scopedStore interface {
	GetScopedModels() providers.ScopedModels
	SetScopedModels(ids []string) error
}

// modelOptions returns the selectable set the picker and cycling use: only
// entries that can actually serve. Unusable entries are dropped here so no
// surface offers a model that would fail on selection (Pi's "available").
func (m *agentTUI) modelOptions() []providers.ModelOption {
	raw := m.rawModelOptions()
	out := make([]providers.ModelOption, 0, len(raw))
	for _, o := range raw {
		if o.Available {
			out = append(out, o)
		}
	}
	return out
}

// rawModelOptions is the runtime's set as reported, before the usable filter.
func (m *agentTUI) rawModelOptions() []providers.ModelOption {
	if po, ok := m.loop.(modelOptionProvider); ok {
		if opts := po.ModelOptions(); len(opts) > 0 {
			return opts
		}
	}
	var out []providers.ModelOption
	for _, p := range m.loop.ModelPresets() {
		out = append(out, providers.ModelOption{Name: p, Target: p, Kind: "preset", Available: true})
	}
	return out
}

// modelBase strips provider prefixes and case so a preset name matches
// the canonical active model ("provider:model", "provider/model").
func modelBase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexAny(s, ":/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func (m *agentTUI) setModel(name string) {
	if err := m.loop.SetModel(name); err != nil {
		m.append(entry{kind: entryError, text: "model: " + err.Error()})
	} else {
		m.append(entry{kind: entryNotice, text: "model → " + m.loop.GetCurrentModel()})
	}
	m.renderTranscript()
}

// resetTranscript starts a fresh transcript epoch: entries are dropped and
// the scrollback cursor (printed) plus the day-divider cursor
// (lastPrintedDay) restart with them. Resetting the slice without the
// cursors silently swallows every later reply — committed entries sit past
// a stale cursor and flushScrollback considers them already printed (the
// "user messages with no Ghost response" shape).
func (m *agentTUI) resetTranscript() {
	m.entries = nil
	m.toolHistory = nil
	m.streaming = ""
	m.printed = 0
	m.lastPrintedDay = ""
}

// loadHistory refills the transcript from the current conversation's stored
// rows, used after switching threads so the terminal shows the same rows
// every other surface sees.
func (m *agentTUI) loadHistory() {
	entries, err := m.loop.LoadHistory(m.session)
	if err != nil {
		m.append(entry{kind: entryNotice, text: "could not load this conversation's history: " + friendlyAgentError(err)})
		return
	}
	m.entries = nil
	for _, h := range entries {
		at := time.Unix(h.Timestamp, 0)
		if h.Timestamp <= 0 {
			at = time.Time{}
		}
		switch h.Role {
		case "user":
			m.entries = append(m.entries, entry{kind: entryUser, text: h.Content, at: at})
		case "assistant":
			m.entries = append(m.entries, entry{kind: entryAssistant, text: h.Content, at: at})
		}
	}
	m.turnCount = 0
	for _, e := range m.entries {
		if e.kind == entryAssistant {
			m.turnCount++
		}
	}
}

// showSession reports where this terminal is in Ghost's single conversation
// model: the shared conversation, or a named side thread — plus model,
// turns, and the topic contexts available.
func (m *agentTUI) showSession() {
	where := "the shared conversation (every surface talks here)"
	if m.session != mainConversationKey {
		where = "a side thread — /main returns to the shared conversation"
	}
	lines := []string{
		"conversation: " + m.session + " — " + where,
		fmt.Sprintf("model: %s · %d turn%s", m.loop.GetCurrentModel(), m.turnCount, plural(m.turnCount)),
	}
	if ctxs := m.loop.ListContexts(); len(ctxs) > 0 {
		lines = append(lines, "context: "+m.loop.CurrentContext(m.session)+" ("+strings.Join(ctxs, ", ")+")")
	}
	m.append(entry{kind: entryNotice, text: strings.Join(lines, "\n")})
}

func (m *agentTUI) showMemory(args []string) tea.Cmd {
	// Delegate to the same read-only paths the console uses: ask Ghost in a
	// turn so memory retrieval stays governed and scoped.
	q := "What do you remember about me?"
	if len(args) > 0 {
		q = "From memory, tell me about: " + strings.Join(args, " ")
	}
	return m.send(q)
}

func (m *agentTUI) showRoutines() tea.Cmd {
	return m.send("What routines do you have scheduled for me?")
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

// spinnerFrames is the "cube" that turns before the activity word in the
// prompt ("▖ Thinking", "▘ Searching"). The four quadrant blocks rotate
// clockwise, reading as a spinning cube. Rendered from spinFrame, advanced
// by the spinner tick while a turn runs, so it is always in motion.
var spinnerFrames = []string{"▖", "▘", "▝", "▗"}

func (m *agentTUI) spinner() string {
	if len(spinnerFrames) == 0 {
		return ""
	}
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

// The transcript lives in the terminal's own scrollback (main screen), not
// in an app-owned viewport. Committed entries are printed once with
// tea.Println, so the terminal's native scrolling — wheel, scrollbar,
// PageUp, and its own buffer — reaches every previous message. This is the
// model the opencode CLI uses, and it is why history is never lost.

// flushScrollback prints any committed entries that have not yet reached
// the scrollback. It is the only place transcript lines are emitted; the
// live View() below renders just the streaming preview, composer and
// footer.
func (m *agentTUI) flushScrollback() tea.Cmd {
	if !m.ready || m.printed >= len(m.entries) {
		return nil
	}
	text := m.pendingScrollback()
	m.printed = len(m.entries)
	return tea.Println(text)
}

// pendingScrollback renders the not-yet-printed entries (with day dividers)
// and advances the printed-day cursor. Split out so it can be asserted in
// tests without a running tea.Program.
func (m *agentTUI) pendingScrollback() string {
	var b strings.Builder
	prevDay := m.lastPrintedDay
	for i := m.printed; i < len(m.entries); i++ {
		e := m.entries[i]
		if d := dayLabel(e.at); d != "" && d != prevDay {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.renderDayDivider(d))
			b.WriteString("\n")
			prevDay = d
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderEntry(e))
		b.WriteString("\n")
	}
	m.lastPrintedDay = prevDay
	return strings.TrimRight(b.String(), "\n")
}

// welcomeScrollback prints the welcome card into the scrollback for a
// genuinely new conversation, so it behaves like the first printed entry.
func (m *agentTUI) welcomeScrollback() tea.Cmd {
	if len(m.entries) > 0 || m.printed > 0 {
		return nil
	}
	return tea.Println(m.welcomeCard())
}

// renderTranscript is kept as the callers' "content changed" signal. With
// the scrollback model it simply flushes newly-committed entries; the live
// View() picks up streaming/composer changes on the next frame.
func (m *agentTUI) renderTranscript() tea.Cmd {
	return m.flushScrollback()
}

// dockPreviewRows is the fixed height of the live preview area directly
// above the composer. It is always present — blank when idle — so the dock
// never changes height at the moment a turn commits. A stable dock is what
// keeps tea.Println (the printed reply) from being clobbered by the next
// repaint; a view that shrank on the same tick lost the reply on screen.
const dockPreviewRows = 1

// dockPreview is the live area above the composer, always exactly
// dockPreviewRows lines: the tail of the streaming reply with a caret while
// a turn runs, blank when idle. The active step is named in the composer's
// top rule; the full tool trail is behind /details (Ctrl+O).
func (m *agentTUI) dockPreview() string {
	w := m.contentWidth()
	line := ""
	if m.working && m.streaming != "" {
		lines := wrapText(strings.ReplaceAll(m.streaming, "\n", " ")+"▍", w)
		if len(lines) > 0 {
			line = styleAssistant.Render(lines[len(lines)-1])
		}
	}
	// Pad to exactly dockPreviewRows lines so the dock height is constant.
	rows := []string{line}
	for len(rows) < dockPreviewRows {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}

// toolIcon maps Ghost tools to the collapsed-row icon language:
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
	tw := m.textWidth()
	switch e.kind {
	case entryUser:
		// User bubble: a name line, then a left-bar panel with the raw
		// text (never markdown-rendered), paragraphs preserved.
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
		head := " " + styleAssistantName.Render(logo+" Ghost · "+m.loop.GetCurrentModel())
		if e.dur > 0 {
			head += styleAssistantMeta.Render(" · " + formatElapsed(e.dur))
		}
		return head + "\n" + m.assistantBlock(e.text)
	case entryTool:
		return styleTool.Render("  ✓ " + cellTruncate(e.text, tw-4))
	case entryNotice:
		return styleNotice.Render("  · " + cellTruncate(e.text, tw-2))
	case entryError:
		var lines []string
		for _, wl := range wrapText(e.text, tw-2) {
			lines = append(lines, "  "+wl)
		}
		return styleErrorCard.Render(strings.Join(lines, "\n"))
	}
	return e.text
}

// assistantBlock renders markdown at the transcript text column and insets
// it by one cell so assistant prose lines up with the user bubble's inner
// text.
func (m *agentTUI) assistantBlock(text string) string {
	body := renderAssistantBody(text, m.textWidth())
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		lines[i] = " " + ln
	}
	return strings.Join(lines, "\n")
}

// welcomeCard is the empty state: the Ghost banner stays exactly as it
// was, but centered like a chat app's conversation start. Rendered
// directly, never stored as an entry, so it can never duplicate.
func (m *agentTUI) welcomeCard() string {
	w := m.contentWidth()
	center := func(s string) string {
		var lines []string
		for _, ln := range strings.Split(s, "\n") {
			lines = append(lines, lipgloss.PlaceHorizontal(w, lipgloss.Center, ln))
		}
		return strings.Join(lines, "\n")
	}
	// The tagline is the Ghost promise: three declaratives, no filler. It
	// wraps on narrow terminals so it is never cut mid-word.
	art := styleGhostArt.Render("▓▒░  👻  G H O S T  ░▒▓")
	tag := styleWelcomeTitle.Render(wrapFirst(ghostTagline, minInt(w-2, 64)))
	cmds := styleWelcomeCmds.Render("  /help      commands & keys\n  /model     switch thinking engine\n  /memory    what Ghost remembers\n  /routines  recurring work")
	return center(art) + "\n" + center(tag) + "\n\n" + center(cmds)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── width helpers ─────────────────────────────────────────────────────
// contentWidth is the full terminal width. Every region runs edge to edge
// — the composer rules, the user/tool background blocks, the palette and
// the footer all share the same canvas — and only the text inside a block
// is inset by one cell, so nothing is left-shifted against the composer
// lines.
func (m *agentTUI) contentWidth() int {
	w := m.width
	if w < 20 {
		w = 20
	}
	return w
}

// textWidth is the transcript text column: the full canvas minus the
// 1-cell inner padding on each side. Renderers wrap/truncate to this and
// then pad the row out to contentWidth().
func (m *agentTUI) textWidth() int {
	w := m.contentWidth() - 2
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

// ─── markdown (Ghost theme, no new deps) ────────────────────────────────
// Ghost's TUI markdown roles: markers are concealed (no ``` fences, no
// #/backtick/bracket noise, URLs hidden behind cyan underlined labels),
// headings bold violet (h1 underlined), **bold** orange, *italic*/quotes
// sand italic, code green with no background, bullets peach, ordered
// numbers cyan, checked green.
func renderAssistantBody(text string, width int) string {
	if width < 20 {
		width = 20
	}
	var out []string
	inCode := false
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
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
		// A table is a header row followed by a delimiter row (| --- |).
		// Render it as a box-drawn grid, matching the opencode CLI.
		if i+1 < len(lines) && isTableRow(trim) && isTableDelimiter(strings.TrimSpace(lines[i+1])) {
			end := i + 2
			for end < len(lines) && isTableRow(strings.TrimSpace(lines[end])) {
				end++
			}
			out = append(out, renderTable(lines[i:end], width)...)
			i = end - 1
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

// ─── markdown tables (opencode-style) ───────────────────────────────────
// A table renders as a box-drawn grid: `┌─┬─┐` / `├─┼─┤` / `└─┴─┘`, a bold
// header row, a separator between every row, and inline styling inside
// cells. Columns size to their natural width and shrink to fit the
// available space, wrapping long cells; if even that cannot fit, the raw
// markdown is shown rather than a broken grid.

// isTableRow reports whether a line is a pipe-delimited table row. A
// single pipe is enough (a two-column table without outer pipes).
func isTableRow(s string) bool {
	return strings.Count(s, "|") >= 1
}

// isTableDelimiter reports whether a line is a markdown table delimiter
// (e.g. `| --- | :--: |`), allowing alignment colons.
func isTableDelimiter(s string) bool {
	if !isTableRow(s) {
		return false
	}
	cells := splitTableRow(s)
	if len(cells) < 2 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' {
				return false
			}
		}
		if !strings.Contains(c, "-") {
			return false
		}
	}
	return true
}

// splitTableRow splits a pipe row into trimmed cells. The leading and
// trailing pipes are optional and dropped.
func splitTableRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// renderTable renders a parsed markdown table into box-drawn lines.
func renderTable(block []string, width int) []string {
	if len(block) < 2 {
		return wrapText(strings.Join(block, "\n"), width)
	}
	header := splitTableRow(block[0])
	numCols := len(header)
	if numCols == 0 {
		return wrapText(strings.Join(block, "\n"), width)
	}
	var rows [][]string
	for _, ln := range block[2:] {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		row := splitTableRow(ln)
		// Normalize to the header's column count.
		for len(row) < numCols {
			row = append(row, "")
		}
		if len(row) > numCols {
			row = row[:numCols]
		}
		rows = append(rows, row)
	}

	// Border overhead: "│ " + (n-1) * " │ " + " │" = 3n + 1.
	borderOverhead := 3*numCols + 1
	availableForCells := width - borderOverhead
	if availableForCells < numCols {
		// Too narrow for a stable grid: show the raw markdown.
		return wrapText(strings.Join(block, "\n"), width)
	}

	// Natural (unwrapped) and minimum (longest word) column widths.
	const maxUnbrokenWord = 30
	natural := make([]int, numCols)
	minWord := make([]int, numCols)
	measure := func(cells []string) {
		for i, c := range cells {
			plain := stripInline(c)
			if w := lipgloss.Width(plain); w > natural[i] {
				natural[i] = w
			}
			if w := longestWordWidth(plain, maxUnbrokenWord); w > minWord[i] {
				minWord[i] = w
			}
		}
	}
	measure(header)
	for _, r := range rows {
		measure(r)
	}
	for i := range minWord {
		if minWord[i] < 1 {
			minWord[i] = 1
		}
	}

	widths := fitColumns(natural, minWord, availableForCells)
	// Ghost's wrapText keeps long words whole, so a word wider than its
	// column would burst the grid. If that happens, the table is too
	// narrow to render cleanly — show the raw markdown instead.
	for i, w := range widths {
		if minWord[i] > w {
			return wrapText(strings.Join(block, "\n"), width)
		}
	}

	var out []string
	var sb strings.Builder
	borderLine := func(left, mid, right string) string {
		sb.Reset()
		sb.WriteString(left)
		for i, w := range widths {
			if i > 0 {
				sb.WriteString(mid)
			}
			sb.WriteString(strings.Repeat("─", w))
		}
		sb.WriteString(right)
		return styleMDTableBorder.Render(sb.String())
	}

	out = append(out, borderLine("┌─", "─┬─", "─┐"))
	out = append(out, renderTableRowPadded(header, widths, styleMDTableHead)...)
	out = append(out, borderLine("├─", "─┼─", "─┤"))
	for ri, row := range rows {
		out = append(out, renderTableRowPadded(row, widths, styleMDTableRow)...)
		if ri < len(rows)-1 {
			out = append(out, borderLine("├─", "─┼─", "─┤"))
		}
	}
	out = append(out, borderLine("└─", "─┴─", "─┘"))
	return out
}

// fitColumns sizes columns to fit availableForCells: natural widths when
// they fit, otherwise each column keeps at least its longest word and the
// remaining space is distributed proportionally (opencode's algorithm).
func fitColumns(natural, minWord []int, availableForCells int) []int {
	n := len(natural)
	totalNatural := 0
	minCells := 0
	for i := range natural {
		totalNatural += natural[i]
		minCells += minWord[i]
	}
	if totalNatural <= availableForCells {
		out := make([]int, n)
		copy(out, natural)
		return out
	}
	// If even the minimums overflow, distribute what we have by weight.
	base := make([]int, n)
	if minCells <= availableForCells {
		copy(base, minWord)
	} else {
		for i := range base {
			base[i] = 1
		}
		extra := availableForCells - n
		if extra > 0 {
			totalWeight := 0
			for _, w := range minWord {
				if w-1 > 0 {
					totalWeight += w - 1
				}
			}
			for i, w := range minWord {
				if totalWeight > 0 && w-1 > 0 {
					base[i] += (w - 1) * extra / totalWeight
				}
			}
		}
	}
	// Grow toward natural widths with the leftover space.
	allocated := 0
	for _, w := range base {
		allocated += w
	}
	remaining := availableForCells - allocated
	for remaining > 0 {
		grew := false
		for i := 0; i < n && remaining > 0; i++ {
			if base[i] < natural[i] {
				base[i]++
				remaining--
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return base
}

// renderTableRowPadded wraps each cell to its column width, pads it, and
// joins the cells with the box's vertical separators. Header cells take the
// header style; body cells take the row style.
func renderTableRowPadded(cells []string, widths []int, style lipgloss.Style) []string {
	wrapped := make([][]string, len(cells))
	height := 1
	for i := range cells {
		wrapped[i] = wrapText(stripInline(cells[i]), maxInt(1, widths[i]))
		if len(wrapped[i]) > height {
			height = len(wrapped[i])
		}
	}
	var out []string
	for line := 0; line < height; line++ {
		var sb strings.Builder
		sb.WriteString(styleMDTableBorder.Render("│"))
		for i := range cells {
			var piece string
			if line < len(wrapped[i]) {
				piece = wrapped[i][line]
			}
			pad := widths[i] - lipgloss.Width(piece)
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(" ")
			sb.WriteString(style.Render(piece + strings.Repeat(" ", pad)))
			sb.WriteString(" ")
			sb.WriteString(styleMDTableBorder.Render("│"))
		}
		out = append(out, sb.String())
	}
	return out
}

// stripInline removes the markdown inline markers so widths are measured on
// the visible text. Cell content is rendered plainly (the grid already
// provides structure); this keeps measuring and painting in agreement.
func stripInline(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "~~", "")
	s = strings.Trim(s, "*_")
	return s
}

// longestWordWidth is the widest single word in s, capped at max.
func longestWordWidth(s string, max int) int {
	best := 0
	for _, w := range strings.Fields(s) {
		if n := lipgloss.Width(w); n > best {
			best = n
		}
	}
	if best > max {
		best = max
	}
	return best
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

// renderInline applies the inline roles in conceal-safe order:
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
// Layout: no top header — the transcript owns the full height. The bottom
// stack is palette popup + prompt box + 2-line footer.
// layout sizes the live bottom region only. The transcript is not laid out
// here: it is printed once into the terminal's scrollback (see
// flushScrollback), so there is no app-owned viewport to size.
func (m *agentTUI) layout() {
	m.input.SetWidth(m.inputWidth())
	m.input.SetHeight(m.composerRows())
	// SetHeight changes the visible window but does not reposition the
	// textarea's internal viewport, and repositioning only works once the
	// wrapped content has been rebuilt against the new height. Render once
	// (to rebuild the viewport content), then nudge so repositionView runs
	// against the new geometry — the caret stays in view (the whole text
	// while it fits, the last rows once past the cap).
	_ = m.input.View()
	m.input, _ = m.input.Update(tea.KeyMsg{})
}

// The composer is responsive, exactly like a real terminal editor: it opens
// at one text row, grows as the sentence wraps onto new rows, and, once it
// reaches its cap, scrolls inside the box instead of growing further. The
// cap is 30% of the terminal height with a floor of five rows, so the
// composer never swallows the transcript on a small terminal.
const composerMinRows = 1

// composerCapRows is the maximum visible text rows: seven, and never more
// than 30% of the terminal. Past it the composer stops growing and scrolls
// to keep the cursor visible, so a long message never swallows the
// transcript. Seven rows is the product cap the user asked for.
const composerMaxRows = 7

func (m *agentTUI) composerCapRows() int {
	n := m.height * 3 / 10
	if n < 5 {
		n = 5
	}
	if n > composerMaxRows {
		n = composerMaxRows
	}
	return n
}

// composerContentRows is the number of visual rows the current input needs
// when wrapped to the composer width. It must match how the textarea
// actually wraps — word wrap, but hard-breaking a word longer than the
// width (a long URL or a long token still grows the box). Measuring with
// wrapText alone undercounts such input and the composer fails to grow.
func (m *agentTUI) composerContentRows() int {
	w := m.inputWidth()
	v := m.input.Value()
	if v == "" {
		return composerMinRows
	}
	n := 0
	for _, line := range strings.Split(v, "\n") {
		n += visualRowCount(line, w)
	}
	if n < composerMinRows {
		n = composerMinRows
	}
	return n
}

// visualRowCount is how many terminal rows one logical line occupies at
// width w, word-wrapping and hard-breaking words longer than w — the
// wrapping the composer's textarea applies.
func visualRowCount(s string, w int) int {
	if w < 1 {
		w = 1
	}
	if s == "" {
		return 1
	}
	rows := 1
	col := 0
	for _, word := range strings.Fields(s) {
		ww := lipgloss.Width(word)
		if col == 0 {
			// A word wider than the row hard-breaks across rows.
			for ww > w {
				rows++
				ww -= w
			}
			col = ww
			continue
		}
		if col+1+ww > w {
			rows++
			col = 0
			for ww > w {
				rows++
				ww -= w
			}
			col = ww
			continue
		}
		col += 1 + ww
	}
	return rows
}

// composerRows is the visible text height: content clamped to the cap.
func (m *agentTUI) composerRows() int {
	n := m.composerContentRows()
	if cap := m.composerCapRows(); n > cap {
		n = cap
	}
	return n
}

// estimatedInputHeight measures the composer exactly as promptBox paints
// it: the visible text rows plus the two rules. Measuring from the same
// composerRows() the paint uses means the estimate and the render can
// never disagree as the box grows and shrinks.
func (m *agentTUI) estimatedInputHeight() int {
	return m.composerRows() + 2
}

// estimatedApprovalHeight measures the exact inline approval block. The
// block stacks its three options on narrow terminals and lays them on one
// row when they fit, so the height is width-dependent; measuring the paint
// keeps layout and render from ever disagreeing.
func (m *agentTUI) estimatedApprovalHeight() int {
	if m.approval == nil {
		return 0
	}
	return len(strings.Split(m.approvalCard(), "\n"))
}

func (m *agentTUI) inputWidth() int {
	// Composer: full-bleed text, no gutter, no side borders.
	w := m.width
	if w < 20 {
		w = 20
	}
	return w
}

func (m *agentTUI) paletteHeight() int {
	if !m.paletteVisible() {
		return 0
	}
	items, _, more := m.paletteWindow()
	n := len(items)
	if n == 0 {
		n = 1 // "No matching commands" row
	}
	if more {
		n++ // the `(n/total)` scroll footer
	}
	return n // bare rows, no border, no side bar
}

// paletteOffset returns the first visible row so the selection stays in a
// maxRows window, centered on the selection.
func (m *agentTUI) paletteOffset(total, maxRows int) int {
	if total <= maxRows {
		return 0
	}
	off := m.paletteSel - maxRows/2
	if off+maxRows > total {
		off = total - maxRows
	}
	if off < 0 {
		off = 0
	}
	return off
}

// ─── view ────────────────────────────────────────────────────────────────
// The View is only the live bottom region — "the dock": the in-progress
// stream preview, the composer (or approval card), the palette / model
// picker, and the footer. Committed transcript lines are printed into the
// terminal's scrollback (flushScrollback), so the terminal owns scrolling
// and every previous message stays reachable.
//
// The dock keeps a STABLE height: the preview area is always reserved (blank
// when idle), so the live view does not change height at the exact frame a
// turn commits. Bubbletea only repaints the lines the view occupies; a view
// that shrinks in the same tick as tea.Println can clobber the just-printed
// reply — which is why a response sometimes only appeared after reopening.
func (m *agentTUI) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "starting Ghost…"
	}
	var b strings.Builder
	// The preview area is ALWAYS present and always dockPreviewRows tall,
	// whether or not a turn is running — a stable anchor for the dock.
	b.WriteString(m.dockPreview())
	b.WriteString("\n")
	if m.approval != nil {
		b.WriteString(m.approvalCard())
	} else {
		b.WriteString(m.promptBox())
	}
	b.WriteString("\n")
	// Both the slash palette and the model picker render BELOW the
	// composer — Ghost floats no modal over the transcript.
	if m.paletteVisible() {
		b.WriteString(m.paletteView())
		b.WriteString("\n")
	}
	if m.modal != nil {
		b.WriteString(m.modalView())
		b.WriteString("\n")
	}
	for i, ln := range m.footerLines() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ln)
	}
	return b.String()
}

// ─── footer ─────────────────────────────────────────────────────────────
// Two dim lines under the prompt box (ux-color-semantics: hints dim, the
// model in its locality color, never decorative):
// line 1: session digest left, `(locality) model` right-aligned.
// line 2: contextual shortcut hints for the current state.
// State and model live here — nowhere else.
func (m *agentTUI) footerLines() []string {
	return []string{m.footerStatsLine(), m.footerKeysLine()}
}

// ghostTagline is Ghost's promise, shown on the welcome card — not in the
// footer. It is the owner-facing identity line.
const ghostTagline = "Your AI. Your Memory. Your Machine."

// footerStatsLine is the session digest on the left, `(locality) model`
// right-aligned with a 2-space minimum gap, truncating gracefully. The
// model carries its locality color (ux-color-semantics: green local, blue
// cloud, muted pod) so "where it ran" reads at a glance.
func (m *agentTUI) footerStatsLine() string {
	model := m.loop.GetCurrentModel()
	local := providerLocality(model)
	// The left half is a quiet Ghost session digest. The live spinner
	// lives only in the composer's top rule, never here, so the working
	// state is stated exactly once.
	left := m.footerSummary()
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
	case m.modal != nil && m.modal.mode == modalScoped:
		keys = "type to filter · enter toggle · ctrl+a/x all/clear · alt+↑↓ reorder · ctrl+s save · esc close"
	case m.modal != nil:
		keys = "type to filter · ↑↓ pick · tab scope · enter select · esc close"
	case m.clarify != nil:
		keys = "type your answer · enter sends · esc aborts the question"
	case m.approval != nil:
		keys = "1 allow once · 2 always allow · 3 deny · esc leaves pending"
	case m.working:
		keys = "enter queues steering · esc aborts · ctrl+o details · / commands"
	case strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/"):
		keys = "↑↓ pick · tab/enter complete · esc dismiss"
	default:
		// Ordered by frequency: discover commands, complete, switch model,
		// then the exit route last so it is never the first thing hit.
		keys = "/ commands · tab complete · ctrl+l model · ctrl+p cycle · esc quit"
	}
	return styleFooterHint.Render(cellTruncate(keys, m.width))
}

// footerSummary is the quiet left half of the stats line: a stable,
// glanceable digest of this Ghost session that does not animate. Every
// figure is real Ghost state — turns completed this session, how many
// topic contexts exist (Ghost's scoped-memory model), and how many
// steering messages are queued. The live spinner lives only in the
// composer's top rule, never here, so the working state is stated once.
func (m *agentTUI) footerSummary() string {
	if m.approval != nil {
		return "waiting for you"
	}
	var parts []string
	if m.turnCount > 0 {
		parts = append(parts, fmt.Sprintf("%d turn%s", m.turnCount, plural(m.turnCount)))
	} else {
		parts = append(parts, "ready")
	}
	n := 0
	func() {
		defer func() { _ = recover() }()
		n = len(m.loop.ListContexts())
	}()
	if n > 1 {
		parts = append(parts, fmt.Sprintf("%d contexts", n))
	}
	if q := len(m.queued); q > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", q))
	}
	return strings.Join(parts, " · ")
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

// activityWord is the live status embedded in the composer's top rule
// while a turn runs. It names what Ghost is doing right now — the active
// tool ("Searching the web…", "Reading notes.md") when one is running,
// otherwise "thinking" — followed by how long, how many tools, and
// anything queued.
func (m *agentTUI) activityWord() string {
	if m.approval != nil {
		return "waiting for you"
	}
	if m.working {
		// The prompt names the live activity and elapsed time only — no
		// tool count (that detail lives in the tool trail behind /details).
		s := fmt.Sprintf("%s %s", m.spinner(), m.activeStepWord())
		if m.elapsed > 0 {
			s += fmt.Sprintf(" · %s", formatElapsed(m.elapsed))
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

// activeStepWord is the current tool's product-language label when a tool
// is running, or "thinking" when Ghost is reasoning without a tool. The
// composer's top rule shows this so the prompt names the real activity
// instead of a generic status.
func (m *agentTUI) activeStepWord() string {
	for i := len(m.toolHistory) - 1; i >= 0; i-- {
		if !m.toolHistory[i].done {
			if label := strings.TrimSpace(m.toolHistory[i].label); label != "" {
				return label
			}
			break
		}
	}
	return "Thinking"
}

func shortModel(s string) string {
	if len(s) > 28 {
		return s[:27] + "…"
	}
	return s
}

// ─── prompt composer ───────────────────────────────────────────────────
// The composer: a top and bottom `─` rule at full width, NO side borders,
// NO prefix, NO placeholder — just the text between two lines. While a
// turn runs, the live status embeds in the top rule
// (`── ⠋ thinking · 4s · 2 tools ──…`). The rule lines themselves keep
// their idle color at all times — only the status text takes the accent —
// so the prompt chrome never changes colour while Ghost works.
func (m *agentTUI) promptBox() string {
	var b strings.Builder
	b.WriteString(m.composerTopRule())
	for _, ln := range strings.Split(m.input.View(), "\n") {
		b.WriteString("\n")
		b.WriteString(ln)
	}
	b.WriteString("\n")
	b.WriteString(stylePromptBar.Render(strings.Repeat("─", m.width)))
	return b.String()
}

// composerTopRule is `── <spinner> thinking … ──…` while a turn runs, a
// plain rule while idle. Always exactly m.width cells. The `─` fill stays
// the idle bar color; only the embedded status is accented.
func (m *agentTUI) composerTopRule() string {
	w := m.width
	if w < 10 {
		w = 10
	}
	if !m.working {
		return stylePromptBar.Render(strings.Repeat("─", w))
	}
	// activityWord already carries the spinner cube + the activity word;
	// embedding it here gives the `── ▖ thinking ──…` border, so the
	// spinner is never rendered twice.
	status := m.activityWord()
	sw := lipgloss.Width(status)
	if sw+6 > w {
		status = cellTruncate(status, w-6)
		sw = lipgloss.Width(status)
	}
	fill := w - 3 - sw - 1 // "── " prefix + status + closing gap
	if fill < 0 {
		fill = 0
	}
	// Rule chars keep the idle bar color; only the status word is accented.
	return stylePromptBar.Render("── ") + styleWorking.Render(status) +
		stylePromptBar.Render(" "+strings.Repeat("─", fill))
}

// paletteView is the autocomplete list: owned by the composer, rendered
// BELOW its bottom rule. No side bar, no border — bare rows with `→ `/`  `
// selection prefixes, a padded primary column, a dim description two
// columns past it, and a `(n/total)` scroll footer. The selected row is
// tinted Ghost-violet; everything else stays dim so the command list never
// competes with the transcript.
const paletteMaxRows = 5
const palettePrimaryCol = 30

// paletteWindow returns the visible slice plus the scroll flag. paletteHeight
// renders from this same window, so geometry can never drift from paint.
func (m *agentTUI) paletteWindow() (items []paletteItem, off int, more bool) {
	all := m.paletteMatches()
	if len(all) <= paletteMaxRows {
		return all, 0, false
	}
	off = m.paletteOffset(len(all), paletteMaxRows)
	end := off + paletteMaxRows
	if end > len(all) {
		end = len(all)
	}
	return all[off:end], off, true
}

func (m *agentTUI) paletteView() string {
	items, off, _ := m.paletteWindow()
	w := m.width
	if w < 20 {
		w = 20
	}
	var b strings.Builder
	put := func(row string) {
		pad := w - lipgloss.Width(row)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(row + strings.Repeat(" ", pad))
		b.WriteString("\n")
	}
	if len(items) == 0 {
		put(stylePaletteNoMatch.Render("  No matching commands"))
		return strings.TrimRight(b.String(), "\n")
	}
	for i, it := range items {
		selected := off+i == m.paletteSel
		put(m.paletteRow(it, selected, w))
	}
	if total := len(m.paletteMatches()); total > paletteMaxRows {
		put(stylePaletteScroll.Render(fmt.Sprintf("  (%d/%d)", m.paletteSel+1, total)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// paletteRow lays out one row: `→ `/`  ` prefix; when a description fits
// in a 30+2 primary column and the surface is wider than 40 cells, the
// description sits in that column, otherwise only the value is shown. The
// selected row is Ghost-violet.
func (m *agentTUI) paletteRow(it paletteItem, selected bool, w int) string {
	prefix := "  "
	if selected {
		prefix = "→ "
	}
	value := "/" + it.name
	if w > 40 && it.desc != "" {
		primary := palettePrimaryCol
		if primary > w-len(prefix)-4 {
			primary = w - len(prefix) - 4
		}
		if primary < 1 {
			primary = 1
		}
		v := cellTruncate(value, primary-2)
		spacing := strings.Repeat(" ", maxInt(1, primary-lipgloss.Width(v)))
		remaining := w - len(prefix) - lipgloss.Width(v) - len(spacing) - 2
		if remaining > 10 {
			desc := cellTruncate(it.desc, remaining)
			if selected {
				return stylePaletteSel.Render(prefix + v + spacing + desc)
			}
			return prefix + v + stylePaletteDesc.Render(spacing+desc)
		}
	}
	v := cellTruncate(value, w-len(prefix)-2)
	if selected {
		return stylePaletteSel.Render(prefix + v)
	}
	return prefix + v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// approvalCard is the inline permission block: a left-bordered panel in
// place of the composer (not a modal) — title, risk note, and the three
// governed choices resolved through the broker resume path.
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
	cInk            = lipgloss.Color("#d8d4cc")
	cMuted          = lipgloss.Color("#8a857c")
	cFaint          = lipgloss.Color("#5c574f")
	cAccent         = lipgloss.Color("#8a86b8")
	cTool           = lipgloss.Color("#6f9c86")
	cErr            = lipgloss.Color("#c86a5c")
	cGold           = lipgloss.Color("#e8c06a")
	cBarIdle        = lipgloss.Color("#6e648a") // visible slate-violet composer bar
	cGreen          = lipgloss.Color("#7fb08a")
	cBlue           = lipgloss.Color("#7fa8c9")
	cViolet         = lipgloss.Color("#a89bc7")
	cCodeBg         = lipgloss.Color("#201c18")
	cSelBg          = lipgloss.Color("#2a251f")
	styleUser       = lipgloss.NewStyle().Foreground(cInk).Bold(true)
	styleAssistant  = lipgloss.NewStyle().Foreground(cInk)
	styleTool       = lipgloss.NewStyle().Foreground(cTool)
	styleToolActive = lipgloss.NewStyle().Foreground(cGreen)
	styleNotice     = lipgloss.NewStyle().Foreground(cMuted)
	styleError      = lipgloss.NewStyle().Foreground(cErr)
	styleWorking    = lipgloss.NewStyle().Foreground(cAccent)
	styleApproval   = lipgloss.NewStyle().Foreground(cGold).Bold(true)

	styleUserName      = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styleAssistantName = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	styleAssistantMeta = lipgloss.NewStyle().Foreground(cFaint)
	styleErrorCard     = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	// User bubble: a single purple bar and plain text on transparent ground.
	// No side bars, no panel background — only the one bar marks the turn.
	styleUserBar   = lipgloss.NewStyle().Foreground(cAccent)
	styleUserText  = lipgloss.NewStyle().Foreground(cInk)
	styleUserPanel = lipgloss.NewStyle().PaddingLeft(1)

	// ─── Ghost palette ───────────────────────────────────────────────
	// One semantic scheme across composer, menus, modal, and approvals
	// (terminal-ui skill: ux-color-semantics), keyed to the Ghost ASCII
	// violet: violet = brand/selection, gold = approvals/warnings only,
	// green = success/done, red = errors, blue = info/links, dim =
	// hints/meta. The palette is bare rows — no side bar, no card — so the
	// selected row is a violet block on transparent ground, the same
	// language as the approval cursor.
	// The selected menu row carries no highlight background — only the `→`
	// pointer and the Ghost-violet foreground mark it, so the command list
	// stays calm while scrolling.
	stylePaletteSel     = lipgloss.NewStyle().Foreground(cViolet).Bold(true)
	stylePaletteDesc    = lipgloss.NewStyle().Foreground(cMuted)
	stylePaletteNoMatch = lipgloss.NewStyle().Foreground(cMuted)
	stylePaletteScroll  = lipgloss.NewStyle().Foreground(cFaint)

	// Footer is transparent dim text — no bar background, so it sits on the
	// terminal instead of a solid block.
	styleFooter     = lipgloss.NewStyle().Foreground(cFaint)
	styleFooterHint = lipgloss.NewStyle().Foreground(cFaint).Italic(true)

	// ux-color-semantics: the footer model carries its locality color so
	// "where it ran" reads at a glance.
	styleModelLocal = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
	styleModelCloud = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	styleModelPod   = lipgloss.NewStyle().Foreground(cMuted).Bold(true)

	// Composer rules + approval bar: the top/bottom `─` rules around the
	// composer keep their idle slate-violet colour at all times (only the
	// status text is accented), and the approval block uses a gold bar.
	stylePromptBar     = lipgloss.NewStyle().Foreground(cBarIdle)
	styleApprovalBar   = lipgloss.NewStyle().Foreground(cGold)
	styleModalTitle    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styleApprovalTitle = lipgloss.NewStyle().Foreground(cGold).Bold(true)
	styleApprovalKeys  = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc"))
	styleApprovalSel   = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGold).Bold(true)
	styleRiskHigh      = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(lipgloss.Color("#c86a5c")).Bold(true)
	styleRiskMid       = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGold).Bold(true)
	styleRiskLow       = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGreen).Bold(true)
	styleRiskDefault   = lipgloss.NewStyle().Foreground(cMuted).Background(cSelBg)

	styleWelcomeTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleGhostArt     = lipgloss.NewStyle().Foreground(cViolet).Bold(true)
	styleWelcomeCmds  = lipgloss.NewStyle().Foreground(cMuted)
	styleDayDivider   = lipgloss.NewStyle().Foreground(cFaint)

	// Markdown roles (dark default): headings violet bold (h1 underlined),
	// strong orange, emphasis/quotes sand italic, code green with no
	// background, bullets peach, ordered numbers cyan, checked green,
	// links cyan underlined with the URL concealed.
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
	// Table grid: dim box borders, violet bold header, plain body cells
	// (matching the opencode CLI's box-drawn tables).
	styleMDTableBorder = lipgloss.NewStyle().Foreground(cFaint)
	styleMDTableHead   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9d7cd8")).Bold(true)
	styleMDTableRow    = lipgloss.NewStyle().Foreground(cInk)
)

func agentHelpText() string {
	return strings.Join([]string{
		"Commands  (/ + Tab completes, ↑/↓ picks · every surface shares one conversation)",
		"  /help              this help",
		"  /session           where this terminal is, the model, and turn count",
		"  /model [name]      show or switch the active model (configured providers)",
		"  /scoped-models     pick models to cycle with Ctrl+P (Ctrl+S saves)",
		"  /context [name]    show or switch topic context (scoped memory/tools)",
		"  /memory [query]    ask Ghost in a turn what it remembers",
		"  /routines          ask Ghost in a turn what it has scheduled",
		"  /thread            open a side thread (a tangent, not the main one)",
		"  /main              return to the shared conversation",
		"  /rewind            put the last message back in the editor",
		"  /details           toggle tool step details",
		"  /clear             clear the screen (keeps the conversation)",
		"  /quit              exit",
		"",
		"Keys",
		"  Enter              send (while working: queue a steering message)",
		"  Enter (question)   answer Ghost's in-flight question in the running turn",
		"  Tab                complete /command",
		"  Esc                abort the turn; queued text returns to the editor",
		"  Ctrl+C             clear editor; twice to quit",
		"  Ctrl+L             open the model picker",
		"  Ctrl+P             cycle the enabled model scope",
		"  Ctrl+O             toggle tool detail",
		"  PgUp/PgDn          scroll transcript",
	}, "\n")
}
