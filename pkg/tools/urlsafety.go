package tools

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

type URLSafetyConfig struct {
	AllowPrivateURLs bool
}

type URLSafety struct {
	config URLSafetyConfig
}

func NewURLSafety(config URLSafetyConfig) *URLSafety {
	return &URLSafety{config: config}
}

func (u *URLSafety) IsSafe(rawURL string) (bool, string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false, "invalid URL"
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false, "only http/https URLs are allowed"
	}

	host := parsed.Hostname()
	if host == "" {
		return false, "no hostname"
	}

	hostname := strings.ToLower(host)

	if u.isCloudMetadataEndpoint(hostname) {
		return false, "cloud metadata endpoint blocked"
	}

	if hostname == "metadata.google.internal" || hostname == "metadata.goog" {
		return false, "cloud metadata endpoint blocked"
	}

	if !u.config.AllowPrivateURLs && u.isReservedHost(hostname) && !fixtureAllowed(hostname, parsed.Port()) {
		return false, "reserved hostname blocked"
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return false, "DNS resolution failed"
	}

	if len(ips) == 0 {
		return false, "no IP addresses resolved"
	}

	fixture := fixtureAllowed(hostname, parsed.Port())
	for _, ip := range ips {
		safe, reason := u.isSafeIP(ip)
		if !safe {
			if fixture && fixtureSkippable(reason) {
				continue
			}
			return false, reason
		}
	}

	return true, ""
}

// fixtureAllowed reports whether an exact host:port is allowlisted via
// GHOST_FIXTURE_ALLOW ("host:port,host:port"). Set ONLY by the golden
// harness when it spawns a local fixture server, so model-driven browser
// cases can drive a deterministic local page through the REAL guard.
// Production never sets it: default deny is unchanged, and cloud metadata
// endpoints stay blocked regardless (checked before this path).
func fixtureAllowed(host, port string) bool {
	allow := strings.TrimSpace(os.Getenv("GHOST_FIXTURE_ALLOW"))
	if allow == "" || port == "" {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(host))
	for _, entry := range strings.Split(allow, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == host+":"+port {
			return true
		}
	}
	return false
}

// fixtureSkippable lists the rejections a fixture endpoint may bypass:
// loopback/private topology only. Metadata, multicast, and unspecified
// stay blocked even for fixtures.
func fixtureSkippable(reason string) bool {
	switch reason {
	case "loopback address blocked",
		"private address blocked",
		"link-local address blocked",
		"CGNAT address blocked",
		"benchmark address blocked":
		return true
	default:
		return false
	}
}

func (u *URLSafety) isCloudMetadataEndpoint(hostname string) bool {
	metadataHosts := []string{
		"169.254.169.254",
		"169.254.170.2",
		"169.254.169.253",
		"100.100.100.200",
		"fd00:ec2::254",
	}
	for _, h := range metadataHosts {
		if hostname == h {
			return true
		}
	}
	return false
}

func (u *URLSafety) isReservedHost(hostname string) bool {
	reserved := []string{
		"localhost",
		"0.0.0.0",
		"[::1]",
		"127.0.0.1",
		"::1",
	}
	for _, r := range reserved {
		if hostname == r {
			return true
		}
	}
	return false
}

func (u *URLSafety) isSafeIP(ip net.IP) (bool, string) {
	if u.config.AllowPrivateURLs {
		return true, ""
	}

	if ip.IsLoopback() {
		return false, "loopback address blocked"
	}

	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false, "link-local address blocked"
	}

	if ip.IsMulticast() {
		return false, "multicast address blocked"
	}

	if ip.IsUnspecified() {
		return false, "unspecified address blocked"
	}

	if ip.IsPrivate() {
		return false, "private address blocked"
	}

	if ip4 := ip.To4(); ip4 != nil {
		if u.isCGNAT(ip4) {
			return false, "CGNAT address blocked"
		}
		if u.isBenchmark(ip4) {
			return false, "benchmark address blocked"
		}
	}

	return true, ""
}

func (u *URLSafety) isCGNAT(ip net.IP) bool {
	if len(ip) != 4 {
		return false
	}
	return ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127
}

func (u *URLSafety) isBenchmark(ip net.IP) bool {
	if len(ip) != 4 {
		return false
	}
	return ip[0] == 198 && (ip[1] == 18 || ip[1] == 19)
}

func DetectSecretsInURL(rawURL string) []string {
	var secrets []string

	patterns := []struct {
		prefix string
		name   string
	}{
		{"sk-", "OpenAI API key"},
		{"sk_live-", "Stripe live key"},
		{"sk_test-", "Stripe test key"},
		{"ghp_", "GitHub personal access token"},
		{"gho_", "GitHub OAuth token"},
		{"ghu_", "GitHub user-to-server token"},
		{"ghs_", "GitHub server-to-server token"},
		{"ghr_", "GitHub refresh token"},
		{"glpat-", "GitLab personal access token"},
		{"xoxb-", "Slack bot token"},
		{"xoxp-", "Slack user token"},
		{"xapp-", "Slack app token"},
		{"AKIA", "AWS access key"},
		{"eyJ", "JWT token"},
	}

	lower := strings.ToLower(rawURL)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p.prefix)) {
			secrets = append(secrets, p.name)
		}
	}

	return secrets
}

func ValidateURL(rawURL string, config URLSafetyConfig) (bool, string) {
	safety := NewURLSafety(config)

	safe, reason := safety.IsSafe(rawURL)
	if !safe {
		return false, reason
	}

	if secrets := DetectSecretsInURL(rawURL); len(secrets) > 0 {
		return false, fmt.Sprintf("URL contains %s", strings.Join(secrets, ", "))
	}

	return true, ""
}
