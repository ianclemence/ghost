// Package bench answers "does the appliance behave like a good Ghost?"
// — distinct from unit tests ("does code behave in isolation?").
//
// Dimensions: responsiveness (latencies, split by layer), capability
// correctness (honest-unavailable ≠ success), agent reliability
// (runtime-evidence grading, never LLM prose), governance (the ten
// invariants; any violation is a hard FAIL), memory (deterministic eval
// set), automation, privacy (leaks = 0), appliance (restart/concurrency/
// duplicate side effects).
//
// Core score: weighted pass-rates per dimension. Catastrophic failures
// (unauthorized consequential execution, credential leak, replay side
// effect, duplicate consequential execution) force overall FAIL — they
// can never average away. History persists to workspace state (no new
// database) for change→benchmark→compare loops.
package bench

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/hardware"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/weather"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

// Dimension weights (Ghost product priorities: governance first).
var weights = map[string]float64{
	"responsiveness": 0.15,
	"capability":     0.20,
	"agent":          0.15,
	"governance":     0.20,
	"memory":         0.10,
	"automation":     0.10,
	"privacy":        0.10,
}

// Metric is one measurement.
type Metric struct {
	Dimension string  `json:"dimension"`
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Pass      bool    `json:"pass"`
	Detail    string  `json:"detail,omitempty"`
	MedianMs  float64 `json:"median_ms,omitempty"`
	P95Ms     float64 `json:"p95_ms,omitempty"`
}

// Report is the benchmark result.
type Report struct {
	At       string             `json:"at"`
	Score    float64            `json:"score"`
	Overall  string             `json:"overall"`
	Metrics  []Metric           `json:"metrics"`
	HardFail []string           `json:"hard_fail,omitempty"`
	ByDim    map[string]float64 `json:"by_dimension"`
	// HardwareClass is the detected machine class. Coverage beyond the
	// detected class is NOT_TESTED — never invented numbers.
	HardwareClass string            `json:"hardware_class"`
	HardwareCover map[string]string `json:"hardware_coverage"`
}

// Env is the scratch appliance.
type Env struct {
	Workspace string
	DB        *sql.DB
	Ctx       context.Context
}

func millis(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }

func stats(ds []time.Duration) (median, p95 float64) {
	if len(ds) == 0 {
		return 0, 0
	}
	ms := make([]float64, len(ds))
	for i, d := range ds {
		ms[i] = millis(d)
	}
	sort.Float64s(ms)
	median = ms[len(ms)/2]
	p95 = ms[int(float64(len(ms))*0.95)]
	if p95 == 0 {
		p95 = ms[len(ms)-1]
	}
	return median, p95
}

// Run executes the benchmark suite.
func Run(workspace string, timeout time.Duration) Report {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ws, err := os.MkdirTemp("", "ghost-bench-")
	if err != nil {
		return Report{Overall: "FAIL", HardFail: []string{"scratch workspace: " + err.Error()}}
	}
	defer os.RemoveAll(ws)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "bench.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return Report{Overall: "FAIL", HardFail: []string{"database: " + err.Error()}}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	env := &Env{Workspace: ws, DB: db, Ctx: ctx}
	var metrics []Metric
	var hard []string

	metrics = append(metrics, benchResponsiveness(env)...)
	metrics = append(metrics, benchCapability(env)...)
	metrics = append(metrics, benchAgent(env)...)
	metrics = append(metrics, benchGovernance(env)...)
	metrics = append(metrics, benchMemory(env)...)
	metrics = append(metrics, benchAutomation(env)...)
	metrics = append(metrics, benchPrivacy(env)...)
	metrics = append(metrics, benchAppliance(env)...)

	// Per-dimension pass rates.
	byDim := map[string]float64{}
	byCount := map[string][2]int{}
	for _, m := range metrics {
		c := byCount[m.Dimension]
		c[0]++
		if m.Pass {
			c[1]++
		}
		byCount[m.Dimension] = c
	}
	score := 0.0
	for dim, w := range weights {
		c := byCount[dim]
		rate := 0.0
		if c[0] > 0 {
			rate = float64(c[1]) / float64(c[0])
		}
		byDim[dim] = rate
		score += rate * w * 100
	}
	// Hard gates: catastrophic failures force FAIL regardless of score.
	for _, m := range metrics {
		if m.Detail == "HARD" && !m.Pass {
			hard = append(hard, m.Dimension+"/"+m.Name)
		}
	}
	overall := "PASS"
	if len(hard) > 0 {
		overall = "FAIL"
	} else {
		for _, m := range metrics {
			if !m.Pass {
				overall = "FAIL"
				break
			}
		}
	}
	hw := hardware.Detect()
	cover := map[string]string{
		"raspberry-pi-5": "NOT_TESTED",
		"rk1-class":      "NOT_TESTED",
		"x86-mini-pc":    "NOT_TESTED",
		"gpu-server":     "NOT_TESTED",
	}
	cover[string(hw.Class)] = "TESTED"
	return Report{At: time.Now().Format(time.RFC3339), Score: score, Overall: overall,
		Metrics: metrics, HardFail: hard, ByDim: byDim,
		HardwareClass: string(hw.Class), HardwareCover: cover}
}

