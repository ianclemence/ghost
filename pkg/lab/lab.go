// Package lab is Ghost's evaluation-lab harness: cheap deterministic
// gates in front of expensive model-judged passes, with append-only
// per-case records, cost/duration budgets, and machine-readable output.
//
// Model: free pre-scan → paid passes → revalidate → triage → export.
// Every paid step appends to the case record (model, tokens, cost,
// session, wave); reruns are strict improvements, never overwrites.
// Interrupted runs resume by skipping completed records. Costs unknown
// are never treated as zero: a set budget with unknown cost fails
// closed.
package lab

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Verdict is a second-pass finding verdict.
type Verdict string

const (
	VerdictTruePositive  Verdict = "true_positive"
	VerdictFalsePositive Verdict = "false_positive"
	VerdictFixed         Verdict = "fixed"
	VerdictUncertain     Verdict = "uncertain"
)

// Triage buckets actionability; Skip is explicit, never default.
type Triage string

const (
	TriageP0   Triage = "P0"
	TriageP1   Triage = "P1"
	TriageP2   Triage = "P2"
	TriageSkip Triage = "skip"
)

// Usage carries measured model consumption for one step. CostUnknown
// means the provider reported no price: budgets treat it as
// unenforceable (fail-closed), never as free.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	CostUSD          float64
	CostUnknown      bool
}

