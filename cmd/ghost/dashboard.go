package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ═════════════════════════════════════════════════════════════════════════════
// GHOST Terminal UI - Premium Edition (Anthropic / Apple Aesthetic)
// Clean, Intentional, Hierarchical, Breathing Room
// ═════════════════════════════════════════════════════════════════════════════

const dashboardVersion = "2.2.0"

// ─── Ghost ASCII Art (Tasteful, integrated) ─────────────────────────────────
const ghostASCII = `
   ╭───╮
  ╭│ ◉ ◉│╮
 ╭││ ╰─╯││╮
 ╰│╰────╯│╯
  ╰─╯  ╰─╯ 
`

// ─── Theme: Apple/Anthropic Cyber Blend ─────────────────────────────────────
var (
	// Base Palette (from user requirements)
	themeBg      = lipgloss.Color("#0d0d0f")
	themeCardBg  = lipgloss.Color("#151518")
	themeCardHi  = lipgloss.Color("#1c1c20")
	themeAccent  = lipgloss.Color("#7aa2f7") // Blue
	themeSuccess = lipgloss.Color("#9ece6a") // Green
	themeWarning = lipgloss.Color("#e0af68") // Yellow/Orange
	themeError   = lipgloss.Color("#f7768e") // Red
	themeGhost   = lipgloss.Color("#bf5af2") // Purple

	// Text Hierarchy
	cTextPrimary   = lipgloss.Color("#f4f4f5")
	cTextSecondary = lipgloss.Color("#a1a1aa")
	cTextTertiary  = lipgloss.Color("#71717a")
	cTextMuted     = lipgloss.Color("#3f3f46")

	// Component Styles
	styleBase = lipgloss.NewStyle().Foreground(cTextPrimary)

	styleCard = lipgloss.NewStyle().
			Background(themeCardBg).
			Border(lipgloss.RoundedBorder(), true).
			BorderForeground(cTextMuted).
			Padding(1, 2)

	styleCardActive = styleCard.
			Background(themeCardHi).
			BorderForeground(themeAccent)

	styleBadge = lipgloss.NewStyle().
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cTextMuted).
			Foreground(cTextSecondary)

	styleTitle = lipgloss.NewStyle().
			Foreground(cTextPrimary).
			Bold(true)
)

// ═════════════════════════════════════════════════════════════════════════════
// Application Models
// ═════════════════════════════════════════════════════════════════════════════

type operatorClient struct {
	BaseURL string
	Secret  string
	HTTP    *http.Client
}

type authSanity struct {
	BridgeSecretConfigured bool `json:"bridge_secret_configured"`
	APIReachable           bool `json:"api_reachable"`
	Blocking               bool `json:"blocking"`
}

type doctorPayload struct {
	Status    string               `json:"status"`
	Checks    []doctorCheckPayload `json:"checks"`
	Timestamp int64                `json:"timestamp"`
	Uptime    int64                `json:"uptime"`
	Version   string               `json:"version"`
	Channels  map[string]interface{} `json:"channels"`
}

type doctorCheckPayload struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type channelHealth struct {
	Running      bool   `json:"running"`
	Enabled      bool   `json:"enabled"`
	LastSuccess  int64  `json:"last_success"`
	LastFailure  int64  `json:"last_failure"`
	FailureCount int    `json:"failure_count"`
	LastSendErr  string `json:"last_send_error"`
	Fatal        bool   `json:"fatal"`
}

type sessionInspector struct {
	RequestedSession string            `json:"requested_session"`
	ActiveSession    map[string]string `json:"active_session"`
	DeliveryTarget   string            `json:"delivery_target"`
	LastRequestID    string            `json:"last_request_id"`
	Timestamp        int64             `json:"timestamp"`
}

type tracePayload struct {
	RequestID string                     `json:"request_id"`
	Events    []traceEvent               `json:"events"`
	Incidents map[string]channelIncident `json:"incidents"`
}

