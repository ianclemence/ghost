package doctor

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	checks := []func(context.Context) CheckResult{
		d.checkDatabase,
		d.checkSchema,
		d.checkClock,
		d.checkProvider,
		d.checkToolRegistry,
		d.checkIntelligence,
		d.checkResources,
		d.checkBrowser,
		d.checkSkillDependencies,
		d.checkCalendarOAuth,
	}
	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, check(ctx))
	}
	// A disabled calendar skill has nothing to diagnose: drop its check from
	// the aggregate instead of nagging the owner about sign-in for a
	// capability that is off. (checkCalendarOAuth itself stays truthful for
	// direct/programmatic callers.)
	if !d.calendarSkillActive() {
		kept := results[:0]
		for _, r := range results {
			if r.Name != "calendar_oauth" {
				kept = append(kept, r)
			}
		}
		results = kept
	}
	return results
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
func (d *Doctor) checkIntelligence(ctx context.Context) CheckResult {
	start := time.Now()
	if d.db == nil {
		return CheckResult{Name: "intelligence", Label: "Ghost Intelligence", Status: "info",
			Message: "State store isn't available yet."}
	}

	durable := countCurrentMemoryEntries(d.workspace)
	var chunks int
	_ = d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_chunks`).Scan(&chunks)

	// A job is "active" while it can still make progress.
	var active int
	_ = d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status IN
		('pending','running','waiting_for_permission','waiting_for_user','paused','retrying','interrupted')`).Scan(&active)

	since := time.Now().Add(-24 * time.Hour)
	var completed, recovered, verifyFailed int
	_ = d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status='succeeded' AND finished_at >= ?`,
		since.Unix()).Scan(&completed)
	_ = d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM canonical_events WHERE type='task.interrupted' AND timestamp >= ?`,
		since.Format(time.RFC3339)).Scan(&recovered)
	_ = d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM canonical_events WHERE type='verification.failed' AND timestamp >= ?`,
		since.Format(time.RFC3339)).Scan(&verifyFailed)

	parts := []string{
		fmt.Sprintf("Memory: %d durable, %d embedded", durable, chunks),
		fmt.Sprintf("Tasks: %d active, %d done in 24h, %d recovered", active, completed, recovered),
	}
	if d.RetrievalSource != nil {
		rs := d.RetrievalSource()
		if rs.RAG.Queries > 0 {
			parts = append(parts, fmt.Sprintf("Semantic retrieval: %.0fms avg over %d", rs.RAG.AvgMs, rs.RAG.Queries))
		}
		if rs.Memo.Queries > 0 {
			parts = append(parts, fmt.Sprintf("Note search: %.0fms avg over %d", rs.Memo.AvgMs, rs.Memo.Queries))
		}
	}
	if verifyFailed > 0 {
		parts = append(parts, fmt.Sprintf("%d verification failure(s) in 24h", verifyFailed))
	}

	status := "ok"
	if verifyFailed > 0 {
		// A failed world-state check is worth surfacing, not hiding.
		status = "warning"
	}
	return CheckResult{
		Name:    "intelligence",
		Label:   "Ghost Intelligence",
		Status:  status,
		Message: strings.Join(parts, ". ") + ".",
		Latency: time.Since(start).Milliseconds(),
	}
}

// checkResources reports live memory and disk headroom so the owner can see
// pressure before it becomes a failure. It never alarms on missing inputs.
func (d *Doctor) checkResources(ctx context.Context) CheckResult {
	start := time.Now()
	s := hardware.Snapshot(d.workspace)
	msg := fmt.Sprintf("RAM %d/%d MB free; disk %d GB free (%d%%)",
		s.MemAvailableMB, s.MemTotalMB, s.DiskFreeGB, s.DiskFreePct)
	status := "ok"
	switch s.Worst() {
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

// countCurrentMemoryEntries counts durable personal-context facts
// (status=current) by scanning the append-only log. Best-effort: a missing
// or unreadable store reports zero rather than failing the check.
func countCurrentMemoryEntries(workspace string) int {
	if workspace == "" {
		return 0
	}
	f, err := os.Open(filepath.Join(workspace, "personal-context", "entries.jsonl"))
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Status == "current" {
			n++
		}
	}
	return n
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
