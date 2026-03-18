package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ═════════════════════════════════════════════════════════════════════════════
// GHOST Terminal UI - Redesigned Edition
// Design: Apple/Anthropic inspired - Clean, Hierarchical, Breathing Room
// ═════════════════════════════════════════════════════════════════════════════

const version = "2.1.0"

// ─── Refined Color System ───────────────────────────────────────────────────
var (
	// Neutrals (dark theme - iOS inspired)
	cBg         = lipgloss.Color("#0d0d0f")
	cSurface    = lipgloss.Color("#151518")
	cSurfaceHi  = lipgloss.Color("#1c1c20")
	cBorder     = lipgloss.Color("#2a2a2e")
	cBorderHi   = lipgloss.Color("#3f3f46")

	// Text hierarchy
	cTextPrimary   = lipgloss.Color("#f4f4f5")
	cTextSecondary = lipgloss.Color("#a1a1aa")
	cTextTertiary  = lipgloss.Color("#71717a")

	// Accents (iOS system colors)
	cAccent  = lipgloss.Color("#0a84ff")
	cSuccess = lipgloss.Color("#30d158")
	cWarning = lipgloss.Color("#ff9f0a")
	cError   = lipgloss.Color("#ff453a")
	cGhost   = lipgloss.Color("#bf5af2")

	// Components
	cardStyle = lipgloss.NewStyle().
			Background(cSurface).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(1, 2)

	cardActiveStyle = lipgloss.NewStyle().
			Background(cSurfaceHi).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cAccent).
			Padding(1, 2)

	badgeStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder)
)

// ═════════════════════════════════════════════════════════════════════════════
// Model
// ═════════════════════════════════════════════════════════════════════════════

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
	
	// Interaction
	selected     int
	expanded     bool
	lastRefresh  time.Time
	lastError    string
	lastErrorAt  time.Time
	isRefreshing bool
	
	// History for sparklines
	history      map[string]string
	failuresSeen map[string]int
	
	programStart time.Time
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

// ═════════════════════════════════════════════════════════════════════════════
// Initialization
// ═════════════════════════════════════════════════════════════════════════════

func initialModel(client *operatorClient) dashboardModel {
	return dashboardModel{
		client:       client,
		channels:     make(map[string]channelHealth),
		history:      make(map[string]string),
		failuresSeen: make(map[string]int),
		programStart: time.Now(),
		trace: tracePayload{
			Incidents: make(map[string]channelIncident),
		},
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(m.client), tickCmd())
}

// ═════════════════════════════════════════════════════════════════════════════
// Update
// ═════════════════════════════════════════════════════════════════════════════

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.channelNames)-1 {
				m.selected++
			}
		case "enter":
			m.expanded = !m.expanded
		case "r":
			if sel := m.selectedChannel(); sel != "" {
				return m, reconnectCmd(m.client, sel)
			}
		case "d":
			m.isRefreshing = true
			return m, fetchDataCmd(m.client)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(tickCmd(), fetchDataCmd(m.client))

	case refreshMsg:
		m.isRefreshing = false
		m.lastRefresh = time.Now()
		
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.lastErrorAt = time.Now()
		} else {
			m.lastError = ""
			m.sanity = msg.sanity
			m.doctor = msg.doctor
			m.channels = msg.channels
			m.inspector = msg.inspector
			m.trace = msg.trace
			
			// Preserve selection
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
			m.lastError = fmt.Sprintf("Reconnect %s: %v", msg.channel, msg.err)
			m.lastErrorAt = time.Now()
		}
		return m, fetchDataCmd(m.client)
	}

	return m, nil
}

// ═════════════════════════════════════════════════════════════════════════════
// View - Main Layout
// ═════════════════════════════════════════════════════════════════════════════

func (m dashboardModel) View() string {
	if m.sanity.Blocking {
		return m.renderLockScreen()
	}

	if m.width < 60 || m.height < 20 {
		return m.renderCompactView()
	}

	// Join vertical: Header + Dashboard + Footer
	// NO fixed heights - each section sizes naturally
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderHeader(),
		m.renderDashboard(),
		m.renderFooter(),
	)
}

