package skills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Calendar integration via gcalcli (retained as execution layer).
//
// Decision: Ghost keeps gcalcli as the Calendar execution layer rather than
// reimplementing Google OAuth + Calendar API directly. Reasons:
//   - gcalcli already handles OAuth device flow, token refresh, and agenda
//     parsing; a direct integration would require a Ghost-managed OAuth
//     client ID, public HTTPS callback, and token lifecycle — large surface
//     for a BYO-hardware product behind NAT with no public inbound.
//   - The Pi has no reliable public HTTPS endpoint for Google redirect URIs.
//     Google rejects plain-http LAN callbacks. gcalcli's --auth-device flow
//     (user visits google.com/device, enters code) works behind NAT with
//     zero inbound, matching Ghost's relay/LAN-only reality.
//   - Migration path stays open: if Ghost later ships a managed OAuth app +
//     relay callback, only this file + readiness change; skills/SKILL.md and
//     chat behavior are unchanged.
//
// Security: token lives in CalendarConfigDir (/var/lib/ghost/.calendar,
// 0600, outside workspace/config backup roots, never in chat/SSE/logs/
// backups). This file never returns token contents — only status and setup
// URLs.
//
// NOTE: the ghost services run with ProtectHome=true, so ~/.config and
// ~/.gcalcli_oauth are invisible/volatile to them. All Ghost-initiated
// gcalcli calls must pass --config-folder CalendarConfigDir; the calendar
// SKILL.md documents the same flag so model-driven agenda calls find it.

// CalendarStatus is the product-level state.
type CalendarStatus string

const (
	CalendarReady            CalendarStatus = "ready"
	CalendarNeedsSetup       CalendarStatus = "needs_setup"
	CalendarNeedsReauth      CalendarStatus = "needs_reauth"
	CalendarToolMissing      CalendarStatus = "tool_missing"
)

// CalendarState describes readiness for UI + chat.
type CalendarState struct {
	Status     CalendarStatus `json:"status"`
	Connected  bool           `json:"connected"`
	Message    string         `json:"message"`
	SetupURL   string         `json:"setup_url,omitempty"`
	NeedsSetup bool           `json:"needs_setup"`
}

// CalendarConfigDir is the persistent, service-writable home for gcalcli
// oauth/config. Deliberately outside the backup-walked roots (config, data,
// workspace) so tokens are never archived.
const CalendarConfigDir = "/var/lib/ghost/.calendar"

// CalendarTokenPaths returns candidate oauth token locations, newest first.
// Ghost-owned dir wins; legacy user-home paths are checked for users who
// connected before the service-owned dir existed (then migrate on next auth).
func CalendarTokenPaths() []string {
	paths := []string{CalendarConfigDir}
	if entries, err := os.ReadDir(CalendarConfigDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				paths = append(paths, filepath.Join(CalendarConfigDir, e.Name()))
			}
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		paths = append(paths,
			filepath.Join(home, ".gcalcli_oauth"),
			filepath.Join(home, ".config", "gcalcli", "oauth"),
		)
	}
	return paths
}

// CalendarConfigArgs returns the --config-folder flag slice for every
// Ghost-initiated gcalcli invocation.
func CalendarConfigArgs() []string {
	return []string{"--config-folder", CalendarConfigDir}
}

// CalendarCheck returns product state. Expired/revoked is detected when
// gcalcli exists but agenda fails with auth markers — callers can pass the
// last exec output to refine needs_reauth vs needs_setup.
func CalendarCheck() CalendarState {
	if _, err := exec.LookPath("gcalcli"); err != nil {
		return CalendarState{Status: CalendarToolMissing, Message: "Calendar tool isn't installed. Install gcalcli to connect Google Calendar.", NeedsSetup: true}
	}
	for _, p := range CalendarTokenPaths() {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		// Inside the service-owned config dir, only token-like files prove
		// auth (config.toml alone does not). Legacy home paths are token
		// files by definition.
		if p == CalendarConfigDir {
			continue
		}
		if dir := filepath.Dir(p); dir == CalendarConfigDir {
			base := strings.ToLower(filepath.Base(p))
			if !(strings.Contains(base, "oauth") || strings.Contains(base, "token") || strings.Contains(base, "credential") || strings.Contains(base, "auth")) {
				continue
			}
		}
		return CalendarState{Status: CalendarReady, Connected: true, Message: "Calendar is connected."}
	}
	return CalendarState{
		Status: CalendarNeedsSetup, NeedsSetup: true,
		Message: "Calendar access isn't connected yet. Connect your calendar in Ghost settings to view your schedule.",
	}
}

// CalendarAuthOutput classifies gcalcli exec output for expired/revoked.
func CalendarAuthOutput(output string) CalendarState {
	lower := strings.ToLower(output)
	for _, marker := range []string{"no oauth", "oauth", "invalid_grant", "revoked", "unauthorized", "authentication required", "reauth"} {
		if strings.Contains(lower, marker) {
			return CalendarState{
				Status: CalendarNeedsReauth, NeedsSetup: true,
				Message: "Calendar access expired or was revoked. Reconnect your calendar in Ghost settings to continue.",
			}
		}
	}
	return CalendarState{Status: CalendarReady, Connected: true, Message: "Calendar is connected."}
}

// CalendarDisconnect removes local oauth tokens (user disconnect flow).
func CalendarDisconnect() error {
	var lastErr error
	removed := false
	for _, p := range CalendarTokenPaths() {
		if err := os.Remove(p); err == nil {
			removed = true
		} else if !os.IsNotExist(err) {
			lastErr = err
		}
	}
	if !removed && lastErr != nil {
		return lastErr
	}
	return nil
}

// ErrCalendarToolMissing reports gcalcli is not on the service PATH.
// The ghost-web service runs as root with a minimal PATH; a user-local
// pip install (e.g. ~/.local/bin) is invisible to it. Callers must surface
// install guidance, not a generic 500.
var ErrCalendarToolMissing = fmt.Errorf("gcalcli not found on service PATH")

// CalendarDeviceFlow starts `gcalcli --config-folder DIR init
// --noauth_local_server` and returns the verification URL for the Web
// Console to display. It does NOT block waiting; the caller polls
// CalendarCheck until the token file appears. gcalcli 4.x has no
// `oauth --auth-device` subcommand — `init` is the auth entrypoint.
func CalendarDeviceFlow(timeout time.Duration) (string, error) {
	if _, err := exec.LookPath("gcalcli"); err != nil {
		return "", ErrCalendarToolMissing
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if err := os.MkdirAll(CalendarConfigDir, 0700); err != nil {
		return "", fmt.Errorf("calendar config dir: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// --noauth_local_server prints a URL to visit plus port-forward/manual
	// instructions for headless systems; no Pi inbound required.
	cmd := exec.CommandContext(ctx, "gcalcli", "--config-folder", CalendarConfigDir, "init", "--noauth_local_server")
	out, err := cmd.CombinedOutput()
	text := string(out)
	// Extract URL (best-effort, never fail hard — UI can fall back to manual).
	url := ""
	for _, line := range strings.Split(text, "\n") {
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "https://") && (strings.Contains(field, "google.com/device") || strings.Contains(field, "accounts.google")) {
				url = strings.Trim(field, ".,;)\"'")
				break
			}
		}
		if url != "" {
			break
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		// init waits on user input; timeout with a captured URL is normal.
		if url != "" {
			return url, nil
		}
		return "", fmt.Errorf("calendar setup timed out waiting for authorization")
	}
	if err != nil && url == "" {
		return "", fmt.Errorf("gcalcli init failed: %s", strings.TrimSpace(text))
	}
	return url, nil
}
