package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
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
	agentLoop *agent.AgentLoop
	logs      []string
	status    string
	width     int
	height    int
}

func initialModel(agentLoop *agent.AgentLoop) dashboardModel {
	return dashboardModel{
		agentLoop: agentLoop,
		logs:      []string{},
		status:    "Active",
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return "tick"
	})
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case string:
		if msg == "tick" {
			// Refresh logs or status
			return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return "tick"
			})
		}
	}
	return m, nil
}

func (m dashboardModel) View() string {
	doc := strings.Builder{}

	doc.WriteString(fmt.Sprintf("Ghost Dashboard v%s\n\n", version))
	doc.WriteString(fmt.Sprintf("Status: %s\n", m.status))
	doc.WriteString(fmt.Sprintf("Active Channel: %s\n", "Telegram")) // Placeholder
	doc.WriteString(fmt.Sprintf("Memory Usage: %s\n", "Low"))        // Placeholder

	doc.WriteString("\nLogs:\n")
	for _, log := range m.logs {
		doc.WriteString(fmt.Sprintf("- %s\n", log))
	}

	doc.WriteString("\nPress 'q' to quit")

	return docStyle.Render(doc.String())
}

func runDashboard() {
	// Initialize minimal agent context for monitoring
	cfg, _ := config.LoadConfig(getConfigPath())
	msgBus := bus.NewMessageBus()
	provider, _ := providers.CreateProvider(cfg)
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	p := tea.NewProgram(initialModel(agentLoop), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
