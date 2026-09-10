package permissions

// Decision is a monotonic authorization outcome. Its ordering is total and
// deny is absorbing: once any layer denies, no later layer can widen the
// result. This is the explicit form of the invariant that the model is
// allowed to be wrong but the system is not allowed to blindly believe it —
// a downstream component may reduce authority but may never overturn a
// stronger denial into an allow.
type Decision int

const (
	// DecisionAllow permits the action.
	DecisionAllow Decision = iota
	// DecisionAsk defers to an approval request.
	DecisionAsk
	// DecisionDeny refuses the action. It is the most restrictive outcome.
	DecisionDeny
)

func (d Decision) String() string {
	switch d {
	case DecisionAsk:
		return "ask"
	case DecisionDeny:
		return "deny"
	default:
		return "allow"
	}
}

// MoreRestrictive returns the more restrictive of two decisions
// (deny > ask > allow). It is commutative and associative, so composition
// order cannot change the result.
func MoreRestrictive(a, b Decision) Decision {
	if a >= b {
		return a
	}
	return b
}

// Combine folds a set of decisions into the most restrictive one. Because
// deny is absorbing, no permutation of the inputs can produce an allow once
// any input is a deny.
func Combine(decisions ...Decision) Decision {
	out := DecisionAllow
	for _, d := range decisions {
		out = MoreRestrictive(out, d)
	}
	return out
}

// VerdictDecision maps a broker verdict to its monotonic decision.
func VerdictDecision(v Verdict) Decision {
	switch v {
	case VerdictAllow:
		return DecisionAllow
	case VerdictDeny:
		return DecisionDeny
	default:
		return DecisionAsk
	}
}
