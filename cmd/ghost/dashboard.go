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

var (
	subtle    = lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}
	highlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	special   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}

	listStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(subtle).
			MarginRight(2).
			Height(8).
			Width(30)

	listHeader = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(subtle).
			MarginRight(2).
			Render

	listItem = lipgloss.NewStyle().PaddingLeft(2).Render

	checkMark = lipgloss.NewStyle().SetString("✓").
			Foreground(special).
			PaddingRight(1).
			String()

	listDone = func(s string) string {
		return checkMark + lipgloss.NewStyle().
			Strikethrough(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#969B86", Dark: "#696969"}).
			Render(s)
	}

	docStyle = lipgloss.NewStyle().Padding(1, 2, 1, 2)
)

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
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Ghost Operator TUI v%s\n", version))
	if !m.lastUpdated.IsZero() {
		b.WriteString(fmt.Sprintf("Updated: %s\n", m.lastUpdated.Format("15:04:05")))
	}
	if m.lastError != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", m.lastError))
	}
	b.WriteString("\nOperator Auth Sanity\n")
	b.WriteString(fmt.Sprintf("  BRIDGE_SECRET configured: %v\n", m.sanity.BridgeSecretConfigured))
	b.WriteString(fmt.Sprintf("  API reachable: %v\n", m.sanity.APIReachable))
	for _, ep := range m.sanity.Endpoints {
		b.WriteString(fmt.Sprintf("  %-16s reachable=%-5v", ep.Name, ep.Reachable))
		if ep.Status > 0 {
			b.WriteString(fmt.Sprintf(" status=%d", ep.Status))
		}
		if ep.Detail != "" {
			b.WriteString(" " + ep.Detail)
		}
		b.WriteString("\n")
	}
	if m.sanity.Blocking {
		b.WriteString("\nLive sections paused until sanity checks pass.\n")
		b.WriteString("\n" + m.actionStatus)
		return docStyle.Render(b.String())
	}
	b.WriteString(fmt.Sprintf("Doctor: %s  Session: %s  Target: %s\n\n", strings.ToUpper(m.doctor.Status), m.inspector.RequestedSession, m.inspector.DeliveryTarget))

	b.WriteString("Channels\n")
	for idx, name := range m.channelNames {
		ch := m.channels[name]
		prefix := "  "
		if idx == m.selected {
			prefix = "› "
		}
		health := "down"
		if ch.Running {
			health = "running"
		}
		b.WriteString(fmt.Sprintf("%s%-10s enabled=%-3v running=%-3v failures=%d\n", prefix, name, ch.Enabled, ch.Running, ch.FailureCount))
		if ch.FatalReason != "" {
			b.WriteString(fmt.Sprintf("   fatal: %s\n", ch.FatalReason))
		}
		if ch.LastSendErr != "" {
			b.WriteString(fmt.Sprintf("   last error: %s\n", ch.LastSendErr))
		}
		if health == "running" && ch.LastSuccess > 0 {
			b.WriteString(fmt.Sprintf("   last success: %s\n", time.Unix(ch.LastSuccess, 0).Format("15:04:05")))
		}
	}

	b.WriteString("\nDelivery Trace\n")
	if len(m.trace.Events) == 0 {
		b.WriteString("  no events\n")
	} else {
		for _, evt := range m.trace.Events {
			b.WriteString(fmt.Sprintf("  %s  %s", time.Unix(evt.At, 0).Format("15:04:05"), evt.State))
			if evt.Channel != "" {
				b.WriteString("  " + evt.Channel)
			}
			if evt.Detail != "" {
				b.WriteString("  " + evt.Detail)
			}
			b.WriteString("\n")
		}
	}
	if len(m.trace.Incidents) > 0 {
		b.WriteString("\nIncidents\n")
		incs := sortedIncidentKeys(m.trace.Incidents)
		for _, name := range incs {
			inc := m.trace.Incidents[name]
			b.WriteString(fmt.Sprintf("  %s failures=%d last=%s at=%s\n", name, inc.FailureCount, inc.LastError, time.Unix(inc.LastAt, 0).Format("15:04:05")))
		}
	}

	b.WriteString("\nDoctor Checks\n")
	if len(m.doctor.Checks) == 0 {
		b.WriteString("  no checks\n")
	} else {
		for _, check := range m.doctor.Checks {
			b.WriteString(fmt.Sprintf("  %-8s %-28s %s\n", check.Status, check.Name, check.Message))
		}
	}
	b.WriteString("\n" + m.actionStatus)
	return docStyle.Render(b.String())
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
