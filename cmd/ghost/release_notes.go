package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bench"
	"github.com/ianclemence/ghost/pkg/changelog"
	"github.com/ianclemence/ghost/pkg/golden"
	"github.com/ianclemence/ghost/pkg/releasenotes"
	"github.com/ianclemence/ghost/pkg/verify"
)

// releaseNotesCmd assembles docs/VERIFICATION-<version>.md from the
// machine-readable outputs Ghost already emits. By default it runs verify
// and benchmark live and attaches golden only from --golden-json (golden
// needs a model and is normally attached from a full evaluation run).
func releaseNotesCmd() {
	outDir := "docs"
	version := ""
	verifyJSON, benchJSON, goldenJSON := "", "", ""
	noVerify, noBench := false, false
	model := ""
	for _, a := range os.Args[2:] {
		switch {
		case a == "--help" || a == "-h":
			releaseNotesHelp()
			return
		case strings.HasPrefix(a, "--out="):
			outDir = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "--version="):
			version = strings.TrimPrefix(a, "--version=")
		case strings.HasPrefix(a, "--verify-json="):
			verifyJSON = strings.TrimPrefix(a, "--verify-json=")
		case strings.HasPrefix(a, "--bench-json="):
			benchJSON = strings.TrimPrefix(a, "--bench-json=")
		case strings.HasPrefix(a, "--golden-json="):
			goldenJSON = strings.TrimPrefix(a, "--golden-json=")
		case a == "--no-verify":
			noVerify = true
		case a == "--no-bench":
			noBench = true
		case strings.HasPrefix(a, "--model="):
			model = strings.TrimPrefix(a, "--model=")
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			releaseNotesHelp()
			os.Exit(1)
		}
	}
	if version == "" {
		version = describeVersion()
	}
	version = strings.TrimPrefix(version, "v")

	workspace := ""
	if cfg, err := loadConfig(); err == nil {
		workspace = cfg.WorkspacePath()
	}
	if workspace == "" {
		workspace = "./workspace"
	}

	in := releasenotes.Input{
		Version: version, Commit: headCommit(), At: time.Now(), Model: model,
		ChangelogBody: changelog.ForVersion(version),
		VerifyCmd:     "ghost verify",
		BenchCmd:      "ghost eval benchmark",
	}
	if verifyJSON != "" {
		rep, err := loadVerifyJSON(verifyJSON)
		if err != nil {
			fmt.Printf("Error loading verify JSON: %v\n", err)
			os.Exit(1)
		}
		in.Verify = rep
		in.VerifyCmd = "ghost verify (attached " + verifyJSON + ")"
	} else if !noVerify {
		in.Verify = verifyReport(workspace)
	}
	if benchJSON != "" {
		rep, err := loadBenchJSON(benchJSON)
		if err != nil {
			fmt.Printf("Error loading benchmark JSON: %v\n", err)
			os.Exit(1)
		}
		in.Bench = rep
		in.BenchCmd = "ghost eval benchmark (attached " + benchJSON + ")"
	} else if !noBench {
		in.Bench = benchReport(workspace)
	}
	if goldenJSON != "" {
		sum, err := loadGoldenJSON(goldenJSON)
		if err != nil {
			fmt.Printf("Error loading golden JSON: %v\n", err)
			os.Exit(1)
		}
		in.Golden = sum
		in.GoldenCmd = "ghost eval golden (attached " + goldenJSON + ")"
		if model == "" && sum.Provider != "" {
			in.Model = sum.Provider + "/" + sum.Model
		}
	}

	page := releasenotes.Render(in)
	name := fmt.Sprintf("VERIFICATION-%s.md", version)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}
	path := filepath.Join(outDir, name)
	if err := os.WriteFile(path, []byte(page), 0644); err != nil {
		fmt.Printf("Error writing page: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %s\n", path)
}

func describeVersion() string {
	out, err := exec.Command("git", "describe", "--tags", "--always").Output()
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(out))
}

func readJSONFile(path string, target interface{}) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// CLI banners (env/config notices) may precede the document and human
	// summaries may follow it on stdout captures; decode the first JSON
	// object and ignore the rest.
	if i := strings.Index(string(raw), "{"); i > 0 {
		raw = raw[i:]
	}
	return json.NewDecoder(strings.NewReader(string(raw))).Decode(target)
}

func loadVerifyJSON(path string) (*verify.Report, error) {
	var rep verify.Report
	if err := readJSONFile(path, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func loadBenchJSON(path string) (*bench.Report, error) {
	var rep bench.Report
	if err := readJSONFile(path, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func loadGoldenJSON(path string) (*golden.Summary, error) {
	var sum golden.Summary
	if err := readJSONFile(path, &sum); err != nil {
		return nil, err
	}
	return &sum, nil
}

func verifyReport(workspace string) *verify.Report {
	rep := verify.Run(verify.Options{Workspace: workspace, Timeout: 5 * time.Minute})
	return &rep
}

func benchReport(workspace string) *bench.Report {
	rep := bench.Run(workspace, 5*time.Minute)
	return &rep
}

func releaseNotesHelp() {
	fmt.Println("Usage: ghost eval release-notes [flags]")
	fmt.Println()
	fmt.Println("Assemble docs/VERIFICATION-<version>.md from Ghost's own")
	fmt.Println("evaluation outputs (verify, benchmark, golden, changelog).")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --out=<dir>        output directory (default: docs)")
	fmt.Println("  --version=<v>      version for the page (default: git describe)")
	fmt.Println("  --verify-json=<f>  attach a saved `ghost verify --json` report instead of running live")
	fmt.Println("  --bench-json=<f>   attach a saved `ghost eval benchmark --json` report instead of running live")
	fmt.Println("  --golden-json=<f>  attach a saved `ghost eval golden --json` summary (golden never runs live here)")
	fmt.Println("  --no-verify        skip the live verify run")
	fmt.Println("  --no-bench         skip the live benchmark run")
	fmt.Println("  --model=<p/m>      model the dynamic suites ran against, for provenance")
}
