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

// Design System - "Cyber/System Monitor" Theme
var (
	// Palette
	colBg      = lipgloss.Color("#0f111a") // Deep Dark Blue/Black
	colSurface = lipgloss.Color("#1a1b26") // Panel Bg
	colBorder  = lipgloss.Color("#3b4261") // Subtle Border
	colActive  = lipgloss.Color("#7aa2f7") // Blue
	colSuccess = lipgloss.Color("#9ece6a") // Green
	colWarn    = lipgloss.Color("#e0af68") // Orange
	colError   = lipgloss.Color("#f7768e") // Red
	colText    = lipgloss.Color("#c0caf5") // FG
	colSub     = lipgloss.Color("#565f89") // Dimmed

	// Styles
	baseStyle = lipgloss.NewStyle().
			Foreground(colText)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Background(colSurface).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Foreground(colActive).
			Bold(true).
			Background(colSurface).
			Padding(0, 1)

	// Status Pills
	pillStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)
)

const ghostASCII = `
  .-.
 (o o)
 | O \
  \   \
   '~~~'
`

const (
	viewOverview = "overview"
	viewChannels = "channels"
	viewTrace    = "trace"
)

type dashboardModel struct {
	client       *operatorClient
	width        int
	height       int
	lastUpdated  time.Time
	lastError    string
	lastErrorAt  time.Time
	actionStatus string
	doctor       doctorPayload
	channels     map[string]channelHealth
	channelNames []string
	selected     int
	inspector    sessionInspector
	trace        tracePayload
	sanity       authSanity
	programStart time.Time
	viewMode     string
	detailOpen   bool
	pulseTicks   int
	failuresSeen map[string]int
	history      map[string]string
}

type tickMsg struct{}
type refreshResultMsg struct {
	sanity    authSanity
	doctor    doctorPayload
	channels  map[string]channelHealth
	inspector sessionInspector
	trace     tracePayload
	err       error
	at        time.Time
}
type reconnectResultMsg struct {
	channel string
	err     error
}

type operatorClient struct {
	baseURL string
	secret  string
	http    *http.Client
	session string
	chatID  string
}

type endpointProbe struct {
	Name      string
	Path      string
	Reachable bool
	Status    int
	Detail    string
}

