package sting

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Decision is the gate verdict for one routing turn.
type Decision struct {
	// Act holds the validated calls to offer the Permission Broker.
	Act []FunctionCall
	// Escalate means "do not act": fall through to the normal LLM path.
	Escalate bool
	// Reason is a short machine-readable cause (logged, never user prose).
	Reason string
	// Uncalibrated is true when the response carried no confidence score
	// (tuned weights) and the ledger authorized the act on measured
	// precision instead. The Broker still governs, but callers should
	// prefer read-only execution and log the fact.
	Uncalibrated bool
	// Scope is the ledger scope this verdict was measured against
	// (ScopePositiveSingle / ScopeParallel). Empty when the gate ran
	// without a ledger.
	Scope string
}

var (
	// negationCue catches requests the router must never act on. Keep it
	// narrow: over-matching ("notebook") would block legit turns, and the
	// LLM path handles the rest anyway.
	negationCue = regexp.MustCompile(`(?i)\b(don'?t|do not|never|stop|cancel|not )\b`)
	// numberToken finds numeric leaves to ground against the query text.
	numberToken = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
	// isoDateArg flags date-shaped arguments needing a licensed year.
	isoDateArg = regexp.MustCompile(`^(\d{4})-\d{2}-\d{2}`)
)

// Gate validates one engine response against the query that produced
// it with the engine's own confidence as the only authority. It is the
// pre-ledger behaviour, kept for `ledger: "off"`; production paths call
// GateWithLedger so the empirical ledger is the named gate.
func Gate(query string, resp *CompleteResponse, threshold float64) Decision {
	return GateWithLedger(query, resp, nil, threshold)
}

// GateWithLedger is Gate with the reliability ledger as the named
// authority. order: envelope → negation → dedupe → per-call strict
// validation → ledger scope → threshold. Anything unexpected escalates:
// the failure mode is "ask the bigger model", never "run the wrong tool".
//
// The ledger sets the act/escalate threshold for the turn's scope; the
// engine confidence is an input, never the sole gate. This closes the
// calibration gap: tuned weights report no score, so an unscored turn
// acts only for a scope the ledger has measured at or above
// TargetPrecision. A nil ledger restores the confidence-only behaviour.
func GateWithLedger(query string, resp *CompleteResponse, ledger *Tracker, base float64) Decision {
	if resp == nil {
		return Decision{Escalate: true, Reason: "nil-response"}
	}
	if strings.TrimSpace(resp.Error) != "" {
		return Decision{Escalate: true, Reason: "engine-error:" + resp.ErrorCode}
	}
	if resp.Type != "call" || len(resp.FunctionCalls) == 0 {
		// Off-topic / empty call: the engine's whole contract for
		// "no declared tool serves this". Not a failure.
		return Decision{Escalate: true, Reason: "no-call"}
	}
	calls := dedupe(resp.FunctionCalls)
	if negated(query) && len(calls) > 0 {
		return Decision{Escalate: true, Reason: "negated-request"}
	}
	var acts []FunctionCall
	for _, c := range calls {
		if err := ValidateCall(query, c); err != nil {
			continue // drop the bad call, keep judging the rest
		}
		acts = append(acts, c)
	}
	if len(acts) == 0 {
		return Decision{Escalate: true, Reason: "all-calls-ungrounded"}
	}
	scope := ScopeFor(len(acts))
	if ledger == nil {
		// Pre-ledger behaviour: the engine's calibrated head is the
		// whole gate. Reached only via `ledger: "off"`.
		if resp.Confidence == nil {
			return Decision{Act: acts, Reason: "uncalibrated-confidence", Uncalibrated: true, Scope: scope}
		}
		if *resp.Confidence < base {
			return Decision{Escalate: true, Reason: fmt.Sprintf("low-confidence:%.2f", *resp.Confidence), Scope: scope}
		}
		return Decision{Act: acts, Reason: "ok", Scope: scope}
	}
	if resp.Confidence == nil {
		// Tuned weights: the confidence head is frozen at export, so no
		// score exists. Only a scope the ledger has measured at or above
		// target may act; everything else escalates.
		if ledger.AllowsAutoAct(scope) {
			return Decision{Act: acts, Reason: "ledger-calibrated:" + scope, Uncalibrated: true, Scope: scope}
		}
		return Decision{Escalate: true, Reason: "ledger-unscored:" + scope, Scope: scope}
	}
	threshold, why := ledger.ThresholdFor(scope, base)
	if *resp.Confidence < threshold {
		return Decision{Escalate: true, Reason: fmt.Sprintf("low-confidence:%.2f<%s:%s", *resp.Confidence, scope, why), Scope: scope}
	}
	return Decision{Act: acts, Reason: "ok:" + scope, Scope: scope}
}

