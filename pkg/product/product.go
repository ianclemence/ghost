// Package product defines Ghost's product-level semantics: completion
// states, error classes, message visibility, and user-facing language rules.
//
// The core rule: user-facing concepts must be product concepts. Internal
// implementation terminology (ports, OAuth internals, provider names, cron
// syntax, circuit breakers) must never leak into ordinary user messages.
// This package gives the runtime deterministic, LLM-independent mappings
// so honesty and clarity do not depend on prompt instructions.
package product

// Completion is the trustworthy terminal state of any externally meaningful
// action. Never report success unless the action actually completed.
type Completion string

const (
	CompletionSuccess                Completion = "success"
	CompletionFailed                 Completion = "failed"
	CompletionPartiallyCompleted     Completion = "partially_completed"
	CompletionWaitingForUser         Completion = "waiting_for_user"
	CompletionWaitingForConfig       Completion = "waiting_for_configuration"
	CompletionWaitingForAuth         Completion = "waiting_for_authorization"
	CompletionWaitingForPermission   Completion = "waiting_for_permission"
	CompletionTemporarilyUnavailable Completion = "temporarily_unavailable"
	CompletionOffline                Completion = "offline"
	CompletionCancelled              Completion = "cancelled"
)

// Terminal reports whether the state ends the action (no further runtime
// work expected without a new trigger or user action).
func (c Completion) Terminal() bool {
	switch c {
	case CompletionSuccess, CompletionFailed, CompletionPartiallyCompleted,
		CompletionOffline, CompletionCancelled:
		return true
	default:
		return false
	}
}

// ErrorClass is the internal, machine-readable failure category. It drives
// retry/fallback decisions and maps to product-language user messages.
type ErrorClass string

const (
	ErrUserInput      ErrorClass = "user_input_error"
	ErrClarification  ErrorClass = "clarification_required"
	ErrAuthRequired   ErrorClass = "authorization_required"
	ErrConfigRequired ErrorClass = "configuration_required"
	ErrPermission     ErrorClass = "permission_required"
	ErrNetwork        ErrorClass = "network_error"
	ErrProvider       ErrorClass = "provider_error"
	ErrRateLimited    ErrorClass = "rate_limited"
	ErrTimeout        ErrorClass = "timeout"
	ErrValidation     ErrorClass = "validation_error"
	ErrExecution      ErrorClass = "execution_error"
	ErrInternal       ErrorClass = "internal_error"
	ErrOffline        ErrorClass = "offline"
	ErrExpired        ErrorClass = "expired"
	ErrRevoked        ErrorClass = "revoked"
)

// Outcome bundles what happened with what the user should hear.
type Outcome struct {
	Completion Completion `json:"completion"`
	Class      ErrorClass `json:"class,omitempty"`
	// UserMessage is safe to show verbatim. It contains no secrets, paths,
	// tokens, provider internals, or stack traces.
	UserMessage string `json:"user_message"`
	// Action is the product-level next step (e.g. "connect_calendar").
	Action string `json:"action,omitempty"`
	// Retryable indicates the runtime may retry/fallback without asking.
	Retryable bool `json:"retryable"`
}

func success(msg string) Outcome {
	return Outcome{Completion: CompletionSuccess, UserMessage: msg}
}

// Success builds a success outcome. Call only after verified completion.
func Success(msg string) Outcome { return success(msg) }

// Failure builds an honest failure outcome from a class + product message.
func Failure(class ErrorClass, msg, action string, retryable bool) Outcome {
	comp := CompletionFailed
	switch class {
	case ErrClarification:
		comp = CompletionWaitingForUser
	case ErrConfigRequired:
		comp = CompletionWaitingForConfig
	case ErrAuthRequired, ErrExpired, ErrRevoked:
		comp = CompletionWaitingForAuth
	case ErrPermission:
		comp = CompletionWaitingForPermission
	case ErrRateLimited, ErrTimeout, ErrProvider, ErrNetwork:
		comp = CompletionTemporarilyUnavailable
	case ErrOffline:
		comp = CompletionOffline
	}
	return Outcome{Completion: comp, Class: class, UserMessage: msg, Action: action, Retryable: retryable}
}

