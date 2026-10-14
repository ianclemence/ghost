package modelhealth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	keepPerModel   = 300
	recentWindow   = 20
	baselineWindow = 100
	minSamples     = 10
	fileName       = "model-health.jsonl"
)

type Outcome string

const (
	OK      Outcome = "ok"
	Failed  Outcome = "failed"
	Empty   Outcome = "empty"
	Refused Outcome = "refused"
)

type Call struct {
	At        time.Time `json:"at"`
	Model     string    `json:"model"`
	Outcome   Outcome   `json:"outcome"`
	LatencyMs int64     `json:"latency_ms"`
	Fallback  bool      `json:"fallback,omitempty"`
}

type Ledger struct {
	mu    sync.Mutex
	path  string
	calls map[string][]Call
}

func Open(workspace string) *Ledger {
	l := &Ledger{path: filepath.Join(workspace, "state", fileName), calls: map[string][]Call{}}
	f, err := os.Open(l.path)
	if err != nil {
		return l
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var c Call
		if json.Unmarshal(sc.Bytes(), &c) == nil && c.Model != "" {
			l.remember(c)
		}
	}
	return l
}

func (l *Ledger) remember(c Call) {
	list := append(l.calls[c.Model], c)
	if len(list) > keepPerModel {
		list = list[len(list)-keepPerModel:]
	}
	l.calls[c.Model] = list
}

func (l *Ledger) Record(c Call) {
	if l == nil || c.Model == "" {
		return
	}
	if c.At.IsZero() {
		c.At = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.remember(c)
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	raw, _ := json.Marshal(c)
	_, _ = f.Write(append(raw, '\n'))
}

type Summary struct {
	Model     string
	Calls     int
	ErrorRate float64
	P50Ms     int64
	P95Ms     int64
	Fallbacks int
	Drift     string
}

func errorRate(cs []Call) float64 {
	if len(cs) == 0 {
		return 0
	}
	bad := 0
	for _, c := range cs {
		if c.Outcome == Failed || c.Outcome == Empty {
			bad++
		}
	}
	return float64(bad) / float64(len(cs))
}

func percentile(cs []Call, p float64) int64 {
	var xs []int64
	for _, c := range cs {
		if c.Outcome == OK && c.LatencyMs > 0 {
			xs = append(xs, c.LatencyMs)
		}
	}
	if len(xs) == 0 {
		return 0
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	i := int(p * float64(len(xs)-1))
	return xs[i]
}

// drift compares the newest calls with the ones before them. A refused
// request is the conversation's fault, not the model's, so it counts for
// neither.
func drift(all []Call) string {
	var cs []Call
	for _, c := range all {
		if c.Outcome != Refused {
			cs = append(cs, c)
		}
	}
	if len(cs) < recentWindow+minSamples {
		return ""
	}
	recent := cs[len(cs)-recentWindow:]
	base := cs[:len(cs)-recentWindow]
	if len(base) > baselineWindow {
		base = base[len(base)-baselineWindow:]
	}
	if er, br := errorRate(recent), errorRate(base); er >= 0.25 && er >= br+0.2 {
		return fmt.Sprintf("failing %.0f%% of recent calls, up from %.0f%%", 100*er, 100*br)
	}
	rp, bp := percentile(recent, .5), percentile(base, .5)
	if bp > 0 && rp > 0 && float64(rp) > 2.5*float64(bp) && rp-bp > 1500 {
		return fmt.Sprintf("replies are %.1fx slower than usual (%.1fs against %.1fs)", float64(rp)/float64(bp), float64(rp)/1000, float64(bp)/1000)
	}
	return ""
}

func (l *Ledger) Summaries() []Summary {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Summary
	for m, cs := range l.calls {
		s := Summary{Model: m, Calls: len(cs), ErrorRate: errorRate(cs), P50Ms: percentile(cs, .5), P95Ms: percentile(cs, .95), Drift: drift(cs)}
		for _, c := range cs {
			if c.Fallback {
				s.Fallbacks++
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// DoctorLine is the one-line state for a health check: ok, or what drifted.
func (l *Ledger) DoctorLine() (status, message string) {
	sums := l.Summaries()
	if len(sums) == 0 {
		return "info", "No model calls recorded yet."
	}
	var drifting []string
	for _, s := range sums {
		if s.Drift != "" {
			drifting = append(drifting, s.Model+": "+s.Drift)
		}
	}
	if len(drifting) > 0 {
		return "warn", strings.Join(drifting, "; ")
	}
	var parts []string
	for _, s := range sums {
		parts = append(parts, fmt.Sprintf("%s %d calls, %.0f%% failed, median %.1fs", s.Model, s.Calls, 100*s.ErrorRate, float64(s.P50Ms)/1000))
	}
	return "ok", strings.Join(parts, "; ")
}
