package sting

import (
	"regexp"
	"sort"
	"strings"
)

// Triage is the Jev-shaped front door: a closed-set decision over the
// turn BEFORE any generative call. Three verdicts, no middle:
//
//	Act    — route to the generative call (args look present)
//	Ask    — skip generation; the turn needs a missing required input
//	         first (the readiness layer asks the precise question)
//	Refuse — skip generation; negated or off-topic, nothing to route
//
// Safety comes from what triage does NOT do: Ask and Refuse both return
// the turn to the normal loop (handled=false). Triage can only SAVE a
// wasted sidecar round trip or aim the next question — it can never
// drop a turn, because only the gate + Broker ever authorize action,
// and only the loop answers the user. A wrong Refuse costs one skipped
// generation that the loop performs anyway; a wrong Act costs nothing
// the gate wouldn't already catch.
type TriageVerdict string

const (
	TriageAct    TriageVerdict = "act"
	TriageAsk    TriageVerdict = "ask"
	TriageRefuse TriageVerdict = "refuse"
)

// TriageOutcome pairs the verdict with routing detail.
type TriageOutcome struct {
	Verdict TriageVerdict `json:"verdict"`
	// Tool names the single best-matching tool for Ask, else "".
	Tool string `json:"tool,omitempty"`
	// Missing lists required params with no query evidence, for Ask.
	Missing []string `json:"missing,omitempty"`
	// Reason is machine-readable cause (logged, never user prose).
	Reason string `json:"reason"`
}

var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "for": true, "of": true,
	"in": true, "on": true, "and": true, "or": true, "is": true, "it": true,
	"me": true, "my": true, "you": true, "your": true, "i": true, "do": true,
	"please": true, "with": true, "at": true, "by": true, "from": true,
	"that": true, "this": true, "what": true, "how": true, "much": true,
}

var wordToken = regexp.MustCompile(`[a-z0-9]+`)

// enumContains reports whether any enum value appears as a query word,
// across the slice shapes registries use ([]string, []interface{}).
func enumContains(enum interface{}, words map[string]bool) bool {
	switch list := enum.(type) {
	case []string:
		for _, s := range list {
			if words[strings.ToLower(s)] {
				return true
			}
		}
	case []interface{}:
		for _, e := range list {
			if s, ok := e.(string); ok && words[strings.ToLower(s)] {
				return true
			}
		}
	}
	return false
}

// Triage classifies one turn against the offered tools. Deterministic,
// no model, no network — the whole point is deciding before spending
// inference.
func Triage(query string, tools []ToolSchema) TriageOutcome {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return TriageOutcome{Verdict: TriageRefuse, Reason: "empty-query"}
	}
	if negated(query) {
		return TriageOutcome{Verdict: TriageRefuse, Reason: "negated"}
	}
	type match struct {
		name    string
		score   int
		missing []string
	}
	var matches []match
	for _, t := range tools {
		keys := toolKeywords(t)
		hits := 0
		for _, w := range queryWords(q) {
			if keys[w] {
				hits++
			}
		}
		if hits == 0 {
			continue
		}
		matches = append(matches, match{
			name:    t.Name,
			score:   hits,
			missing: missingRequired(q, t),
		})
	}
	if len(matches) == 0 {
		// No keyword overlap: still Act. Off-topic refusal is the
		// engine's core competence (empty call []); a keyword matcher
		// must never outrank it. False-act costs one cheap local call
		// the gate escalates; false-refuse would push the turn to a
		// bigger model. Asymmetry decides: bias to Act.
		return TriageOutcome{Verdict: TriageAct, Reason: "no-match-try-generate"}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	best := matches[0]
	if len(best.missing) > 0 {
		return TriageOutcome{Verdict: TriageAsk, Tool: best.name,
			Missing: best.missing, Reason: "missing-required"}
	}
	return TriageOutcome{Verdict: TriageAct, Tool: best.name, Reason: "args-present"}
}

// toolKeywords derives matchable words from a tool's name and
// description: snake_case tokens plus distinctive description words.
func toolKeywords(t ToolSchema) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(t.Name, "_") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" && !stopwords[part] {
			out[part] = true
		}
	}
	for _, w := range queryWords(strings.ToLower(t.Description)) {
		if !stopwords[w] && len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

func queryWords(q string) []string {
	return wordToken.FindAllString(strings.ToLower(q), -1)
}

// missingRequired lists required params with no query evidence:
// numerics need a query number present, enums need a short query to
// carry no value implicitly, strings need non-keyword content beyond
// the tool match itself. The gate enforces exact membership and
// bounds later; triage only decides whether generation is worthwhile.
func missingRequired(query string, t ToolSchema) []string {
	props, _ := t.Parameters["properties"].(map[string]interface{})
	if props == nil {
		return nil
	}
	var req []string
	switch v := t.Parameters["required"].(type) {
	case []string:
		req = v
	case []interface{}:
		for _, r := range v {
			if s, ok := r.(string); ok {
				req = append(req, s)
			}
		}
	}
	nums := sourceNumbers(query)
	words := map[string]bool{}
	for _, w := range queryWords(query) {
		words[w] = true
	}
	keys := toolKeywords(t)
	var missing []string
	for _, name := range req {
		spec, _ := props[name].(map[string]interface{})
		if spec == nil {
			continue
		}
		typ, _ := spec["type"].(string)
		switch typ {
		case "integer", "number":
			// Numeric required: presence suffices for triage (the
			// gate checks bounds and exact grounding later).
			if len(nums) == 0 {
				missing = append(missing, name)
			}
		default:
			if enum, ok := spec["enum"]; ok {
				hit := enumContains(enum, words)
				// Enum required: triage asks only when NO enum value
				// appears AND the query is too short to carry one
				// implicitly. (The gate enforces exact membership later.)
				if !hit && len(queryWords(query)) <= 3 {
					missing = append(missing, name)
				}
				continue
			}
			// String required: missing when the query holds nothing
			// beyond tool keywords ("search the web" vs "search X").
			content := false
			for _, w := range queryWords(query) {
				if !keys[w] && !stopwords[w] {
					content = true
				}
			}
			if !content {
				missing = append(missing, name)
			}
		}
	}
	return missing
}