// ─── Header ─────────────────────────────────────────────────────────────────

func (m dashboardModel) renderHeader() string {
	// Row 1: Brand + Status + Uptime + Version
	brand := lipgloss.NewStyle().Foreground(cGhost).Bold(true).Render("◉ GHOST")
	status := m.renderConnectionStatus()
	uptime := lipgloss.NewStyle().Foreground(cTextTertiary).Render(formatDuration(time.Since(m.programStart)))
	ver := lipgloss.NewStyle().Foreground(cTextTertiary).Render("v" + version)

	spacer := strings.Repeat(" ", max(1, m.width-lipgloss.Width(brand)-lipgloss.Width(status)-lipgloss.Width(uptime)-lipgloss.Width(ver)-6))
	
	topRow := lipgloss.JoinHorizontal(lipgloss.Center, brand, "  ", status, spacer, uptime, "  ", ver)
	
	// Row 2: Status pills
	pills := m.renderStatusPills()
	
	return lipgloss.JoinVertical(lipgloss.Left, topRow, "", pills, "")
}

func (m dashboardModel) renderConnectionStatus() string {
	if m.isRefreshing {
		return badgeStyle.BorderForeground(cAccent).Foreground(cAccent).Render("⟳ Syncing")
	}
	if m.lastError != "" && time.Since(m.lastErrorAt) < 30*time.Second {
		return badgeStyle.BorderForeground(cError).Foreground(cError).Render("● Error")
	}
	return badgeStyle.BorderForeground(cSuccess).Foreground(cSuccess).Render("● Live")
}

func (m dashboardModel) renderStatusPills() string {
	doctorStatus := m.doctor.Status
	if doctorStatus == "" {
		doctorStatus = "unknown"
	}
	
	pills := []string{
		m.renderPill("System", doctorStatus, statusColor(doctorStatus)),
		m.renderPill("API", boolStatus(m.sanity.APIReachable), boolColor(m.sanity.APIReachable)),
		m.renderPill("Channels", fmt.Sprintf("%d", len(m.channels)), cTextSecondary),
		m.renderPill("Latency", m.formatLatency(), m.latencyColor()),
	}
	
	return lipgloss.JoinHorizontal(lipgloss.Top, pills...)
}

func (m dashboardModel) renderPill(label, value string, color lipgloss.Color) string {
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		lipgloss.NewStyle().Foreground(cTextTertiary).Render(label),
		lipgloss.NewStyle().Foreground(color).Bold(true).Render(value),
	)
	
	return lipgloss.NewStyle().
		Background(cSurface).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(0, 2).
		MarginRight(1).
		Render(content)
}

// ─── Dashboard ──────────────────────────────────────────────────────────────

func (m dashboardModel) renderDashboard() string {
	// Calculate available height (header ~5 lines, footer ~1 line)
	availableHeight := m.height - 7
	if availableHeight < 10 {
		availableHeight = 10
	}

	// Split: 42% channels, 58% activity
	leftWidth := int(float64(m.width) * 0.42)
	rightWidth := m.width - leftWidth - 2

	left := m.renderChannelsPanel(leftWidth, availableHeight)
	right := m.renderActivityPanel(rightWidth, availableHeight)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

// ─── Channels Panel ─────────────────────────────────────────────────────────

func (m dashboardModel) renderChannelsPanel(width, height int) string {
	title := lipgloss.NewStyle().Foreground(cTextPrimary).Bold(true).Render("Channels")
	
	if len(m.channelNames) == 0 {
		content := lipgloss.NewStyle().Foreground(cTextTertiary).Render("No channels configured")
		return cardStyle.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, title, "", content))
	}

	var items []string
	for i, name := range m.channelNames {
		items = append(items, m.renderChannelItem(name, i == m.selected, width-4))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, items...)
	return cardStyle.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, title, "", content))
}

