package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Connection-class errors must name the fix (terminal-ui skill:
// ux-error-messages), and the footer line must always fit the width.
func TestDashboardFooterActionableAndFits(t *testing.T) {
	if !isConnRefused("dial tcp 127.0.0.1:8766: connect: connection refused") {
		t.Errorf("connection refused must classify as gateway-down")
	}
	if isConnRefused("rate limit exceeded") {
		t.Errorf("rate limits must not classify as gateway-down")
	}
	for _, w := range []int{80, 100, 160} {
		m := dashboardModel{width: w, height: 30, lastError: "dial tcp 127.0.0.1:8766: connect: connection refused " + strings.Repeat("x", 200)}
		for _, line := range strings.Split(m.renderFooter(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("width %d: footer overflows (%d): %q", w, got, line)
			}
		}
		if !strings.Contains(m.renderFooter(), "ghost serve") {
			t.Errorf("long errors should still hint the remedy at width %d", w)
		}
	}
}

// The transmitting indicator must appear while a chat round-trip is in
// flight and clear on response (terminal-ui skill: ux-progress-indicators).
func TestDashboardChatPendingIndicator(t *testing.T) {
	m := initialModel(nil)
	m.chatPending = true
	m.updateChatViewport()
	if !strings.Contains(m.chatLog.View(), "transmitting") {
		t.Errorf("pending chat must show a transmitting row")
	}
	m.chatPending = false
	m.updateChatViewport()
	if strings.Contains(m.chatLog.View(), "transmitting") {
		t.Errorf("resolved chat must clear the transmitting row")
	}
}
