package workflow

import (
	"context"
	"sort"
	"strings"
)

// Thread is one inbox item. Body is fetched lazily: headers-only triage
// ends waiting/reference/noise threads without ever reading the body.
type Thread struct {
	ID      string `json:"id"`
	Sender  string `json:"sender"`
	Subject string `json:"subject"`
	Snippet string `json:"snippet"`
	Body    string `json:"body,omitempty"`
	Fetched bool   `json:"fetched,omitempty"`
}

// Disposition is the typed verdict per thread.
type Disposition string

const (
	DispUrgentReply   Disposition = "urgent_reply"
	DispReply         Disposition = "reply"
	DispActionNoReply Disposition = "action_no_reply"
	DispWaiting       Disposition = "waiting"
	DispReference     Disposition = "reference"
	DispNoise         Disposition = "noise"
	DispEscalated     Disposition = "escalated"
)

// TriageResult is the per-thread compiled outcome.
type TriageResult struct {
	ThreadID    string      `json:"thread_id"`
	Disposition Disposition `json:"disposition"`
	Confidence  float64     `json:"confidence"`
	BodyFetched bool        `json:"body_fetched"`
	Reason      string      `json:"reason,omitempty"`
}

// CouldStopQuestion is the Jev-tuned wording (validated offline 2026-10-14:
// "invoice due Friday" noul 0.96). Threshold lives in code, not here.
func CouldStopQuestion() Question {
	return Question{
		ID:           "could_stop",
		Kind:         QNoul,
		Instructions: "This thread needs the owner's action or decision (deadline, blocker, money, or security).",
	}
}

// DispositionQuestion lists disposition criteria once, in the owner's words.
func DispositionQuestion() Question {
	return Question{
		ID:           "disposition",
		Kind:         QChoice,
		Instructions: "Which inbox disposition fits this thread",
		Criteria: map[string]string{
			"urgent_reply":    "Needs reply fast, deadline or blocker",
			"reply":           "Needs a normal reply",
			"action_no_reply": "Needs action but no reply",
			"waiting":         "Waiting on someone else",
			"reference":       "FYI only",
			"noise":           "Newsletter or noise",
		},
	}
}

// stopperScore ranks threads so stoppers are researched first.
func stopperScore(t Thread) int {
	text := strings.ToLower(t.Sender + " " + t.Subject + " " + t.Snippet)
	score := 0
	for _, m := range stopperMarks {
		if strings.Contains(text, m) {
			score += 2
		}
	}
	for _, m := range noiseMarks {
		if strings.Contains(text, m) {
			score -= 3
		}
	}
	return score
}

// OrderStoppersFirst sorts a copy: potential stoppers first, noise last.
func OrderStoppersFirst(in []Thread) []Thread {
	out := append([]Thread(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return stopperScore(out[i]) > stopperScore(out[j]) })
	return out
}

// Triage compiles the inbox routine: could-stop filter, stoppers-first
// ordering, early-stop on headers, lazy body fetch, escalate-on-uncertain.
// Fetch is called at most once per thread and only for reply/action
// candidates. Returns results in stoppers-first order.
func Triage(ctx context.Context, be Backend, threads []Thread, fetch func(Thread) string, noulCut, choiceConf float64) []TriageResult {
	if be == nil {
		be = RuleBackend{}
	}
	if noulCut <= 0 {
		noulCut = 0.5
	}
	if choiceConf <= 0 {
		choiceConf = 0.6
	}
	ordered := OrderStoppersFirst(threads)
	out := make([]TriageResult, 0, len(ordered))
	couldStop := CouldStopQuestion()
	disposition := DispositionQuestion()
	for _, t := range ordered {
		state := map[string]interface{}{
			"sender": t.Sender, "subject": t.Subject, "snippet": t.Snippet,
		}
		ans, err := be.Evaluate(ctx, state, couldStop)
		if err != nil {
			out = append(out, TriageResult{ThreadID: t.ID, Disposition: DispEscalated, Reason: err.Error()})
			continue
		}
		_, v, err := CheckNoul(ans)
		if err != nil {
			out = append(out, TriageResult{ThreadID: t.ID, Disposition: DispEscalated, Reason: err.Error()})
			continue
		}
		// Early-stop path: low could-stop score from headers alone. No body
		// fetch, no disposition call. This is the 30%-skipped-work lever.
		if v < 0.3 {
			d := DispNoise
			if strings.Contains(strings.ToLower(t.Sender+t.Subject), "waiting") {
				d = DispWaiting
			} else if v >= 0.15 {
				d = DispReference
			}
			out = append(out, TriageResult{ThreadID: t.ID, Disposition: d, Confidence: 1 - v})
			continue
		}
		dans, err := be.Evaluate(ctx, state, disposition)
		if err != nil {
			out = append(out, TriageResult{ThreadID: t.ID, Disposition: DispEscalated, Reason: err.Error()})
			continue
		}
		choice, err := CheckChoice(dans, choiceConf)
		if err != nil {
			// Escalate-on-uncertain: exactly what Jev probe showed
			// (urgent_reply conf 0.56 < 0.60 gate => human batch).
			out = append(out, TriageResult{ThreadID: t.ID, Disposition: DispEscalated, Confidence: dans.Confidence, Reason: err.Error()})
			continue
		}
		d := Disposition(choice)
		fetched := false
		// Lazy enrichment: full body only when a reply/action needs voice
		// calibration. Waiting/reference/noise never pay for it.
		if (d == DispReply || d == DispUrgentReply || d == DispActionNoReply) && fetch != nil && !t.Fetched {
			_ = fetch(t)
			fetched = true
		}
		out = append(out, TriageResult{ThreadID: t.ID, Disposition: d, Confidence: dans.Confidence, BodyFetched: fetched})
	}
	return out
}