type authSanity struct {
	BridgeSecretConfigured bool
	APIReachable           bool
	Endpoints              []endpointProbe
	Blocking               bool
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type doctorPayload struct {
	Status    string                   `json:"status"`
	Checks    []doctorCheck            `json:"checks"`
	Channels  map[string]channelHealth `json:"channels"`
	Timestamp int64                    `json:"timestamp"`
}
type channelHealth struct {
	Enabled      bool   `json:"enabled"`
	Running      bool   `json:"running"`
	Fatal        bool   `json:"fatal"`
	FatalReason  string `json:"fatal_reason"`
	FailureCount int    `json:"failure_count"`
	LastSendErr  string `json:"last_send_error"`
	LastFailure  int64  `json:"last_failure_at"`
	LastSuccess  int64  `json:"last_success_at"`
}
type sessionInspector struct {
	RequestedSession string `json:"requested_session"`
	DeliveryTarget   string `json:"delivery_target"`
	LastRequestID    string `json:"last_request_id"`
	ActiveSession    struct {
		Channel string `json:"channel"`
		ChatID  string `json:"chat_id"`
	} `json:"active_session"`
}
type traceEvent struct {
	RequestID string `json:"request_id"`
	State     string `json:"state"`
	At        int64  `json:"at"`
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	Detail    string `json:"detail"`
}
type channelIncident struct {
	Channel      string `json:"channel"`
	FailureCount int    `json:"failure_count"`
	LastError    string `json:"last_error"`
	LastAt       int64  `json:"last_at"`
}
type tracePayload struct {
	RequestID string                     `json:"request_id"`
	Events    []traceEvent               `json:"events"`
	Incidents map[string]channelIncident `json:"incidents"`
}

func initialModel(client *operatorClient) dashboardModel {
	return dashboardModel{
		client:       client,
		actionStatus: "TAB cycle • ENTER detail • F1/F2/F3 views • R reconnect • D refresh • Q quit",
		channels:     map[string]channelHealth{},
		programStart: time.Now(),
		viewMode:     viewOverview,
		failuresSeen: map[string]int{},
		history:      map[string]string{},
		trace: tracePayload{
			Incidents: map[string]channelIncident{},
		},
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(fetchSnapshotCmd(m.client), scheduleTick())
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "f1":
			m.viewMode = viewOverview
		case "f2":
			m.viewMode = viewChannels
		case "f3":
			m.viewMode = viewTrace
		case "tab":
			if len(m.channelNames) > 0 {
				m.selected = (m.selected + 1) % len(m.channelNames)
				m.actionStatus = fmt.Sprintf("Selected channel: %s", m.selectedChannel())
			}
		case "enter":
			m.detailOpen = !m.detailOpen
		case "d":
			return m, fetchSnapshotCmd(m.client)
		case "r":
			ch := m.selectedChannel()
			if ch != "" {
				return m, reconnectCmd(m.client, ch)
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		if m.pulseTicks > 0 {
			m.pulseTicks--
		}
		return m, tea.Batch(fetchSnapshotCmd(m.client), scheduleTick())
	case refreshResultMsg:
		m.sanity = msg.sanity
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.lastErrorAt = msg.at
		} else {
			m.lastError = ""
			m.lastUpdated = msg.at
			m.doctor = msg.doctor
			m.channels = msg.channels
			m.inspector = msg.inspector
			m.trace = msg.trace
			m.channelNames = sortedKeys(msg.channels)
			m.updateChannelHistory()
			m.pulseTicks = 2
			if len(m.channelNames) > 0 && m.selected >= len(m.channelNames) {
				m.selected = 0
			}
		}
	case reconnectResultMsg:
		if msg.err != nil {
			m.actionStatus = fmt.Sprintf("Reconnect failed for %s: %v", msg.channel, msg.err)
			m.lastError = msg.err.Error()
			m.lastErrorAt = time.Now()
		} else {
			m.actionStatus = fmt.Sprintf("Reconnect triggered for %s", msg.channel)
			return m, fetchSnapshotCmd(m.client)
		}
	}
	return m, nil
}

func (m dashboardModel) View() string {
	if m.sanity.Blocking {
		return m.renderLockScreen()
	}
	width := m.width
	height := m.height
	if width <= 0 {
		width = 140
	}
	if height <= 0 {
		height = 42
	}
	header := m.renderHeader(width)
	bodyHeight := height - 7
	if bodyHeight < 12 {
		bodyHeight = 12
	}
	var content string
	switch m.viewMode {
	case viewChannels:
		content = m.renderChannelsMode(width, bodyHeight)
	case viewTrace:
		content = m.renderTraceMode(width, bodyHeight)
	default:
		content = m.renderOverviewMode(width, bodyHeight)
	}
	footer := m.renderFooter(width)
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (m dashboardModel) renderHeader(width int) string {
	uptime := time.Since(m.programStart).Round(time.Second)
	ascii := lipgloss.NewStyle().Foreground(colSuccess).Render(ghostASCII)
	logo := titleStyle.Render("GHOST SYSTEM MONITOR")
	stats := lipgloss.NewStyle().Foreground(colSub).Render(fmt.Sprintf("UP %s", uptime))
	ver := lipgloss.NewStyle().Foreground(colActive).Render("v" + version)
	strip := m.renderHealthStrip()
	w := width - lipgloss.Width(ascii) - lipgloss.Width(logo) - lipgloss.Width(stats) - lipgloss.Width(ver) - lipgloss.Width(strip) - 8
	if w < 0 {
		w = 0
	}
	spacer := strings.Repeat(" ", w)
	bar := lipgloss.JoinHorizontal(lipgloss.Center,
		ascii,
		" ",
		logo,
		"  ",
		stats,
		" ",
		spacer,
		strip,
		" ",
		ver,
	)
	return panelStyle.
		Width(width - 2).
		Height(1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder).
		Render(bar)
}

func (m dashboardModel) renderOverviewMode(width, bodyHeight int) string {
	leftWidth := int(float64(width) * 0.42)
	if leftWidth < 46 {
		leftWidth = 46
	}
	rightWidth := width - leftWidth - 3
	if rightWidth < 46 {
		rightWidth = 46
	}
	left := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderChannels(leftWidth),
		m.renderDoctor(leftWidth),
	)
	right := m.renderTrace(rightWidth, bodyHeight)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m dashboardModel) renderChannelsMode(width, bodyHeight int) string {
	listWidth := int(float64(width) * 0.56)
	if listWidth < 52 {
		listWidth = 52
	}
	drawerWidth := width - listWidth - 3
	if drawerWidth < 36 {
		drawerWidth = 36
	}
	list := m.renderChannels(listWidth)
	var right string
	if m.detailOpen {
		right = m.renderChannelDrawer(drawerWidth, bodyHeight)
	} else {
		right = m.renderDoctor(drawerWidth)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, list, right)
}

func (m dashboardModel) renderTraceMode(width, bodyHeight int) string {
	traceWidth := int(float64(width) * 0.68)
	if traceWidth < 56 {
		traceWidth = 56
	}
	sideWidth := width - traceWidth - 3
	if sideWidth < 32 {
		sideWidth = 32
	}
	trace := m.renderTrace(traceWidth, bodyHeight)
	side := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderDoctor(sideWidth),
		m.renderMiniChannels(sideWidth),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, trace, side)
}

