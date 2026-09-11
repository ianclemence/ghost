package personalcontext

import "fmt"

// Lifetime is the memory tier a belief belongs to. It is the storage-side
// analogue of the brief's HOT / WARM / DURABLE / RECONSTRUCTABLE classes.
//
// Personal Context only ever stores durable or reconstructable beliefs:
// working (turn) and session (conversation) state live in the session store
// and RAG, not here. The tier is explicit so a derived belief can be
// reclaimed under storage pressure without touching canonical facts.
type Lifetime string

const (
	// LifetimeDurable is a belief intended to remain useful across sessions
	// (preferences, stable facts, decisions). Canonical; never auto-reclaimed.
	LifetimeDurable Lifetime = "durable"
	// LifetimeReconstructable is a belief cheap to recreate from canonical
	// evidence (a derived summary, a cached inference). Eligible for cleanup.
	LifetimeReconstructable Lifetime = "reconstructable"
)

// ValidLifetime reports whether l is a known tier.
func ValidLifetime(l Lifetime) bool {
	return l == LifetimeDurable || l == LifetimeReconstructable
}

// Trust is the provenance-derived confidence class of a belief. It answers
// "how much authority does this memory carry by default?" — the brief's
// explicit-user-fact > tool-observation > model-inference ordering.
type Trust string

const (
	// TrustCanonical: the user said it, corrected it, or edited it by hand.
	TrustCanonical Trust = "canonical"
	// TrustObserved: established from non-model evidence (document, import,
	// workflow, command).
	TrustObserved Trust = "observed"
	// TrustInferred: produced by model inference/summary — evidence, not
	// authority, until confirmed.
	TrustInferred Trust = "inferred"
	// TrustUntrusted: network-origin content no user confirmed. Retrievable,
	// rankable, but never promotable: a poisoned page must not become truth
	// by confidence score. Only a stronger (canonical/observed) source on
	// the same entry lifts it.
	TrustUntrusted Trust = "untrusted"
	// TrustUnknown: legacy record with no provenance. Treated conservatively;
	// provenance is never manufactured to fill the gap.
	TrustUnknown Trust = "unknown"
)

// ProvenanceClass derives the trust class from an entry's strongest source.
// A single canonical source outranks any number of inferred ones. A web
// source taints only when nothing stronger exists: user confirmation of a
// web fact upgrades the entry normally.
func ProvenanceClass(e Entry) Trust {
	best := TrustUnknown
	tainted := false
	for _, src := range e.Sources {
		// Origin dominates the kind label: a web source taints even when
		// described as inferred. Only explicit user authority or
		// non-model evidence outranks the taint.
		if src.Type == SourceWeb {
			tainted = true
		}
		switch {
		case src.Kind == SourceUserDeclared || src.Kind == SourceUserCorrected ||
			src.Kind == SourceManual || src.Type == SourceManualEdit:
			return TrustCanonical
		case src.Type == SourceDocument || src.Type == SourceWorkflow ||
			src.Type == SourceImport || src.Type == SourceCommand:
			if best == TrustUnknown {
				best = TrustObserved
			}
		case src.Type == SourceAgentInference || src.Kind == SourceInferred:
			if best == TrustUnknown {
				best = TrustInferred
			}
		}
	}
	if tainted && (best == TrustUnknown || best == TrustInferred) {
		return TrustUntrusted
	}
	return best
}

// PromotionPolicy decides whether a candidate belief may become current
// (durable) or must remain a held-back candidate. It is the guard against
// self-reinforcing hallucination: a model inference is not promoted to
// canonical truth just because it was produced.
type PromotionPolicy struct {
	// MinInferredConfidence is the confidence a model-inferred belief needs
	// to be promoted directly. Below it, the belief is stored as uncertain
	// (inspectable, but not surfaced as current) pending confirmation.
	MinInferredConfidence float64
}

// DefaultPromotionPolicy is the conservative default. Inferences at or above
// the threshold promote; weaker ones are held as candidates.
func DefaultPromotionPolicy() PromotionPolicy {
	return PromotionPolicy{MinInferredConfidence: 0.5}
}

// PromotionDecision is the outcome of evaluating a candidate.
type PromotionDecision struct {
	// Promote: the candidate may be stored as current (durable).
	Promote bool
	// Status: the status the candidate should carry when stored.
	Status Status
	// Reason: human-readable, safe to record in provenance/observability.
	Reason string
}

// Evaluate classifies a candidate entry under the policy. It never mutates
// the entry. Trust classes:
//   - canonical/observed: promoted as current.
//   - inferred: promoted only at or above MinInferredConfidence, else uncertain.
//   - untrusted (network origin, unconfirmed): NEVER promoted, at any
//     confidence — held as a candidate until the user confirms it.
//   - unknown (no provenance): held as uncertain; provenance is not invented.
func (p PromotionPolicy) Evaluate(e Entry) PromotionDecision {
	threshold := p.MinInferredConfidence
	if threshold <= 0 {
		threshold = DefaultPromotionPolicy().MinInferredConfidence
	}
	switch ProvenanceClass(e) {
	case TrustCanonical:
		return PromotionDecision{Promote: true, Status: StatusCurrent, Reason: "explicit user or manual provenance"}
	case TrustObserved:
		return PromotionDecision{Promote: true, Status: StatusCurrent, Reason: "non-model evidence"}
	case TrustUntrusted:
		return PromotionDecision{Promote: false, Status: StatusUncertain, Reason: "untrusted network origin; held until user-confirmed"}
	case TrustInferred:
		if e.Confidence >= threshold {
			return PromotionDecision{Promote: true, Status: StatusCurrent, Reason: fmt.Sprintf("inference at confidence %.2f >= %.2f", e.Confidence, threshold)}
		}
		return PromotionDecision{Promote: false, Status: StatusUncertain, Reason: fmt.Sprintf("model inference at confidence %.2f below %.2f; held as candidate", e.Confidence, threshold)}
	default:
		return PromotionDecision{Promote: false, Status: StatusUncertain, Reason: "no provenance; held as candidate"}
	}
}

// normalize fills migration defaults for records loaded from an older log.
// It only fills what is missing; it never rewrites or invents provenance.
func (e *Entry) normalize() {
	if e.Lifetime == "" {
		// Personal Context has always been durable memory; old entries
		// without the field are durable, not reconstructable.
		e.Lifetime = LifetimeDurable
	}
}
