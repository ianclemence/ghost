package doctor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// Connected-app OAuth deployment checks (Gmail, Outlook, Spotify).
// Same shape as checkCalendarOAuth: validate env + redirect URL + DNS,
// suppressed when the owning skill is disabled. Only warn — OAuth is
// optional per app — so missing config is "warning", malformed is "error".

func (d *Doctor) skillActive(name string) bool {
	if d == nil || d.workspace == "" {
		return true // unknown: don't suppress
	}
	fi, err := os.Stat(filepath.Join(d.workspace, "skills", name, "SKILL.md"))
	if err != nil || fi.IsDir() {
		return false
	}
	return true
}

func oauthDeploymentCheck(d *Doctor, name, label, skill, clientID, secret, redirect, suffix string) CheckResult {
	start := time.Now()
	active := true
	if d != nil {
		active = d.skillActive(skill)
	}
	if !active {
		return CheckResult{
			Name: name, Label: label,
			Status:  "ok",
			Message: "Skill is disabled; no sign-in configuration is needed.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	clientID = strings.TrimSpace(clientID)
	secret = strings.TrimSpace(secret)
	redirect = strings.TrimSpace(redirect)
	if clientID == "" || secret == "" || redirect == "" {
		return CheckResult{
			Name: name, Label: label,
			Status:  "warning",
			Message: "One-click sign-in isn't configured yet (needs an OAuth client ID, secret, and redirect URL).",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	u, err := url.Parse(redirect)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return CheckResult{
			Name: name, Label: label,
			Status:  "error",
			Message: "The redirect URL must be an absolute http(s) URL.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if u.Scheme != "https" {
		return CheckResult{
			Name: name, Label: label,
			Status:  "warning",
			Message: "The redirect URL isn't HTTPS — fine for LAN testing, but providers require HTTPS for production.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	resolver := net.Resolver{}
	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := resolver.LookupHost(rctx, u.Hostname()); err != nil {
		return CheckResult{
			Name: name, Label: label,
			Status:  "error",
			Message: "The redirect host doesn't resolve. Register a reachable relay or LAN address with the provider.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if !strings.HasSuffix(u.Path, suffix) {
		return CheckResult{
			Name: name, Label: label,
			Status:  "warning",
			Message: "The redirect URL should end with " + suffix + " to match Ghost's handler.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	return CheckResult{
		Name: name, Label: label,
		Status:  "ok",
		Message: "Sign-in is configured and the redirect host resolves.",
		Latency: time.Since(start).Milliseconds(),
	}
}

// checkConnectedServices collapses the six per-service setup rows (calendar,
// gmail, outlook, spotify, github, notion) into one diagnostics row.
// Diagnostics is health, not inventory: owners don't need five warnings to
// learn three integrations are unconnected. A disabled skill contributes
// nothing — same suppression the aggregate previously applied by dropping
// rows, now by excluding from counts. Returns info when no service skill is
// enabled, which RunAll omits. Individual checks stay truthful for
// direct/programmatic callers.
func (d *Doctor) checkConnectedServices(ctx context.Context) CheckResult {
	start := time.Now()
	done := func(status, msg string) CheckResult {
		return CheckResult{Name: "connected_services", Label: "Connected services", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
	}
	services := []struct {
		label string
		skill string
		check func(context.Context) CheckResult
	}{
		{"Calendar", "calendar", d.checkCalendarOAuth},
		{"Gmail", "email", d.checkGmailOAuth},
		{"Outlook", "email", d.checkOutlookOAuth},
		{"Spotify", "spotify", d.checkSpotifyOAuth},
		{"GitHub", "github", d.checkGithubToken},
		{"Notion", "notion", d.checkNotionToken},
	}
	var ready, missing []string
	for _, s := range services {
		if d != nil && !d.skillActive(s.skill) {
			continue
		}
		r := s.check(ctx)
		if r.Status == "ok" {
			ready = append(ready, s.label)
			continue
		}
		if r.Status == "error" {
			return done("error", s.label+": "+r.Message)
		}
		missing = append(missing, s.label)
	}
	if len(ready)+len(missing) == 0 {
		return done("info", "no connected-service skills enabled")
	}
	if len(missing) == 0 {
		return done("ok", fmt.Sprintf("%d connected: %s", len(ready), strings.Join(ready, ", ")))
	}
	if len(ready) == 0 {
		return done("warning", fmt.Sprintf("none connected — %s need setup (see Connected Apps)",
			strings.Join(missing, ", ")))
	}
	return done("warning", fmt.Sprintf("%d of %d connected — %s ready; %s need setup (see Connected Apps)",
		len(ready), len(ready)+len(missing),
		strings.Join(ready, ", "), strings.Join(missing, ", ")))
}

// checkGmailOAuth validates the shared Google OAuth client + Gmail callback.
func (d *Doctor) checkGmailOAuth(ctx context.Context) CheckResult {
	_ = ctx
	return oauthDeploymentCheck(d, "gmail_oauth", "Gmail sign-in", "email",
		os.Getenv("GHOST_GOOGLE_CLIENT_ID"), os.Getenv("GHOST_GOOGLE_CLIENT_SECRET"),
		os.Getenv("GHOST_GMAIL_REDIRECT_URL"), "/oauth/gmail/callback")
}

// checkOutlookOAuth validates the Microsoft app registration.
func (d *Doctor) checkOutlookOAuth(ctx context.Context) CheckResult {
	_ = ctx
	return oauthDeploymentCheck(d, "outlook_oauth", "Outlook sign-in", "email",
		os.Getenv("GHOST_OUTLOOK_CLIENT_ID"), os.Getenv("GHOST_OUTLOOK_CLIENT_SECRET"),
		os.Getenv("GHOST_OUTLOOK_REDIRECT_URL"), "/oauth/outlook/callback")
}

// checkSpotifyOAuth validates the Spotify app registration.
func (d *Doctor) checkSpotifyOAuth(ctx context.Context) CheckResult {
	_ = ctx
	return oauthDeploymentCheck(d, "spotify_oauth", "Spotify sign-in", "spotify",
		os.Getenv("GHOST_SPOTIFY_CLIENT_ID"), os.Getenv("GHOST_SPOTIFY_CLIENT_SECRET"),
		os.Getenv("GHOST_SPOTIFY_REDIRECT_URL"), "/oauth/spotify/callback")
}

// checkGithubToken validates the GitHub PAT presence (paste-key flow).
func (d *Doctor) checkGithubToken(ctx context.Context) CheckResult {
	_ = ctx
	return tokenPresenceCheck(d, "github_token", "GitHub token", "github", credentials.GithubConfigured())
}

// checkNotionToken validates the Notion integration token presence.
func (d *Doctor) checkNotionToken(ctx context.Context) CheckResult {
	_ = ctx
	return tokenPresenceCheck(d, "notion_token", "Notion token", "notion", credentials.NotionConfigured())
}

// tokenPresenceCheck validates paste-key connected apps (GitHub PAT, Notion
// token). Same suppression shape as OAuth: skipped when owning skill is
// disabled, warning when unconfigured, ok when a token is present. Tokens
// themselves are never echoed.
func tokenPresenceCheck(d *Doctor, name, label, skill string, present bool) CheckResult {
	start := time.Now()
	active := true
	if d != nil {
		active = d.skillActive(skill)
	}
	if !active {
		return CheckResult{
			Name: name, Label: label,
			Status:  "ok",
			Message: "Skill is disabled; no token is needed.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if !present {
		return CheckResult{
			Name: name, Label: label,
			Status:  "warning",
			Message: "Not connected yet (paste a read-only token in Connected Apps).",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	return CheckResult{
		Name: name, Label: label,
		Status:  "ok",
		Message: "Token is configured.",
		Latency: time.Since(start).Milliseconds(),
	}
}
