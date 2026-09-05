package doctor

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// checkCalendarOAuth validates the deployment's Google OAuth client
// configuration before the deployer opens the Google Cloud Console.
// Everything checkable in code is checked here; the remaining Console
// clicks are listed (not performed) in the message.
func (d *Doctor) checkCalendarOAuth(ctx context.Context) CheckResult {
	start := time.Now()
	clientID := strings.TrimSpace(os.Getenv("GHOST_GOOGLE_CLIENT_ID"))
	secret := strings.TrimSpace(os.Getenv("GHOST_GOOGLE_CLIENT_SECRET"))
	redirect := strings.TrimSpace(os.Getenv("GHOST_CALENDAR_REDIRECT_URL"))

	if clientID == "" || secret == "" || redirect == "" {
		return CheckResult{
			Name: "calendar_oauth", Label: "Calendar sign-in",
			Status:  "warning",
			Message: "Calendar one-click sign-in isn't configured yet (needs a Google OAuth client ID, secret, and redirect URL). Device-flow setup still works; see the verification checklist for production.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if !strings.HasSuffix(clientID, ".apps.googleusercontent.com") {
		return CheckResult{
			Name: "calendar_oauth", Label: "Calendar sign-in",
			Status:  "error",
			Message: "The Google client ID doesn't look right (it should end with .apps.googleusercontent.com).",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	u, err := url.Parse(redirect)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return CheckResult{
			Name: "calendar_oauth", Label: "Calendar sign-in",
			Status:  "error",
			Message: "The calendar redirect URL must be an absolute http(s) URL.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if u.Scheme != "https" {
		return CheckResult{
			Name: "calendar_oauth", Label: "Calendar sign-in",
			Status:  "warning",
			Message: "The calendar redirect URL isn't HTTPS — fine for LAN testing, but Google requires HTTPS for production verification.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	// DNS must resolve for the registered host; dial is bounded and fast.
	host := u.Hostname()
	resolver := net.Resolver{}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := resolver.LookupHost(rctx, host); err != nil {
		return CheckResult{
			Name: "calendar_oauth", Label: "Calendar sign-in",
			Status:  "error",
			Message: "The redirect host doesn't resolve. Register a reachable relay or LAN address in the Google Console.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if !strings.HasSuffix(u.Path, "/oauth/calendar/callback") {
		return CheckResult{
			Name: "calendar_oauth", Label: "Calendar sign-in",
			Status:  "warning",
			Message: "The redirect URL should end with /oauth/calendar/callback to match Ghost's handler.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	return CheckResult{
		Name: "calendar_oauth", Label: "Calendar sign-in",
		Status:  "ok",
		Message: "Calendar sign-in is configured and the redirect host resolves.",
		Latency: time.Since(start).Milliseconds(),
	}
}