func (m dashboardModel) renderChannelItem(name string, selected bool, width int) string {
	ch := m.channels[name]
	
	// Status styling
	statusColor := cTextTertiary
	statusIcon := "○"
	statusText := "Standby"
	
	if ch.Running {
		statusColor = cSuccess
		statusIcon = "●"
		statusText = "Active"
	} else if ch.Enabled {
		statusColor = cError
		statusIcon = "●"
		statusText = "Failed"
	}

	// Build row
	nameCol := lipgloss.NewStyle().Width(12).Foreground(cTextPrimary).Render(truncate(name, 12))
	statusCol := lipgloss.NewStyle().Foreground(statusColor).Render(fmt.Sprintf("%s %s", statusIcon, statusText))
	sparkCol := lipgloss.NewStyle().Foreground(cTextTertiary).Render(m.renderSparkline(name, width-20))
	
	gap := width - 12 - lipgloss.Width(statusCol) - lipgloss.Width(sparkCol)
	if gap < 1 {
		gap = 1
	}
	
	row := lipgloss.JoinHorizontal(lipgloss.Top, nameCol, statusCol, strings.Repeat(" ", gap), sparkCol)

	if selected {
		style := cardActiveStyle
		if !m.expanded {
			style = cardStyle.BorderForeground(cAccent)
		}
		
		details := ""
		if m.expanded {
			details = m.renderChannelDetails(name, width-6)
		} else {
			details = lipgloss.NewStyle().Foreground(cTextTertiary).Render("↵ Expand for details")
		}
		
		return style.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, row, "", details))
	}

	return lipgloss.NewStyle().Padding(0, 1).Render(row)
}

func (m dashboardModel) renderSparkline(name string, width int) string {
	hist := m.history[name]
	if len(hist) > width {
		hist = hist[len(hist)-width:]
	}
	
	// Convert to blocks: █ = success, ▆ = failure, ░ = idle
	var blocks []rune
	for _, c := range hist {
		switch c {
		case '█':
			blocks = append(blocks, '█')
		case '▆':
			blocks = append(blocks, '▆')
		default:
			blocks = append(blocks, '░')
		}
	}
	
	return string(blocks)
}

func (m dashboardModel) renderChannelDetails(name string, width int) string {
	ch := m.channels[name]
	
	lines := []string{
		fmt.Sprintf("Last success: %s", formatUnix(ch.LastSuccess)),
		fmt.Sprintf("Last failure: %s", formatUnix(ch.LastFailure)),
		fmt.Sprintf("Failures: %d", ch.FailureCount),
	}
	
	if ch.LastSendErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(cError).Render(fmt.Sprintf("Error: %s", truncate(ch.LastSendErr, width-10))))
	}
	
	if ch.Fatal {
		lines = append(lines, lipgloss.NewStyle().Foreground(cWarning).Render("⚠ Fatal condition"))
	}
	
	lines = append(lines, "", lipgloss.NewStyle().Foreground(cAccent).Render("[R] Reconnect channel"))
	
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ─── Activity Panel ─────────────────────────────────────────────────────────

func (m dashboardModel) renderActivityPanel(width, height int) string {
	title := lipgloss.NewStyle().Foreground(cTextPrimary).Bold(true).Render("Live Activity")
	
	var sections []string
	
	if m.inspector.LastRequestID != "" {
		reqInfo := lipgloss.NewStyle().Foreground(cTextSecondary).Render(fmt.Sprintf("Request: %s", truncate(m.inspector.LastRequestID, width-12)))
		sections = append(sections, reqInfo)
	}
	
	sections = append(sections, "", m.renderEventLog(width-4, height-8))
	
	if len(m.trace.Incidents) > 0 {
		sections = append(sections, "", m.renderIncidents(width-4))
	}
	
	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return cardStyle.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, title, "", content))
}

