package workflow

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Gate is a deterministic predicate over state. Gates are pure Go so
// threshold retunes are code diffs, replayable without re-research.
type Gate func(state map[string]interface{}) bool

// Outcome is one node execution: what ran, what it decided, whether a
// human must review. Recorded to the ledger by the caller.
type Outcome struct {
	NodeID     string                 `json:"node_id"`
	NodeType   NodeType               `json:"node_type"`
	Decision   string                 `json:"decision,omitempty"`
	Confidence float64                `json:"confidence,omitempty"`
	Escalated  bool                   `json:"escalated,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	State      map[string]interface{} `json:"state,omitempty"`
	LatencyMs  int64                  `json:"latency_ms"`
}

// Runner executes workflows. Backend answers typed questions; Gates decide
// what follows. Escalate marks the run for human review and stops the chain.
type Runner struct {
	Backend Backend
	Gates   map[string]Gate
	// ChoiceThreshold is the minimum confidence to accept a Choice answer.
	ChoiceThreshold float64
	// NoulThreshold is the could-stop cutoff (e.g. 0.5).
	NoulThreshold float64
	// OnOutcome observes every node outcome (ledger writer in production).
	OnOutcome func(Outcome)
}

func (r *Runner) threshold() float64 {
	if r.ChoiceThreshold <= 0 {
		return 0.6
	}
	return r.ChoiceThreshold
}

func (r *Runner) noulCut() float64 {
	if r.NoulThreshold <= 0 {
		return 0.5
	}
	return r.NoulThreshold
}

func (r *Runner) emit(o Outcome) {
	if r.OnOutcome != nil {
		r.OnOutcome(o)
	}
}

// Run executes the workflow root against state. Returns final state and
// whether the run escalated to a human.
func (r *Runner) Run(ctx context.Context, w *Workflow, state map[string]interface{}) (map[string]interface{}, bool, error) {
	if err := Validate(w); err != nil {
		return state, false, err
	}
	if state == nil {
		state = map[string]interface{}{}
	}
	escalated, err := r.execNode(ctx, &w.Root, state)
	return state, escalated, err
}

func (r *Runner) execNode(ctx context.Context, n *Node, state map[string]interface{}) (bool, error) {
	start := time.Now()
	finish := func(o Outcome) {
		o.LatencyMs = time.Since(start).Milliseconds()
		r.emit(o)
	}
	switch n.Type {
	case NodeChain:
		for i := range n.Steps {
			esc, err := r.execNode(ctx, &n.Steps[i], state)
			if err != nil || esc {
				return esc, err
			}
		}
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: "ok"})
		return false, nil
	case NodeJudge:
		be := r.Backend
		if be == nil {
			be = RuleBackend{}
		}
		ans, err := be.Evaluate(ctx, state, *n.Question)
		if err != nil {
			finish(Outcome{NodeID: n.ID, NodeType: n.Type, Escalated: true, Reason: err.Error()})
			return true, nil
		}
		switch n.Question.Kind {
		case QNoul:
			ok, v, err := CheckNoul(ans)
			if err != nil || !ok {
				reason := "noul range check failed"
				if err != nil {
					reason = err.Error()
				}
				finish(Outcome{NodeID: n.ID, NodeType: n.Type, Escalated: true, Reason: reason})
				return true, nil
			}
			key := "judge_" + n.Question.ID
			state[key] = v
			dec := "no"
			if v >= r.noulCut() {
				dec = "yes"
			}
			finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: dec, Confidence: v})
			return false, nil
		default:
			choice, err := CheckChoice(ans, r.threshold())
			if err != nil {
				finish(Outcome{NodeID: n.ID, NodeType: n.Type, Escalated: true, Reason: err.Error()})
				return true, nil
			}
			state["judge_"+n.Question.ID] = choice
			state["judge_"+n.Question.ID+"_conf"] = ans.Confidence
			finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: choice, Confidence: ans.Confidence})
			return false, nil
		}
	case NodeCode:
		g, ok := r.Gates[n.Gate]
		if !ok {
			finish(Outcome{NodeID: n.ID, NodeType: n.Type, Escalated: true, Reason: "unknown gate " + n.Gate})
			return true, nil
		}
		dec := "false"
		if g(state) {
			dec = "true"
		}
		state["code_"+n.ID] = dec
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: dec})
		return false, nil
	case NodeRoute:
		// Route reads the last relevant decision key; uncertain fallback is
		// mandatory so low-confidence runs have somewhere safe to go.
		branch := pickBranch(state)
		next, ok := n.Branches[branch]
		if !ok {
			next, ok = n.Branches["uncertain"]
		}
		if !ok {
			finish(Outcome{NodeID: n.ID, NodeType: n.Type, Escalated: true, Reason: "no branch for " + branch})
			return true, nil
		}
		// Branch targets are recorded, not executed here: the caller maps
		// node ids to sub-runs. Record the routing decision on state.
		state["route_"+n.ID] = next
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: next})
		return false, nil
	case NodeSift:
		list, _ := state["items"].([]map[string]interface{})
		kept := make([]map[string]interface{}, 0, len(list))
		be := r.Backend
		if be == nil {
			be = RuleBackend{}
		}
		for _, item := range list {
			pass := true
			for _, q := range n.Questions {
				ans, err := be.Evaluate(ctx, item, q)
				if err != nil {
					pass = false
					break
				}
				if q.Kind == QNoul {
					_, v, err := CheckNoul(ans)
					if err != nil || v < r.noulCut() {
						pass = false
						break
					}
				}
			}
			if pass {
				kept = append(kept, item)
			}
		}
		// Store kept indices back in encounter order.
		out := make([]interface{}, len(kept))
		for i, k := range kept {
			out[i] = k
		}
		state["sifted"] = out
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: fmt.Sprintf("kept %d/%d", len(kept), len(list))})
		return false, nil
	case NodeMap:
		raw, _ := state[n.ListKey].([]interface{})
		if raw == nil {
			if ml, ok := state[n.ListKey].([]map[string]interface{}); ok {
				raw = make([]interface{}, len(ml))
				for i, m := range ml {
					raw[i] = m
				}
			}
		}
		sem := make(chan struct{}, n.Concurrency)
		var mu sync.Mutex
		var firstErr error
		escalated := false
		for _, elem := range raw {
			sem <- struct{}{}
			m, _ := elem.(map[string]interface{})
			if m == nil {
				<-sem
				continue
			}
			// Bounded sequential-equivalent: sub runs see a copy, write
			// whitelisted keys back. Keeps map deterministic for replay.
			sub := copyMap(m)
			esc, err := r.execNode(ctx, n.Sub, sub)
			mu.Lock()
			for k, v := range sub {
				if _, existed := m[k]; !existed {
					m[k] = v
				}
			}
			if esc {
				escalated = true
			}
			if err != nil && firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
			<-sem
		}
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: fmt.Sprintf("mapped %d", len(raw)), Escalated: escalated})
		return escalated, firstErr
	case NodeLoop:
		for i := 0; i < n.MaxIter; i++ {
			esc, err := r.execNode(ctx, n.Body, state)
			if err != nil || esc {
				return esc, err
			}
			g, ok := r.Gates[n.Until]
			if !ok {
				return true, fmt.Errorf("loop %q: unknown until gate %q", n.ID, n.Until)
			}
			if g(state) {
				finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: fmt.Sprintf("done after %d", i+1)})
				return false, nil
			}
		}
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Decision: "max_iter"})
		return false, nil
	case NodeEscalate:
		finish(Outcome{NodeID: n.ID, NodeType: n.Type, Escalated: true, Reason: n.Reason})
		return true, nil
	default:
		return true, fmt.Errorf("unknown node type %q", n.Type)
	}
}

// pickBranch reads the most recent judge/route-relevant decision.
func pickBranch(state map[string]interface{}) string {
	// Prefer explicit prior route keys in sorted order for determinism.
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(k) > 6 && k[:6] == "judge_" {
			if s, ok := state[k].(string); ok && s != "" {
				return s
			}
		}
	}
	if v, ok := state["branch"].(string); ok && v != "" {
		return v
	}
	return "uncertain"
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
