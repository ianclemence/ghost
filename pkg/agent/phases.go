package agent

import "context"

// Honest progress phases.
//
// The client already receives `queued` and `agent_processing` frames. What it
// never received was anything covering the silent stretch in between — the
// memory embedding (measured ~2.8 s on the live device), context assembly, and
// the provider's first token (~0.9 s round trip. A user watching a still screen
// for four seconds cannot tell work from a hang.
//
// A phase is emitted only when the runtime is genuinely about to do that work,
// so a phase can never claim progress that is not happening. Completion stays
// bound to evidence: no phase here says "done".
type PhaseSink func(phase, detail string)

type phaseSinkKey struct{}

// WithPhaseSink attaches a per-turn progress sink. Nil-safe: the loop simply
// does not advance interaction when no sink is listening.
func WithPhaseSink(ctx context.Context, fn PhaseSink) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, phaseSinkKey{}, fn)
}

// emitPhase reports one truthful phase. Unknown ctx or sink is a no-op.
func emitPhase(ctx context.Context, phase, detail string) {
	if ctx == nil {
		return
	}
	if fn, ok := ctx.Value(phaseSinkKey{}).(PhaseSink); ok && fn != nil {
		fn(phase, detail)
	}
}

// ServedBy identifies the model that actually produced a response: the
// runtime's record of where the owner's words were processed, so surfaces
// state it from execution rather than infer it from settings.
type ServedBy struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Local is true when the provider runs on the owner's hardware
	// (Ollama, vLLM, …): nothing left the Pod for this call.
	Local bool `json:"local"`
}

type servedSinkKey struct{}

// WithServedBySink registers fn to receive every model call that served
// during the turn (a turn with several iterations reports several).
func WithServedBySink(ctx context.Context, fn func(ServedBy)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, servedSinkKey{}, fn)
}

func emitServedBy(ctx context.Context, s ServedBy) {
	if ctx == nil {
		return
	}
	if fn, ok := ctx.Value(servedSinkKey{}).(func(ServedBy)); ok && fn != nil {
		fn(s)
	}
}

// AggregateServedBy reduces a turn's serving records to what the owner is
// told: the last model that answered, and Local only when every call in the
// turn stayed on the Pod — one cloud call makes the turn a cloud turn.
func AggregateServedBy(all []ServedBy) (ServedBy, bool) {
	if len(all) == 0 {
		return ServedBy{}, false
	}
	out := all[len(all)-1]
	out.Local = true
	for _, s := range all {
		if !s.Local {
			out.Local = false
		}
	}
	return out, true
}

// ReportServedBy lets a transport that relays a remote turn (the gateway
// client) deliver the daemon's served_by record to a caller's sink, so the
// embedded and remote TUIs learn where a turn ran the same way.
func ReportServedBy(ctx context.Context, s ServedBy) { emitServedBy(ctx, s) }
