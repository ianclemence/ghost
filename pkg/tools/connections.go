package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/skills"
)

// ConnectionsTool reports which external apps Ghost can reach and the exact
// next step to finish setup. Read-only: it never returns, stores, or asks for
// secrets, and it never sends them through chat. The model uses it to answer
// "is my Gmail connected?" and to walk the owner through setup on web, phone,
// or terminal.
type ConnectionsTool struct{}

func NewConnectionsTool() *ConnectionsTool { return &ConnectionsTool{} }

func (t *ConnectionsTool) Name() string { return "connections" }

func (t *ConnectionsTool) Description() string {
	return "Check which external apps are connected (Gmail, Google Calendar, Outlook, Spotify, Home Assistant, GitHub, Notion, weather, flights) and get the exact next step to connect one. Use when the user asks to connect or check an app, or whether something is set up. Never returns secrets and never asks the user to paste a secret into chat — the owner pastes it once in the secure screen or terminal prompt."
}

func (t *ConnectionsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "status", "begin"},
				"description": "list every app with status, status for one app, or begin for setup steps",
			},
			"app": map[string]interface{}{
				"type":        "string",
				"description": "App id or name, e.g. gmail, notion, home-assistant, home assistant",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ConnectionsTool) Timeout() time.Duration { return 5 * time.Second }

type connEntry struct {
	id        string
	name      string
	auth      string
	setup     string
	connected bool
	help      string
}

// connectionEntries is the single snapshot of every first-party app and its
// live readiness. Status comes from the vault/account checks that already
// gate the real tools, so this list can never disagree with them.
func connectionEntries() []connEntry {
	var out []connEntry
	for _, c := range connectedapp.FirstParty() {
		e := connEntry{
			id:    c.ID,
			name:  c.DisplayName,
			auth:  string(c.AuthKind),
			setup: string(c.Setup),
			help:  c.Help,
		}
		switch c.ID {
		case "google-calendar":
			e.connected = skills.CalendarCheck().Connected
		case "gmail":
			e.connected = skills.GmailWebStatus().Connected
		case "outlook":
			e.connected = skills.OutlookWebStatus().Connected
		case "spotify":
			e.connected = skills.SpotifyWebStatus().Connected
		case "home-assistant":
			e.connected = credentials.HassConfigured()
		case "github":
			e.connected = credentials.GithubConfigured()
		case "notion":
			e.connected = credentials.NotionKey() != ""
		default:
			e.connected = credentials.ProviderKey(c.Provider) != ""
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// setupSteps returns plain-language next steps with no secret material. The
// path differs by how the app authenticates: browser sign-in, pasted key,
// or a URL+token pair. Secrets always go into a secure screen or a terminal
// prompt — never through the model.
func setupSteps(e connEntry) string {
	switch e.setup {
	case string(connectedapp.SetupConsoleOAuth):
		return fmt.Sprintf("Sign-in needed. Open Ghost's web console or phone app → Apps → %s → Configure. A sign-in page opens and Ghost finishes on its own. You can't complete this one from a terminal.", e.name)
	case string(connectedapp.SetupPastePair):
		return fmt.Sprintf("Open Ghost's web console or phone app → Apps → %s → Configure and paste the instance URL plus its long-lived token. Stored encrypted on your device.", e.name)
	default:
		return fmt.Sprintf("Open Ghost's web console or phone app → Apps → %s → Configure and paste the key once. Stored encrypted on your device. If you prefer the terminal, Ghost can run the sign-in flow with you there.", e.name)
	}
}

func (t *ConnectionsTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action := strings.ToLower(strings.TrimSpace(sarg(args, "action")))
	query := strings.ToLower(strings.TrimSpace(sarg(args, "app")))
	entries := connectionEntries()

	find := func() (connEntry, bool) {
		if query == "" {
			return connEntry{}, false
		}
		norm := strings.NewReplacer("-", " ", "_", " ").Replace(query)
		for _, e := range entries {
			if strings.ToLower(e.id) == query || strings.ToLower(e.name) == query {
				return e, true
			}
			if strings.Contains(strings.ToLower(e.name), norm) || strings.Contains(strings.ToLower(e.id), norm) {
				return e, true
			}
		}
		return connEntry{}, false
	}

	switch action {
	case "", "list":
		var sb strings.Builder
		sb.WriteString("Connected apps (what you can ask Ghost to use):\n")
		for _, e := range entries {
			state := "not connected"
			if e.connected {
				state = "connected"
			}
			fmt.Fprintf(&sb, "- %s — %s\n", e.name, state)
		}
		sb.WriteString("\nAsk about one app to get its setup steps.")
		return NewToolResult(strings.TrimSpace(sb.String()))

	case "status", "begin":
		e, ok := find()
		if !ok {
			return ErrorResult(fmt.Sprintf("I don't know an app called %q. Connected apps are: %s.", sarg(args, "app"), entryNames(entries)))
		}
		if e.connected {
			if action == "begin" {
				return NewToolResult(fmt.Sprintf("%s is already connected — you can just ask me to use it. Nothing to set up.", e.name))
			}
			return NewToolResult(fmt.Sprintf("%s is connected. %s", e.name, e.help))
		}
		return NewToolResult(fmt.Sprintf("%s is not connected yet. %s", e.name, setupSteps(e)))

	default:
		return ErrorResult("Use action \"list\", \"status\", or \"begin\".")
	}
}

func entryNames(entries []connEntry) string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	return strings.Join(names, ", ")
}