func (m dashboardModel) renderEventLog(width, maxHeight int) string {
	if len(m.trace.Events) == 0 {
		return lipgloss.NewStyle().Foreground(cTextTertiary).Render("No recent activity")
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
	icon := "•"
	color := cTextSecondary
	
	s := strings.ToLower(evt.State)
	switch {
	case strings.Contains(s, "deliver") || strings.Contains(s, "sent"):
		icon = "✓"
		color = cSuccess
	case strings.Contains(s, "fail") || strings.Contains(s, "error"):
		icon = "✕"
		color = cError
	case strings.Contains(s, "queue") || strings.Contains(s, "process"):
		icon = "◉"
		color = cWarning
	}
	
	if isLatest {
		icon = lipgloss.NewStyle().Foreground(cAccent).Render(icon)
	} else {
		icon = lipgloss.NewStyle().Foreground(color).Render(icon)
	}
	
	timeStr := lipgloss.NewStyle().Foreground(cTextTertiary).Width(8).Render(formatTimeShort(time.Unix(evt.At, 0)))
	stateStr := lipgloss.NewStyle().Foreground(color).Width(10).Render(strings.ToLower(evt.State))
	
	detail := ""
	if evt.Channel != "" {
		detail = fmt.Sprintf("[%s]", evt.Channel)
	}
	if evt.Detail != "" {
		detail += " " + evt.Detail
	}
	detail = truncate(detail, width-22)
	detailStr := lipgloss.NewStyle().Foreground(cTextSecondary).Render(detail)
	
	return fmt.Sprintf("%s %s %s %s", icon, timeStr, stateStr, detailStr)
}

func (m dashboardModel) renderIncidents(width int) string {
	title := lipgloss.NewStyle().Foreground(cWarning).Render("Recent Issues")
	
	var items []string
	for _, inc := range m.sortedIncidents() {
		if len(items) >= 3 {
			break
		}
		line := fmt.Sprintf("• %s: %s (%s ago)",
			inc.Channel,
			truncate(inc.LastError, width-25),
			formatDuration(time.Since(time.Unix(inc.LastAt, 0))),
		)
		items = append(items, lipgloss.NewStyle().Foreground(cTextSecondary).Render(line))
	}
	
	return lipgloss.JoinVertical(lipgloss.Left, title, lipgloss.JoinVertical(lipgloss.Left, items...))
}

// ─── Footer ─────────────────────────────────────────────────────────────────

func (m dashboardModel) renderFooter() string {
	controls := "↑↓ Select • ↵ Expand • R Reconnect • D Refresh • Q Quit"
	if m.lastError != "" {
		controls = lipgloss.NewStyle().Foreground(cError).Render(fmt.Sprintf("Error: %s", truncate(m.lastError, 40)))
	}
	
	refresh := ""
	if !m.lastRefresh.IsZero() {
		refresh = fmt.Sprintf("Updated %s ago", formatDuration(time.Since(m.lastRefresh)))
	}
	
	gap := m.width - lipgloss.Width(controls) - lipgloss.Width(refresh) - 4
	if gap < 1 {
		gap = 1
	}
	
	return lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 2).
		Foreground(cTextTertiary).
		Background(cBg).
		Render(controls + strings.Repeat(" ", gap) + refresh)
}

// ─── Lock Screen ────────────────────────────────────────────────────────────

