package golden

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/tools"
	_ "modernc.org/sqlite"
)

// Classification of a failed conversation.
type Classification string

const (
	ModelBehavior Classification = "model_behavior"
	Runtime       Classification = "runtime"
	Provider      Classification = "provider"
	TestHarness   Classification = "test_harness"
	Environment   Classification = "environment"
	Configuration Classification = "configuration"
	Passed        Classification = "passed"
	Skipped       Classification = "skipped" // supported but intentionally not run (e.g. Qwen)
)

// Verdict of one conversation.
type Verdict string

const (
	VerdictPass Verdict = "pass"
	VerdictFail Verdict = "fail"
	VerdictSkip Verdict = "skip"
)

// AssertionResult records one assertion outcome.
type AssertionResult struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Hard   bool   `json:"hard,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// CaseResult is the full outcome of one conversation.
type CaseResult struct {
	ID             string            `json:"id"`
	Category       Category          `json:"category"`
	Title          string            `json:"title"`
	Verdict        Verdict           `json:"verdict"`
	Classification Classification    `json:"classification,omitempty"`
	Assertions     []AssertionResult `json:"assertions"`
	Responses      []string          `json:"responses"` // final response per person, trimmed
	Turns          int               `json:"turns"`
	DurationMs     int64             `json:"duration_ms"`
	Error          string            `json:"error,omitempty"`
}

// personRun is one person's executed conversation within a case.
type personRun struct {
	responses []string
	ws        string
}

// Summary aggregates a run.
type Summary struct {
	SuiteVersion int                     `json:"suite_version"`
	Model        string                  `json:"model"`
	Provider     string                  `json:"provider"`
	At           string                  `json:"at"`
	Commit       string                  `json:"commit,omitempty"`
	Total        int                     `json:"total"`
	Passed       int                     `json:"passed"`
	Failed       int                     `json:"failed"`
	Skipped      int                     `json:"skipped"`
	HardFails    int                     `json:"hard_fails"`
	DurationMs   int64                   `json:"duration_ms"`
	ByCategory   map[Category]CatSummary `json:"by_category"`
	Results      []CaseResult            `json:"results"`
}

// CatSummary is a per-category rollup.
type CatSummary struct {
	Total, Passed, Failed, Skipped, HardFails int
}

// Target names the model/provider to evaluate.
type Target struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (t Target) String() string { return t.Provider + "/" + t.Model }

// Runner executes conversations. Unsupported/skipped targets (Qwen) are
// reported as SUPPORTED/NOT RUN and never invoked.
type Runner struct {
	Target    Target
	ConfigDir string // optional dir with config.json + .secrets.json
	Offline   bool   // run the whole suite with the provider unreachable (offline validation)
	Suite     []Conversation
	Log       func(format string, a ...interface{})
}

func (r *Runner) logf(format string, a ...interface{}) {
	if r.Log != nil {
		r.Log(format, a...)
	}
}

func isQwen(t Target) bool {
	return strings.Contains(strings.ToLower(t.Model), "qwen")
}

// SkipReason for supported-but-not-run targets.
func SkipReason(t Target) string {
	if isQwen(t) {
		return "supported but intentionally NOT RUN: Qwen is too slow on the development appliance for this evaluation pass"
	}
	return "supported but not selected for this run"
}

// Run executes the suite for the runner's target.
func (r *Runner) Run() Summary {
	start := time.Now()
	sum := Summary{
		SuiteVersion: SuiteVersion,
		Model:        r.Target.Model, Provider: r.Target.Provider,
		At: time.Now().Format(time.RFC3339), ByCategory: map[Category]CatSummary{},
	}
	for _, c := range r.Suite {
		cr := r.runCase(c)
		sum.Total++
		switch cr.Verdict {
		case VerdictPass:
			sum.Passed++
		case VerdictFail:
			sum.Failed++
		case VerdictSkip:
			sum.Skipped++
		}
		if hasHardFail(cr) {
			sum.HardFails++
		}
		cs := sum.ByCategory[c.Category]
		cs.Total++
		switch cr.Verdict {
		case VerdictPass:
			cs.Passed++
		case VerdictFail:
			cs.Failed++
		case VerdictSkip:
			cs.Skipped++
		}
		if hasHardFail(cr) {
			cs.HardFails++
		}
		sum.ByCategory[c.Category] = cs
		sum.Results = append(sum.Results, cr)
	}
	sum.DurationMs = time.Since(start).Milliseconds()
	return sum
}

func hasHardFail(cr CaseResult) bool {
	for _, a := range cr.Assertions {
		if a.Hard && !a.Pass {
			return true
		}
	}
	return false
}

// qwenResult is used when a supported model is skipped.
func qwenResult(c Conversation, reason string) CaseResult {
	return CaseResult{
		ID: c.ID, Category: c.Category, Title: c.Title,
		Verdict: VerdictSkip, Classification: Skipped,
		Assertions: []AssertionResult{{Name: "supported_not_run", Pass: false, Detail: reason}},
	}
}

func caseSession(c Conversation, p Person) string {
	if p.Session != "" {
		return c.ID + "::" + p.Session
	}
	return c.ID + "::" + p.Name
}

func (r *Runner) runCase(c Conversation) CaseResult {
	cr := CaseResult{ID: c.ID, Category: c.Category, Title: c.Title, Verdict: VerdictPass}
	start := time.Now()
	defer func() { cr.DurationMs = time.Since(start).Milliseconds() }()

	if isQwen(r.Target) {
		cr = qwenResult(c, SkipReason(r.Target))
		return cr
	}

	root, err := os.MkdirTemp("", "ghost-golden-")
	if err != nil {
		return failCase(cr, TestHarness, err.Error())
	}
	if os.Getenv("GHOST_GOLDEN_KEEP") != "" {
		fmt.Fprintf(os.Stderr, "golden: keeping workspace %s for %s\n", root, c.ID)
	} else {
		defer os.RemoveAll(root)
	}

	// Shared or per-person workspaces.
	var sharedWS string
	var dbs []*sql.DB
	var runs []personRun

	for i, p := range c.People {
		ws := ""
		if c.SharedWorkspace {
			if sharedWS == "" {
				sharedWS = filepath.Join(root, "ws")
			}
			ws = sharedWS
		} else {
			ws = filepath.Join(root, "ws_"+p.Name)
		}
		if err := os.MkdirAll(ws, 0755); err != nil {
			return failCase(cr, TestHarness, err.Error())
		}
		// Pre-seed context mapping + memories (files), so the loop's own
		// store and the governance context store load them.
		sessionKey := caseSession(c, p)
		if err := seedContextAndMemory(ws, p, sessionKey, r.Target); err != nil {
			return failCase(cr, TestHarness, "seed: "+err.Error())
		}

		cfg, err := r.configFor(ws)
		if err != nil {
			return failCase(cr, Configuration, err.Error())
		}
		if c.Offline && !r.Offline {
			setProviderUnreachable(cfg, r.Target.Provider)
		}
		provider, err := providers.CreateProvider(cfg)
		if err != nil {
			return failCase(cr, Configuration, "provider: "+err.Error())
		}
		msgBus := bus.NewMessageBus()
		loop, err := agent.NewAgentLoop(cfg, msgBus, provider)
		if err != nil {
			return failCase(cr, Configuration, "agent init: "+err.Error())
		}
		// Real-time governance wiring (broker + events + contexts) exactly
		// like the gateway wires it.
		gov, db, err := wireGovernance(loop, ws, c.Fixture)
		if err != nil {
			return failCase(cr, Runtime, "governance: "+err.Error())
		}
		_ = gov
		dbs = append(dbs, db)
		if c.SharedWorkspace && i == 0 {
			sharedWS = ws
		}

		session := sessionKey
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		var perTurn []string
		for ti, t := range p.Turns {
			cr.Turns++
			rt, rerr := loop.ProcessDirectWithChannel(ctx, t.User, session, "web", "chat", nil, nil, nil)
			resp := ""
			if rerr != nil {
				resp = "[error] " + rerr.Error()
				if len(p.Turns) == ti+1 {
					cr.Classification = Environment
				}
			} else {
				resp = rt
			}
			perTurn = append(perTurn, strings.TrimSpace(resp))
		}
		cancel()
		cr.Responses = append(cr.Responses, perTurn...)
		runs = append(runs, personRun{responses: perTurn, ws: ws})
	}
	_ = sharedWS

	// Evaluate expectations against the collected state.
	ok, asserts := r.evaluate(c, runs)
	cr.Assertions = asserts
	if !ok {
		cr.Verdict = VerdictFail
		if cr.Classification == "" || cr.Classification == Passed {
			cr.Classification = classifyFailure(c, asserts, runs)
		}
	}
	for _, d := range dbs {
		if d != nil {
			closeDB(d)
		}
	}
	return cr
}

func closeDB(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}

func failCase(cr CaseResult, cls Classification, msg string) CaseResult {
	cr.Verdict = VerdictFail
	cr.Classification = cls
	cr.Error = msg
	cr.Assertions = []AssertionResult{{Name: "setup", Pass: false, Hard: true, Detail: msg}}
	return cr
}

// configFor builds a config for one person's workspace, with the target
// provider/model and (for offline conversations) an unreachable provider.
func (r *Runner) configFor(ws string) (*config.Config, error) {
	var cfg *config.Config
	var err error
	if r.ConfigDir != "" {
		cfg, err = config.LoadConfig(filepath.Join(r.ConfigDir, "config.json"))
	} else {
		cfg = config.DefaultConfig()
	}
	if err != nil {
		return nil, err
	}
	cfg.Agents.Defaults.Workspace = ws
	if r.Target.Provider != "" {
		cfg.Agents.Defaults.Provider = r.Target.Provider
	}
	if r.Target.Model != "" {
		cfg.Agents.Defaults.Model = r.Target.Model
	}
	if r.Offline {
		setProviderUnreachable(cfg, r.Target.Provider)
	}
	// A run dir config may not list the model preset; keep model_list
	// consistent so routing/selectModel finds it.
	cfg.Agents.ModelList = []config.ModelPreset{{Name: "target", Provider: cfg.Agents.Defaults.Provider, Model: cfg.Agents.Defaults.Model}}
	return cfg, nil
}

func setProviderUnreachable(cfg *config.Config, provider string) {
	switch provider {
	case "deepseek":
		cfg.Providers.DeepSeek.APIBase = "http://127.0.0.1:1"
	case "anthropic":
		cfg.Providers.Anthropic.APIBase = "http://127.0.0.1:1"
	case "openai":
		cfg.Providers.OpenAI.APIBase = "http://127.0.0.1:1"
	case "groq":
		cfg.Providers.Groq.APIBase = "http://127.0.0.1:1"
	case "ollama":
		cfg.Providers.Ollama.APIBase = "http://127.0.0.1:1"
	default:
		cfg.Providers.DeepSeek.APIBase = "http://127.0.0.1:1"
	}
}

// seedContextAndMemory creates any requested contexts + session mapping and
// writes seed memories to the store file BEFORE the loop opens it.
func seedContextAndMemory(ws string, p Person, sessionKey string, t Target) error {
	if err := os.MkdirAll(ws, 0755); err != nil {
		return err
	}
	// Contexts (non-personal) + session mapping must exist before the loop.
	if p.Context != "" && p.Context != "personal" {
		gid, err := ghoststate.EnsureIdentity(ws)
		if err != nil {
			return err
		}
		cs, err := contexts.Open(ws, gid.GhostID)
		if err != nil {
			return err
		}
		if _, exists := cs.Get(p.Context); !exists {
			kind := contexts.KindProject
			if p.Context == "work" {
				kind = contexts.KindWork
			}
			if _, err := cs.Create(kind, p.Context); err != nil {
				return err
			}
		}
		if err := cs.SetSessionContext(sessionKey, p.Context); err != nil {
			return err
		}
	}
	if len(p.SeedMemories) == 0 {
		return nil
	}
	st, err := personalcontext.Open(ws)
	if err != nil {
		return err
	}
	for _, m := range p.SeedMemories {
		if err := seedEntry(st, m); err != nil {
			return err
		}
	}
	return nil
}

func seedEntry(st *personalcontext.Store, m MemorySeed) error {
	raw, err := personalcontext.RawValue(m.Value)
	if err != nil {
		return err
	}
	kind := personalcontext.Kind(m.Kind)
	if kind == "" {
		kind = personalcontext.KindFact
	}
	e := personalcontext.Entry{
		ID: "seed-" + fmt.Sprintf("%d", time.Now().UnixNano()), Kind: kind,
		Subject: "user", Predicate: m.Predicate, Value: raw,
		Status: personalcontext.StatusCurrent, Confidence: 0.95,
		Scopes: append([]string{}, m.Scopes...),
		Sources: []personalcontext.Source{{
			Type: personalcontext.SourceImport, Kind: personalcontext.SourceUserDeclared,
			Ref: "seed:1", Timestamp: time.Now().UTC(),
		}},
	}
	_, err = st.Create(e)
	return err
}

// wireGovernance attaches broker/events/contexts and applies the case
// fixture (simulated provider tool behind the real tool boundary).
func wireGovernance(loop *agent.AgentLoop, ws string, fx Fixture) (*agent.Governance, *sql.DB, error) {
	db := loop.DB()
	broker, err := permissions.Open(db, permissions.ModeAsk, 0)
	if err != nil {
		return nil, db, err
	}
	events, err := cevents.Open(db, filepath.Join(ws, "events"))
	if err != nil {
		return nil, db, err
	}
	gid := "ghost-local"
	if id, err := ghoststate.LoadIdentity(ws); err == nil && id != nil {
		gid = id.GhostID
	}
	cs, err := contexts.Open(ws, gid)
	if err != nil {
		return nil, db, err
	}
	gov := agent.NewGovernance(events, broker, gid, "agent-main")
	gov.Contexts = cs
	loop.SetGovernance(gov)
	// Apply the fixture AFTER building the loop so the real dispatch path
	// (deterministic network dispatch / tool execution) hits the fixture.
	if err := applyFixture(loop, fx); err != nil {
		return nil, db, err
	}
	return gov, db, nil
}

// applyFixture overrides provider-backed tools with a simulated provider
// behind the SAME tool name/boundary (no runtime bypass).
func applyFixture(loop *agent.AgentLoop, fx Fixture) error {
	switch fx {
	case FixtureWeatherOK, FixtureWeatherFail, FixtureWeatherBad:
		var body string
		switch fx {
		case FixtureWeatherOK:
			body = `{"current":{"temperature_2m":26.0,"weather_code":1,"time":"2026-09-05T10:00"}}`
		case FixtureWeatherFail:
			body = "provider down"
		case FixtureWeatherBad:
			body = `{"current":{"temperature_2m":"banana"}}`
		}
		_ = body
		ft := newFixtureTool("weather_now", func() *tools.ToolResult {
			switch fx {
			case FixtureWeatherOK:
				return tools.NewToolResult("Weather in Bangkok: 26.0°C, rain (via fixture).")
			case FixtureWeatherFail:
				return tools.ErrorResult("I couldn't retrieve the latest weather data right now. Please try again in a bit.")
			default:
				return tools.ErrorResult("I got an unexpected response and didn't want to guess. Please try again.")
			}
		})
		loop.RegisterTool(ft)
	}
	return nil
}

// newFixtureTool builds a minimal tools.Tool.
type fixtureTool struct {
	name string
	run  func() *tools.ToolResult
}

func (f *fixtureTool) Name() string        { return f.name }
func (f *fixtureTool) Description() string { return "simulated " + f.name }
func (f *fixtureTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (f *fixtureTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return f.run()
}

func newFixtureTool(name string, run func() *tools.ToolResult) tools.Tool {
	return &fixtureTool{name: name, run: run}
}

// json helpers kept local to avoid import churn in tests.
func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// responseAssertionNames are checks on model output (vs runtime state).
var responseAssertionNames = map[string]bool{
	"last_response_contains": true, "last_response_not_contains": true,
	"any_response_contains": true, "asks_clarification": true,
}

// runtimeAssertionNames are checks on runtime state (memory, permissions,
// routines, events, dedup) — failures here are runtime, not model prose.
var runtimeAssertionNames = map[string]bool{
	"memory_present": true, "memory_superseded": true, "memory_current": true,
	"memory_absent": true, "routine_count": true, "grant_present": true,
	"denial_recorded": true, "no_unintended_grant": true,
	"no_unauthorized_exec": true,
	"cross_user_isolation": true,
}

func classifyFailure(c Conversation, asserts []AssertionResult, runs []personRun) Classification {
	hadError := false
	for _, r := range runs {
		for _, resp := range r.responses {
			if strings.HasPrefix(resp, "[error]") {
				hadError = true
			}
		}
	}
	onlyResponse := true
	onlyRuntime := true
	for _, a := range asserts {
		if a.Pass {
			continue
		}
		base := strings.SplitN(a.Name, "[", 2)[0]
		if !responseAssertionNames[base] {
			onlyResponse = false
		}
		if !runtimeAssertionNames[base] {
			onlyRuntime = false
		}
	}
	if hadError && onlyResponse {
		return Environment
	}
	if onlyRuntime {
		return Runtime
	}
	return ModelBehavior
}
