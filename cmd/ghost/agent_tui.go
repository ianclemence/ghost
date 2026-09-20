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

	// approval is set when a turn ends with a durable permission request;
	// the editor is replaced by Allow once / Always allow / Deny choices.
	approval *pendingApproval
}

type pendingApproval struct {
	id    string
	title string
	risk  string
}

const agentPrompt = "› "

func newAgentTUI(loop agentRuntime, session string) *agentTUI {
	ta := textarea.New()
	ta.Placeholder = "Ask Ghost anything…"
	ta.Prompt = agentPrompt
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
	return textarea.Blink
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

	case streamChunkMsg:
		m.streaming += msg.text
		m.renderTranscript()
		return m, nil

	case toolCallMsg:
		m.toolCount++
		m.toolLine = msg.label
		m.renderTranscript()
		return m, nil

	case turnDoneMsg:
		m.working = false
		m.toolLine = ""
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.append(entry{kind: entryError, text: "✗ " + friendlyAgentError(msg.err)})
		} else {
			text := strings.TrimSpace(msg.text)
			if text == "" && m.streaming == "" {
				text = "(no response)"
			}
			// If the turn is blocked on a durable approval, show it as a card
			// rather than a wall of text. The paused call resumes through the
			// governed path when the owner answers.
			if id, title, risk, ok := m.loop.PendingApproval(m.session); ok {
				m.approval = &pendingApproval{id: id, title: title, risk: risk}
				m.append(entry{kind: entryNotice, text: "needs your approval"})
			} else {
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
	return m, cmd
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

	case tea.KeyUp:
		m.recallHistory(-1)
		return m, nil
	case tea.KeyDown:
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
	m.streaming = ""
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
		m.renderTranscript()
	case "new":
		m.session = "cli:" + fmt.Sprintf("%d", time.Now().UnixNano())
		m.entries = nil
		m.append(entry{kind: entryNotice, text: "new conversation: " + m.session})
		m.renderTranscript()
	case "session":
		m.append(entry{kind: entryNotice, text: fmt.Sprintf("session: %s · model: %s", m.session, m.loop.GetCurrentModel())})
	case "model":
		if len(args) == 0 {
			m.listModels()
		} else {
			m.setModel(strings.Join(args, " "))
		}
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

func (m *agentTUI) renderTranscript() {
	if !m.ready {
		return
	}
	var b strings.Builder
	for i, e := range m.entries {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderEntry(e))
		b.WriteString("\n")
	}
	if m.working {
		if m.streaming != "" {
			b.WriteString("\n")
			b.WriteString(styleAssistant.Render(logo + " " + m.streaming))
			b.WriteString("\n")
		}
		if m.toolLine != "" {
			b.WriteString(styleTool.Render("  ⚙ " + m.toolLine))
			b.WriteString("\n")
		}
		b.WriteString(styleWorking.Render("  ◍ working…"))
		b.WriteString("\n")
	}
	m.viewport.SetContent(strings.TrimRight(b.String(), "\n"))
	m.viewport.GotoBottom()
}

func (m *agentTUI) renderEntry(e entry) string {
	switch e.kind {
	case entryUser:
		return styleUser.Render("› " + e.text)
	case entryAssistant:
		return styleAssistant.Render(logo + " " + e.text)
	case entryTool:
		if !m.showTools {
			return styleTool.Render("  ⚙ " + e.text)
		}
		return styleTool.Render("  ⚙ " + e.text)
	case entryNotice:
		return styleNotice.Render("  · " + e.text)
	case entryError:
		return styleError.Render("  " + e.text)
	}
	return e.text
}

func (m *agentTUI) layout() {
	inputH := 3
	statusH := 1
	vpH := m.height - inputH - statusH - 1
	if vpH < 1 {
		vpH = 1
	}
	if m.viewport.Width == 0 {
		m.viewport = viewport.New(m.width, vpH)
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpH
	}
	m.input.SetWidth(m.width - 2)
}

// ─── view ────────────────────────────────────────────────────────────────

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
	b.WriteString(m.statusLine())
	b.WriteString("\n")
	if m.approval != nil {
		b.WriteString(m.approvalCard())
	} else {
		b.WriteString(m.input.View())
	}
	return b.String()
}

// approvalCard is the inline permission prompt. It states the risk in owner
// language and offers the three governed choices. It occupies the editor's
// place so the decision is the only thing in front of the user.
func (m *agentTUI) approvalCard() string {
	title := m.approval.title
	if title == "" {
		title = "Ghost needs your approval"
	}
	var b strings.Builder
	b.WriteString(styleApproval.Render(" ⚑ " + title))
	b.WriteString("\n")
	if note := approvalRiskNote(m.approval.risk); note != "" {
		b.WriteString(styleNotice.Render("   " + note))
		b.WriteString("\n")
	}
	b.WriteString(styleNotice.Render("   [1] allow once   [2] always allow   [3] deny"))
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
	cInk           = lipgloss.Color("#d8d4cc")
	cMuted         = lipgloss.Color("#8a857c")
	cFaint         = lipgloss.Color("#5c574f")
	cAccent        = lipgloss.Color("#8a86b8")
	cTool          = lipgloss.Color("#6f9c86")
	cErr           = lipgloss.Color("#c86a5c")
	styleUser      = lipgloss.NewStyle().Foreground(cInk).Bold(true)
	styleAssistant = lipgloss.NewStyle().Foreground(cInk)
	styleTool      = lipgloss.NewStyle().Foreground(cTool)
	styleNotice    = lipgloss.NewStyle().Foreground(cMuted)
	styleError     = lipgloss.NewStyle().Foreground(cErr)
	styleWorking   = lipgloss.NewStyle().Foreground(cAccent)
	styleApproval  = lipgloss.NewStyle().Foreground(lipgloss.Color("#e8c06a")).Bold(true)
	styleStatus    = lipgloss.NewStyle().Foreground(cMuted).Background(lipgloss.Color("#141210"))
)

func agentHelpText() string {
	return strings.Join([]string{
		"Commands",
		"  /help              this help",
		"  /model [name]      show or switch the active model",
		"  /new               start a fresh conversation",
		"  /session           show session and model",
		"  /memory [query]    ask what Ghost remembers",
		"  /context [name]    show or switch topic context (scoped memory/tools)",
		"  /rewind            put the last message back in the editor",
		"  /routines          what Ghost does for you",
		"  /clear             clear the screen",
		"  /quit              exit",
		"",
		"Keys",
		"  Enter              send (while working: queue a steering message)",
		"  Esc                abort the turn; queued text returns to the editor",
		"  Ctrl+C             clear editor; twice to quit",
		"  Ctrl+L             cycle model presets",
		"  Ctrl+O             toggle tool detail",
		"  PgUp/PgDn          scroll transcript",
	}, "\n")
}