func (m dashboardModel) renderChannels(width int) string {
	var rows []string
	rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("CHANNEL MATRIX "+m.viewHint()))
	rows = append(rows, "")
	if len(m.channelNames) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("No channels yet — enable gateway channels to start delivery."))
	}
	for i, name := range m.channelNames {
		ch := m.channels[name]
		borderColor := colBorder
		statusColor := colSub
		statusIcon := "●"
		statusText := "OFFLINE"
		if i == m.selected {
			if m.pulseTicks > 0 {
				borderColor = colSuccess
			} else {
				borderColor = colActive
			}
		}
		if ch.Running {
			statusColor = colSuccess
			statusText = "RUNNING"
		} else if ch.Enabled {
			statusColor = colError
			statusText = "FAILING"
			statusIcon = "✖"
		} else {
			statusText = "DISABLED"
		}
		nameLine := lipgloss.NewStyle().Foreground(colText).Bold(true).Render(strings.ToUpper(name))
		statusLine := lipgloss.NewStyle().Foreground(statusColor).Render(fmt.Sprintf("%s %s", statusIcon, statusText))
		spark := lipgloss.NewStyle().Foreground(colSub).Render(m.history[name])
		statLine := lipgloss.NewStyle().Foreground(colSub).Render(fmt.Sprintf("fails %d • ok %s • fail %s", ch.FailureCount, relativeUnix(ch.LastSuccess), relativeUnix(ch.LastFailure)))
		if ch.LastSendErr != "" {
			statLine = lipgloss.NewStyle().Foreground(colError).Render("Last Err: " + truncate(ch.LastSendErr, 20))
		}
		gap := width - 8 - lipgloss.Width(nameLine) - lipgloss.Width(statusLine)
		if gap < 1 {
			gap = 1
		}
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1).
			Width(width - 4).
			Render(
				lipgloss.JoinVertical(lipgloss.Left,
					lipgloss.JoinHorizontal(lipgloss.Top, nameLine, strings.Repeat(" ", gap), statusLine),
					spark,
					statLine,
				),
			)
		rows = append(rows, box)
	}
	return panelStyle.
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m dashboardModel) renderTrace(width, height int) string {
	latency := m.pipelineLatency()
	title := lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("LIVE TRACE • req "+safeValue(m.inspector.LastRequestID))
	latencyBadge := m.latencyBadge(latency)
	var lines []string
	if len(m.trace.Events) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colSub).Render("No trace yet — send a message via "+safeValue(m.inspector.DeliveryTarget)+" to populate lifecycle events."))
	} else {
		start := 0
		maxLines := height - 4
		if maxLines < 5 {
			maxLines = 5
		}
		if len(m.trace.Events) > maxLines {
			start = len(m.trace.Events) - maxLines
		}
		for i := start; i < len(m.trace.Events); i++ {
			evt := m.trace.Events[i]
			icon, color := traceSeverity(evt.State, evt.Detail)
			line := fmt.Sprintf(
				"%s %s %s",
				lipgloss.NewStyle().Foreground(color).Bold(true).Render(icon),
				lipgloss.NewStyle().Foreground(colSub).Render(relativeUnix(evt.At)),
				lipgloss.NewStyle().Foreground(color).Bold(true).Render(strings.ToUpper(evt.State)),
			)
			if evt.Channel != "" {
				line += " " + lipgloss.NewStyle().Foreground(colText).Render("["+evt.Channel+"]")
			}
			if evt.Detail != "" {
				line += " " + lipgloss.NewStyle().Foreground(colSub).Render(truncate(evt.Detail, width-34))
			}
			if i == len(m.trace.Events)-1 && m.pulseTicks > 0 {
				line = lipgloss.NewStyle().Foreground(colSuccess).Render("• " + line)
			}
			lines = append(lines, line)
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	topGap := width - lipgloss.Width(title) - lipgloss.Width(latencyBadge) - 2
	if topGap < 1 {
		topGap = 1
	}
	head := lipgloss.JoinHorizontal(lipgloss.Top, title, strings.Repeat(" ", topGap), latencyBadge)
	return panelStyle.
		Width(width).
		Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, head, "", content))
}