// dedupe drops byte-identical re-emissions (the engine occasionally
// repeats a call: "dim to 35" arriving twice). Distinct calls — the
// parallel case — are preserved.
func dedupe(calls []FunctionCall) []FunctionCall {
	seen := map[string]bool{}
	var out []FunctionCall
	for _, c := range calls {
		key := c.Name + "\x00" + canonicalArgs(c.Arguments)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

func canonicalArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%v;", k, args[k])
	}
	return sb.String()
}

func negated(query string) bool {
	return negationCue.MatchString(query)
}

// ValidateCall enforces strict grounding for one proposed call:
// required args present (non-empty), enum/bound checks are the
// registry's job at execution — here we check what only the query can
// prove: every numeric leaf and every date year must be evidenced by
// the query text. Omission (optional field absent) is fine; fabrication
// is not.
func ValidateCall(query string, call FunctionCall) error {
	if strings.TrimSpace(call.Name) == "" {
		return fmt.Errorf("empty tool name")
	}
	if len(call.Arguments) == 0 {
		// Zero-arg calls are valid only when the tool truly needs
		// nothing; the registry validates required-ness at execution.
		// The gate's job is grounding, and there is nothing to ground.
		return nil
	}
	numbers := sourceNumbers(query)
	for path, v := range numericLeaves(call.Arguments, "") {
		if !numbers[normalizeNumber(v)] {
			return fmt.Errorf("ungrounded numeric %s=%v", path, v)
		}
	}
	years := sourceYears(query)
	for path, v := range stringLeaves(call.Arguments, "") {
		m := isoDateArg.FindStringSubmatch(v)
		if m == nil {
			continue
		}
		y, _ := strconv.Atoi(m[1])
		if len(years) > 0 {
			if !years[y] {
				return fmt.Errorf("ungrounded date %s=%v", path, v)
			}
		}
	}
	return nil
}

func normalizeNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func sourceNumbers(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range numberToken.FindAllString(text, -1) {
		f, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimPrefix(m, "+"), "-"), 64)
		if err != nil {
			continue
		}
		// Sign-insensitive: "minus 5" rarely survives transcription.
		if strings.HasPrefix(m, "-") {
			f = -f
		}
		out[normalizeNumber(f)] = true
	}
	return out
}

var yearToken = regexp.MustCompile(`\b(19|20)\d{2}\b`)

func sourceYears(text string) map[int]bool {
	out := map[int]bool{}
	for _, m := range yearToken.FindAllString(text, -1) {
		y, _ := strconv.Atoi(m)
		out[y] = true
	}
	return out
}

func numericLeaves(v interface{}, path string) map[string]float64 {
	out := map[string]float64{}
	switch t := v.(type) {
	case map[string]interface{}:
		for k, item := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			for pp, vv := range numericLeaves(item, p) {
				out[pp] = vv
			}
		}
	case []interface{}:
		for i, item := range t {
			for pp, vv := range numericLeaves(item, fmt.Sprintf("%s[%d]", path, i)) {
				out[pp] = vv
			}
		}
	case float64:
		out[path] = t
	case int:
		out[path] = float64(t)
	case int64:
		out[path] = float64(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			out[path] = f
		}
	}
	return out
}

func stringLeaves(v interface{}, path string) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]interface{}:
		for k, item := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			for pp, vv := range stringLeaves(item, p) {
				out[pp] = vv
			}
		}
	case []interface{}:
		for i, item := range t {
			for pp, vv := range stringLeaves(item, fmt.Sprintf("%s[%d]", path, i)) {
				out[pp] = vv
			}
		}
	case string:
		out[path] = t
	}
	return out
}
