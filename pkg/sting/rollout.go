package sting

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Rollout-evidence logging is the data substrate every future Sting
// improvement loop consumes — filtered SFT (RFT), preference pairs,
// calibration. Ghost is unusually lucky here: most router teams must
// pay humans to grade rollouts, but our executions run against reality
// and return evidence. A rollout graded by its own evidence is a free
// (state, action, reward) triple — reinforcement learning with
// verifiable rewards, without the RL machinery.
//
// Privacy: a rollout contains the raw user query. The log lives next
// to conversation history (workspace state, 0600) and is OFF unless
// the operator sets a path. Same sensitivity, same handling.

// MaxRolloutBytes caps the log: constrained devices must never have
// disk eaten by telemetry. Oldest data is dropped by rotation.
const MaxRolloutBytes = 2 << 20 // 2MB

// Verdict grades one executed call from the facts the runtime already
// has. It deliberately separates routing quality from world luck: a
// grounded, authorized call that the world fails (timeout, provider
// down) is still a GOOD routing example — punishing it would teach the
// router superstition.
type Verdict string

const (
	// VerdictRoutedOK: gate-passed, executed, runtime produced an answer.
	VerdictRoutedOK Verdict = "routed-ok"
	// VerdictWorldFailed: gate-passed and executed, but the tool reported
	// a world-side failure (network, provider, not-found). Routing was
	// correct; keep for SFT, it teaches the right mapping.
	VerdictWorldFailed Verdict = "world-failed"
	// VerdictRouterFault: the call was malformed (unknown tool, missing
	// required arg, schema rejection) — the gate should have caught it.
	// Never train on these; mine them for gate tests instead.
	VerdictRouterFault Verdict = "router-fault"
	// VerdictEscalated: no act (gate refuse, low confidence, sidecar
	// down). Safe abstention. Coverage (false abstention) is measured
	// only with labels, in harness — never inferred here.
	VerdictEscalated Verdict = "escalated"
)

// Reward maps a rollout to a scalar for filtering and future
// preference learning. Asymmetric by design: on a personal AI,
// a wrong act costs far more than a miss, so fabrication is -1 while
// abstention is 0. Misses (false abstention) are invisible here by
// construction — the harness measures those.
func Reward(v Verdict) float64 {
	switch v {
	case VerdictRoutedOK, VerdictWorldFailed:
		return 1
	case VerdictEscalated:
		return 0
	default:
		return -1
	}
}

// Rollout is one router turn, fully graded.
type Rollout struct {
	At         string         `json:"at"`
	Query      string         `json:"query"`
	Tools      []string       `json:"tools"`
	Triage     string         `json:"triage,omitempty"`
	Proposed   []FunctionCall `json:"proposed,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Gate       string         `json:"gate"`
	// Scope is the ledger scope the gate measured against, so rollouts
	// can ratchet the same scopes the gate reads.
	Scope   string      `json:"scope,omitempty"`
	Acted   []ActedCall `json:"acted,omitempty"`
	Weights string      `json:"weights"`
}

// ActedCall pairs an executed call with its runtime verdict.
type ActedCall struct {
	Call    FunctionCall `json:"call"`
	Verdict Verdict      `json:"verdict"`
	Reward  float64      `json:"reward"`
}

// Recorder appends rollouts as JSONL. Nil-safe and error-swallowing by
// contract: logging must never fail, slow, or alter a turn. A nil
// *Recorder (disabled) is a no-op.
type Recorder struct {
	path    string
	weights string
	current *Rollout
}

// NewRecorder returns a recorder writing to path, or nil when path is
// empty (disabled). Weights tags which head produced the rollouts
// ("base" or the tuned archive name) so ledgers never mix populations.
func NewRecorder(path, weights string) *Recorder {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if weights == "" {
		weights = "base"
	}
	return &Recorder{path: path, weights: weights}
}

// Begin opens a rollout for one query over the offered tool names.
func (r *Recorder) Begin(query string, tools []string) {
	if r == nil {
		return
	}
	r.current = &Rollout{
		At:      time.Now().UTC().Format(time.RFC3339),
		Query:   query,
		Tools:   tools,
		Weights: r.weights,
	}
}

// Triaged records the pre-generation verdict. Ask/Refuse flush
// immediately: no generative call follows, so the rollout is complete.
func (r *Recorder) Triaged(o TriageOutcome) {
	if r == nil || r.current == nil {
		return
	}
	r.current.Triage = string(o.Verdict) + ":" + o.Reason
	if o.Verdict != TriageAct {
		r.current.Gate = "triage-" + string(o.Verdict)
		r.Flush()
	}
}

// Propose records the engine response and gate verdict.
func (r *Recorder) Propose(resp *CompleteResponse, d Decision) {
	if r == nil || r.current == nil {
		return
	}
	if resp != nil {
		r.current.Proposed = resp.FunctionCalls
		r.current.Confidence = resp.Confidence
	}
	r.current.Gate = d.Reason
	r.current.Scope = d.Scope
	if d.Escalate {
		r.Flush()
	}
}

// Acted records one executed call. worldFailed must be true when the
// tool ran but reported a world-side (not routing-side) failure; when
// in doubt pass false — a pessimistic log beats a flattering one.
func (r *Recorder) Acted(call FunctionCall, executed, worldFailed bool) {
	if r == nil || r.current == nil {
		return
	}
	v := VerdictRoutedOK
	if !executed {
		v = VerdictRouterFault
	} else if worldFailed {
		v = VerdictWorldFailed
	}
	r.current.Acted = append(r.current.Acted, ActedCall{
		Call:    call,
		Verdict: v,
		Reward:  Reward(v),
	})
}

// Flush appends the open rollout and closes it. Best-effort: any IO
// error drops the rollout silently (a lost sample beats a failed turn),
// and the file rotates past MaxRolloutBytes by starting over — recent
// behavior outranks ancient behavior for calibration anyway.
func (r *Recorder) Flush() {
	if r == nil || r.current == nil {
		return
	}
	roll := r.current
	r.current = nil
	raw, err := json.Marshal(roll)
	if err != nil {
		return
	}
	if st, err := os.Stat(r.path); err == nil && st.Size() > MaxRolloutBytes {
		_ = os.Remove(r.path)
	}
	_ = os.MkdirAll(filepath.Dir(r.path), 0700)
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

// AggregateRollouts folds the rollout-evidence log into tr and returns
// the number of graded turns recorded. Each turn contributes once for
// its scope: correct when it executed at least one call and no call was
// a router fault (a world failure still counts — routing was right); a
// router fault is incorrect. Turns that never executed (escalated,
// no-call) carry no routing evidence and are skipped.
//
// Precision from rollouts is an upper bound: false abstentions are
// invisible here by construction, which is exactly why the harness
// measures those. A corrupt line is skipped, never fatal — a damaged log
// must not cost the whole ledger.
func AggregateRollouts(tr *Tracker, path string) (int, error) {
	if tr == nil {
		return 0, fmt.Errorf("sting: nil tracker")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxRolloutBytes)
	recorded := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var roll Rollout
		if err := json.Unmarshal(line, &roll); err != nil {
			continue
		}
		if roll.Scope == "" || len(roll.Acted) == 0 {
			continue
		}
		correct := true
		for _, a := range roll.Acted {
			if a.Verdict == VerdictRouterFault {
				correct = false
				break
			}
		}
		tr.Record(roll.Scope, correct)
		recorded++
	}
	if err := scanner.Err(); err != nil {
		return recorded, err
	}
	return recorded, nil
}