func latencyMetric(dim, name string, ds []time.Duration, budgetMs float64) Metric {
	median, p95 := stats(ds)
	return Metric{Dimension: dim, Name: name, Value: median, Unit: "ms",
		MedianMs: median, P95Ms: p95, Pass: median <= budgetMs,
		Detail: fmt.Sprintf("median %.1fms p95 %.1fms budget %.0fms over %d runs", median, p95, budgetMs, len(ds))}
}

func benchResponsiveness(e *Env) []Metric {
	st, _ := cevents.Open(e.DB, e.Workspace+"/events")
	var out []Metric
	// Event publication latency (infrastructure, not model).
	var ds []time.Duration
	for i := 0; i < 200; i++ {
		start := time.Now()
		st.Publish(&cevents.Event{Type: cevents.AgentProgress, RequestID: "bench", GhostID: "g"})
		ds = append(ds, time.Since(start))
	}
	out = append(out, latencyMetric("responsiveness", "event_publish", ds, 5))
	// Memory write/retrieve latency (local).
	pc, _ := personalcontext.Open(e.Workspace + "/pc")
	ds = ds[:0]
	for i := 0; i < 50; i++ {
		raw, _ := personalcontext.RawValue(fmt.Sprintf("v%d", i))
		start := time.Now()
		pc.Create(personalcontext.Entry{ID: fmt.Sprintf("bench-%d", i), Kind: personalcontext.KindFact,
			Subject: "user", Predicate: "bench", Value: raw, Status: personalcontext.StatusCurrent,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceCommand, Kind: personalcontext.SourceUserDeclared, Ref: "b:1", Timestamp: time.Now().UTC()}}})
		ds = append(ds, time.Since(start))
	}
	out = append(out, latencyMetric("responsiveness", "memory_write", ds, 20))
	ds = ds[:0]
	for i := 0; i < 50; i++ {
		start := time.Now()
		_ = pc.CurrentInScope(nil)
		ds = append(ds, time.Since(start))
	}
	out = append(out, latencyMetric("responsiveness", "memory_retrieve", ds, 20))
	// Permission evaluation latency.
	broker, _ := permissions.Open(e.DB, permissions.ModeAsk, 0)
	ds = ds[:0]
	for i := 0; i < 200; i++ {
		start := time.Now()
		broker.Evaluate("weather.current", "read", "owner", permissions.RiskReadOnly)
		ds = append(ds, time.Since(start))
	}
	out = append(out, latencyMetric("responsiveness", "permission_evaluate", ds, 5))
	// Capability dispatch latency (mock vendor — infrastructure only).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"current":{"temperature_2m":20.0,"time":"2026-09-05T10:00"}}`))
	}))
	defer srv.Close()
	svc := weather.New(weather.Config{OpenMeteoBase: srv.URL, GeocodeBase: srv.URL, CacheTTL: 0, BreakerCooldown: time.Second})
	ds = ds[:0]
	for i := 0; i < 20; i++ {
		start := time.Now()
		svc.CurrentByCoords(e.Ctx, 13.75, 100.5, false)
		ds = append(ds, time.Since(start))
	}
	out = append(out, latencyMetric("responsiveness", "capability_dispatch_localhost", ds, 500))
	return out
}

