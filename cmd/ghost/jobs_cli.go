package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ianclemence/ghost/pkg/jobs"
	"github.com/ianclemence/ghost/pkg/tools"
)

// jobsCmd is `ghost jobs`: what Ghost takes on, from the terminal, through
// the same applyJob the app and the console use.
//
//	ghost jobs [list] [--json]
//	ghost jobs on <job> [HH:MM] [--topic "..."]
//	ghost jobs off <job>
func jobsCmd() {
	var pos []string
	asJSON, topic := false, ""
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			asJSON = true
		case a == "--topic" && i+1 < len(args):
			topic = args[i+1]
			i++
		case strings.HasPrefix(a, "--topic="):
			topic = strings.TrimPrefix(a, "--topic=")
		case a == "--help" || a == "-h":
			jobsHelp()
			return
		default:
			pos = append(pos, a)
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	ws := cfg.WorkspacePath()
	if len(pos) == 0 || (pos[0] == "list" && len(pos) == 1) {
		listJobs(ws, asJSON)
		return
	}
	op := strings.ToLower(pos[0])
	if (op != "on" && op != "off") || len(pos) < 2 || len(pos) > 3 || (op == "off" && len(pos) != 2) {
		jobsHelp()
		os.Exit(1)
	}
	j, ok := findJobRef(pos[1])
	if !ok {
		fmt.Printf("No job called %q. `ghost jobs` lists them.\n", pos[1])
		os.Exit(1)
	}
	at := ""
	if len(pos) == 3 {
		at = pos[2]
	} else if op == "on" {
		if st, _ := jobs.Open(ws).All(); st[j.ID].Settings.Time != "" {
			at = st[j.ID].Settings.Time
		}
	}
	_, database, _, err := openRoutinesCLI()
	if err != nil {
		fmt.Printf("Error opening routines: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()
	set, next, err := applyJob(database.DB, ws, tools.DeviceLocation().String(), j, op == "on", at, topic)
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), errJobInvalid.Error()+": ")
		if errors.Is(err, errJobsUnavailable) {
			msg = "Routines are unavailable right now."
		}
		fmt.Println(msg)
		os.Exit(1)
	}
	if op == "off" {
		fmt.Printf("%s is off.\n", j.Title)
		return
	}
	line := fmt.Sprintf("%s is on: %s at %s", j.Title, strings.ToLower(j.When), set.Time)
	if next != nil {
		line += ", next " + next.In(tools.DeviceLocation()).Format("Mon 2 Jan 15:04")
	}
	fmt.Println(line + ".")
}

// findJobRef accepts a job id or a unique prefix of its id or title.
func findJobRef(ref string) (jobs.Job, bool) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if j, ok := jobs.Find(ref); ok {
		return j, true
	}
	var hit jobs.Job
	n := 0
	for _, j := range jobs.Catalog {
		if strings.HasPrefix(j.ID, ref) || strings.HasPrefix(strings.ToLower(j.Title), ref) {
			hit = j
			n++
		}
	}
	return hit, n == 1
}

func listJobs(ws string, asJSON bool) {
	states, _ := jobs.Open(ws).All()
	if asJSON {
		type row struct {
			jobs.Job
			Enabled  bool          `json:"enabled"`
			Settings jobs.Settings `json:"settings"`
		}
		out := []row{}
		for _, j := range jobs.Catalog {
			st := states[j.ID]
			out = append(out, row{Job: j, Enabled: st.Enabled || j.Time == "", Settings: st.Settings})
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return
	}
	for _, j := range jobs.Catalog {
		st := states[j.ID]
		mark, when := "  ", j.When
		if j.Time != "" {
			at := j.Time
			if st.Enabled {
				mark, at = "● ", st.Settings.Time
			}
			when += " at " + at
		}
		fmt.Printf("%s%-14s %-24s %s\n", mark, j.ID, j.Title, when)
		if st.Settings.Topic != "" {
			fmt.Printf("  %-14s learning: %s\n", "", st.Settings.Topic)
		}
	}
	fmt.Println("\n● on.  ghost jobs on <job> [HH:MM]  ·  ghost jobs off <job>")
}

func jobsHelp() {
	fmt.Println("Usage: ghost jobs [list] [--json]")
	fmt.Println("       ghost jobs on <job> [HH:MM] [--topic \"what to learn\"]")
	fmt.Println("       ghost jobs off <job>")
	fmt.Println()
	fmt.Println("What Ghost takes on for you: a morning brief, your inbox, bills,")
	fmt.Println("trips, meals, life admin, the home, health, learning. A job that is")
	fmt.Println("on runs as a routine at its time and tells you in the app.")
}
