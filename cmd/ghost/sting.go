package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/sting"
	"github.com/ianclemence/ghost/pkg/tools"
)

// stingCmd runs the Sting offline-router surface:
//
//	ghost sting status      sidecar health + effective config (no secrets)
//	ghost sting dump-tools  router-subset tool schemas as JSON (flywheel input)
//	ghost sting calibrate   per-scope act/escalate thresholds from the ledger
//	ghost sting learn       ratchet the ledger from the rollout-evidence log
//	ghost sting select      rank router tools for a query (embeddings, live)
func stingCmd() {
	if len(os.Args) < 3 {
		stingHelp()
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	switch os.Args[2] {
	case "status":
		stingStatusCmd(cfg)
	case "dump-tools":
		stingDumpToolsCmd(cfg)
	case "calibrate":
		stingCalibrateCmd(cfg, os.Args[3:])
	case "learn":
		stingLearnCmd(cfg, os.Args[3:])
	case "select":
		stingSelectCmd(cfg, os.Args[3:])
	default:
		fmt.Printf("Unknown sting command: %s\n", os.Args[2])
		stingHelp()
	}
}

func stingHelp() {
	fmt.Println("Usage: ghost sting <status|dump-tools|calibrate|learn|select>")
	fmt.Println()
	fmt.Println("  status      Show Sting config + sidecar reachability (no secrets)")
	fmt.Println("  dump-tools  Print the router-subset tool schemas as JSON")
	fmt.Println("  calibrate   Print per-scope thresholds [--ledger=path] [--base=0.5]")
	fmt.Println("  learn       Ratchet the ledger from the rollout log [--rollouts=path] [--out=path]")
	fmt.Println("  select      Rank router tools for a query: select \"<query>\"")
	fmt.Println()
	fmt.Println("The reliability ledger is the gate: scopes below target precision")
	fmt.Println("escalate, and unscored (tuned) turns act only for measured scopes.")
}

// stingStatusCmd reports effective config and probes the sidecar. The
// probe is the only network use; a stopped sidecar is "reachable: no",
// never an error — the turn path escalates the same way.
func stingStatusCmd(cfg *config.Config) {
	n := cfg.Sting
	url := strings.TrimSpace(n.SidecarURL)
	if url == "" {
		url = sting.DefaultSidecarURL
	}
	threshold := n.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.5
	}
	timeout := n.TimeoutSecs
	if timeout <= 0 {
		timeout = 15
	}
	fmt.Printf("enabled: %v\n", n.Enabled)
	fmt.Printf("sidecar_url: %s\n", url)
	fmt.Printf("confidence_threshold: %.2f\n", threshold)
	fmt.Printf("timeout_secs: %d\n", timeout)
	if strings.TrimSpace(n.Weights) == "" {
		fmt.Println("weights: (base)")
	} else {
		fmt.Println("weights: (tuned archive configured)")
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(n.Ledger), "off"):
		fmt.Println("ledger: off (confidence-only gate)")
	default:
		lp := stingLedgerPath(cfg)
		switch {
		case lp == "":
			fmt.Println("ledger: (priors)")
		default:
			if _, err := os.Stat(lp); err == nil {
				fmt.Printf("ledger: %s\n", lp)
			} else {
				fmt.Printf("ledger: %s (missing; using priors)\n", lp)
			}
		}
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Get(url + "/health")
	if err != nil {
		fmt.Println("reachable: no")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		fmt.Println("reachable: yes")
	} else {
		fmt.Printf("reachable: no (status %d)\n", resp.StatusCode)
	}
}