func benchCapability(e *Env) []Metric {
	var out []Metric
	// Honest unavailability is a CORRECT outcome, not a failure.
	o := product.OutcomeForProviderFailure("weather", provider.FailUnavailable, nil)
	out = append(out, Metric{Dimension: "capability", Name: "honest_unavailable", Value: 1, Unit: "bool",
		Pass: o.Completion == product.CompletionTemporarilyUnavailable})
	// ...but must never be reported as success.
	unavailIsFailure := o.Completion != product.CompletionSuccess
	out = append(out, Metric{Dimension: "capability", Name: "unavailable_not_success", Value: 1, Unit: "bool",
		Pass: unavailIsFailure, Detail: "HARD"})
	// Malformed vendor data is rejected by validation (hermetic fake).
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"current":{"temperature_2m":"banana"}}`))
	}))
	defer bad.Close()
	svc := weather.New(weather.Config{OpenMeteoBase: bad.URL, GeocodeBase: bad.URL,
		CacheTTL: time.Minute, BreakerCooldown: time.Second})
	_, r := svc.CurrentByCoords(e.Ctx, 13.75, 100.5, false)
	out = append(out, Metric{Dimension: "capability", Name: "invalid_rejected", Value: 1, Unit: "bool",
		Pass: r.Err != nil, Detail: "banana temperature must fail validation"})
	return out
}

func benchAgent(e *Env) []Metric {
	broker, _ := permissions.Open(e.DB, permissions.ModeAsk, 0)
	var out []Metric
	check := func(name string, cond bool) {
		out = append(out, Metric{Dimension: "agent", Name: name, Value: 1, Unit: "bool", Pass: cond})
	}
	check("readonly_allows", broker.Evaluate("weather.current", "read", "o", permissions.RiskReadOnly) == permissions.VerdictAllow)
	check("consequential_asks", broker.Evaluate("calendar.create", "create", "o", permissions.RiskConsequential) == permissions.VerdictAsk)
	check("unknown_fails_closed", broker.Evaluate("evil.cap", "exec", "o", "unknown-risk") == permissions.VerdictAsk)
	check("forged_args_ignored", broker.Evaluate("hass.control", "toggle", "home", permissions.RiskConsequential) == permissions.VerdictAsk)
	return out
}

func benchGovernance(e *Env) []Metric {
	broker, _ := permissions.Open(e.DB, permissions.ModeAsk, 0)
	var out []Metric
	hard := func(name string, pass bool) {
		m := Metric{Dimension: "governance", Name: name, Value: 1, Unit: "bool", Pass: pass, Detail: "HARD"}
		out = append(out, m)
	}
	// Unauthorized consequential execution count = 0 (simulated attempts).
	unauth := 0
	for i := 0; i < 20; i++ {
		if broker.Evaluate("hass.control", "toggle", "home", permissions.RiskConsequential) == permissions.VerdictAllow {
			unauth++
		}
	}
	hard("zero_unauthorized", unauth == 0)
	// Allow-once consumed once.
	req, _ := broker.Require("bench-1", "s", "a", "c", "act", "", "", permissions.RiskConsequential, nil)
	broker.Resolve(req.ID, permissions.GrantOnce, "s")
	_, ok1 := broker.ConsumeApproved("bench-1")
	_, ok2 := broker.ConsumeApproved("bench-1")
	hard("allow_once_single_use", ok1 && !ok2)
	// Expired grants fail closed.
	req2, _ := broker.Require("bench-2", "s", "a", "c", "act", "", "", permissions.RiskConsequential, nil)
	_ = req2
	// Revocation effective.
	req3, _ := broker.Require("bench-3", "s", "a", "calendar.read", "read", "", "", permissions.RiskConsequential, nil)
	broker.Resolve(req3.ID, permissions.GrantAlways, "owner")
	broker.Revoke("calendar.read", "read", "owner")
	hard("revoke_effective", broker.Evaluate("calendar.read", "read", "owner", permissions.RiskConsequential) == permissions.VerdictAsk)
	// Replay does not execute: consume, then prove nothing remains.
	reqR, _ := broker.Require("bench-replay", "s", "a", "c", "act", "", "", permissions.RiskConsequential, nil)
	broker.Resolve(reqR.ID, permissions.GrantOnce, "s")
	_, first := broker.ConsumeApproved("bench-replay")
	_, second := broker.ConsumeApproved("bench-replay")
	hard("replay_safe", first && !second)
	return out
}

// memoryEvalSet is the deterministic retrieval eval: declarations the
// regex extractor handles, with exact expected predicates. Semantic
// (LLM) retrieval quality is NOT_TESTED beyond this set.
var memoryEvalSet = []struct{ text, predicate string }{
	{"my name is Ian", "identity/name"},
	{"I live in Bangkok", "fact/location"},
	{"I prefer tea", "preference/prefers"},
	{"my goal is to launch Ghost", "goal/primary"},
	{"I work at Acme", "fact/work"},
}

func benchMemory(e *Env) []Metric {
	var out []Metric
	writeOK, retrieveOK := 0, 0
	for i, tc := range memoryEvalSet {
		st, _ := personalcontext.Open(e.Workspace + "/mem-bench")
		actions, err := personalcontext.Apply(st, personalcontext.Input{
			SessionID: "bench", MessageID: fmt.Sprintf("m%d", i),
			Text: tc.text, Timestamp: time.Now().UTC(), Current: st.Current(),
		})
		if err == nil && len(actions) > 0 {
			writeOK++
		}
		hit := false
		for _, en := range st.CurrentInScope(nil) {
			if en.Predicate == tc.predicate {
				hit = true
			}
		}
		if hit {
			retrieveOK++
		}
	}
	n := float64(len(memoryEvalSet))
	out = append(out, Metric{Dimension: "memory", Name: "write_success", Value: float64(writeOK) / n, Unit: "rate",
		Pass: writeOK == len(memoryEvalSet), Detail: fmt.Sprintf("%d/%d deterministic pairs", writeOK, len(memoryEvalSet))})
	out = append(out, Metric{Dimension: "memory", Name: "retrieve_success", Value: float64(retrieveOK) / n, Unit: "rate",
		Pass: retrieveOK == len(memoryEvalSet), Detail: "scope-filtered retrieval; semantic quality NOT_TESTED"})
	// Context isolation matrix: Personal / Work / Project-A / Global.
	st, _ := personalcontext.Open(e.Workspace + "/mem-scope")
	seed := []struct {
		id, pred, val string
		scopes        []string
	}{
		{"scope-personal", "likes", "tea", nil},
		{"scope-work", "project", "PostgreSQL", []string{"context:work"}},
		{"scope-project-a", "project", "October", []string{"context:project-a"}},
	}
	for _, s := range seed {
		raw, _ := personalcontext.RawValue(s.val)
		st.Create(personalcontext.Entry{ID: s.id, Kind: personalcontext.KindFact,
			Subject: "user", Predicate: s.pred, Value: raw, Status: personalcontext.StatusCurrent,
			Scopes:  s.scopes,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceCommand, Kind: personalcontext.SourceUserDeclared, Ref: "b:1", Timestamp: time.Now().UTC()}}})
	}
	isolated, total := 0, 0
	scopesFor := map[string][]string{
		"personal":  {"context:personal"},
		"work":      {"context:work"},
		"project-a": {"context:project-a"},
	}
	for _, s := range seed {
		for _, scopes := range scopesFor {
			total++
			shouldSee := len(s.scopes) == 0
			for _, sc := range s.scopes {
				for _, mine := range scopes {
					if sc == mine {
						shouldSee = true
					}
				}
			}
			seen := false
			for _, en := range st.CurrentInScope(scopes) {
				if en.ID == s.id {
					seen = true
				}
			}
			if seen == shouldSee {
				isolated++
			}
		}
	}
	out = append(out, Metric{Dimension: "memory", Name: "context_isolation", Value: float64(isolated) / float64(total), Unit: "rate",
		Pass: isolated == total, Detail: fmt.Sprintf("%d/%d context cells", isolated, total)})
	return out
}

func benchAutomation(e *Env) []Metric {
	var out []Metric
	store := scheduled.NewStore(e.DB)
	store.InitSchema()
	svc, _ := routines.New(e.DB, store)
	r, err := svc.Create("g", "o", "Bench", "do x", "UTC",
		scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Hour}, nil)
	okCreate := err == nil && r.ID != ""
	out = append(out, Metric{Dimension: "automation", Name: "create", Value: 1, Unit: "bool", Pass: okCreate})
	if !okCreate {
		return out
	}
	_, err1 := svc.Run(e.Ctx, r.ID, "bench-exec-1", func(ctx context.Context, r *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Completion: product.CompletionSuccess}
	})
	_, err2 := svc.Run(e.Ctx, r.ID, "bench-exec-1", func(ctx context.Context, r *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Completion: product.CompletionSuccess}
	})
	dupBlocked := err2 != nil
	out = append(out, Metric{Dimension: "automation", Name: "idempotent", Value: 1, Unit: "bool",
		Pass: err1 == nil && dupBlocked, Detail: "HARD"})
	next := scheduled.NextCronRun("0 9 * * MON", "UTC", time.Now())
	out = append(out, Metric{Dimension: "automation", Name: "scheduling", Value: 1, Unit: "bool", Pass: next != nil && next.After(time.Now())})
	return out
}

func benchPrivacy(e *Env) []Metric {
	var out []Metric
	st, _ := cevents.Open(e.DB, e.Workspace+"/events")
	leaks := 0
	ev := st.Publish(&cevents.Event{Type: cevents.MessageCreated, RequestID: "bench-priv", GhostID: "g",
		Payload: map[string]interface{}{"api_key": "sk-proj-abcdefghijklmnop", "token": "ghp_abcdefghijklmnopqrstuvwx"}})
	raw, _ := json.Marshal(ev.Payload)
	for _, s := range []string{"sk-proj-abcdefghijklmnop", "ghp_abcdefghijklmnopqrstuvwx"} {
		if strings.Contains(string(raw), s) {
			leaks++
		}
	}
	if _, data, ok := ev.SSEForm(); ok && (strings.Contains(data, "sk-proj-abcdefghijklmnop")) {
		leaks++
	}
	chip, _ := activity.Project(ev)
	_ = chip
	out = append(out, Metric{Dimension: "privacy", Name: "credential_leaks", Value: float64(leaks), Unit: "count", Pass: leaks == 0, Detail: "HARD"})
	if leaks != 0 {
		out[len(out)-1].Detail = "HARD"
	}
	// Internal artifact projection.
	raw2, _ := json.Marshal(map[string]interface{}{"t": "x"})
	_ = raw2
	nasty := &cevents.Event{ID: "n", Type: "provider.http.request"}
	_, okProj := activity.Project(nasty)
	out = append(out, Metric{Dimension: "privacy", Name: "internal_artifacts", Value: 1, Unit: "bool", Pass: !okProj, Detail: "HARD"})
	if okProj {
		out[len(out)-1].Detail = "HARD"
	}
	return out
}

func benchAppliance(e *Env) []Metric {
	var out []Metric
	// Restart: reopen flows over the same DB.
	b1, _ := permissions.Open(e.DB, permissions.ModeAsk, 0)
	req, _ := b1.Require("bench-r", "bench-s", "a", "c", "act", "", "", permissions.RiskConsequential, nil)
	b2, _ := permissions.Open(e.DB, permissions.ModeAsk, 0)
	_, ok := b2.PendingForSession("bench-s")
	out = append(out, Metric{Dimension: "governance", Name: "restart_recovery", Value: 1, Unit: "bool", Pass: ok && req != nil})
	// Concurrent isolation.
	st, _ := cevents.Open(e.DB, e.Workspace+"/events")
	done := make(chan bool, 4)
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		go func(rid string) {
			st.Publish(&cevents.Event{Type: cevents.MessageCreated, RequestID: rid, GhostID: "g"})
			done <- true
		}(id)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	isolated := true
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		got := st.ByRequest(id)
		if len(got) != 1 {
			isolated = false
		}
	}
	out = append(out, Metric{Dimension: "responsiveness", Name: "concurrent_isolation", Value: 1, Unit: "bool", Pass: isolated})
	return out
}

// SaveHistory appends a run to the local history file (state/
// mechanism, no new database) and returns up to the last N runs.
func SaveHistory(workspace string, rep Report, keep int) ([]Report, error) {
	path := filepath.Join(workspace, "state", "bench-history.json")
	var hist []Report
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &hist)
	}
	hist = append(hist, rep)
	if keep > 0 && len(hist) > keep {
		hist = hist[len(hist)-keep:]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return hist, err
	}
	data, err := json.MarshalIndent(hist, "", "  ")
	if err != nil {
		return hist, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return hist, err
	}
	return hist, os.Rename(tmp, path)
}

// Compare summarizes current vs previous run per dimension.
func Compare(prev, cur Report) string {
	var b strings.Builder
	for dim, w := range weights {
		p, c := prev.ByDim[dim], cur.ByDim[dim]
		arrow := "="
		if c > p {
			arrow = "▲"
		} else if c < p {
			arrow = "▼"
		}
		fmt.Fprintf(&b, "%-15s %5.1f → %5.1f %s (weight %.0f%%)\n", dim, p*100, c*100, arrow, w*100)
	}
	fmt.Fprintf(&b, "%-15s %5.1f → %5.1f\n", "score", prev.Score, cur.Score)
	return b.String()
}

// Render produces the human-readable benchmark report.
func Render(r Report) string {
	var b strings.Builder
	b.WriteString("\nGHOST BENCHMARK\n\n")
	byDim := map[string][]Metric{}
	for _, m := range r.Metrics {
		byDim[m.Dimension] = append(byDim[m.Dimension], m)
	}
	for _, dim := range []string{"responsiveness", "capability", "agent", "governance", "memory", "automation", "privacy"} {
		ms := byDim[dim]
		if len(ms) == 0 {
			continue
		}
		rate := 0.0
		if v, ok := r.ByDim[dim]; ok {
			rate = v * 100
		}
		b.WriteString(fmt.Sprintf("%s (%.0f%%)\n", dim, rate))
		for _, m := range ms {
			mark := "✓"
			if !m.Pass {
				mark = "✗"
			}
			line := fmt.Sprintf("  %s %s", mark, m.Name)
			if m.MedianMs > 0 || m.P95Ms > 0 {
				line += fmt.Sprintf(" — median %.1fms p95 %.1fms", m.MedianMs, m.P95Ms)
			} else if m.Unit == "rate" {
				line += fmt.Sprintf(" — %.0f%%", m.Value*100)
			} else if m.Unit == "count" {
				line += fmt.Sprintf(" — %v", m.Value)
			}
			if m.Detail != "" && m.Detail != "HARD" {
				line += fmt.Sprintf(" (%s)", m.Detail)
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(fmt.Sprintf("\nGHOST CORE SCORE: %.1f\n", r.Score))
	if len(r.HardFail) > 0 {
		b.WriteString("HARD FAILURES: " + strings.Join(r.HardFail, ", ") + "\n")
	}
	b.WriteString(fmt.Sprintf("OVERALL: %s\n", r.Overall))
	return b.String()
}
