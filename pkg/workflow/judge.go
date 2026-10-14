package workflow

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// Answer is the typed result of one question. NoulVal is set for QNoul
// (0..1), Choice+Probs+Confidence for QChoice/QScore. Confidence below the
// code gate threshold must escalate, never silently accept.
type Answer struct {
	QuestionID string             `json:"question_id"`
	NoulVal    float64            `json:"noul,omitempty"`
	Choice     string             `json:"choice,omitempty"`
	Probs      map[string]float64 `json:"probs,omitempty"`
	Confidence float64            `json:"confidence,omitempty"`
}

// Backend evaluates one typed question against state. Implementations:
// RuleBackend (on-Pod, default), LLM-backed adapters (optional, injected).
// There is deliberately NO Jev client in this package: Jev is used offline
// to tune wording/thresholds, never at Ghost runtime.
type Backend interface {
	Evaluate(ctx context.Context, state map[string]interface{}, q Question) (Answer, error)
}

// stateText flattens state values into searchable text for RuleBackend.
func stateText(state map[string]interface{}) string {
	var sb strings.Builder
	for _, v := range state {
		fmt.Fprintf(&sb, "%v ", v)
	}
	return strings.ToLower(sb.String())
}

// RuleBackend is a deterministic keyword scorer for stable, high-volume
// micro-decisions. Fail-closed: unknown questions return an error so the
// engine escalates rather than guessing.
type RuleBackend struct{}

// Evaluate implements Backend.
func (RuleBackend) Evaluate(_ context.Context, state map[string]interface{}, q Question) (Answer, error) {
	text := stateText(state)
	switch q.Kind {
	case QNoul:
		return evalNoul(q.ID, text), nil
	case QChoice:
		return evalChoice(q.ID, text, q.Criteria), nil
	case QScore:
		return evalScore(q.ID, text), nil
	default:
		return Answer{}, fmt.Errorf("rule backend: unknown question kind %q", q.Kind)
	}
}

// stopperMarks are deadline/blocker/money/security signals. Kept in one
// place so rubric changes are diffable and versioned beside the workflow.
var stopperMarks = []string{"deadline", "due", "urgent", "blocker", "blocked", "payment", "invoice", "security", "breach", "asap"}

var noiseMarks = []string{"newsletter", "unsubscribe", "digest", "promo", "sale ", "weekly roundup"}

func evalNoul(id, text string) Answer {
	score := 0.15 // prior: most threads are not stoppers
	hits := 0
	for _, m := range stopperMarks {
		if strings.Contains(text, m) {
			hits++
		}
	}
	for _, m := range noiseMarks {
		if strings.Contains(text, m) {
			return Answer{QuestionID: id, NoulVal: 0.04}
		}
	}
	switch {
	case hits >= 3:
		score = 0.96
	case hits == 2:
		score = 0.82
	case hits == 1:
		score = 0.58
	}
	return Answer{QuestionID: id, NoulVal: round2(score)}
}

func evalChoice(id, text string, criteria map[string]string) Answer {
	// Score each option by criterion keyword overlap; unknown options error.
	if len(criteria) == 0 {
		return Answer{QuestionID: id, Choice: "uncertain", Confidence: 0}
	}
	scores := make(map[string]float64, len(criteria))
	total := 0.0
	for opt, crit := range criteria {
		s := 0.1
		for _, w := range strings.Fields(strings.ToLower(crit)) {
			if len(w) > 3 && strings.Contains(text, w) {
				s += 1.0
			}
		}
		// Direct option-name mention is weak evidence of fit.
		if strings.Contains(text, strings.ToLower(opt)) {
			s += 0.2
		}
		scores[opt] = s
		total += s
	}
	best, bestScore := "uncertain", 0.0
	for opt, s := range scores {
		if s > bestScore {
			best, bestScore = opt, s
		}
	}
	probs := make(map[string]float64, len(scores))
	for opt, s := range scores {
		probs[opt] = round2(s / total)
	}
	conf := round2(bestScore / (total + 1.0))
	return Answer{QuestionID: id, Choice: best, Probs: probs, Confidence: conf}
}

func evalScore(id, text string) Answer {
	// 0..1 urgency score from stopper density.
	hits := 0
	for _, m := range stopperMarks {
		if strings.Contains(text, m) {
			hits++
		}
	}
	s := math.Min(1, float64(hits)/4.0)
	return Answer{QuestionID: id, NoulVal: round2(s), Confidence: round2(0.4 + 0.15*float64(hits))}
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// CheckNoul enforces a fail-closed gate: value must be in [0,1].
// Returns (pass, value, error). Error means escalate, never default-pass.
func CheckNoul(a Answer) (bool, float64, error) {
	if math.IsNaN(a.NoulVal) || math.IsInf(a.NoulVal, 0) || a.NoulVal < 0 || a.NoulVal > 1 {
		return false, 0, fmt.Errorf("noul out of range for %q", a.QuestionID)
	}
	return true, a.NoulVal, nil
}

// CheckChoice enforces confidence gating: conf < threshold => escalate.
func CheckChoice(a Answer, threshold float64) (string, error) {
	if a.Choice == "" || a.Choice == "uncertain" {
		return "", fmt.Errorf("choice uncertain for %q", a.QuestionID)
	}
	if math.IsNaN(a.Confidence) || a.Confidence < threshold {
		return "", fmt.Errorf("choice %q confidence %.2f below %.2f", a.QuestionID, a.Confidence, threshold)
	}
	return a.Choice, nil
}
