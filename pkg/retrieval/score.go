// Package retrieval provides a deterministic, inspectable ranking function for
// memory candidates. It is the software analogue of a hierarchical sparse
// indexer's second stage: a cheap coarse candidate set (semantic over-fetch,
// structured filtering) is narrowed by a weighted score whose components are
// individually observable, so a ranking decision is debuggable rather than
// hidden inside a model call.
package retrieval

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Candidate is one retrievable memory item with the signals needed to score it.
// Missing signals are zero and contribute nothing (never fabricated).
type Candidate struct {
	ID         string
	Content    string
	Source     string
	CreatedAt  time.Time
	Similarity float64 // semantic similarity, 0..1
	Confidence float64 // belief confidence, 0..1 (0 = unknown)
	Trust      string  // canonical | observed | inferred | untrusted | unknown
	Superseded bool    // retired belief: strong negative signal
}

// Weights are the per-component multipliers. They are explicit and configurable
// so scoring can be tuned per deployment without code changes.
type Weights struct {
	Semantic      float64
	Lexical       float64
	Recency       float64
	Confidence    float64
	SourceQuality float64
	Staleness     float64 // penalty
	Contradiction float64 // penalty
}

// DefaultWeights favors semantic relevance, then lexical overlap, with recency
// and provenance as tie-breakers. Penalties are bounded.
func DefaultWeights() Weights {
	return Weights{
		Semantic:      1.0,
		Lexical:       0.6,
		Recency:       0.3,
		Confidence:    0.2,
		SourceQuality: 0.2,
		Staleness:     0.5,
		Contradiction: 1.0,
	}
}

// RecencyHalfLife is the age at which the recency component halves.
const RecencyHalfLife = 30 * 24 * time.Hour

// StalenessThreshold is the age beyond which the staleness penalty ramps in.
const StalenessThreshold = 180 * 24 * time.Hour

// Components is the per-signal breakdown of a score. It exists so Doctor and
// tests can explain WHY a candidate ranked where it did.
type Components struct {
	Semantic      float64
	Lexical       float64
	Recency       float64
	Confidence    float64
	SourceQuality float64
	Staleness     float64
	Contradiction float64
	Total         float64
}

// SourceQuality maps provenance trust to a [0,1] quality score. Canonical user
// statements outrank observations, which outrank model inference. Unknown
// (legacy, no provenance) sits between observed and inferred: not fabricated,
// not dismissed. Untrusted (network-origin, no user confirmation) ranks
// below everything: retrievable, but never winning ties.
func SourceQuality(trust string) float64 {
	switch strings.ToLower(strings.TrimSpace(trust)) {
	case "canonical":
		return 1.0
	case "observed":
		return 0.7
	case "inferred":
		return 0.3
	case "untrusted":
		return 0.1
	default:
		return 0.4
	}
}

// Score computes the weighted components for one candidate against a query.
func Score(c Candidate, query string, now time.Time, w Weights) Components {
	var comp Components
	comp.Semantic = w.Semantic * clamp01(c.Similarity)
	comp.Lexical = w.Lexical * lexicalOverlap(query, c.Content)
	if !c.CreatedAt.IsZero() {
		age := now.Sub(c.CreatedAt)
		if age < 0 {
			age = 0
		}
		comp.Recency = w.Recency * math.Exp(-age.Seconds()/RecencyHalfLife.Seconds())
		if age > StalenessThreshold {
			over := age - StalenessThreshold
			// Ramp to at most 1.0 over one more staleness window.
			comp.Staleness = w.Staleness * math.Min(1, over.Seconds()/StalenessThreshold.Seconds())
		}
	}
	if c.Confidence > 0 {
		comp.Confidence = w.Confidence * clamp01(c.Confidence)
	}
	comp.SourceQuality = w.SourceQuality * SourceQuality(c.Trust)
	if c.Superseded {
		comp.Contradiction = w.Contradiction
	}
	comp.Total = comp.Semantic + comp.Lexical + comp.Recency + comp.Confidence +
		comp.SourceQuality - comp.Staleness - comp.Contradiction
	return comp
}

