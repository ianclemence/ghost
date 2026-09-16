package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/clock"
	"github.com/ianclemence/ghost/pkg/hardware"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/schema"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/tools"
)

type CheckResult struct {
	Name    string `json:"name"`
	Label   string `json:"label,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Latency int64  `json:"latency_ms,omitempty"`
}

type Doctor struct {
	db        *sql.DB
	provider  providers.LLMProvider
	registry  *tools.ToolRegistry
	workspace string
	// configPath binds the appliance config for the vault check.
	// Empty means unbound (info, not an error).
	configPath string
	// Estate is the configured model inventory (primary + fallbacks).
	// Optional: when set, the provider check reports the whole estate and
	// flags cloud entries missing credentials.
	Estate []providers.ProviderInfo
	// RetrievalSource reports observed retrieval latency aggregates
	// (populated by the agent runtime; nil = nothing observed yet).
	RetrievalSource func() RetrievalStats
}

// RetrievalStats carries per-path retrieval observations for the
// intelligence check. Counts and milliseconds only — never query content.
type RetrievalStats struct {
	RAG  RetrievalPath
	Memo RetrievalPath
}

// RetrievalPath aggregates one retrieval path ("rag", "memo").
type RetrievalPath struct {
	Queries int64
	AvgMs   float64
	LastMs  int64
}

// SetRetrievalSource installs the agent runtime's retrieval observer.
// Nil-safe and optional: without it the intelligence check reports counts
// only and marks retrieval latency unobserved.
func (d *Doctor) SetRetrievalSource(fn func() RetrievalStats) {
	if d == nil {
		return
	}
	d.RetrievalSource = fn
}

func New(db *sql.DB, provider providers.LLMProvider, registry *tools.ToolRegistry, workspace string) *Doctor {
	return &Doctor{
		db:        db,
		provider:  provider,
		registry:  registry,
		workspace: workspace,
	}
}

// Rebind points the Doctor at the CURRENT runtime provider and model estate.
// The Doctor is created once at startup with the boot-time provider; without
// this, switching the default AI model at runtime (e.g. ollama -> deepseek)
// left the health/doctor AI check pinging the old provider and reporting the
// old estate. Call it whenever the active model/provider changes.
func (d *Doctor) Rebind(p providers.LLMProvider, estate []providers.ProviderInfo) {
	if d == nil {
		return
	}
	if p != nil {
		d.provider = p
	}
	d.Estate = estate
}

func (d *Doctor) RunAll(ctx context.Context) []CheckResult {
	// Order is the user-visible diagnostics order: core health first
	// (can Ghost think, act, remember?), capabilities next, storage and
	// security risk after, setup state last. Length scales with problems,
	// not inventory — see the omission filter below.
	checks := []func(context.Context) CheckResult{
		d.checkDatabase,
		d.checkSchema,
		d.checkClock,
		d.checkProvider,
		d.checkToolRegistry,
		d.checkBrowser,
		d.checkSkillDependencies,
		d.checkDiskPressure,
		d.checkResources,
		d.checkVault,
		d.checkRoutinesFailing,
		d.checkConnectedServices,
		d.checkLastGolden,
		d.checkEvalSpend,
	}
	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, check(ctx))
	}
	// Diagnostics is health, not inventory: a row with nothing to report is
	// omitted rather than rendered as permanent info noise. Golden and spend
	// appear once runs/turns exist; the vault row appears once a config is
	// bound (unbound means the check didn't run, not that all is well); the
	// connected-services aggregate appears once at least one service skill
	// is enabled.
	kept := results[:0]
	for _, r := range results {
		if r.Status == "info" && (r.Name == "last_golden" || r.Name == "eval_spend" || r.Name == "vault" || r.Name == "connected_services") {
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

func (d *Doctor) checkBrowser(ctx context.Context) CheckResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, "agent-browser", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:    "browser_env",
			Label:   "Web browsing",
			Status:  "info",
			Message: "Not installed — and that's okay. Ghost can still work, it just can't browse web pages for you.",
			Latency: time.Since(start).Milliseconds(),
		}
	}

	version := strings.TrimSpace(string(out))
	return CheckResult{
		Name:    "browser_env",
		Label:   "Web browsing",
		Status:  "ok",
		Message: fmt.Sprintf("Web browsing is ready (%s)", version),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkDatabase(ctx context.Context) CheckResult {
	start := time.Now()
	if d.db == nil {
		return CheckResult{Name: "database", Label: "Memory", Status: "error", Message: "Ghost's memory isn't configured."}
	}
	err := d.db.PingContext(ctx)
	if err != nil {
		return CheckResult{
			Name:    "database",
			Label:   "Memory",
			Status:  "error",
			Message: "Ghost's memory is having trouble: " + err.Error(),
			Latency: time.Since(start).Milliseconds(),
		}
	}
	return CheckResult{
		Name:    "database",
		Label:   "Memory",
		Status:  "ok",
		Message: "Ghost's memory is healthy.",
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkSchema(ctx context.Context) CheckResult {
	start := time.Now()
	if d.db == nil {
		return CheckResult{Name: "schema", Label: "Database schema", Status: "error", Message: "Ghost's memory isn't configured."}
	}
	ok, at, err := schema.CheckCurrent(d.db)
	if err != nil {
		return CheckResult{
			Name:    "schema",
			Label:   "Database schema",
			Status:  "error",
			Message: "Could not determine schema version: " + err.Error(),
			Latency: time.Since(start).Milliseconds(),
		}
	}
	if !ok {
		return CheckResult{
			Name:    "schema",
			Label:   "Database schema",
			Status:  "error",
			Message: fmt.Sprintf("Database schema is at version %d but this Ghost needs version %d. Restart Ghost to migrate, or restore from a backup.", at, schema.CurrentVersion),
			Latency: time.Since(start).Milliseconds(),
		}
	}
	return CheckResult{
		Name:    "schema",
		Label:   "Database schema",
		Status:  "ok",
		Message: fmt.Sprintf("Database schema is current (version %d).", at),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkClock(ctx context.Context) CheckResult {
	start := time.Now()
	switch st := clock.Assess(); st {
	case clock.Invalid:
		return CheckResult{
			Name:    "clock",
			Label:   "System clock",
			Status:  "error",
			Message: "System clock is not trustworthy; scheduled automations are held until time is sane. Check network time sync.",
			Latency: time.Since(start).Milliseconds(),
		}
	case clock.Synced:
		return CheckResult{
			Name:    "clock",
			Label:   "System clock",
			Status:  "ok",
			Message: "System clock is synchronized.",
			Latency: time.Since(start).Milliseconds(),
		}
	default:
		return CheckResult{
			Name:    "clock",
			Label:   "System clock",
			Status:  "warning",
			Message: "System clock looks sane but NTP sync is " + st.String() + "; automation runs normally.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
}

func (d *Doctor) checkProvider(ctx context.Context) CheckResult {
	start := time.Now()
	if d.provider == nil {
		return CheckResult{Name: "provider", Label: "AI", Status: "error", Message: "No AI provider is set up yet."}
	}
	model := d.provider.GetDefaultModel()
	if model == "" {
		return CheckResult{Name: "provider", Label: "AI", Status: "warning", Message: "No model is selected yet. Pick one in the AI section."}
	}

	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, err := d.provider.Chat(checkCtx, []providers.Message{
		{Role: "user", Content: "ping"},
	}, nil, model, map[string]interface{}{
		"max_tokens":  8,
		"temperature": 0,
	})
	if err != nil {
		return CheckResult{
			Name:    "provider",
			Label:   "AI",
			Status:  "error",
			Message: "Couldn't reach the AI: " + err.Error(),
			Latency: time.Since(start).Milliseconds(),
		}
	}
	msg := fmt.Sprintf("AI is ready (%s).", model)
	// Keep the message simple for the owner: which model is ready and which
	// credentials are missing. The full model inventory (primary/fallback
	// roles, local vs cloud) is server-side detail (providers.DescribeEstate),
	// not user-facing copy.
	if missing := missingKeys(d.Estate); len(missing) > 0 {
		return CheckResult{
			Name:   "provider",
			Label:  "AI",
			Status: "warning",
			Message: msg + " Missing credentials for: " + strings.Join(missing, ", ") +
				". Those fallbacks will be skipped until keys are added.",
			Latency: time.Since(start).Milliseconds(),
		}
	}
	return CheckResult{
		Name:    "provider",
		Label:   "AI",
		Status:  "ok",
		Message: msg,
		Latency: time.Since(start).Milliseconds(),
	}
}

// missingKeys lists cloud models with no credential configured.
func missingKeys(estate []providers.ProviderInfo) []string {
	var out []string
	for _, e := range estate {
		if e.Kind == "cloud" && !e.HasCredential {
			out = append(out, e.Model)
		}
	}
	return out
}

func (d *Doctor) checkToolRegistry(ctx context.Context) CheckResult {
	start := time.Now()
	if d.registry == nil {
		return CheckResult{Name: "tool_registry", Label: "Tools", Status: "error", Message: "Ghost's tools aren't available."}
	}
	names := d.registry.List()
	if len(names) == 0 {
		return CheckResult{Name: "tool_registry", Label: "Tools", Status: "warning", Message: "No tools are ready."}
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			return CheckResult{Name: "tool_registry", Label: "Tools", Status: "error", Message: "A tool is not set up correctly."}
		}
		if _, exists := seen[name]; exists {
			return CheckResult{Name: "tool_registry", Label: "Tools", Status: "error", Message: fmt.Sprintf("A tool is registered twice: %s", name)}
		}
		seen[name] = struct{}{}
		if _, ok := d.registry.Get(name); !ok {
			return CheckResult{Name: "tool_registry", Label: "Tools", Status: "error", Message: fmt.Sprintf("A tool is missing: %s", name)}
		}
	}
	return CheckResult{
		Name:    "tool_registry",
		Label:   "Tools",
		Status:  "ok",
		Message: fmt.Sprintf("%d tools are ready.", len(names)),
		Latency: time.Since(start).Milliseconds(),
	}
}

// checkIntelligence reports the runtime's durable state at a glance:
// memory (durable facts + embeddings), tasks (active/completed/recovered),
// verification failures, and observed retrieval latency. It is operational
// and count-based — never query content, never hidden reasoning.
// checkResources reports live RAM headroom so the owner sees memory pressure
// before it becomes a failure. Disk has its own authority (checkDiskPressure
// in health.go); reporting it here too produced two rows for one resource.
// It never alarms on missing inputs.
func (d *Doctor) checkResources(ctx context.Context) CheckResult {
	start := time.Now()
	s := hardware.Snapshot(d.workspace)
	msg := fmt.Sprintf("RAM %d/%d MB free", s.MemAvailableMB, s.MemTotalMB)
	status := "ok"
	switch s.Memory {
	case hardware.PressureCritical:
		status = "error"
		msg += " — critical: Ghost will reduce context and retrieval to keep running."
	case hardware.PressureWarning:
		status = "warning"
		msg += " — running low: Ghost is trimming derived caches and context."
	}
	return CheckResult{
		Name:    "resources",
		Label:   "Resources",
		Status:  status,
		Message: msg,
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkSkillDependencies(ctx context.Context) CheckResult {
	start := time.Now()
	if d.workspace == "" {
		return CheckResult{
			Name:    "skill_dependencies",
			Label:   "Skills",
			Status:  "warning",
			Message: "Ghost can't find its skills.",
			Latency: time.Since(start).Milliseconds(),
		}
	}

	report := skills.CheckSkillDependencies(d.workspace)

	// Only core (zero-setup) skills count against the user. Optional skills —
	// those that need a local binary, hardware, or an external service — are
	// labeled "Needs setup" instead of being held against a normal install.
	missing := map[string][]string{}
	for _, res := range report.Results {
		if skills.IsOptionalSkill(res.Skill) {
			continue
		}
		if len(res.Missing) > 0 {
			missing[res.Skill] = res.Missing
		}
	}

	if len(missing) == 0 {
		return CheckResult{
			Name:    "skill_dependencies",
			Label:   "Skills",
			Status:  "ok",
			Message: "All skills are ready.",
			Latency: time.Since(start).Milliseconds(),
		}
	}

	return CheckResult{
		Name:    "skill_dependencies",
		Label:   "Skills",
		Status:  "warning",
		Message: fmt.Sprintf("%d skill(s) need extra software to run: %s", len(missing), formatMissingSkills(missing)),
		Latency: time.Since(start).Milliseconds(),
	}
}

// formatMissingSkills turns a skill → missing-commands map into a short,
// plain-language sentence for normal users.
func formatMissingSkills(missing map[string][]string) string {
	parts := make([]string, 0, len(missing))
	for skill := range missing {
		if skill == "" {
			continue
		}
		switch skill {
		case "tmux":
			parts = append(parts, "the terminal skill (install tmux)")
		case "calendar":
			parts = append(parts, "the calendar skill (it needs Google Calendar access)")
		case "hardware":
			parts = append(parts, "the hardware skill (install i2c-tools)")
		default:
			parts = append(parts, "the "+skill+" skill")
		}
	}
	return strings.Join(parts, ", ")
}

func status(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}