// Step is one appended history entry: what ran, on what, at what cost.
type Step struct {
	At         string  `json:"at"`
	Phase      string  `json:"phase"`
	Model      string  `json:"model"`
	Provider   string  `json:"provider"`
	Session    string  `json:"session"`
	Wave       int     `json:"wave"`
	Verdict    string  `json:"verdict"`
	Usage      Usage   `json:"usage"`
	Triage     Triage  `json:"triage,omitempty"`
	Revalidate Verdict `json:"revalidate,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// CaseRecord is the unit of work: one eval case with an append-only
// history. Status derives from steps, never stored separately.
type CaseRecord struct {
	ID     string `json:"id"`
	Kind   string `json:"kind,omitempty"`
	Steps  []Step `json:"steps"`
	Status string `json:"status"` // pending | passed | failed | error
}

// Options bounds a lab run.
type Options struct {
	MaxCostUSD       float64
	MaxDuration      time.Duration
	Limit            int // max cases to touch, 0 = all
	Filter           string
	Wave             int
	Session          string
	AllowUnknownCost bool
}

// ErrBoundary reports a hit budget: re-running resumes past completed
// records, so a boundary is a pause, not a loss.
var ErrBoundary = errors.New("lab: cost or duration boundary reached")

// Run tracks budget state across steps.
type Run struct {
	Opts    Options
	started time.Time
	Spent   float64
	Touched int
}

// NewRun starts budget accounting.
func NewRun(opts Options) *Run {
	return &Run{Opts: opts, started: time.Now().UTC()}
}

// Check enforces MaxCostUSD/MaxDuration before each paid step. Unknown
// cost with a set budget fails closed unless AllowUnknownCost.
func (r *Run) Check(u Usage) error {
	if r.Opts.MaxDuration > 0 && time.Since(r.started) > r.Opts.MaxDuration {
		return fmt.Errorf("%w: duration %s exceeds %s", ErrBoundary, time.Since(r.started).Round(time.Second), r.Opts.MaxDuration)
	}
	if u.CostUnknown {
		if r.Opts.MaxCostUSD > 0 && !r.Opts.AllowUnknownCost {
			return fmt.Errorf("lab: cost unknown with $%.2f budget set (fail-closed; set AllowUnknownCost to proceed)", r.Opts.MaxCostUSD)
		}
		return nil
	}
	if r.Opts.MaxCostUSD > 0 && r.Spent+u.CostUSD > r.Opts.MaxCostUSD {
		return fmt.Errorf("%w: $%.2f + $%.2f exceeds $%.2f", ErrBoundary, r.Spent, u.CostUSD, r.Opts.MaxCostUSD)
	}
	return nil
}

// Commit records spent cost after a paid step.
func (r *Run) Commit(u Usage) {
	if !u.CostUnknown {
		r.Spent += u.CostUSD
	}
	r.Touched++
	if r.Opts.Limit > 0 && r.Touched >= r.Opts.Limit {
		return
	}
}

// AtLimit reports whether the case limit is reached.
func (r *Run) AtLimit() bool {
	return r.Opts.Limit > 0 && r.Touched >= r.Opts.Limit
}

// GateCheck is one free deterministic check: no model, no network,
// milliseconds. Name explains itself in reports.
type GateCheck struct {
	Name string
	Run  func() error
}

// GateResult is the free-gate outcome.
type GateResult struct {
	Passed []string
	Failed map[string]string
}

// RunFreeGate executes deterministic checks. A failing gate blocks paid
// passes: never start an expensive run while cheap coverage fails.
func RunFreeGate(checks []GateCheck) GateResult {
	res := GateResult{Failed: map[string]string{}}
	for _, c := range checks {
		if err := c.Run(); err != nil {
			res.Failed[c.Name] = err.Error()
			continue
		}
		res.Passed = append(res.Passed, c.Name)
	}
	sort.Strings(res.Passed)
	return res
}

// OK reports whether every gate passed.
func (g GateResult) OK() bool { return len(g.Failed) == 0 }

// Store persists case records under dir, one JSON file per case.
type Store struct {
	Dir string
}

// Open creates the record dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) path(id string) string {
	safe := ""
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			safe += string(r)
		} else {
			safe += "_"
		}
	}
	return filepath.Join(s.Dir, safe+".json")
}

// Load reads a record; missing IDs return a pending record, never an error.
func (s *Store) Load(id string) (*CaseRecord, error) {
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return &CaseRecord{ID: id, Status: "pending"}, nil
		}
		return nil, err
	}
	var rec CaseRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("parse lab record %s: %w", id, err)
	}
	return &rec, nil
}

// AppendStep appends one step and derives status atomically (temp +
// rename). completed verdicts: passed unless any failed/error step.
func (s *Store) AppendStep(id string, step Step) (*CaseRecord, error) {
	rec, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	if step.At == "" {
		step.At = time.Now().UTC().Format(time.RFC3339)
	}
	rec.Steps = append(rec.Steps, step)
	rec.Status = deriveStatus(rec.Steps)
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".record-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, s.path(id)); err != nil {
		return nil, err
	}
	return rec, nil
}

func deriveStatus(steps []Step) string {
	if len(steps) == 0 {
		return "pending"
	}
	for _, st := range steps {
		if st.Verdict == "error" {
			return "error"
		}
		if st.Verdict == "failed" {
			return "failed"
		}
	}
	last := steps[len(steps)-1]
	if last.Verdict == "passed" {
		return "passed"
	}
	return "pending"
}

// Pending lists records without a terminal verdict: the resume set.
func (s *Store) Pending(ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		rec, err := s.Load(id)
		if err != nil {
			return nil, err
		}
		if rec.Status == "pending" || rec.Status == "error" {
			out = append(out, id)
		}
	}
	return out, nil
}

// WaveResult is one case outcome in a red-team wave.
type WaveResult struct {
	CaseID  string
	Verdict string // passed | failed | error
}

// CompareWaves diffs two waves: regressions (passed then not) must be
// empty for a wave to graduate; fixes (failed then passed) are the
// wave's yield. Wave N+1 must not regress wave N.
func CompareWaves(prev, cur []WaveResult) (regressions, fixes []string) {
	before := map[string]string{}
	for _, r := range prev {
		before[r.CaseID] = r.Verdict
	}
	seen := map[string]bool{}
	for _, r := range cur {
		seen[r.CaseID] = true
		old, ok := before[r.CaseID]
		if !ok {
			continue // new case: neither regression nor fix
		}
		if old == "passed" && r.Verdict != "passed" {
			regressions = append(regressions, r.CaseID)
		}
		if old != "passed" && r.Verdict == "passed" {
			fixes = append(fixes, r.CaseID)
		}
	}
	_ = seen
	return regressions, fixes
}

// Event is one machine-readable log line.
type Event struct {
	At    string                 `json:"at"`
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data,omitempty"`
}

// Emitter writes JSONL events: agents and CI parse the stream.
type Emitter struct {
	w io.Writer
}

// NewEmitter wraps w (nil discards).
func NewEmitter(w io.Writer) *Emitter {
	if w == nil {
		w = io.Discard
	}
	return &Emitter{w: w}
}

// Emit writes one event line. Never fails the run on write errors.
func (e *Emitter) Emit(event string, data map[string]interface{}) {
	raw, _ := json.Marshal(Event{At: time.Now().UTC().Format(time.RFC3339), Event: event, Data: data})
	fmt.Fprintln(e.w, string(raw))
}