// Ranked is a candidate paired with its score breakdown.
type Ranked struct {
	Candidate  Candidate
	Components Components
}

// Rank scores every candidate and returns the top `limit` (limit <= 0 = all),
// ordered by total descending. Ties break on ID for deterministic output.
func Rank(cands []Candidate, query string, now time.Time, w Weights, limit int) []Ranked {
	out := make([]Ranked, 0, len(cands))
	for _, c := range cands {
		out = append(out, Ranked{Candidate: c, Components: Score(c, query, now, w)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Components.Total != out[j].Components.Total {
			return out[i].Components.Total > out[j].Components.Total
		}
		return out[i].Candidate.ID < out[j].Candidate.ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// lexicalOverlap is the fraction of distinct query terms (len >= 2) present in
// the content. Cheap, deterministic, no tokenizer dependency.
func lexicalOverlap(query, content string) float64 {
	terms := distinctTerms(query)
	if len(terms) == 0 {
		return 0
	}
	lower := strings.ToLower(content)
	hit := 0
	for _, t := range terms {
		if strings.Contains(lower, t) {
			hit++
		}
	}
	return float64(hit) / float64(len(terms))
}

func distinctTerms(s string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(f)) < 2 {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ApproxTokens estimates token count cheaply (4 runes per token). It is a
// budget heuristic, not a tokenizer: good enough to bound context, never used
// for billing.
func ApproxTokens(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// Budget bounds how much retrieved material may enter the model prompt.
// It is applied after ranking, so the highest-value items win the space.
type Budget struct {
	MaxTokens int
}

// LayerBudgets bounds each provenance layer separately, so a long tail of
// weak or untrusted matches can never crowd user-stated facts out of the
// prompt. Canonical (what the owner said) is never starved by lower layers;
// the three layers sum to the single memory-context budget.
type LayerBudgets struct {
	Canonical int // user-stated facts
	Trusted   int // observed and unknown provenance
	Other     int // inferred and untrusted
}

// DefaultLayerBudgets splits the 2000-token memory context across layers:
// room for the facts that matter most first, scraps for the rest.
func DefaultLayerBudgets() LayerBudgets {
	return LayerBudgets{Canonical: 1000, Trusted: 600, Other: 400}
}

// Total is the whole memory context the layers may occupy together.
func (b LayerBudgets) Total() int { return b.Canonical + b.Trusted + b.Other }

// TierOf maps a provenance trust label to its budget layer.
func TierOf(trust string) string {
	switch strings.ToLower(strings.TrimSpace(trust)) {
	case "canonical":
		return "canonical"
	case "observed", "unknown", "":
		return "trusted"
	default:
		return "other"
	}
}

// FitLayers fits each already-ranked layer within its own budget and emits
// canonical first, then trusted, then other. Layers never borrow from each
// other: spare canonical space does not invite weaker material in.
func (b LayerBudgets) FitLayers(canonical, trusted, other []string) []string {
	out := make([]string, 0, len(canonical)+len(trusted)+len(other))
	out = append(out, Budget{MaxTokens: b.Canonical}.Fit(canonical)...)
	out = append(out, Budget{MaxTokens: b.Trusted}.Fit(trusted)...)
	out = append(out, Budget{MaxTokens: b.Other}.Fit(other)...)
	return out
}

// DefaultBudget is the memory-context budget: room for several recalled
// facts plus their provenance. Ranked best-first, so extra space helps
// strong matches and never drowns the model in weak ones.
func DefaultBudget() Budget { return Budget{MaxTokens: 2000} }

// Fit returns the longest prefix of items (already ranked) whose combined
// approximate token count fits the budget. An item larger than the whole
// budget is skipped rather than truncating mid-fact; a zero budget fits
// nothing. If nothing fits, the first item is still returned only when it
// alone is within budget — otherwise the result is empty (never a partial
// fact).
func (b Budget) Fit(items []string) []string {
	if b.MaxTokens <= 0 {
		return nil
	}
	var out []string
	used := 0
	for _, it := range items {
		t := ApproxTokens(it)
		if used+t > b.MaxTokens {
			continue
		}
		out = append(out, it)
		used += t
	}
	return out
}
