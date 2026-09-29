package watch

import (
	"context"
	"strconv"
	"time"

	"github.com/ianclemence/ghost/pkg/providers/flight"
)

// FlightProbe adapts the real flight capability to the Probe interface. It
// holds a lookup function rather than a service so pkg/watch stays free of
// provider configuration: the agent closes over its configured service
// (keys, cache, breaker) and hands the function over.
type FlightProbe struct {
	// Lookup resolves a flight number through the configured providers.
	Lookup func(ctx context.Context, number string) (flight.Flight, error)
	// Keyed reports whether any provider credential is present. A probe
	// without keys is never called — creation refuses honestly instead of
	// pretending to track.
	Keyed bool
}

// NewFlightProbe builds the probe. A nil lookup or missing keys yields an
// unavailable probe.
func NewFlightProbe(lookup func(ctx context.Context, number string) (flight.Flight, error), keyed bool) *FlightProbe {
	return &FlightProbe{Lookup: lookup, Keyed: keyed}
}

// Name implements Probe.
func (p *FlightProbe) Name() string { return "flight" }

// Available implements Probe.
func (p *FlightProbe) Available() bool {
	return p != nil && p.Lookup != nil && p.Keyed
}

// Fetch implements Probe: one flight number in, the observable state out.
// Only fields the vendor actually returned are included — an absent gate is
// absent, never an invented one.
func (p *FlightProbe) Fetch(ctx context.Context, w Watch) (map[string]string, string, error) {
	if p == nil || p.Lookup == nil {
		return nil, "", failf(FailUnavailable, "flight tracking is not connected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	f, err := p.Lookup(callCtx, w.Entity)
	if err != nil {
		return nil, "", &ProbeError{Class: FailTransient, Err: err}
	}
	return FlightState(f), Excerpt(FlightState(f)), nil
}

// FlightState projects a validated flight result onto watch state. Keys are
// the observable fields a notice may cite; underscore keys are metadata
// that never diffs.
func FlightState(f flight.Flight) map[string]string {
	out := map[string]string{}
	if f.Number != "" {
		out["number"] = f.Number
	}
	if f.Status != "" {
		out["status"] = string(f.Status)
	}
	if f.Gate != "" {
		out["gate"] = f.Gate
	}
	if f.Terminal != "" {
		out["terminal"] = f.Terminal
	}
	if !f.Scheduled.IsZero() {
		out["scheduled"] = f.Scheduled.UTC().Format(time.RFC3339)
	}
	if f.DelayMin != nil && *f.DelayMin != 0 {
		out["delay_min"] = strconv.Itoa(*f.DelayMin)
	}
	if f.Airline != "" {
		out["airline"] = f.Airline
	}
	if f.From != "" {
		out["from"] = f.From
	}
	if f.To != "" {
		out["to"] = f.To
	}
	out["_source"] = f.Provenance
	return out
}