func (m dashboardModel) renderDoctor(width int) string {
	title := lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("SYSTEM DIAGNOSTICS")
	var rows []string
	rows = append(rows, title, "")
	rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("Target: "+safeValue(m.inspector.DeliveryTarget)))
	rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("Last update: "+relativeTime(m.lastUpdated)))
	rows = append(rows, "")
	if len(m.doctor.Checks) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("No checks run yet — press D to refresh doctor checks."))
	} else {
		for _, check := range m.doctor.Checks {
			icon := "●"
			color := colSuccess
			if check.Status != "ok" && check.Status != "healthy" {
				icon = "▲"
				color = colWarn
				if check.Status == "error" || strings.Contains(strings.ToLower(check.Message), "fail") {
					icon = "✖"
					color = colError
				}
			}
			rows = append(rows, lipgloss.NewStyle().Foreground(color).Render(icon)+" "+truncate(check.Name, width-10))
		}
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m dashboardModel) renderMiniChannels(width int) string {
	rows := []string{lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("CHANNEL SNAPSHOT"), ""}
	if len(m.channelNames) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("No channels available."))
	} else {
		for _, name := range m.channelNames {
			ch := m.channels[name]
			marker := "●"
			color := colSub
			if ch.Running {
				color = colSuccess
			} else if ch.Enabled {
				color = colError
				marker = "✖"
			}
			rows = append(rows, lipgloss.NewStyle().Foreground(color).Render(marker)+" "+name+" "+truncate(m.history[name], 16))
		}
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m dashboardModel) renderChannelDrawer(width, height int) string {
	name := m.selectedChannel()
	var rows []string
	rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("CHANNEL DETAIL DRAWER"), "")
	if name == "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("Select a channel with TAB."))
	} else {
		ch := m.channels[name]
		rows = append(rows, lipgloss.NewStyle().Foreground(colText).Bold(true).Render(strings.ToUpper(name)))
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("running: "+boolWord(ch.Running)))
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("enabled: "+boolWord(ch.Enabled)))
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render(fmt.Sprintf("failures: %d", ch.FailureCount)))
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("last success: "+relativeUnix(ch.LastSuccess)))
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("last failure: "+relativeUnix(ch.LastFailure)))
		rows = append(rows, "")
		if ch.FatalReason != "" {
			rows = append(rows, lipgloss.NewStyle().Foreground(colError).Render("fatal: "+truncate(ch.FatalReason, width-8)))
		}
		if ch.LastSendErr != "" {
			rows = append(rows, lipgloss.NewStyle().Foreground(colWarn).Render("send err: "+truncate(ch.LastSendErr, width-8)))
		}
		if ch.FatalReason == "" && ch.LastSendErr == "" {
			rows = append(rows, lipgloss.NewStyle().Foreground(colSuccess).Render("No active fault markers."))
		}
	}
	return panelStyle.
		Width(width).
		Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m dashboardModel) renderFooter(width int) string {
	help := m.actionStatus
	if m.lastError != "" {
		help = "ERROR " + truncate(m.lastError, 48) + " • " + relativeTime(m.lastErrorAt)
	}
	incidents := m.incidentMemoryLane()
	mode := strings.ToUpper(m.viewMode)
	left := lipgloss.NewStyle().Foreground(colSub).Render("MODE "+mode+" • "+help)
	right := lipgloss.NewStyle().Foreground(colWarn).Render(incidents)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Foreground(colSub).
		Background(colBg).
		Render(left + strings.Repeat(" ", gap) + right)
}