type traceEvent struct {
	RequestID string `json:"request_id"`
	State     string `json:"state"`
	At        int64  `json:"at"`
	Channel   string `json:"channel,omitempty"`
	ChatID    string `json:"chat_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type channelIncident struct {
	Channel      string `json:"channel"`
	FailureCount int    `json:"failure_count"`
	LastError    string `json:"last_error"`
	LastAt       int64  `json:"last_at"`
}

// ═════════════════════════════════════════════════════════════════════════════
// TUI Dashboard Model
// ═════════════════════════════════════════════════════════════════════════════

// Modes
const (
	ModeDashboard = iota
	ModeChat
)

type dashboardModel struct {
	client       *operatorClient
	width, height int

	// Data
	doctor       doctorPayload
	channels     map[string]channelHealth
	channelNames []string
	inspector    sessionInspector
	trace        tracePayload
	sanity       authSanity

	// State
	mode         int
	selected     int
	expanded     bool
	lastRefresh  time.Time
	lastError    string
	lastErrorAt  time.Time
	isRefreshing bool
	programStart time.Time

	// History mapping for micro-sparklines
	history      map[string]string
	failuresSeen map[string]int

	// Sub-components
	textInput textinput.Model
	chatLog   viewport.Model
	chatMsgs  []string
}

type tickMsg struct{}
type refreshMsg struct {
	sanity    authSanity
	doctor    doctorPayload
	channels  map[string]channelHealth
	inspector sessionInspector
	trace     tracePayload
	err       error
}
type reconnectMsg struct {
	channel string
	err     error
}
type chatResponseMsg struct {
	text string
	err  error
}

// ═════════════════════════════════════════════════════════════════════════════
// Initialization
// ═════════════════════════════════════════════════════════════════════════════

func initialModel(client *operatorClient) dashboardModel {
	ti := textinput.New()
	ti.Placeholder = "Message Ghost (Press Esc to cancel)..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60
	ti.PromptStyle = lipgloss.NewStyle().Foreground(themeAccent).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(cTextPrimary)

	vp := viewport.New(60, 10)
	vp.Style = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(cTextMuted).
		PaddingLeft(1)

	return dashboardModel{
		client:       client,
		channels:     make(map[string]channelHealth),
		history:      make(map[string]string),
		failuresSeen: make(map[string]int),
		programStart: time.Now(),
		trace: tracePayload{
			Incidents: make(map[string]channelIncident),
		},
		mode:         ModeDashboard,
		textInput:    ti,
		chatLog:      vp,
		chatMsgs:     []string{"Connected to Ghost Agent. Press '/' to chat."},
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		fetchDataCmd(m.client),
		tickCmd(),
	)
}

// ═════════════════════════════════════════════════════════════════════════════
// Update logic
// ═════════════════════════════════════════════════════════════════════════════

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}

		// Handle Chat Mode
		if m.mode == ModeChat {
			switch msg.Type {
			case tea.KeyEsc:
				m.mode = ModeDashboard
				m.textInput.Blur()
				return m, nil
			case tea.KeyEnter:
				v := m.textInput.Value()
				if strings.TrimSpace(v) != "" {
					m.chatMsgs = append(m.chatMsgs, lipgloss.NewStyle().Foreground(cTextSecondary).Render("You: ")+v)
					m.textInput.SetValue("")
					m.updateChatViewport()
					cmds = append(cmds, sendChatCmd(m.client, v))
				}
				return m, tea.Batch(cmds...)
			}

			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Handle Dashboard Mode
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.expanded = false
			}
		case "down", "j":
			if m.selected < len(m.channelNames)-1 {
				m.selected++
				m.expanded = false
			}
		case "enter":
			if len(m.channelNames) > 0 {
				m.expanded = !m.expanded
			}
		case "r":
			if sel := m.selectedChannel(); sel != "" {
				m.lastError = "Reconnecting " + sel + "..."
				cmds = append(cmds, reconnectCmd(m.client, sel))
			}
		case "d":
			m.isRefreshing = true
			cmds = append(cmds, fetchDataCmd(m.client))
		case "/":
			m.mode = ModeChat
			m.textInput.Focus()
			return m, textinput.Blink
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chatLog.Width = msg.Width - 4
		m.updateChatViewport()

	case tickMsg:
		cmds = append(cmds, tickCmd(), fetchDataCmd(m.client))

	case refreshMsg:
		m.isRefreshing = false
		m.lastRefresh = time.Now()

		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.lastErrorAt = time.Now()
		} else {
			m.sanity = msg.sanity
			m.doctor = msg.doctor
			m.channels = msg.channels
			m.inspector = msg.inspector
			m.trace = msg.trace

			oldSel := ""
			if m.selected < len(m.channelNames) {
				oldSel = m.channelNames[m.selected]
			}
			m.channelNames = sortedKeys(msg.channels)
			m.selected = indexOf(m.channelNames, oldSel)
			if m.selected == -1 {
				m.selected = 0
			}
			m.updateHistory()
		}

	case reconnectMsg:
		if msg.err != nil {
			m.lastError = fmt.Sprintf("Reconnect %s failed: %v", msg.channel, msg.err)
			m.lastErrorAt = time.Now()
		} else {
			m.lastError = ""
		}
		cmds = append(cmds, fetchDataCmd(m.client))

	case chatResponseMsg:
		if msg.err != nil {
			m.chatMsgs = append(m.chatMsgs, lipgloss.NewStyle().Foreground(themeError).Render("Error: ")+msg.err.Error())
		} else {
			m.chatMsgs = append(m.chatMsgs, lipgloss.NewStyle().Foreground(themeSuccess).Render("Ghost: ")+msg.text)
		}
		m.updateChatViewport()
	}

	return m, tea.Batch(cmds...)
}

func (m *dashboardModel) updateChatViewport() {
	m.chatLog.SetContent(strings.Join(m.chatMsgs, "\n"))
	m.chatLog.GotoBottom()
}

// ═════════════════════════════════════════════════════════════════════════════
// View Layout
// ═════════════════════════════════════════════════════════════════════════════

func (m dashboardModel) View() string {
	if m.sanity.Blocking {
		return m.renderLockScreen()
	}

	if m.width < 80 || m.height < 24 {
		return m.renderCompactView()
	}

	// Dynamic Layout Calculation
	header := m.renderHeader()
	footer := m.renderFooter()

	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)
	availableHeight := m.height - headerHeight - footerHeight

	var body string
	if m.mode == ModeChat {
		body = m.renderChat(m.width, availableHeight)
	} else {
		body = m.renderDashboardBody(m.width, availableHeight)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// ─── Header ─────────────────────────────────────────────────────────────────

func (m dashboardModel) renderHeader() string {
	// Art on the left
	artStyle := lipgloss.NewStyle().Foreground(cTextMuted).PaddingRight(2)
	art := artStyle.Render(ghostASCII)

	// Status indicators on the right
	brand := lipgloss.NewStyle().Foreground(themeGhost).Bold(true).Render("◉ GHOST")
	versionStr := lipgloss.NewStyle().Foreground(cTextMuted).Render("v" + dashboardVersion)
	uptimeStr := lipgloss.NewStyle().Foreground(cTextSecondary).Render("Up: " + formatDuration(time.Since(m.programStart)))

	connectionStatus := m.renderConnectionStatus()

	topInfo := lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", versionStr, "  •  ", connectionStatus, "  •  ", uptimeStr)

	pills := m.renderStatusPills()
	rightNav := lipgloss.JoinVertical(lipgloss.Left, topInfo, "", pills)

	headerBlock := lipgloss.JoinHorizontal(lipgloss.Center, art, rightNav)

	// Pad header to take exactly 6 lines to prevent layout shift
	return lipgloss.NewStyle().Height(7).Render(headerBlock)
}

func (m dashboardModel) renderConnectionStatus() string {
	if m.isRefreshing {
		return styleBadge.Copy().BorderForeground(themeAccent).Foreground(themeAccent).Render("⟳ Syncing")
	}
	if m.lastError != "" && time.Since(m.lastErrorAt) < 30*time.Second {
		return styleBadge.Copy().BorderForeground(themeError).Foreground(themeError).Render("● Interrupted")
	}
	return styleBadge.Copy().BorderForeground(themeSuccess).Foreground(themeSuccess).Render("● Live")
}

func (m dashboardModel) renderStatusPills() string {
	sysStatus := m.doctor.Status
	if sysStatus == "" {
		sysStatus = "unknown"
	}

	channelsActive := 0
	for _, c := range m.channels {
		if c.Running {
			channelsActive++
		}
	}

	pills := []string{
		m.renderPill("SYSTEM", sysStatus, statusColor(sysStatus)),
		m.renderPill("API", boolStatus(m.sanity.APIReachable), boolColor(m.sanity.APIReachable)),
		m.renderPill("CHANNELS", fmt.Sprintf("%d/%d", channelsActive, len(m.channels)), themeAccent),
		m.renderPill("LATENCY", m.formatLatency(), m.latencyColor()),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, pills...)
}

func (m dashboardModel) renderPill(label, value string, color lipgloss.Color) string {
	valStr := lipgloss.NewStyle().Foreground(color).PaddingLeft(1).Render(value)
	lblStr := lipgloss.NewStyle().Foreground(cTextTertiary).Render(label)
	
	comp := lipgloss.JoinHorizontal(lipgloss.Top, lblStr, valStr)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, true, false, false). // Right border only
		BorderForeground(cTextMuted).
		PaddingRight(2).
		MarginRight(2).
		Render(comp)
}

// ─── Dashboard Body ─────────────────────────────────────────────────────────

func (m dashboardModel) renderDashboardBody(width, height int) string {
	leftWidth := int(float64(width) * 0.45)
	rightWidth := width - leftWidth - 2

	left := m.renderChannelsPanel(leftWidth, height)
	right := m.renderActivityPanel(rightWidth, height)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

// ─── Channels Panel ─────────────────────────────────────────────────────────

func (m dashboardModel) renderChannelsPanel(width, height int) string {
	title := styleTitle.Render("Channels & Routings")

	if len(m.channelNames) == 0 {
		return styleCard.Width(width).Height(height).Render(
			lipgloss.JoinVertical(lipgloss.Left, title, "", lipgloss.NewStyle().Foreground(cTextTertiary).Render("No channels bound to this instance.")),
		)
	}

	var items []string
	for i, name := range m.channelNames {
		items = append(items, m.renderChannelItem(name, i == m.selected, width-4))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, items...)
	return styleCard.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, title, "", content),
	)
}

func (m dashboardModel) renderChannelItem(name string, selected bool, width int) string {
	ch := m.channels[name]

	statusColor := cTextTertiary
	statusIcon := "○"
	
	if ch.Running {
		statusColor = themeSuccess
		statusIcon = "●"
	} else if ch.Enabled {
		statusColor = themeError
		statusIcon = "●"
	}

	indicator := lipgloss.NewStyle().Foreground(statusColor).Render(statusIcon)
	nameLbl := lipgloss.NewStyle().Foreground(cTextPrimary).Width(14).Render(truncate(name, 14))
	spark := lipgloss.NewStyle().Foreground(cTextTertiary).Render(m.renderSparkline(name, width-22))

	row := lipgloss.JoinHorizontal(lipgloss.Center, indicator, " ", nameLbl, "  ", spark)

	if selected {
		style := styleCardActive.Copy().Width(width).Padding(0, 1)
		
		var details string
		if m.expanded {
			details = m.renderChannelDetails(name, width-4)
		} else {
			details = lipgloss.NewStyle().Foreground(cTextMuted).Render("  ↳ Press Enter to expand")
		}

		paddedRow := lipgloss.NewStyle().Padding(0, 1).Render(row)
		content := lipgloss.JoinVertical(lipgloss.Left, paddedRow, details)
		return style.Render(content)
	}

	return lipgloss.NewStyle().Padding(0, 2).Render(row)
}

func (m dashboardModel) renderSparkline(name string, width int) string {
	hist := m.history[name]
	if len(hist) > width {
		hist = hist[len(hist)-width:]
	}
	var blocks []rune
	for _, c := range hist {
		switch c {
		case '█': blocks = append(blocks, '█')
		case '▆': blocks = append(blocks, '▆')
		default:  blocks = append(blocks, '·')
		}
	}
	return string(blocks)
}

func (m dashboardModel) renderChannelDetails(name string, width int) string {
	ch := m.channels[name]

	var lines []string
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Last check:  %s", formatUnix(ch.LastSuccess)))
	lines = append(lines, fmt.Sprintf("  Failures:    %d", ch.FailureCount))

	if ch.LastSendErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeError).Render(fmt.Sprintf("  Error: %s", truncate(ch.LastSendErr, width-10))))
	}
	
	if ch.Fatal {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeWarning).Render("  ⚠ Needs manual intervention"))
	}
	
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(themeAccent).Render("  [R] Reconnect"))
	lines = append(lines, "")

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ─── Activity Panel ─────────────────────────────────────────────────────────

func (m dashboardModel) renderActivityPanel(width, height int) string {
	title := styleTitle.Render("Telemetry & Tracing")
	
	var sections []string

	if m.inspector.LastRequestID != "" {
		reqInfo := lipgloss.NewStyle().Foreground(themeAccent).Render(fmt.Sprintf("Tracking ID: %s", truncate(m.inspector.LastRequestID, width-16)))
		sections = append(sections, reqInfo)
	}

	logHeight := height - 8
	if logHeight < 5 {
		logHeight = 5
	}
	
	sections = append(sections, "", m.renderEventLog(width-4, logHeight))

	if len(m.trace.Incidents) > 0 {
		sections = append(sections, "", m.renderIncidents(width-4))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return styleCard.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, title, "", content),
	)
}

func (m dashboardModel) renderEventLog(width, maxHeight int) string {
	if len(m.trace.Events) == 0 {
		return lipgloss.NewStyle().Foreground(cTextMuted).Render("  Waiting for signals...")
	}

	var lines []string
	start := max(0, len(m.trace.Events)-maxHeight)

	for i := start; i < len(m.trace.Events); i++ {
		evt := m.trace.Events[i]
		lines = append(lines, m.renderEventLine(evt, width, i == len(m.trace.Events)-1))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m dashboardModel) renderEventLine(evt traceEvent, width int, isLatest bool) string {
	icon := "│"
	color := cTextMuted

	s := strings.ToLower(evt.State)
	switch {
	case strings.Contains(s, "deliver") || strings.Contains(s, "sent") || strings.Contains(s, "completed"):
		icon = "●"
		color = themeSuccess
	case strings.Contains(s, "fail") || strings.Contains(s, "error"):
		icon = "✕"
		color = themeError
	case strings.Contains(s, "queue") || strings.Contains(s, "process"):
		icon = "◎"
		color = themeWarning
	}
	
	if isLatest {
		icon = lipgloss.NewStyle().Foreground(themeAccent).Render("◆")
	} else {
		icon = lipgloss.NewStyle().Foreground(color).Render(icon)
	}

	timeStr := lipgloss.NewStyle().Foreground(cTextMuted).Width(6).Render(formatTimeShort(time.Unix(evt.At, 0)))
	stateStr := lipgloss.NewStyle().Foreground(color).Width(16).Render(strings.ToLower(evt.State))

	detail := ""
	if evt.Channel != "" {
		detail = fmt.Sprintf("[%s]", evt.Channel)
	}
	if evt.Detail != "" {
		detail += " " + evt.Detail
	}
	detail = truncate(detail, width-30)
	detailStr := lipgloss.NewStyle().Foreground(cTextSecondary).Render(detail)

	return fmt.Sprintf("%s %s %s %s", timeStr, icon, stateStr, detailStr)
}

func (m dashboardModel) renderIncidents(width int) string {
	title := lipgloss.NewStyle().Foreground(themeWarning).Render("Active Incidents")
	var items []string
	
	for _, inc := range m.sortedIncidents() {
		if len(items) >= 2 { break }
		line := fmt.Sprintf("  • %s: %s", inc.Channel, truncate(inc.LastError, width-20))
		items = append(items, lipgloss.NewStyle().Foreground(cTextMuted).Render(line))
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, lipgloss.JoinVertical(lipgloss.Left, items...))
}

// ─── Chat View ──────────────────────────────────────────────────────────────

func (m dashboardModel) renderChat(width, height int) string {
	title := styleTitle.Render("Agent Interface")
	
	m.chatLog.Height = height - 6
	m.textInput.Width = width - 4
	
	chatBox := styleCard.Width(width).Height(height).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			m.chatLog.View(),
			"",
			lipgloss.NewStyle().Foreground(cTextMuted).Render(strings.Repeat("─", width-4)),
			m.textInput.View(),
		),
	)
	return chatBox
}

// ─── Footer ─────────────────────────────────────────────────────────────────

func (m dashboardModel) renderFooter() string {
	controls := "↑/↓ Select  •  ↵ Expand  •  C/:/ Chat  •  R Reconnect  •  D Resync  •  Q Quit"
	if m.mode == ModeChat {
		controls = "Esc Return to Dashboard  •  ↵ Send Message"
	}

	if m.lastError != "" {
		controls = lipgloss.NewStyle().Foreground(themeError).Render("Err: " + truncate(m.lastError, 50))
	}

	refresh := ""
	if !m.lastRefresh.IsZero() {
		refresh = "Updated " + formatDuration(time.Since(m.lastRefresh)) + " ago"
	}

	gap := m.width - lipgloss.Width(controls) - lipgloss.Width(refresh) - 4
	if gap < 1 {
		gap = 1
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Padding(1, 2).
		Foreground(cTextMuted).
		Background(themeBg).
		Render(controls + strings.Repeat(" ", gap) + refresh)
}

// ─── Fallback Screens ───────────────────────────────────────────────────────

func (m dashboardModel) renderLockScreen() string {
	box := styleCard.BorderForeground(themeError).Padding(3, 5).Render(
		lipgloss.JoinVertical(
			lipgloss.Center,
			lipgloss.NewStyle().Foreground(themeError).Bold(true).Render("API Connection Refused"),
			"",
			lipgloss.NewStyle().Foreground(cTextSecondary).Render("Ghost is offline or unreachable."),
			"",
			m.renderSanityChecks(),
		),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m dashboardModel) renderSanityChecks() string {
	var lines []string
	add := func(name string, ok bool) {
		icon, color := "●", themeSuccess
		if !ok { icon, color = "●", themeError }
		lines = append(lines, fmt.Sprintf("%s %s", lipgloss.NewStyle().Foreground(color).Render(icon), name))
	}
	add("Bridge Secret Present", m.sanity.BridgeSecretConfigured)
	add("Endpoint Reachable", m.sanity.APIReachable)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m dashboardModel) renderCompactView() string {
	return lipgloss.NewStyle().Padding(2, 4).Foreground(themeGhost).Render("GHOST v" + dashboardVersion + " • Expand Terminal")
}

// ═════════════════════════════════════════════════════════════════════════════
// Commands
// ═════════════════════════════════════════════════════════════════════════════

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func fetchDataCmd(client *operatorClient) tea.Cmd {
	return func() tea.Msg {
		sanity := client.sanityCheck()
		if sanity.Blocking { return refreshMsg{sanity: sanity} }
		
		doctor, channels, err := client.fetchDoctorAndChannels()
		if err != nil { return refreshMsg{sanity: sanity, err: err} }
		
		inspector, err := client.fetchSessionInspector()
		if err != nil { return refreshMsg{sanity: sanity, err: err} }
		
		trace := tracePayload{Incidents: make(map[string]channelIncident)}
		if inspector.LastRequestID != "" {
			trace, _ = client.fetchTrace(inspector.LastRequestID)
		}
		
		return refreshMsg{sanity: sanity, doctor: doctor, channels: channels, inspector: inspector, trace: trace}
	}
}

func reconnectCmd(client *operatorClient, channel string) tea.Cmd {
	return func() tea.Msg {
		return reconnectMsg{channel: channel, err: client.reconnect(channel)}
	}
}

func sendChatCmd(client *operatorClient, message string) tea.Cmd {
	return func() tea.Msg {
		payload, _ := json.Marshal(map[string]string{
			"content":     message,
			"channel":     "cli",
			"session_key": "dashboard:local",
		})

		req, err := http.NewRequest("POST", client.BaseURL+"/v1/chat", bytes.NewBuffer(payload))
		if err != nil {
			return chatResponseMsg{err: err}
		}
		
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Client-Type", "cli")
		if client.Secret != "" {
			req.Header.Set("X-Ghost-Secret", client.Secret)
		}

		resp, err := client.HTTP.Do(req)
		if err != nil {
			return chatResponseMsg{err: err}
		}
		defer resp.Body.Close()

		// The /v1/chat endpoint actually streams SSE lines. For simplicity in the TUI fire-and-forget,
		// we just read until we see standard responses or [DONE].
		// A full SSE parser would look for "data: " chunks.
		
		scanner := bufio.NewScanner(resp.Body)
		var agentReply string

		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				if dataStr == "[DONE]" {
					break
				}
				
				var evt map[string]interface{}
				if err := json.Unmarshal([]byte(dataStr), &evt); err == nil {
					// We can capture tool statuses or lifecycle events if we want
					if evt["type"] == "tool_status" {
						agentReply += fmt.Sprintf("\n  [🔧 %v]", evt["label"])
					}
				} else {
					// It might just be a text string chunk (how streaming works usually)
					var textChunk string
					if json.Unmarshal([]byte(dataStr), &textChunk) == nil {
						agentReply += textChunk
					}
				}
			}
		}

		if agentReply == "" {
			agentReply = "(Agent processed request without text response)"
		}

		return chatResponseMsg{text: strings.TrimSpace(agentReply)}
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// Client Implementation
// ═════════════════════════════════════════════════════════════════════════════

func newOperatorClient(baseURL, secret string) *operatorClient {
	return &operatorClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Secret:  secret,
		HTTP:    &http.Client{Timeout: 30 * time.Second}, // Longer timeout for chat SSE reading
	}
}

func (c *operatorClient) get(path string, target interface{}) error {
	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil { return err }

	if c.Secret != "" { req.Header.Set("X-Ghost-Secret", c.Secret) }

	resp, err := c.HTTP.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK { return fmt.Errorf("status %d", resp.StatusCode) }
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *operatorClient) sanityCheck() authSanity {
	var res struct{ Status string `json:"status"` }
	err := c.get("/v1/health", &res)
	
	s := authSanity{
		BridgeSecretConfigured: c.Secret != "",
		APIReachable:           err == nil,
	}
	s.Blocking = !s.APIReachable
	return s
}

func (c *operatorClient) fetchDoctorAndChannels() (doctorPayload, map[string]channelHealth, error) {
	var res doctorPayload
	err := c.get("/v1/doctor", &res)
	if err != nil { return doctorPayload{}, nil, err }

	channels := make(map[string]channelHealth)
	for name, raw := range res.Channels {
		if data, err := json.Marshal(raw); err == nil {
			var ch channelHealth
			if json.Unmarshal(data, &ch) == nil { channels[name] = ch }
		}
	}
	return res, channels, nil
}

func (c *operatorClient) fetchSessionInspector() (sessionInspector, error) {
	var res sessionInspector
	return res, c.get("/v1/session/inspect", &res)
}

func (c *operatorClient) fetchTrace(requestID string) (tracePayload, error) {
	var res tracePayload
	return res, c.get("/v1/traces?request_id="+url.QueryEscape(requestID), &res)
}

func (c *operatorClient) reconnect(channel string) error {
	payload, _ := json.Marshal(map[string]string{"channel": channel})
	req, err := http.NewRequest("POST", c.BaseURL+"/v1/channels/reconnect", bytes.NewBuffer(payload))
	if err != nil { return err }

	req.Header.Set("Content-Type", "application/json")
	if c.Secret != "" { req.Header.Set("X-Ghost-Secret", c.Secret) }

	resp, err := c.HTTP.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK { return fmt.Errorf("status %d", resp.StatusCode) }
	return nil
}

// ═════════════════════════════════════════════════════════════════════════════
// Entry Point
// ═════════════════════════════════════════════════════════════════════════════

func runDashboard() {
	port := 8766
	if p := os.Getenv("GHOST_API_PORT"); p != "" { fmt.Sscanf(p, "%d", &port) }
	
	secret := os.Getenv("BRIDGE_SECRET")
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	
	client := newOperatorClient(baseURL, secret)
	p := tea.NewProgram(initialModel(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running dashboard: %v\n", err)
		os.Exit(1)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// Helpers
// ═════════════════════════════════════════════════════════════════════════════

func (m dashboardModel) selectedChannel() string {
	if m.selected < 0 || m.selected >= len(m.channelNames) { return "" }
	return m.channelNames[m.selected]
}

func (m dashboardModel) updateHistory() {
	for name, ch := range m.channels {
		h := m.history[name]
		var next rune
		if ch.Running { next = '█' } else if ch.FailureCount > m.failuresSeen[name] { next = '▆' } else { next = '░' }
		h += string(next)
		if len(h) > 40 { h = h[1:] }
		m.history[name] = h
		m.failuresSeen[name] = ch.FailureCount
	}
}

func (m dashboardModel) formatLatency() string {
	if len(m.trace.Events) < 2 { return "—" }
	first := m.trace.Events[0].At
	last := m.trace.Events[len(m.trace.Events)-1].At
	if first <= 0 || last <= first { return "—" }
	d := time.Duration(last-first) * time.Second
	if d < time.Second { return fmt.Sprintf("%dms", d.Milliseconds()) }
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func (m dashboardModel) latencyColor() lipgloss.Color {
	if len(m.trace.Events) < 2 { return cTextTertiary }
	first := m.trace.Events[0].At
	last := m.trace.Events[len(m.trace.Events)-1].At
	d := time.Duration(last-first) * time.Second
	switch {
	case d > 10*time.Second: return themeError
	case d > 3*time.Second: return themeWarning
	default: return themeSuccess
	}
}

func (m dashboardModel) sortedIncidents() []channelIncident {
	var items []channelIncident
	for _, inc := range m.trace.Incidents { items = append(items, inc) }
	sort.Slice(items, func(i, j int) bool { return items[i].LastAt > items[j].LastAt })
	return items
}

func statusColor(status string) lipgloss.Color {
	switch strings.ToLower(status) {
	case "ok", "healthy": return themeSuccess
	case "warning", "degraded": return themeWarning
	case "error", "failed": return themeError
	default: return cTextTertiary
	}
}

func boolStatus(v bool) string {
	if v { return "Connected" }
	return "Offline"
}

func boolColor(v bool) lipgloss.Color {
	if v { return themeSuccess }
	return themeError
}

func formatUnix(ts int64) string {
	if ts <= 0 { return "never" }
	return formatDuration(time.Since(time.Unix(ts, 0))) + " ago"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute { return fmt.Sprintf("%ds", int(d.Seconds())) }
	if d < time.Hour { return fmt.Sprintf("%dm", int(d.Minutes())) }
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func formatTimeShort(t time.Time) string { return t.Format("15:04") }
func truncate(s string, max int) string {
	if len(s) <= max || max <= 1 { return s }
	return s[:max-1] + "…"
}
func sortedKeys(m map[string]channelHealth) []string {
	keys := make([]string, 0, len(m))
	for k := range m { keys = append(keys, k) }
	sort.Strings(keys)
	return keys
}
func indexOf(slice []string, item string) int {
	for i, s := range slice { if s == item { return i } }
	return -1
}
func max(a, b int) int {
	if a > b { return a }
	return b
}