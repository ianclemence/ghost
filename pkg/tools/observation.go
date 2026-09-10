package tools

import (
	"strings"
	"time"
)

// Observation is a normalized view of one tool execution. Every tool result
// becomes an observation so context compilation, trajectory replay, and
// recovery can reason about WHAT happened without parsing free-form prose.
type Observation struct {
	Tool            string    `json:"tool"`
	Status          string    `json:"status"` // success | error | timeout
	ErrorClass      string    `json:"error_class,omitempty"`
	Retryable       bool      `json:"retryable"`
	Reconstructable bool      `json:"reconstructable"`
	ObservedAt      time.Time `json:"observed_at"`
	Summary         string    `json:"summary,omitempty"` // bounded, redaction-safe
}

// ErrorClass is a normalized failure category. Retry policy keys off this, not
// off raw error strings.
type ErrorClass string

const (
	ErrNone       ErrorClass = ""
	ErrValidation ErrorClass = "validation"
	ErrPermission ErrorClass = "permission"
	ErrNotFound   ErrorClass = "not_found"
	ErrTimeout    ErrorClass = "timeout"
	ErrNetwork    ErrorClass = "network"
	ErrTransient  ErrorClass = "transient"
	ErrInternal   ErrorClass = "internal"
)

// Retryable reports whether a class is safe to retry. Only transient,
// network, and timeout failures qualify. Validation, permission, not-found,
// and unknown internal errors are deterministic: retrying them wastes budget.
func (c ErrorClass) Retryable() bool {
	switch c {
	case ErrTimeout, ErrNetwork, ErrTransient:
		return true
	default:
		return false
	}
}

// ClassifyError maps a failed result to an error class using bounded string
// heuristics. It is intentionally conservative: an unrecognized error is
// internal and NOT retried.
func ClassifyError(res *ToolResult) ErrorClass {
	if res == nil || !res.IsError {
		return ErrNone
	}
	if res.TimedOut {
		return ErrTimeout
	}
	msg := strings.ToLower(res.ForLLM)
	if res.Err != nil {
		msg += " " + strings.ToLower(res.Err.Error())
	}
	switch {
	case containsAny(msg, "timed out", "timeout", "deadline exceeded"):
		return ErrTimeout
	case containsAny(msg, "permission", "denied", "not authorized", "forbidden", "unauthorized"):
		return ErrPermission
	case containsAny(msg, "invalid", "validation", "required", "schema", "malformed"):
		return ErrValidation
	case containsAny(msg, "not found", "no such", "does not exist", "missing"):
		return ErrNotFound
	case containsAny(msg, "connection", "network", "unavailable", "temporary", "reset by peer", "eof", "refused"):
		return ErrNetwork
	case containsAny(msg, "rate limit", "try again", "busy", "throttl"):
		return ErrTransient
	default:
		return ErrInternal
	}
}

// NewObservation normalizes a tool result. Reconstructable marks large or
// listing-style output that the runtime should treat as cheap to recreate
// rather than as durable state.
func NewObservation(tool string, res *ToolResult) Observation {
	o := Observation{Tool: tool, ObservedAt: time.Now().UTC(), Status: "success"}
	if res == nil {
		o.Status = "error"
		o.ErrorClass = string(ErrInternal)
		return o
	}
	if res.IsError {
		o.Status = "error"
		if res.TimedOut {
			o.Status = "timeout"
		}
		o.ErrorClass = string(ClassifyError(res))
		o.Retryable = ClassifyError(res).Retryable()
	}
	o.Reconstructable = isReconstructableOutput(res.ForLLM)
	o.Summary = summarize(res.ForLLM, 240)
	return o
}

// isReconstructableOutput flags output that is derived from the world and can
// be re-read on demand (directory listings, large dumps) rather than being a
// durable user artifact.
func isReconstructableOutput(s string) bool {
	if len([]rune(s)) > 2000 {
		return true
	}
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "dir:") || strings.Contains(lower, "dir:  ") && strings.Contains(lower, "file:")
}

// summarize bounds and lightly normalizes text for an observation. It never
// includes secrets by construction (callers pass already-redacted text where
// relevant); it only truncates.
func summarize(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len([]rune(s)) > max {
		r := []rune(s)
		return string(r[:max]) + "…"
	}
	return s
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// VerificationContract names the world-state check a mutating tool should pass
// through. It is documentation and observability: tools that implement
// VerifiableTool do the real check; this table lets Doctor/observability state
// the intended contract even for tools whose verifier is external.
var VerificationContract = map[string]string{
	"write_file":    "read_file",
	"append_file":   "read_file",
	"edit_file":     "read_file",
	"calendar":      "calendar.get",
	"schedule":      "schedule.list",
	"cron":          "cron.list",
	"remember":      "context_get",
	"memory_curate": "context_get",
}

// ContractFor returns the verification contract for a tool, or "" if the tool
// is read-only / has no declared contract.
func ContractFor(tool string) string { return VerificationContract[tool] }
