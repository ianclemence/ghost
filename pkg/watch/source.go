package watch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// A probe answers one question about the world: what does this thing look
// like right now? It is consulted by the clock (cadence.go), never by the
// model, and its failure modes are explicit so Ghost can distinguish "I
// cannot check right now" from "nothing changed".
type Probe interface {
	// Name is the source id stored on the watch.
	Name() string
	// Available reports whether this probe can answer at all (keys
	// present, directory exists). A probe that is unavailable is never
	// called — creation refuses honestly instead.
	Available() bool
	// Fetch returns the current state. Metadata keys (underscore-prefixed)
	// carry provenance; everything else is observable state that may
	// change. The returned evidence excerpt is what a notice will cite.
	Fetch(ctx context.Context, w Watch) (state map[string]string, excerpt string, err error)
}

// FailureClass distinguishes why a probe failed, because the honest response
// differs: a transient network blip is retried, an unconfigured source is
// not, and an unknown entity says so instead of pretending.
type FailureClass string

const (
	// FailTransient is a retryable failure (timeout, 5xx, rate limit).
	FailTransient FailureClass = "transient"
	// FailUnavailable means the source cannot answer (not configured,
	// directory gone). Retrying will not help until it is reconnected.
	FailUnavailable FailureClass = "unavailable"
	// FailNotFound means the entity does not exist at the source — an
	// honest "I couldn't find that", never fabricated state.
	FailNotFound FailureClass = "not_found"
	// FailInvalid means the source answered with something unparseable.
	FailInvalid FailureClass = "invalid"
)

// ProbeError carries a failure class out of a probe.
type ProbeError struct {
	Class FailureClass
	Err   error
}

func (e *ProbeError) Error() string {
	if e.Err == nil {
		return string(e.Class)
	}
	return string(e.Class) + ": " + e.Err.Error()
}

func (e *ProbeError) Unwrap() error { return e.Err }

// failf builds a classified probe error.
func failf(class FailureClass, format string, args ...interface{}) *ProbeError {
	return &ProbeError{Class: class, Err: fmt.Errorf(format, args...)}
}

// ClassOf extracts the failure class from a probe error; anything
// unclassified counts as transient (retry with backoff, never fake data).
func ClassOf(err error) FailureClass {
	var pe *ProbeError
	if errors.As(err, &pe) {
		return pe.Class
	}
	return FailTransient
}

// Retryable reports whether a failure should be retried rather than ending
// the watch.
func Retryable(err error) bool {
	switch ClassOf(err) {
	case FailUnavailable, FailNotFound:
		return false
	default:
		return true
	}
}

// Registry is the set of probes the runtime may consult. The agent builds
// one at startup (flight if configured, sandbox always) and hands it to the
// poller; creation resolves a candidate's source through it.
type Registry struct {
	probes map[string]Probe
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{probes: map[string]Probe{}}
}

// Register adds a probe (nil ignored) under its own name, unless a probe
// with that name already exists.
func (r *Registry) Register(p Probe) {
	if p == nil || r == nil {
		return
	}
	if r.probes == nil {
		r.probes = map[string]Probe{}
	}
	if _, exists := r.probes[p.Name()]; exists {
		return
	}
	r.probes[p.Name()] = p
}

// Get returns a probe by name.
func (r *Registry) Get(name string) (Probe, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.probes[name]
	return p, ok
}

// Names lists the registered probes, sorted (deterministic policy output).
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.probes))
	for n := range r.probes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// AvailableNames lists the probes that can actually answer right now.
func (r *Registry) AvailableNames() []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, n := range r.Names() {
		if p := r.probes[n]; p != nil && p.Available() {
			out = append(out, n)
		}
	}
	return out
}

// Resolve picks the source for a watch under this registry: a flight
// prefers its real provider, everything else falls to the sandbox file
// source. It returns ok=false when no AVAILABLE probe can answer — the
// caller refuses the watch rather than watching it with a source that
// cannot respond.
func (r *Registry) Resolve(kind Kind) (source string, ok bool) {
	if r == nil {
		return "", false
	}
	if kind == KindFlight {
		if p, found := r.probes["flight"]; found && p.Available() {
			return "flight", true
		}
	}
	if p, found := r.probes["sandbox"]; found && p.Available() {
		return "sandbox", true
	}
	return "", false
}

// Override wraps a Probe with a test-injected replacement. Golden fixtures
// and unit tests install one to make an external source answer without any
// network.
type Override struct {
	Source string
	Fn     func(ctx context.Context, w Watch) (map[string]string, string, error)
}

// Name implements Probe.
func (o *Override) Name() string { return o.Source }

// Available implements Probe.
func (o *Override) Available() bool { return o != nil && o.Fn != nil }

// Fetch implements Probe.
func (o *Override) Fetch(ctx context.Context, w Watch) (map[string]string, string, error) {
	if o == nil || o.Fn == nil {
		return nil, "", failf(FailUnavailable, "no probe override")
	}
	return o.Fn(ctx, w)
}

// StaticProbe answers from a fixed map — a stand-in for any source in tests
// that need deterministic state without touching files.
type StaticProbe struct {
	Source  string
	State   map[string]string
	Enabled bool
	Err     error
}

// Name implements Probe.
func (s *StaticProbe) Name() string { return s.Source }

// Available implements Probe.
func (s *StaticProbe) Available() bool { return s != nil && s.Enabled }

// Fetch implements Probe.
func (s *StaticProbe) Fetch(ctx context.Context, w Watch) (map[string]string, string, error) {
	if s.Err != nil {
		return nil, "", s.Err
	}
	out := map[string]string{}
	for k, v := range s.State {
		out[k] = v
	}
	return out, Excerpt(out), nil
}

// Excerpt renders a state map as one bounded evidence line: the observable
// fields only, never metadata, never invented words.
func Excerpt(state map[string]string) string {
	keys := make([]string, 0, len(state))
	for k := range state {
		if Metadata(k) || strings.TrimSpace(state[k]) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+state[k])
	}
	if len(parts) > 6 {
		parts = parts[:6]
	}
	return strings.Join(parts, ", ")
}

// Timeout bounds one probe. A watch check must never hang the loop.
const Timeout = 10 * time.Second
