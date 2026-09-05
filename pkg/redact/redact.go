// Package redact is Ghost's centralized secret boundary.
//
// Borrowed pattern (OpenMausBot redact.ts): redact at the transport
// boundary, not at each call site. Every event payload, log field,
// activity summary, diagnostic dump, and persisted assistant message
// passes through here before leaving the runtime core.
//
// Rules: secret-shaped KEYS lose values (stable markers keep redaction
// idempotent); unmistakable credential SHAPES (key prefixes, bearer,
// PEM, JWT) are masked in free text. Prose is never rewritten — only
// shapes that cannot be innocent match.
package redact

import (
	"fmt"
	"regexp"
	"strings"
)

// Marker for an already-redacted value (idempotent re-masking).
var redactedMarker = regexp.MustCompile(`^«redacted \d+ chars»$`)

func mask(s string) string {
	if redactedMarker.MatchString(s) {
		return s
	}
	return fmt.Sprintf("«redacted %d chars»", len(s))
}

var secretKeyParts = []string{
	"token", "secret", "password", "passwd", "apikey", "api_key",
	"authorization", "auth_token", "private_key", "access_key", "client_secret",
}

func isSecretKey(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range secretKeyParts {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// `key` alone only when standalone or suffix (API_KEY, xai_key) —
	// never `keyboard`.
	if matched, _ := regexp.MatchString(`(^|[_.\-])keys?$`, lower); matched {
		return true
	}
	return false
}

// Map redacts secret values from a string map, preserving shape
// (key present, value masked) for debuggability.
func Map(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if isSecretKey(k) {
			out[k] = mask(v)
		} else {
			out[k] = v
		}
	}
	return out
}

// Any redacts secret keys inside nested structures (maps/lists).
func Any(v interface{}) interface{} {
	switch tv := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(tv))
		for k, val := range tv {
			if isSecretKey(k) {
				if s, ok := val.(string); ok {
					out[k] = mask(s)
				} else {
					out[k] = "«redacted»"
				}
			} else {
				out[k] = Any(val)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(tv))
		for i, e := range tv {
			out[i] = Any(e)
		}
		return out
	case string:
		return Text(tv)
	default:
		return v
	}
}

var keyPrefixes = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-(?:ant-|proj-|live-|test-)?[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9\-]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{30,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\b`),
	regexp.MustCompile(`\b[0-9a-f]{64}\b`), // sha256-shaped tokens
}

var bearerRe = regexp.MustCompile(`(?i)(\bBearer\s+)([A-Za-z0-9._~+/=-]{12,})`)
var pemRe = regexp.MustCompile(`(-----BEGIN [A-Z ]*PRIVATE KEY-----)([\s\S]*?)(-----END [A-Z ]*PRIVATE KEY-----)`)
var kvRe = regexp.MustCompile(`\b((?:[A-Za-z0-9_\-]*_)?(?:api[_-]?key|apikey|secret|token|password|passwd|authorization|auth[_-]?token|access[_-]?key|private[_-]?key)s?)(["']?\s*[=:]\s*)(["']?)([A-Za-z0-9._~+/=-]{8,})`)

// Text masks credential shapes in free text. Short strings pass through
// (a generic long-hex heuristic would rewrite real code).
func Text(s string) string {
	if len(s) < 8 {
		return s
	}
	out := s
	for _, re := range keyPrefixes {
		out = re.ReplaceAllStringFunc(out, mask)
	}
	out = bearerRe.ReplaceAllString(out, `${1}«redacted»`)
	out = pemRe.ReplaceAllString(out, `${1}«redacted key material»${3}`)
	out = kvRe.ReplaceAllStringFunc(out, func(m string) string {
		parts := kvRe.FindStringSubmatch(m)
		if len(parts) != 5 {
			return m
		}
		return parts[1] + parts[2] + parts[3] + mask(parts[4]) + parts[3]
	})
	return out
}
