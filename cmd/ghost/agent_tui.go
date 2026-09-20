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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ianclemence/ghost/pkg/agent"
)

// ─── messages ─────────────────────────────────────────────────────────────

// agentProgram is the running tea.Program, set at launch so the turn goroutine
// can push streamed events back into the UI.
var agentProgram *tea.Program

// streamChunkMsg carries one streamed token/segment from the agent.
type streamChunkMsg struct{ text string }

// toolCallMsg reports the model invoking a tool (label is product language).
type toolCallMsg struct{ tool, label string }

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
}

// ─── model ───────────────────────────────────────────────────────────────

// agentRuntime is the slice of the agent loop the TUI needs. Depending on an
// interface (not the concrete loop) keeps the UI testable without a full
// runtime and makes the coupling explicit.
type agentRuntime interface {
	GetCurrentModel() string
	ModelPresets() []string
	SetModel(target string) error
	Steering() *agent.SteeringManager
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
	// the editor is replaced by Allow once / Always allow / Deny choices.
	approval *pendingApproval
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
	label string
	start time.Time
	done  bool
	dur   time.Duration
}

const agentPrompt = "› "

func newAgentTUI(loop agentRuntime, session string) *agentTUI {
	ta := textarea.New()
	ta.Placeholder = "Message Ghost…  (/ for commands, Enter to send)"
	ta.Prompt = "❯ "
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
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
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
		label := strings.TrimSpace(msg.label)
		if label == "" {
			return m, nil
		}
		// Dedupe: providers often re-report the same active tool. Only the
		// active (last, unfinished) step spins; repeats update it in place.
		if n := len(m.toolHistory); n > 0 && !m.toolHistory[n-1].done && m.toolHistory[n-1].label == label {
			m.toolLine = label
			m.renderTranscript()
			return m, nil
		}
		now := time.Now()
		if n := len(m.toolHistory); n > 0 && !m.toolHistory[n-1].done {
			m.toolHistory[n-1].done = true
			m.toolHistory[n-1].dur = now.Sub(m.toolHistory[n-1].start)
		}
		m.toolHistory = append(m.toolHistory, toolStep{label: label, start: now})
		m.toolCount = len(m.toolHistory)
		m.toolLine = label
		m.renderTranscript()
		return m, nil

	case turnDoneMsg:
		m.working = false
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
				m.append(entry{kind: entryAssistant, text: text})
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

func (m *agentTUI) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While an approval is pending, the keyboard is the approval card: 1/2/3
	// (or a/A/d) answer it. This is the only input the card accepts, so a
	// stray keystroke cannot accidentally approve anything.
	if m.approval != nil {
		switch msg.String() {
		case "1", "a":
			return m.resolveApproval("allow once")
		case "2", "A":
			return m.resolveApproval("always allow")
		case "3", "d":
			return m.resolveApproval("deny")
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
		if m.working {
			// Abort the turn; return queued text to the editor.
			m.loop.Steering().HardAbort(m.session)
			if len(m.queued) > 0 {
				m.input.SetValue(strings.Join(m.queued, "\n"))
				m.queued = nil
			}
			m.append(entry{kind: entryNotice, text: "aborted"})
			m.renderTranscript()
			return m, nil
		}
		return m, nil

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
			m.input.SetValue("/" + items[m.paletteSel].name + " ")
			m.input.Update(tea.KeyMsg{Type: tea.KeyEnd})
			return m, nil
		}
		return m, nil

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
		// Shift+Enter inserts a newline; Enter sends/queues.
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
		m.loop.Steering().Inject(agent.SteeringMessage{
			Content:    text,
			SessionKey: m.session,
			Channel:    "cli",
			ChatID:     "direct",
		})
		m.append(entry{kind: entryNotice, text: "↳ queued for the current turn: " + text})
		m.renderTranscript()
		return
	}

	m.append(entry{kind: entryUser, text: text})
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

// resolveApproval answers a pending approval by sending the recognized grant
// phrase as a normal turn. This is deliberate: the reply travels through the
// SAME governed resume path the console and mobile use (CheckApprovalReply),
// so the CLI can never authorize around the broker. The card clears first so
// the phrase is sent as an ordinary message, not re-interpreted as a key.
func (m *agentTUI) resolveApproval(phrase string) (tea.Model, tea.Cmd) {
	m.approval = nil
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

func (m *agentTUI) paletteVisible() bool {
	// The approval dialog owns the keyboard while up; otherwise the palette
	// stays available even mid-turn so commands stay reachable.
	if m.approval != nil {
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
			m.listModels()
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

func (m *agentTUI) listModels() {
	presets := m.loop.ModelPresets()
	if len(presets) == 0 {
		m.append(entry{kind: entryNotice, text: "active model: " + m.loop.GetCurrentModel() + " (no presets)"})
	} else {
		var b strings.Builder
		b.WriteString("active model: " + m.loop.GetCurrentModel() + "\n")
		b.WriteString("presets: " + strings.Join(presets, ", ") + "\n")
		b.WriteString("switch with /model <name>")
		m.append(entry{kind: entryNotice, text: b.String()})
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

func (m *agentTUI) renderTranscript() {
	if !m.ready {
		return
	}
	var b strings.Builder
	if len(m.entries) == 0 && m.streaming == "" && !m.working {
		b.WriteString(m.welcomeCard())
		b.WriteString("\n")
	}
	for i, e := range m.entries {
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
	shown := 0
	limit := len(steps)
	if !m.showTools && limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		s := steps[i]
		if !s.done {
			b.WriteString(styleToolActive.Render("  " + m.spinner() + " " + cellTruncate(s.label, w-6)))
		} else if m.showTools {
			b.WriteString(styleTool.Render(fmt.Sprintf("  ✓ %s (%s)", cellTruncate(s.label, w-12), formatElapsed(s.dur))))
		} else {
			b.WriteString(styleTool.Render("  ✓ " + cellTruncate(s.label, w-6)))
		}
		b.WriteString("\n")
		shown++
		_ = shown
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
		var lines []string
		lines = append(lines, styleUserName.Render("You"))
		for _, wl := range wrapText(e.text, w-2) {
			lines = append(lines, styleUserCard.Render("❯ "+wl))
		}
		return strings.Join(lines, "\n")
	case entryAssistant:
		head := styleAssistantName.Render(logo + " Ghost · " + providerLocality(m.loop.GetCurrentModel()))
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
	title := styleWelcomeTitle.Render(logo + " Ghost")
	sub := styleNotice.Render(wrapFirst("Your AI on your machine — it remembers, acts with approval, and shows where it ran.", w))
	cmds := styleWelcomeCmds.Render("  /help      commands & keys\n  /model     switch thinking engine\n  /memory    what Ghost remembers\n  /routines  recurring work")
	return title + "\n" + sub + "\n" + cmds
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

// ─── markdown-lite (no new deps) ─────────────────────────────────────────
// pi and opencode both render assistant markdown (headers, bold, code,
// lists, quotes). We do a lightweight pass with lipgloss so code blocks
// and inline code stand out without pulling in glamour.

func renderAssistantBody(text string, width int) string {
	if width < 20 {
		width = 20
	}
	lines := strings.Split(text, "\n")
	var out []string
	inCode := false
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			out = append(out, styleCodeFence.Render(cellTruncate(trim, width)))
			continue
		}
		if inCode {
			for _, wl := range wrapText(ln, width-2) {
				out = append(out, styleCodeBlock.Render("  "+wl))
			}
			continue
		}
		switch {
		case strings.HasPrefix(trim, "### "):
			out = append(out, styleH3.Render("◆ "+cellTruncate(strings.TrimPrefix(trim, "### "), width-4)))
		case strings.HasPrefix(trim, "## "):
			out = append(out, styleH2.Render("◆ "+cellTruncate(strings.TrimPrefix(trim, "## "), width-4)))
		case strings.HasPrefix(trim, "# "):
			out = append(out, styleH1.Render("◆ "+cellTruncate(strings.TrimPrefix(trim, "# "), width-4)))
		case strings.HasPrefix(trim, "> "):
			// Wrap the quote body, keep the bar on each visual line.
			first := true
			for _, wl := range wrapText(strings.TrimPrefix(trim, "> "), width-4) {
				_ = first
				out = append(out, styleQuote.Render("▍ "+wl))
				first = false
			}
		case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
			for _, wl := range wrapText(trim[2:], width-6) {
				out = append(out, styleList.Render("  • "+wl))
			}
		case isOrderedList(trim):
			dot := strings.Index(trim, ".")
			for _, wl := range wrapText(strings.TrimSpace(trim[dot+1:]), width-6) {
				out = append(out, styleList.Render("  "+trim[:dot+1]+" "+wl))
			}
		case trim == "":
			out = append(out, "")
		default:
			// Body lines render plain (the block already has its Ghost
			// header); inline spans carry the emphasis.
			for _, wl := range wrapText(ln, width) {
				out = append(out, styleAssistant.Render(renderInline(wl)))
			}
		}
	}
	return strings.Join(out, "\n")
}

func isOrderedList(s string) bool {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] == ' '
}

// renderInline handles **bold**, `code`, and *emphasis* with lipgloss spans.
func renderInline(s string) string {
	s = renderSpan(s, "`", styleInlineCode.Render)
	s = renderSpan(s, "**", styleBold.Render)
	return s
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
func (m *agentTUI) layout() {
	headerH := 1
	footerH := 1
	paletteH := m.paletteHeight()
	inputH := m.estimatedInputHeight()
	if m.approval != nil {
		inputH = m.estimatedApprovalHeight()
	}
	vpH := m.height - headerH - footerH - paletteH - inputH - 1
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
}

// estimatedInputHeight mirrors inputBox without rendering it: title row (1)
// + border (2) + textarea lines clamped to the box.
func (m *agentTUI) estimatedInputHeight() int {
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > 6 {
		lines = 6
	}
	return lines + 3
}

func (m *agentTUI) estimatedApprovalHeight() int {
	// Title + risk note + keys + border, wrapped to content width.
	return 6
}

func (m *agentTUI) inputWidth() int {
	w := m.width - 6
	if w < 20 {
		w = 20
	}
	return w
}

func (m *agentTUI) paletteHeight() int {
	if !m.paletteVisible() {
		return 0
	}
	n := len(m.paletteMatches())
	if n > 6 {
		n = 6
	}
	return n + 1
}

// ─── view ────────────────────────────────────────────────────────────────

func (m *agentTUI) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "starting Ghost…"
	}
	// Pure composition: header, transcript, popover, prompt, footer each
	// render exactly once. Geometry was frozen in layout(); rendering here
	// must not mutate it.
	var b strings.Builder
	b.WriteString(m.headerBar())
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.paletteVisible() {
		b.WriteString(m.paletteView())
		b.WriteString("\n")
	}
	if m.approval != nil {
		b.WriteString(m.approvalCard())
	} else {
		b.WriteString(m.inputBox(m.input.View()))
	}
	b.WriteString("\n")
	b.WriteString(m.footerHints())
	return b.String()
}

// headerBar is the opencode-style top bar: product + model pill + locality
// + context on the left, session + turn count + state on the right.
// It degrades gracefully on narrow terminals: context pill first, then the
// session, then the model get shortened — never wrapped or truncated mid-word.
func (m *agentTUI) headerBar() string {
	model := m.loop.GetCurrentModel()
	local := providerLocality(model)
	ctx := ""
	func() {
		defer func() { _ = recover() }()
		if m.loop != nil {
			ctx = m.loop.CurrentContext(m.session)
		}
	}()
	state := m.stateWord()
	right := styleHeaderRight.Render(shortSession(m.session) + " · " + state)
	budget := m.width - lipgloss.Width(right) - 2
	if budget < 12 {
		// Extremely narrow: state only.
		return styleHeader.Width(m.width).Render(cellTruncate(shortSession(m.session)+" · "+state, m.width))
	}
	left := styleHeaderLogo.Render(logo+" Ghost") + "  " +
		styleModelPill.Render("◈ "+shortModel(model)) + " " +
		localityPill(local)
	if ctx != "" && lipgloss.Width(left)+lipgloss.Width(styleCtxPill.Render("❖ "+ctx))+1 <= budget {
		left += " " + styleCtxPill.Render("❖ "+ctx)
	}
	if lipgloss.Width(left) > budget {
		// Drop the model pill, keep identity + locality.
		left = styleHeaderLogo.Render(logo+" Ghost") + "  " + localityPill(local)
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return styleHeader.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func (m *agentTUI) stateWord() string {
	if m.approval != nil {
		return "waiting for you"
	}
	if m.working {
		s := "working " + m.spinner()
		if m.toolCount > 0 {
			s += fmt.Sprintf(" · %d tool%s", m.toolCount, plural(m.toolCount))
		}
		return s
	}
	if m.turnCount == 0 {
		return "ready"
	}
	return fmt.Sprintf("ready · %d turn%s", m.turnCount, plural(m.turnCount))
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

func localityPill(local string) string {
	switch local {
	case "local":
		return styleLocalPill.Render("● local")
	case "cloud":
		return styleCloudPill.Render("● cloud")
	default:
		return stylePodPill.Render("● pod")
	}
}

// inputBox is the pi/opencode-style bordered editor with a title row.
// The title names the *state* (never the placeholder text) so it can't echo.
func (m *agentTUI) inputBox(inner string) string {
	title := "prompt"
	if m.working {
		title = "working — enter queues steering"
	} else if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
		title = "command — tab completes, enter runs"
	}
	head := styleInputTitle.Render(" " + title + " ")
	box := styleInputBox.Width(m.width - 2).Render(inner)
	return head + "\n" + box
}

// paletteView is the "/" autocomplete popup (pi-style command palette).
func (m *agentTUI) paletteView() string {
	items := m.paletteMatches()
	if len(items) > 6 {
		items = items[:6]
	}
	var b strings.Builder
	for i, it := range items {
		row := fmt.Sprintf("  /%-10s %s", it.name, it.desc)
		if i == m.paletteSel {
			b.WriteString(stylePaletteSel.Render("▸" + row))
		} else {
			b.WriteString(stylePaletteRow.Render(" " + row))
		}
		if i+1 < len(items) {
			b.WriteString("\n")
		}
	}
	return stylePaletteBox.Width(m.width - 2).Render(b.String())
}

// footerHints is the opencode-style key bar under the editor.
// Single line, always truncated to the terminal width — never wrapped.
func (m *agentTUI) footerHints() string {
	keys := "enter send · esc abort · ctrl+l model · ctrl+o details · / commands"
	if m.working {
		keys = "enter queues · esc aborts · " + keys
	}
	for lipgloss.Width(keys)+2 > m.width && strings.Contains(keys, " · ") {
		i := strings.LastIndex(keys, " · ")
		keys = keys[:i]
	}
	return styleFooter.Width(m.width).Render(cellTruncate(keys, m.width-2))
}

// approvalCard is the inline permission prompt. It states the risk in owner
// language and offers the three governed choices. It occupies the editor's
// place so the decision is the only thing in front of the user.
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
	var b strings.Builder
	b.WriteString(styleApprovalTitle.Render("⚑ "+cellTruncate(title, m.contentWidth()-16)) + "  " + badge)
	b.WriteString("\n")
	if note := approvalRiskNote(m.approval.risk); note != "" {
		for _, wl := range wrapText(note, m.contentWidth()-4) {
			b.WriteString(styleNotice.Render("  " + wl))
			b.WriteString("\n")
		}
	}
	b.WriteString(styleApprovalKeys.Render("  [1] allow once    [2] always allow    [3] deny"))
	b.WriteString(styleNotice.Render("  esc leaves pending"))
	return styleApprovalBox.Width(m.width - 2).Render(b.String())
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

	styleUserCard      = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleUserName      = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styleAssistantName = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	styleErrorCard     = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	styleToolActive    = lipgloss.NewStyle().Foreground(cGreen)
	styleBold          = lipgloss.NewStyle().Bold(true).Foreground(cInk)

	styleHeader      = lipgloss.NewStyle().Foreground(cMuted).Background(cBgBar)
	styleHeaderLogo  = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleHeaderRight = lipgloss.NewStyle().Foreground(cFaint)
	styleModelPill   = lipgloss.NewStyle().Foreground(cViolet).Bold(true)
	styleLocalPill   = lipgloss.NewStyle().Foreground(cGreen)
	styleCloudPill   = lipgloss.NewStyle().Foreground(cBlue)
	stylePodPill     = lipgloss.NewStyle().Foreground(cMuted)
	styleCtxPill     = lipgloss.NewStyle().Foreground(cGold)

	styleInputTitle = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styleInputBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Padding(0, 1)

	stylePaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Background(cBgPanel).Padding(0, 1)
	stylePaletteRow = lipgloss.NewStyle().Foreground(cMuted)
	stylePaletteSel = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Background(cSelBg).Bold(true)

	styleFooter = lipgloss.NewStyle().Foreground(cFaint).Background(cBgBar)

	styleApprovalBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGold).Background(cBgPanel).Padding(0, 1)
	styleApprovalTitle = lipgloss.NewStyle().Foreground(cGold).Bold(true)
	styleApprovalKeys  = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleRiskHigh      = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(lipgloss.Color("#c86a5c")).Bold(true)
	styleRiskMid       = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGold).Bold(true)
	styleRiskLow       = lipgloss.NewStyle().Foreground(lipgloss.Color("#1b1815")).Background(cGreen).Bold(true)
	styleRiskDefault   = lipgloss.NewStyle().Foreground(cMuted).Background(cSelBg)

	styleWelcomeTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleWelcomeCmds  = lipgloss.NewStyle().Foreground(cMuted)

	styleH1         = lipgloss.NewStyle().Foreground(cGold).Bold(true)
	styleH2         = lipgloss.NewStyle().Foreground(lipgloss.Color("#efe9dc")).Bold(true)
	styleH3         = lipgloss.NewStyle().Foreground(cViolet).Bold(true)
	styleQuote      = lipgloss.NewStyle().Foreground(cMuted).Italic(true)
	styleList       = lipgloss.NewStyle().Foreground(cInk)
	styleInlineCode = lipgloss.NewStyle().Foreground(cGreen).Background(cCodeBg)
	styleCodeBlock  = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9c2b4")).Background(cCodeBg)
	styleCodeFence  = lipgloss.NewStyle().Foreground(cFaint)
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
		"  Tab                complete /command",
		"  Esc                abort the turn; queued text returns to the editor",
		"  Ctrl+C             clear editor; twice to quit",
		"  Ctrl+L             cycle model presets",
		"  Ctrl+O             toggle tool detail",
		"  PgUp/PgDn          scroll transcript",
	}, "\n")
}
