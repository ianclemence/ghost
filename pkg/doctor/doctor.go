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
		d.checkPython,
		d.checkBrowser,
		d.checkSkillDependencies,
	}
	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, check(ctx))
	}
	return results
}

func (d *Doctor) checkPython(ctx context.Context) CheckResult {
	start := time.Now()

	// Check python3
	cmd := exec.CommandContext(ctx, "python3", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try python
		cmd = exec.CommandContext(ctx, "python", "--version")
		out, err = cmd.CombinedOutput()
	}

	if err != nil {
		return CheckResult{
			Name:    "python_env",
			Status:  "error",
			Message: "python/python3 not found",
			Latency: time.Since(start).Milliseconds(),
		}
	}

	version := strings.TrimSpace(string(out))

	// Check pip
	cmd = exec.CommandContext(ctx, "pip", "--version")
	if err := cmd.Run(); err != nil {
		return CheckResult{
			Name:    "python_env",
			Status:  "warning",
			Message: fmt.Sprintf("%s (pip not found)", version),
			Latency: time.Since(start).Milliseconds(),
		}
	}

	return CheckResult{
		Name:    "python_env",
		Status:  "ok",
		Message: fmt.Sprintf("%s (pip available)", version),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkBrowser(ctx context.Context) CheckResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, "agent-browser", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:    "browser_env",
			Status:  "info",
			Message: "browser automation not installed (optional)",
			Latency: time.Since(start).Milliseconds(),
		}
	}

	version := strings.TrimSpace(string(out))
	return CheckResult{
		Name:    "browser_env",
		Status:  "ok",
		Message: fmt.Sprintf("agent-browser %s", version),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkDatabase(ctx context.Context) CheckResult {
	start := time.Now()
	if d.db == nil {
		return CheckResult{Name: "database", Status: "error", Message: "database not configured"}
	}
	err := d.db.PingContext(ctx)
	return CheckResult{
		Name:    "database",
		Status:  status(err),
		Message: errMsg(err),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkProvider(ctx context.Context) CheckResult {
	start := time.Now()
	if d.provider == nil {
		return CheckResult{Name: "provider", Status: "error", Message: "provider not configured"}
	}
	model := d.provider.GetDefaultModel()
	if model == "" {
		return CheckResult{Name: "provider", Status: "warning", Message: "default model is empty"}
	}

	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, err := d.provider.Chat(checkCtx, []providers.Message{
		{Role: "user", Content: "ping"},
	}, nil, model, map[string]interface{}{
		"max_tokens":  8,
		"temperature": 0,
	})
	return CheckResult{
		Name:    "provider",
		Status:  status(err),
		Message: errMsg(err),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkToolRegistry(ctx context.Context) CheckResult {
	start := time.Now()
	if d.registry == nil {
		return CheckResult{Name: "tool_registry", Status: "error", Message: "tool registry not configured"}
	}
	names := d.registry.List()
	if len(names) == 0 {
		return CheckResult{Name: "tool_registry", Status: "warning", Message: "no tools registered"}
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			return CheckResult{Name: "tool_registry", Status: "error", Message: "empty tool name found"}
		}
		if _, exists := seen[name]; exists {
			return CheckResult{Name: "tool_registry", Status: "error", Message: fmt.Sprintf("duplicate tool: %s", name)}
		}
		seen[name] = struct{}{}
		if _, ok := d.registry.Get(name); !ok {
			return CheckResult{Name: "tool_registry", Status: "error", Message: fmt.Sprintf("tool missing: %s", name)}
		}
	}
	return CheckResult{
		Name:    "tool_registry",
		Status:  "ok",
		Message: fmt.Sprintf("%d tools registered", len(names)),
		Latency: time.Since(start).Milliseconds(),
	}
}

func (d *Doctor) checkSkillDependencies(ctx context.Context) CheckResult {
	start := time.Now()
	if d.workspace == "" {
		return CheckResult{
			Name:    "skill_dependencies",
			Status:  "warning",
			Message: "workspace not configured",
			Latency: time.Since(start).Milliseconds(),
		}
	}

	report := skills.CheckSkillDependencies(d.workspace)
	if !report.HasMissing() {
		return CheckResult{
			Name:    "skill_dependencies",
			Status:  "ok",
			Message: fmt.Sprintf("all %d skill prerequisites satisfied", len(report.Results)),
			Latency: time.Since(start).Milliseconds(),
		}
	}

	missingSkills := 0
	missingCount := 0
	for _, res := range report.Results {
		if len(res.Missing) > 0 {
			missingSkills++
			missingCount += len(res.Missing)
		}
	}

	return CheckResult{
		Name:    "skill_dependencies",
		Status:  "warning",
		Message: fmt.Sprintf("%d skill(s) missing %d command(s): %s", missingSkills, missingCount, report.Summary()),
		Latency: time.Since(start).Milliseconds(),
	}
}

func status(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
