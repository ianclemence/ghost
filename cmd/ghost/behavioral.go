package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/golden"
)

// behavioralCmd runs the Behavioral Golden 100 (or a filtered subset):
//
//	ghost golden behavioral
//	ghost golden behavioral --family memory
//	ghost golden behavioral --style fragmented --tier 2
//	ghost golden behavioral --cases G056,G100
//	ghost golden behavioral --json
func behavioralCmd() {
	configDir := os.Getenv("GHOST_CONFIG_DIR")
	modelSpec := ""
	asJSON := false
	offline := false
	var family golden.Family
	var style golden.Style
	var tier golden.Tier
	casesFilter := ""
	stateDir := ""

	for _, a := range os.Args[3:] {
		switch {
		case a == "--json":
			asJSON = true
		case a == "--offline":
			offline = true
		case strings.HasPrefix(a, "--model="):
			modelSpec = strings.TrimPrefix(a, "--model=")
		case strings.HasPrefix(a, "--family="):
			family = golden.Family(strings.TrimPrefix(a, "--family="))
		case strings.HasPrefix(a, "--style="):
			style = golden.Style(strings.TrimPrefix(a, "--style="))
		case strings.HasPrefix(a, "--tier="):
			var t int
			fmt.Sscanf(strings.TrimPrefix(a, "--tier="), "%d", &t)
			tier = golden.Tier(t)
		case strings.HasPrefix(a, "--cases="):
			casesFilter = strings.TrimPrefix(a, "--cases=")
		case strings.HasPrefix(a, "--state-dir="):
			stateDir = strings.TrimPrefix(a, "--state-dir=")
		case a == "--help" || a == "-h":
			behavioralHelp()
			return
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			behavioralHelp()
			os.Exit(1)
		}
	}

	targets := golden.DiscoverTargets(configDir)
	if modelSpec == "" && len(targets) > 0 {
		modelSpec = targets[0].Target.String()
	}
	target := golden.Select(modelSpec)
	if target.Model == "" && target.Provider == "" {
		fmt.Println("No model target available. Pass --model=provider/model or configure a provider.")
		os.Exit(1)
	}

	suite := golden.FilterBehavioral(golden.BehavioralSuite(), family, style, tier, casesFilter)
	if len(suite) == 0 {
		fmt.Println("No behavioral scenarios matched the filter.")
		os.Exit(1)
	}

	runner := &golden.Runner{
		Target: target, ConfigDir: configDir, Offline: offline, Suite: suite,
		Log: func(format string, a ...interface{}) { fmt.Fprintf(os.Stderr, format+"\n", a...) },
	}
	rep := runner.RunBehavioral()
	if commit := headCommit(); commit != "" {
		rep.Commit = commit
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

	if asJSON {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Print(golden.RenderBehavioral(rep))
	}

	if rep.Failed > 0 || rep.HardFailed > 0 {
		os.Exit(1)
	}
}

func behavioralHelp() {
	fmt.Println("Usage: ghost golden behavioral [flags]")
	fmt.Println()
	fmt.Println("Run the Behavioral Golden 100: realistic conversations graded on")
	fmt.Println("understanding, authority, execution, evidence, recovery, and experience.")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --model=provider/model   target model (default: configured model)")
	fmt.Println("  --family=<name>          conversation|memory|actions|permission|failure|browser|computer|routines|artifacts|adversarial")
	fmt.Println("  --style=<name>           filter by conversation style (e.g. fragmented)")
	fmt.Println("  --tier=<1-5>             filter by difficulty tier")
	fmt.Println("  --cases=G001,G056        filter by scenario id")
	fmt.Println("  --offline                run with the provider unreachable")
	fmt.Println("  --json                   machine-readable output")
	fmt.Println("  --state-dir=<dir>        workspace for history (default: configured workspace)")
}
