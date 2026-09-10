package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/db"
)

// replayCmd implements `ghost replay <trajectory-id>` and
// `ghost replay --list`. It reconstructs one execution trace from durable
// canonical events. Replay describes; it never re-executes or mutates state.
func replayCmd() {
	args := os.Args[2:]
	list := false
	asJSON := false
	id := ""
	for _, a := range args {
		switch a {
		case "--list", "-l":
			list = true
		case "--json":
			asJSON = true
		case "--help", "-h":
			replayHelp()
			return
		default:
			if len(a) > 0 && a[0] != '-' {
				id = a
			}
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	ws := cfg.WorkspacePath()
	database, err := db.NewDB(ws)
	if err != nil {
		fmt.Printf("Error opening state store: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()
	stream, err := cevents.Open(database.DB, filepath.Join(ws, "events"))
	if err != nil {
		fmt.Printf("Error opening event stream: %v\n", err)
		os.Exit(1)
	}

	if list || id == "" {
		ids := stream.RecentTrajectories(50)
		if len(ids) == 0 {
			fmt.Println("No trajectories recorded yet.")
			return
		}
		for _, t := range ids {
			fmt.Println(t)
		}
		return
	}

	sum := stream.DescribeTrajectory(id)
	if sum == nil {
		fmt.Printf("No events found for trajectory %q.\n", id)
		os.Exit(1)
	}
	if asJSON {
		raw, _ := json.MarshalIndent(sum, "", "  ")
		fmt.Println(string(raw))
		return
	}
	renderTrajectory(sum)
}

func renderTrajectory(s *cevents.TrajectorySummary) {
	fmt.Printf("Trajectory %s\n", s.TrajectoryID)
	if s.Outcome != "" {
		fmt.Printf("  Outcome:  %s\n", s.Outcome)
	}
	if s.Effort != "" {
		fmt.Printf("  Effort:   %s\n", s.Effort)
	}
	if s.DurationMs > 0 {
		fmt.Printf("  Duration: %dms\n", s.DurationMs)
	}
	if len(s.Models) > 0 {
		fmt.Printf("  Models:   %v\n", s.Models)
	}
	if len(s.Tools) > 0 {
		fmt.Printf("  Tools:    %v\n", s.Tools)
	}
	if len(s.Verifications) > 0 {
		fmt.Printf("  Verified: %v\n", s.Verifications)
	}
	if len(s.Fallbacks) > 0 {
		fmt.Printf("  Fallback: %v\n", s.Fallbacks)
	}
	if len(s.Errors) > 0 {
		fmt.Printf("  Errors:   %v\n", s.Errors)
	}
	fmt.Println("  Steps:")
	for _, st := range s.Steps {
		status := st.Status
		if status == "" {
			status = "-"
		}
		fmt.Printf("    [%d] %-22s %s\n", st.Seq, st.Type, status)
	}
}

func replayHelp() {
	fmt.Println("Usage: ghost replay <trajectory-id> [--json]")
	fmt.Println("       ghost replay --list")
	fmt.Println()
	fmt.Println("Reconstruct one execution trace (model, effort, tools, verification,")
	fmt.Println("fallback, outcome) from durable events. Replay never re-executes.")
}
