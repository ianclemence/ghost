package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/golden"
)

// goldenCmd runs the Ghost Golden Conversation Suite against a supported
// model. Qwen is a supported target but is intentionally NOT run here
// (too slow on the development appliance); selecting it reports NOT RUN.
func goldenCmd() {
	configDir := os.Getenv("GHOST_CONFIG_DIR")
	modelSpec := ""
	asJSON := false
	compare := false
	offline := false
	suiteFilter := ""
	casesFilter := ""
	stateDir := ""
	for _, a := range os.Args[2:] {
		switch {
		case a == "--json":
			asJSON = true
		case a == "--compare":
			compare = true
		case a == "--offline":
			offline = true
		case strings.HasPrefix(a, "--model="):
			modelSpec = strings.TrimPrefix(a, "--model=")
		case strings.HasPrefix(a, "--suite="):
			suiteFilter = strings.TrimPrefix(a, "--suite=")
		case strings.HasPrefix(a, "--cases="):
			casesFilter = strings.TrimPrefix(a, "--cases=")
		case strings.HasPrefix(a, "--state-dir="):
			stateDir = strings.TrimPrefix(a, "--state-dir=")
		case a == "--help" || a == "-h":
			goldenHelp()
			return
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			goldenHelp()
			os.Exit(1)
		}
	}

	// Discover targets and resolve the model selection.
	targets := golden.DiscoverTargets(configDir)
	if modelSpec == "" {
		if len(targets) > 0 {
			modelSpec = targets[0].Target.String()
		}
	}
	target := golden.Select(modelSpec)
	if target.Model == "" && target.Provider == "" {
		fmt.Println("No model target available. Pass --model=provider/model or configure a provider.")
		os.Exit(1)
	}

	// Build the conversation list (all, or filtered by category / ids).
	suite := golden.Suite()
	if suiteFilter != "" {
		suite = filterSuiteByCategory(suite, suiteFilter)
	}
	if casesFilter != "" {
		suite = filterSuiteByIDs(suite, casesFilter)
	}
	if len(suite) == 0 {
		fmt.Println("No conversations matched the filter.")
		os.Exit(1)
	}

	runner := &golden.Runner{
		Target: target, ConfigDir: configDir, Offline: offline, Suite: suite,
		Log: func(format string, a ...interface{}) { fmt.Fprintf(os.Stderr, format+"\n", a...) },
	}
	sum := runner.Run()
	sum.SuiteVersion = golden.SuiteVersion
	if commit := headCommit(); commit != "" {
		sum.Commit = commit
	}

	if stateDir == "" {
		cfgPath := configDir
		if cfgPath == "" {
			cfgPath = filepath.Dir(getConfigPath())
		}
		if cfg, err := config.LoadConfig(filepath.Join(cfgPath, "config.json")); err == nil && cfg.WorkspacePath() != "" {
			stateDir = cfg.WorkspacePath()
		}
	}

	// Persist history.
	if stateDir != "" {
		if _, err := golden.SaveHistory(stateDir, sum, 50); err != nil {
			fmt.Fprintf(os.Stderr, "warning: golden history not saved: %v\n", err)
		}
	}

	if asJSON {
		raw, _ := json.MarshalIndent(sum, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Print(golden.RenderSummary(sum))
	}

	// Compare to the previous run when requested.
	if compare && stateDir != "" {
		if hist, err := golden.LoadHistory(stateDir); err == nil && len(hist) >= 2 {
			prev := hist[len(hist)-2].Summary
			fmt.Print(golden.CompareSummary(prev, sum))
		}
	}

	if sum.Failed > 0 {
		os.Exit(1)
	}
}

func goldenHelp() {
	fmt.Println("Usage: ghost golden [flags]")
	fmt.Println()
	fmt.Println("Evaluate the Golden Conversation Suite against a supported model.")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --model=provider/model   target model (default: configured model)")
	fmt.Println("  --suite=<category>       filter by category (memory, permission, ...)")
	fmt.Println("  --cases=id1,id2,...      filter by conversation ids")
	fmt.Println("  --offline                run with the provider unreachable (offline validation)")
	fmt.Println("  --json                   machine-readable JSON output")
	fmt.Println("  --compare                show delta vs the previous saved run")
	fmt.Println("  --state-dir=<dir>        workspace for golden history (default: configured workspace)")
	fmt.Println()
	fmt.Println("Models: any provider Ghost supports. Qwen is supported but intentionally")
	fmt.Println("NOT RUN on the development appliance (too slow); selecting it reports")
	fmt.Println("SUPPORTED/NOT RUN rather than pass or fail.")
}

func filterSuiteByCategory(suite []golden.Conversation, cat string) []golden.Conversation {
	var out []golden.Conversation
	for _, c := range suite {
		if string(c.Category) == strings.ToLower(strings.TrimSpace(cat)) {
			out = append(out, c)
		}
	}
	return out
}

func filterSuiteByIDs(suite []golden.Conversation, ids string) []golden.Conversation {
	want := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = true
		}
	}
	var out []golden.Conversation
	for _, c := range suite {
		if want[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// gitCommit returns the short HEAD commit when available.
func headCommit() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
