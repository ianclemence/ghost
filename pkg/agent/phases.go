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
