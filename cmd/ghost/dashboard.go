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

type dashboardModel struct {
	client       *operatorClient
	width        int
	height       int
	lastUpdated  time.Time
	lastError    string
	actionStatus string
	doctor       doctorPayload
	channels     map[string]channelHealth
	channelNames []string
	selected     int
	inspector    sessionInspector
	trace        tracePayload
	sanity       authSanity
	programStart time.Time
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
		actionStatus: "Press TAB to change channel • R reconnect • D refresh • Q quit",
		channels:     map[string]channelHealth{},
		programStart: time.Now(),
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
		case "tab":
			if len(m.channelNames) > 0 {
				m.selected = (m.selected + 1) % len(m.channelNames)
				m.actionStatus = fmt.Sprintf("Selected channel: %s", m.selectedChannel())
			}
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
		return m, tea.Batch(fetchSnapshotCmd(m.client), scheduleTick())
	case refreshResultMsg:
		m.sanity = msg.sanity
		if msg.err != nil {
			m.lastError = msg.err.Error()
		} else {
			m.lastError = ""
			m.lastUpdated = msg.at
			m.doctor = msg.doctor
			m.channels = msg.channels
			m.inspector = msg.inspector
			m.trace = msg.trace
			m.channelNames = sortedKeys(msg.channels)
			if len(m.channelNames) > 0 && m.selected >= len(m.channelNames) {
				m.selected = 0
			}
		}
	case reconnectResultMsg:
		if msg.err != nil {
			m.actionStatus = fmt.Sprintf("Reconnect failed for %s: %v", msg.channel, msg.err)
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

	// Layout Dimensions
	headerHeight := 3
	footerHeight := 3
	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 10 {
		contentHeight = 10 // Min height
	}

	leftWidth := int(float64(m.width) * 0.4)
	rightWidth := m.width - leftWidth - 2 // -2 for borders/padding

	// 1. Header
	header := m.renderHeader()

	// 2. Left Column (Channels, Doctor, Controls)
	channelsPanel := m.renderChannels(leftWidth)
	doctorPanel := m.renderDoctor(leftWidth)
	
	leftCol := lipgloss.JoinVertical(lipgloss.Left,
		channelsPanel,
		doctorPanel,
	)

	// 3. Right Column (Trace Log)
	tracePanel := m.renderTrace(rightWidth, contentHeight)

	// 4. Footer (Status Bar)
	footer := m.renderFooter()

	// Combine Content
	content := lipgloss.JoinHorizontal(lipgloss.Top,
		leftCol,
		tracePanel,
	)

	// Full Page
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		footer,
	)
}

func (m dashboardModel) renderHeader() string {
	uptime := time.Since(m.programStart).Round(time.Second)
	
	ascii := lipgloss.NewStyle().Foreground(colSuccess).Render(ghostASCII)
	logo := titleStyle.Render("GHOST SYSTEM MONITOR")
	stats := lipgloss.NewStyle().Foreground(colSub).Render(fmt.Sprintf("UPTIME: %s  |  API: %s", uptime, m.client.baseURL))
	ver := lipgloss.NewStyle().Foreground(colActive).Render("v" + version)

	// Spacer
	w := m.width - lipgloss.Width(ascii) - lipgloss.Width(logo) - lipgloss.Width(stats) - lipgloss.Width(ver) - 6
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
		spacer,
		ver,
	)
	
	return panelStyle.
		Width(m.width - 2).
		Height(1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder).
		Render(bar)
}

func (m dashboardModel) renderChannels(width int) string {
	var rows []string
	
	// Title
	rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("CHANNEL MATRIX"))
	rows = append(rows, "")

	if len(m.channelNames) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("No channels detected"))
	}

	for i, name := range m.channelNames {
		ch := m.channels[name]
		
		// Styles
		borderColor := colBorder
		statusColor := colSub
		statusIcon := "●"
		statusText := "OFFLINE"

		if i == m.selected {
			borderColor = colActive
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

		// Box Content
		nameLine := lipgloss.NewStyle().Foreground(colText).Bold(true).Render(strings.ToUpper(name))
		statusLine := lipgloss.NewStyle().Foreground(statusColor).Render(fmt.Sprintf("%s %s", statusIcon, statusText))
		
		statLine := lipgloss.NewStyle().Foreground(colSub).Render(fmt.Sprintf("Fails: %d", ch.FailureCount))
		if ch.LastSendErr != "" {
			statLine = lipgloss.NewStyle().Foreground(colError).Render("Last Err: " + truncate(ch.LastSendErr, 20))
		}

		// Render Box
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1).
			Width(width - 4). // Adjust for padding
			Render(
				lipgloss.JoinVertical(lipgloss.Left,
					lipgloss.JoinHorizontal(lipgloss.Top, nameLine, strings.Repeat(" ", width-4-lipgloss.Width(nameLine)-lipgloss.Width(statusLine)), statusLine),
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
	title := lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("LIVE TRACES // " + m.inspector.LastRequestID)
	
	var lines []string
	if len(m.trace.Events) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colSub).Render("Waiting for events..."))
	} else {
		for _, evt := range m.trace.Events {
			ts := time.Unix(evt.At, 0).Format("15:04:05.000")
			
			stateColor := colActive
			switch evt.State {
			case "error", "failed":
				stateColor = colError
			case "sent", "delivered":
				stateColor = colSuccess
			}

			row := fmt.Sprintf("%s  %s", 
				lipgloss.NewStyle().Foreground(colSub).Render(ts),
				lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(fmt.Sprintf("%-12s", strings.ToUpper(evt.State))),
			)
			
			if evt.Channel != "" {
				row += lipgloss.NewStyle().Foreground(colText).Render(" [" + evt.Channel + "]")
			}
			if evt.Detail != "" {
				detail := evt.Detail
				if len(detail) > width - 40 {
					detail = detail[:width-40] + "..."
				}
				row += lipgloss.NewStyle().Foreground(colSub).Render(" " + detail)
			}
			lines = append(lines, row)
		}
	}

	// Pad with empty lines to keep height stable
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	
	return panelStyle.
		Width(width).
		Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, "", content))
}

func (m dashboardModel) renderDoctor(width int) string {
	title := lipgloss.NewStyle().Foreground(colSub).Bold(true).Render("SYSTEM DIAGNOSTICS")
	
	var rows []string
	rows = append(rows, title, "")
	
	// Session Info
	rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("Session Target:"))
	rows = append(rows, lipgloss.NewStyle().Foreground(colText).Render(m.inspector.DeliveryTarget))
	rows = append(rows, "")

	// Doctor Checks
	if len(m.doctor.Checks) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colSub).Render("No checks run"))
	} else {
		for _, check := range m.doctor.Checks {
			icon := "✓"
			color := colSuccess
			if check.Status != "ok" && check.Status != "healthy" {
				icon = "!"
				color = colWarn
				if check.Status == "error" {
					color = colError
				}
			}
			
			line := fmt.Sprintf("%s %s", 
				lipgloss.NewStyle().Foreground(color).Render(icon),
				lipgloss.NewStyle().Foreground(colText).Render(check.Name),
			)
			rows = append(rows, line)
		}
	}

	return panelStyle.
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m dashboardModel) renderFooter() string {
	help := m.actionStatus
	if m.lastError != "" {
		help = "ERROR: " + m.lastError
	}
	
	return lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 1).
		Foreground(colSub).
		Background(colBg).
		Render(help)
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