func (m dashboardModel) renderLockScreen() string {
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		lipgloss.NewStyle().Foreground(cError).Bold(true).Render("Connection Required"),
		"",
		lipgloss.NewStyle().Foreground(cTextSecondary).Render("Unable to reach GHOST API"),
		"",
		m.renderSanityChecks(),
	)
	
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cError).
		Padding(2, 4).
		Render(content)
	
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m dashboardModel) renderSanityChecks() string {
	var lines []string
	
	check := func(name string, ok bool, detail string) string {
		icon := "✓"
		color := cSuccess
		if !ok {
			icon = "✕"
			color = cError
		}
		line := fmt.Sprintf("%s %s", lipgloss.NewStyle().Foreground(color).Render(icon), name)
		if detail != "" {
			line += lipgloss.NewStyle().Foreground(cTextTertiary).Render(" (" + detail + ")")
		}
		return line
	}
	
	lines = append(lines, check("Bridge Secret", m.sanity.BridgeSecretConfigured, ""))
	lines = append(lines, check("API Reachable", m.sanity.APIReachable, ""))
	
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ─── Compact View ───────────────────────────────────────────────────────────

func (m dashboardModel) renderCompactView() string {
	lines := []string{"GHOST " + version, ""}
	
	for _, name := range m.channelNames {
		ch := m.channels[name]
		status := "○"
		if ch.Running {
			status = "●"
		} else if ch.Enabled {
			status = "✕"
		}
		lines = append(lines, fmt.Sprintf("%s %s", status, name))
	}
	
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
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
		if sanity.Blocking {
			return refreshMsg{sanity: sanity}
		}
		
		doctor, channels, err := client.fetchDoctorAndChannels()
		if err != nil {
			return refreshMsg{sanity: sanity, err: err}
		}
		
		inspector, err := client.fetchSessionInspector()
		if err != nil {
			return refreshMsg{sanity: sanity, err: err}
		}
		
		trace := tracePayload{Incidents: make(map[string]channelIncident)}
		if inspector.LastRequestID != "" {
			trace, _ = client.fetchTrace(inspector.LastRequestID)
		}
		
		return refreshMsg{
			sanity:    sanity,
			doctor:    doctor,
			channels:  channels,
			inspector: inspector,
			trace:     trace,
		}
	}
}

func reconnectCmd(client *operatorClient, channel string) tea.Cmd {
	return func() tea.Msg {
		err := client.reconnect(channel)
		return reconnectMsg{channel: channel, err: err}
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// Helpers
// ═════════════════════════════════════════════════════════════════════════════

func (m dashboardModel) selectedChannel() string {
	if m.selected < 0 || m.selected >= len(m.channelNames) {
		return ""
	}
	return m.channelNames[m.selected]
}

func (m dashboardModel) updateHistory() {
	for name, ch := range m.channels {
		h := m.history[name]
		var next rune
		if ch.Running {
			next = '█'
		} else if ch.FailureCount > m.failuresSeen[name] {
			next = '▆'
		} else {
			next = '░'
		}
		h += string(next)
		if len(h) > 40 {
			h = h[1:]
		}
		m.history[name] = h
		m.failuresSeen[name] = ch.FailureCount
	}
}

func (m dashboardModel) formatLatency() string {
	if len(m.trace.Events) < 2 {
		return "—"
	}
	first := m.trace.Events[0].At
	last := m.trace.Events[len(m.trace.Events)-1].At
	if first <= 0 || last <= first {
		return "—"
	}
	d := time.Duration(last-first) * time.Second
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func (m dashboardModel) latencyColor() lipgloss.Color {
	if len(m.trace.Events) < 2 {
		return cTextTertiary
	}
	first := m.trace.Events[0].At
	last := m.trace.Events[len(m.trace.Events)-1].At
	d := time.Duration(last-first) * time.Second
	
	switch {
	case d > 10*time.Second:
		return cError
	case d > 3*time.Second:
		return cWarning
	default:
		return cSuccess
	}
}

func (m dashboardModel) sortedIncidents() []channelIncident {
	var items []channelIncident
	for _, inc := range m.trace.Incidents {
		items = append(items, inc)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastAt > items[j].LastAt })
	return items
}

func statusColor(status string) lipgloss.Color {
	switch strings.ToLower(status) {
	case "ok", "healthy":
		return cSuccess
	case "warning", "degraded":
		return cWarning
	case "error", "failed":
		return cError
	default:
		return cTextTertiary
	}
}

func boolStatus(v bool) string {
	if v {
		return "Connected"
	}
	return "Offline"
}

func boolColor(v bool) lipgloss.Color {
	if v {
		return cSuccess
	}
	return cError
}

func formatUnix(ts int64) string {
	if ts <= 0 {
		return "never"
	}
	return formatDuration(time.Since(time.Unix(ts, 0))) + " ago"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func formatTimeShort(t time.Time) string {
	return t.Format("15:04")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func sortedKeys(m map[string]channelHealth) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}