// userMessages maps error classes to default product-language messages.
// Capability-specific messages override these (see FriendlyFor).
var userMessages = map[ErrorClass]string{
	ErrUserInput:      "I couldn't understand that well enough to act on it. Could you rephrase?",
	ErrClarification:  "I need a bit more detail before I can do that.",
	ErrAuthRequired:   "That connection needs to be renewed before I can continue.",
	ErrConfigRequired: "That isn't connected yet. Connect it in Ghost settings to continue.",
	ErrPermission:     "I don't have permission to do that yet.",
	ErrNetwork:        "The network request failed. I'll try again shortly.",
	ErrProvider:       "That data source is temporarily unavailable. I'll try again shortly.",
	ErrRateLimited:    "That service is busy right now. I'll try again shortly.",
	ErrTimeout:        "That took too long to respond. I'll try again shortly.",
	ErrValidation:     "I got an unexpected response and didn't want to guess. Please try again.",
	ErrExecution:      "That didn't complete. Nothing was changed.",
	ErrInternal:       "Something went wrong on my side. Please try again.",
	ErrOffline:        "Ghost is offline, so I can't reach that right now.",
	ErrExpired:        "That connection expired and needs to be renewed.",
	ErrRevoked:        "That connection was revoked and needs to be set up again.",
}

// capabilityHints overrides the generic message per capability.
var capabilityHints = map[string]map[ErrorClass]string{
	"calendar": {
		ErrConfigRequired: "Your calendar isn't connected yet. Connect Google Calendar to continue.",
		ErrAuthRequired:   "Your calendar connection needs to be renewed.",
		ErrExpired:        "Your calendar connection expired. Reconnect it to continue.",
		ErrRevoked:        "Your calendar access was revoked. Reconnect it to continue.",
		ErrOffline:        "Ghost is offline, so I can't reach your calendar right now.",
	},
	"flight": {
		ErrConfigRequired: "Flight tracking isn't connected yet.",
		ErrProvider:       "Flight data is temporarily unavailable. I'll try again shortly.",
		ErrRateLimited:    "Flight data is busy right now. I'll try again shortly.",
	},
	"weather": {
		ErrProvider: "Weather data is temporarily unavailable. I'll try again shortly.",
		ErrOffline:  "Ghost is offline, so I can't fetch fresh weather right now.",
	},
	"reminder": {
		ErrClarification: "What time should I remind you?",
	},
}

// FriendlyFor returns the product-language message for a capability +
// error class. It never includes implementation terminology.
func FriendlyFor(capability string, class ErrorClass) string {
	if m, ok := capabilityHints[capability]; ok {
		if s, ok := m[class]; ok {
			return s
		}
	}
	if s, ok := userMessages[class]; ok {
		return s
	}
	return "I couldn't complete that right now. Please try again in a bit."
}

// Visibility separates internal traces from user-facing content at the
// type level so the UI/API can serialize only approved categories.
type Visibility string

const (
	VisInternalTrace Visibility = "internal_trace"
	VisToolActivity  Visibility = "tool_activity"
	VisSystemEvent   Visibility = "system_event"
	VisModelContext  Visibility = "model_context"
	VisUserMessage   Visibility = "user_visible_message"
	VisUserError     Visibility = "user_visible_error"
)

// UserVisible reports whether a category may be serialized to UI/API.
func (v Visibility) UserVisible() bool {
	return v == VisUserMessage || v == VisUserError
}

// Event is a runtime execution event with a visibility category. The
// activity layer and diagnostics layer are different projections of the
// same underlying events.
type Event struct {
	Visibility Visibility `json:"visibility"`
	// Category is the typed semantic vocabulary (e.g. "provider.failed",
	// "memory.created", "automation.triggered"). Raw implementation events
	// must not be used as the primary vocabulary.
	Category string `json:"category"`
	// Summary is human language for the activity stream.
	Summary string `json:"summary"`
	// RequestID/SessionID/Capability correlate the event internally.
	RequestID  string `json:"request_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Capability string `json:"capability,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	// DurationMs and Result are safe, non-secret fields.
	DurationMs int64  `json:"duration_ms,omitempty"`
	Result     string `json:"result,omitempty"`
}

// HumanActivity renders the activity-stream line for user-visible events.
// Technical traces return "" (excluded from the ordinary activity stream).
func (e Event) HumanActivity() string {
	if !e.Visibility.UserVisible() {
		return ""
	}
	return e.Summary
}
