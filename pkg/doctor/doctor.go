package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
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
}

func New(db *sql.DB, provider providers.LLMProvider, registry *tools.ToolRegistry, workspace string) *Doctor {
	return &Doctor{
		db:        db,
		provider:  provider,
		registry:  registry,
		workspace: workspace,
	}
}

func (d *Doctor) RunAll(ctx context.Context) []CheckResult {
	checks := []func(context.Context) CheckResult{
		d.checkDatabase,
		d.checkProvider,
		d.checkToolRegistry,
		d.checkBrowser,
		d.checkSkillDependencies,
	}
	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, check(ctx))
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
	return CheckResult{
		Name:    "provider",
		Label:   "AI",
		Status:  "ok",
		Message: fmt.Sprintf("AI is ready (%s).", model),
		Latency: time.Since(start).Milliseconds(),
	}
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
