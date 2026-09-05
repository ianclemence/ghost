package skills

import (
	"strings"
	"time"
)

// VerificationPacket is the Google OAuth verification submission content,
// generated from the implementation so the deployer copies facts instead
// of writing claims. Secrets are never included: the packet carries the
// client ID and redirect URIs (public registration data), never the
// client secret or any token.
type VerificationPacket struct {
	GeneratedAt     string             `json:"generated_at"`
	ApplicationType string             `json:"application_type"`
	Scopes          []ScopeInfo        `json:"scopes"`
	RedirectURIs    []string           `json:"redirect_uris"`
	DataHandling    map[string]string  `json:"data_handling"`
	Checklist       []VerificationItem `json:"checklist"`
}

// VerificationPacketFor builds the submission packet for the deployment's
// redirect URIs (relay and/or LAN-direct). Empty redirect list yields a
// packet the deployer must complete — never fabricated URIs.
func VerificationPacketFor(redirectURIs []string) VerificationPacket {
	var cleaned []string
	for _, u := range redirectURIs {
		if s := strings.TrimSpace(u); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return VerificationPacket{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		ApplicationType: "Web application (OAuth 2.0 web-server flow with offline access)",
		Scopes:          CalendarScopeJustification(),
		RedirectURIs:    cleaned,
		DataHandling: map[string]string{
			"token_storage":  "Refresh tokens are stored server-side only, in a 0600 file outside backup roots; access tokens are held in memory.",
			"token_logging":  "Tokens, codes, and secrets are never written to logs, chat, SSE, diagnostics, or backups (structurally redacted).",
			"backup_restore": "Credentials are excluded from backups; restore requires reconnecting the calendar (documented product behavior).",
			"scope_minimum":  "Read-only scope by default; events scope only when the user invokes the event-writing capability.",
			"revocation":     "Revoked/expired credentials are detected and reported as reconnection prompts, never silent failures.",
			"multi_device":   "Tokens belong to the owner's Ghost appliance, not to individual devices.",
		},
		Checklist: CalendarVerificationChecklist(),
	}
}