// stingDumpToolsCmd prints the bounded router subset (sting.Subset over
// actually-registered tools) in sting.ToolSchema JSON — the versioned
// input to the dataset flywheel (sting-sidecar/flywheel/synthesize.py).
func stingDumpToolsCmd(cfg *config.Config) {
	out := stingRouterSchemas(cfg)
	if err := sting.ValidateSubset(stingSchemaNames(out)); err != nil {
		fmt.Fprintf(os.Stderr, "no routable tools: %v\n", err)
		os.Exit(1)
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
}

// stingRouterSchemas builds the live registry and returns the bounded
// router subset as sting schemas — one source for dump-tools, select,
// and (later) the serving pre-filter.
func stingRouterSchemas(cfg *config.Config) []sting.ToolSchema {
	reg := tools.NewToolRegistry()
	reg.Register(tools.NewWebSearchTool(tools.WebSearchToolOptions{DuckDuckGoEnabled: true, DuckDuckGoMaxResults: 5}))
	reg.Register(tools.NewWebFetchTool(50000))
	reg.Register(tools.NewWeatherTool(""))
	reg.Register(tools.NewCurrencyTool())
	reg.Register(tools.NewMemoryRecall(cfg.WorkspacePath()))

	names := sting.Subset(reg.List(), sting.DefaultRouterTools, sting.MaxRouterTools)
	byName := map[string]sting.ToolSchema{}
	for _, d := range reg.ToProviderDefs() {
		params := d.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object"}
		}
		byName[d.Function.Name] = sting.ToolSchema{
			Name:        d.Function.Name,
			Description: d.Function.Description,
			Parameters:  params,
		}
	}
	var out []sting.ToolSchema
	for _, n := range names {
		if s, ok := byName[n]; ok {
			out = append(out, s)
		}
	}
	return out
}

func stingSchemaNames(s []sting.ToolSchema) []string {
	out := make([]string, 0, len(s))
	for _, t := range s {
		out = append(out, t.Name)
	}
	return out
}

// stingLedgerPath resolves the configured ledger for CLI display: an
// explicit path or the workspace default. "off" and an unset workspace
// return "".
func stingLedgerPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	v := strings.TrimSpace(cfg.Sting.Ledger)
	if strings.EqualFold(v, "off") {
		return ""
	}
	if v != "" {
		return v
	}
	if ws := cfg.WorkspacePath(); ws != "" {
		return filepath.Join(ws, "state", "sting-ledger.json")
	}
	return ""
}

// stingSelectCmd ranks the router tools for one query with the
// on-device embedder: the zero-training Jev-shaped switch, live.
// Prints ranked tools with cosine scores plus embed latency.
func stingSelectCmd(cfg *config.Config, args []string) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		fmt.Println("Usage: ghost sting select \"<query>\"")
		os.Exit(1)
	}
	query := args[0]
	schemas := stingRouterSchemas(cfg)
	if err := sting.ValidateSubset(stingSchemaNames(schemas)); err != nil {
		fmt.Fprintf(os.Stderr, "no routable tools: %v\n", err)
		os.Exit(1)
	}
	base := strings.TrimSpace(cfg.Providers.Ollama.APIBase)
	model := strings.TrimSpace(cfg.Agents.Defaults.EmbeddingModel)
	emb := sting.NewOllamaEmbedder(base, model, 90)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	qv, err := emb.Embed(ctx, query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "embedder unavailable (is Ollama up?): %v\n", err)
		os.Exit(1)
	}
	queryMS := time.Since(start).Milliseconds()
	vecs := map[string][]float32{}
	var toolsMS int64
	for _, s := range schemas {
		t0 := time.Now()
		v, err := emb.Embed(ctx, sting.ToolText(s))
		if err != nil {
			fmt.Fprintf(os.Stderr, "embed failed for %s: %v\n", s.Name, err)
			os.Exit(1)
		}
		toolsMS += time.Since(t0).Milliseconds()
		vecs[s.Name] = v
	}
	fmt.Printf("query_embed_ms: %d  tools_embed_ms: %d (fingerprint %s)\n",
		queryMS, toolsMS, sting.Fingerprint(schemas))
	for _, r := range sting.Select(qv, schemas, vecs, 0) {
		fmt.Printf("  %-16s %.4f\n", r.Name, r.Score)
	}
}