func (m dashboardModel) renderHealthStrip() string {
	selected := m.selectedChannel()
	if selected == "" {
		selected = "none"
	}
	lastErrAge := "none"
	if m.lastError != "" {
		lastErrAge = relativeTime(m.lastErrorAt)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		statusChip("doctor", strings.ToUpper(safeValue(m.doctor.Status)), statusColor(m.doctor.Status)),
		" ",
		statusChip("api", boolWord(m.sanity.APIReachable), pickColor(m.sanity.APIReachable, colSuccess, colError)),
		" ",
		statusChip("secret", boolWord(m.sanity.BridgeSecretConfigured), pickColor(m.sanity.BridgeSecretConfigured, colSuccess, colError)),
		" ",
		statusChip("channel", selected, colActive),
		" ",
		statusChip("last_err", lastErrAge, colWarn),
	)
}

func (m dashboardModel) updateChannelHistory() {
	for name, ch := range m.channels {
		h := m.history[name]
		next := "·"
		if ch.Running {
			next = "█"
		}
		if ch.FailureCount > m.failuresSeen[name] {
			next = "▆"
		}
		h += next
		if len(h) > 24 {
			h = h[len(h)-24:]
		}
		m.history[name] = h
		m.failuresSeen[name] = ch.FailureCount
	}
}

func (m dashboardModel) pipelineLatency() time.Duration {
	if len(m.trace.Events) < 2 {
		return 0
	}
	first := m.trace.Events[0].At
	last := m.trace.Events[len(m.trace.Events)-1].At
	if first <= 0 || last <= first {
		return 0
	}
	return time.Duration(last-first) * time.Second
}

func (m dashboardModel) latencyBadge(d time.Duration) string {
	if d <= 0 {
		return statusChip("latency", "—", colSub)
	}
	color := colSuccess
	if d > 8*time.Second {
		color = colError
	} else if d > 3*time.Second {
		color = colWarn
	}
	return statusChip("latency", d.String(), color)
}