// stingCalibrateCmd prints the act/escalate threshold per scope from the
// reliability ledger that gates routing: the configured/workspace ledger
// when present, otherwise the shipped priors; --ledger=path overrides.
// Read-only; changes nothing.
func stingCalibrateCmd(cfg *config.Config, args []string) {
	ledger := ""
	base := cfg.Sting.ConfidenceThreshold
	if base <= 0 {
		base = 0.5
	}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--ledger="):
			ledger = strings.TrimPrefix(a, "--ledger=")
		case strings.HasPrefix(a, "--base="):
			var v float64
			if _, err := fmt.Sscanf(strings.TrimPrefix(a, "--base="), "%f", &v); err == nil && v > 0 {
				base = v
			}
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			stingHelp()
			os.Exit(1)
		}
	}
	tr := sting.NewTracker(sting.DefaultPriors())
	source := "priors"
	if ledger == "" {
		ledger = stingLedgerPath(cfg)
	}
	if ledger != "" {
		loaded, err := sting.LoadTracker(ledger)
		if err != nil {
			// Explicit --ledger must exist; the workspace default may not
			// yet, in which case the shipped priors are the honest source.
			if strings.Contains(strings.Join(args, " "), "--ledger=") {
				fmt.Fprintf(os.Stderr, "cannot load ledger: %v\n", err)
				os.Exit(1)
			}
		} else {
			tr = loaded
			source = ledger
		}
	}
	fmt.Printf("ledger: %s\n", source)
	printLedger(tr, base)
}

// printLedger renders the act/escalate table shared by calibrate and learn.
func printLedger(tr *sting.Tracker, base float64) {
	if base <= 0 {
		base = 0.5
	}
	fmt.Printf("%-18s %5s %9s %10s  %s\n", "scope", "n", "precision", "threshold", "verdict")
	for _, scope := range tr.Scopes() {
		prec, n := tr.Precision(scope)
		th, reason := tr.ThresholdFor(scope, base)
		fmt.Printf("%-18s %5d %9.2f %10.2f  %s\n", scope, n, prec, th, reason)
	}
}

// stingLearnCmd folds the rollout-evidence log into the reliability
// ledger and writes it back — the producer that lets T0 (ledger +
// rollouts) actually ratchet. It rebuilds from the shipped priors plus
// the whole (rotating) rollout log each run, so it is idempotent:
// running it twice does not double-count. Read the log, write the
// ledger; nothing else changes.
func stingLearnCmd(cfg *config.Config, args []string) {
	rollouts := ""
	out := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--rollouts="):
			rollouts = strings.TrimPrefix(a, "--rollouts=")
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			stingHelp()
			os.Exit(1)
		}
	}
	if rollouts == "" {
		rollouts = stingRolloutLogPath(cfg)
	}
	if rollouts == "" {
		fmt.Fprintln(os.Stderr, "no rollout log configured; pass --rollouts=path")
		os.Exit(1)
	}
	if out == "" {
		out = stingLedgerPath(cfg)
	}
	if out == "" {
		fmt.Fprintln(os.Stderr, "no ledger path configured; pass --out=path")
		os.Exit(1)
	}
	tr := sting.NewTracker(sting.DefaultPriors())
	n, err := sting.AggregateRollouts(tr, rollouts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read rollouts: %v\n", err)
		os.Exit(1)
	}
	if err := tr.Save(out); err != nil {
		fmt.Fprintf(os.Stderr, "cannot write ledger: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ledger: %s  (%d graded turns from %s)\n", out, n, rollouts)
	printLedger(tr, cfg.Sting.ConfidenceThreshold)
}

// stingRolloutLogPath resolves the rollout log for CLI use: an explicit
// path or the workspace default. "off" and an unset workspace return "".
func stingRolloutLogPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	v := strings.TrimSpace(cfg.Sting.RolloutLog)
	if strings.EqualFold(v, "off") {
		return ""
	}
	if v != "" {
		return v
	}
	if ws := cfg.WorkspacePath(); ws != "" {
		return filepath.Join(ws, "state", "sting-rollouts.jsonl")
	}
	return ""
}