func (m dashboardModel) incidentMemoryLane() string {
	if len(m.trace.Incidents) == 0 {
		return "incidents: clear"
	}
	type item struct {
		text string
		at   int64
	}
	items := make([]item, 0, len(m.trace.Incidents))
	dedupe := map[string]int{}
	for _, inc := range m.trace.Incidents {
		key := strings.TrimSpace(inc.Channel + " " + inc.LastError)
		if key == "" {
			key = inc.Channel + " unknown"
		}
		dedupe[key] += inc.FailureCount
		items = append(items, item{text: key, at: inc.LastAt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at > items[j].at })
	seen := map[string]bool{}
	parts := []string{}
	for _, it := range items {
		if seen[it.text] {
			continue
		}
		seen[it.text] = true
		parts = append(parts, truncate(fmt.Sprintf("%s x%d", it.text, dedupe[it.text]), 34))
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "incidents: clear"
	}
	return "incidents: " + strings.Join(parts, " | ")
}

func (m dashboardModel) viewHint() string {
	if m.viewMode == viewChannels && m.detailOpen {
		return "• ENTER hides detail"
	}
	if m.viewMode == viewChannels {
		return "• ENTER shows detail"
	}
	return "• TAB selects channel"
}

func traceSeverity(state, detail string) (string, lipgloss.Color) {
	s := strings.ToLower(state + " " + detail)
	if strings.Contains(s, "fail") || strings.Contains(s, "error") {
		return "✖", colError
	}
	if strings.Contains(s, "queue") || strings.Contains(s, "process") || strings.Contains(s, "retry") {
		return "▲", colWarn
	}
	if strings.Contains(s, "deliver") || strings.Contains(s, "sent") {
		return "●", colSuccess
	}
	return "•", colActive
}

func statusChip(label, value string, color lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(color).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Render(label + ":" + value)
}

func statusColor(status string) lipgloss.Color {
	switch strings.ToLower(status) {
	case "ok", "healthy":
		return colSuccess
	case "warning", "degraded":
		return colWarn
	case "error", "failed":
		return colError
	default:
		return colActive
	}
}

func pickColor(v bool, yes, no lipgloss.Color) lipgloss.Color {
	if v {
		return yes
	}
	return no
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func relativeUnix(ts int64) string {
	if ts <= 0 {
		return "never"
	}
	return relativeTime(time.Unix(ts, 0))
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func safeValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

func (m dashboardModel) renderLockScreen() string {
	// Center content
	content := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(colError).
		Padding(1, 3).
		Render(lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(colError).Bold(true).Render("ACCESS RESTRICTED"),
			"",
			"Security checks failed.",
			"Please verify BRIDGE_SECRET and API reachability.",
			"",
			renderSanityList(m.sanity),
		))

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content,
	)
}

func renderSanityList(s authSanity) string {
	var lines []string
	
	check := func(label string, ok bool, detail string) string {
		icon := "✓"
		color := colSuccess
		if !ok {
			icon = "✖"
			color = colError
		}
		row := fmt.Sprintf("%s %-20s", 
			lipgloss.NewStyle().Foreground(color).Render(icon),
			label,
		)
		if detail != "" {
			row += lipgloss.NewStyle().Foreground(colSub).Render(" (" + detail + ")")
		}
		return row
	}

	lines = append(lines, check("BRIDGE_SECRET", s.BridgeSecretConfigured, ""))
	lines = append(lines, check("API REACHABILITY", s.APIReachable, ""))
	
	for _, ep := range s.Endpoints {
		lines = append(lines, check("  "+ep.Name, ep.Reachable, ep.Detail))
	}
	
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func (m dashboardModel) selectedChannel() string {
	if len(m.channelNames) == 0 || m.selected < 0 || m.selected >= len(m.channelNames) {
		return ""
	}
	return m.channelNames[m.selected]
}

func scheduleTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func fetchSnapshotCmd(client *operatorClient) tea.Cmd {
	return func() tea.Msg {
		sanity := client.sanityCheck()
		if sanity.Blocking {
			return refreshResultMsg{
				sanity: sanity,
				at:     time.Now(),
			}
		}
		doctor, channelsData, err := client.fetchDoctorAndChannels()
		if err != nil {
			return refreshResultMsg{sanity: sanity, err: err, at: time.Now()}
		}
		inspector, err := client.fetchSessionInspector()
		if err != nil {
			return refreshResultMsg{sanity: sanity, err: err, at: time.Now()}
		}
		trace := tracePayload{Incidents: map[string]channelIncident{}}
		if inspector.LastRequestID != "" {
			trace, err = client.fetchTrace(inspector.LastRequestID)
			if err != nil {
				return refreshResultMsg{sanity: sanity, err: err, at: time.Now()}
			}
		}
		return refreshResultMsg{
			sanity:    sanity,
			doctor:    doctor,
			channels:  channelsData,
			inspector: inspector,
			trace:     trace,
			at:        time.Now(),
		}
	}
}

func reconnectCmd(client *operatorClient, channel string) tea.Cmd {
	return func() tea.Msg {
		err := client.reconnect(channel)
		return reconnectResultMsg{channel: channel, err: err}
	}
}

func runDashboard() {
	client := newOperatorClient()
	p := tea.NewProgram(initialModel(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

func newOperatorClient() *operatorClient {
	port := os.Getenv("GHOST_API_PORT")
	if strings.TrimSpace(port) == "" {
		port = "8766"
	}
	host := strings.TrimSpace(os.Getenv("GHOST_API_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s:%s", host, port)
	session := strings.TrimSpace(os.Getenv("GHOST_OPERATOR_SESSION"))
	if session == "" {
		session = "mobile:default"
	}
	chatID := strings.TrimSpace(os.Getenv("GHOST_OPERATOR_CHAT_ID"))
	if chatID == "" {
		chatID = "default"
	}
	return &operatorClient{
		baseURL: base,
		secret:  strings.TrimSpace(os.Getenv("BRIDGE_SECRET")),
		http: &http.Client{
			Timeout: 5 * time.Second,
		},
		session: session,
		chatID:  chatID,
	}
}

func (c *operatorClient) request(method, path string, body []byte, target interface{}) error {
	req, err := http.NewRequest(method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		req.Header.Set("X-Ghost-Secret", c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *operatorClient) requestStatus(method, path string) (int, error) {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	if c.secret != "" {
		req.Header.Set("X-Ghost-Secret", c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (c *operatorClient) sanityCheck() authSanity {
	s := authSanity{
		BridgeSecretConfigured: strings.TrimSpace(c.secret) != "",
		APIReachable:           false,
		Endpoints:              []endpointProbe{},
	}
	apiStatus, apiErr := c.requestStatus(http.MethodGet, "/v1/health")
	if apiErr == nil && apiStatus < 500 {
		s.APIReachable = true
	}
	probes := []struct {
		name string
		path string
	}{
		{name: "doctor", path: "/v1/doctor"},
		{name: "channels", path: "/v1/channels/status"},
		{name: "inspector", path: "/v1/session/inspect?session=mobile:default&channel=mobile&chat_id=default"},
		{name: "traces", path: "/v1/traces?request_id=probe"},
	}
	for _, p := range probes {
		status, err := c.requestStatus(http.MethodGet, p.path)
		probe := endpointProbe{Name: p.name, Path: p.path}
		if err != nil {
			probe.Detail = err.Error()
			probe.Reachable = false
		} else {
			probe.Status = status
			probe.Reachable = status != http.StatusNotFound && status < 500
			switch status {
			case http.StatusUnauthorized, http.StatusForbidden:
				probe.Detail = "auth required"
			default:
				probe.Detail = fmt.Sprintf("http %d", status)
			}
		}
		s.Endpoints = append(s.Endpoints, probe)
	}
	s.Blocking = !s.BridgeSecretConfigured || !s.APIReachable
	for _, p := range s.Endpoints {
		if !p.Reachable {
			s.Blocking = true
			break
		}
	}
	return s
}

func (c *operatorClient) fetchDoctorAndChannels() (doctorPayload, map[string]channelHealth, error) {
	var doctor doctorPayload
	if err := c.request(http.MethodGet, "/v1/doctor", nil, &doctor); err != nil {
		return doctor, nil, err
	}
	var channelsResp struct {
		Channels map[string]channelHealth `json:"channels"`
	}
	if err := c.request(http.MethodGet, "/v1/channels/status", nil, &channelsResp); err != nil {
		if doctor.Channels != nil {
			return doctor, doctor.Channels, nil
		}
		return doctor, nil, err
	}
	return doctor, channelsResp.Channels, nil
}

func (c *operatorClient) fetchSessionInspector() (sessionInspector, error) {
	var out sessionInspector
	session := url.QueryEscape(c.session)
	chatID := url.QueryEscape(c.chatID)
	path := fmt.Sprintf("/v1/session/inspect?session=%s&channel=mobile&chat_id=%s", session, chatID)
	err := c.request(http.MethodGet, path, nil, &out)
	return out, err
}

func (c *operatorClient) fetchTrace(requestID string) (tracePayload, error) {
	var out tracePayload
	path := fmt.Sprintf("/v1/traces?request_id=%s", url.QueryEscape(requestID))
	err := c.request(http.MethodGet, path, nil, &out)
	if out.Incidents == nil {
		out.Incidents = map[string]channelIncident{}
	}
	return out, err
}

func (c *operatorClient) reconnect(channel string) error {
	payload, _ := json.Marshal(map[string]string{"channel": channel})
	return c.request(http.MethodPost, "/v1/channels/reconnect", payload, nil)
}

func sortedKeys(data map[string]channelHealth) []string {
	out := make([]string, 0, len(data))
	for k := range data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedIncidentKeys(data map[string]channelIncident) []string {
	out := make([]string, 0, len(data))
	for k := range data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
