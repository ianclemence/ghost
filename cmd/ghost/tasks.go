package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// tasksCmd implements `ghost tasks`, the terminal surface for durable work:
// routines with plan → progress → receipt visibility, backed by the same
// routines service the gateway API serves. No new execution semantics:
// pause/resume/cancel/delete resolve through the existing service.
func tasksCmd() {
	args := os.Args[2:]
	asJSON := false
	var positional []string
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case a == "--help" || a == "-h":
			tasksHelp()
			return
		default:
			positional = append(positional, a)
		}
	}
	svc, database, gid, err := openRoutinesCLI()
	if err != nil {
		fmt.Printf("Error opening routines: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	if len(positional) == 0 {
		listTasks(svc, gid, asJSON)
		return
	}
	op := strings.ToLower(positional[0])
	if op == "list" && len(positional) == 1 {
		listTasks(svc, gid, asJSON)
		return
	}
	if len(positional) != 2 {
		tasksHelp()
		os.Exit(1)
	}
	id := resolveRoutineID(svc, gid, positional[1])
	if id == "" {
		fmt.Printf("No routine matches %q.\n", positional[1])
		os.Exit(1)
	}
	var opErr error
	switch op {
	case "pause":
		opErr = svc.Pause(id)
	case "resume":
		opErr = svc.Resume(id)
	case "cancel":
		opErr = svc.Cancel(id)
	case "delete":
		opErr = svc.Delete(id)
	default:
		fmt.Printf("Unknown tasks command: %s\n", op)
		tasksHelp()
		os.Exit(1)
	}
	if opErr != nil {
		fmt.Printf("Error: %v\n", opErr)
		os.Exit(1)
	}
	if asJSON {
		fmt.Printf("{\"ok\":true,\"op\":%q,\"id\":%q}\n", op, id)
		return
	}
	fmt.Printf("%s %s\n", op, shortRoutineID(id))
}

func openRoutinesCLI() (*routines.Service, *db.DB, string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, "", err
	}
	ws := cfg.WorkspacePath()
	database, err := db.NewDB(ws)
	if err != nil {
		return nil, nil, "", err
	}
	gid := "ghost-local"
	if id, err := ghoststate.LoadIdentity(ws); err == nil && id != nil && id.GhostID != "" {
		gid = id.GhostID
	}
	store := scheduled.NewStore(database.DB)
	if err := store.InitSchema(); err != nil {
		database.Close()
		return nil, nil, "", err
	}
	svc, err := routines.New(database.DB, store)
	if err != nil {
		database.Close()
		return nil, nil, "", err
	}
	return svc, database, gid, nil
}

// resolveRoutineID accepts a full id or a unique prefix, mirroring resolve
// semantics elsewhere in the CLI. Empty when nothing matches.
func resolveRoutineID(svc *routines.Service, ghostID, ref string) string {
	var hit string
	matches := 0
	for _, r := range svc.List(ghostID, 100) {
		if r.ID == ref || strings.HasPrefix(r.ID, ref) {
			hit = r.ID
			matches++
		}
	}
	if matches == 1 {
		return hit
	}
	return ""
}

func shortRoutineID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func listTasks(svc *routines.Service, ghostID string, asJSON bool) {
	all := svc.List(ghostID, 100)
	if asJSON {
		raw, _ := json.MarshalIndent(map[string]interface{}{"routines": all}, "", "  ")
		fmt.Println(string(raw))
		return
	}
	if len(all) == 0 {
		fmt.Println("No routines. Say \"every Monday at 9 remind me to…\" and Ghost figures out the rest.")
		return
	}
	for i, r := range all {
		when := ""
		if r.NextRun != nil {
			when = " · next " + r.NextRun.Local().Format("Mon 15:04")
		}
		last := ""
		if r.LastRun != nil {
			last = fmt.Sprintf(" · last %s ago", roundDur(time.Since(*r.LastRun)))
		}
		fmt.Printf("%2d  %-12s %-8s %s%s%s\n", i+1, shortRoutineID(r.ID), r.Status, r.Name, when, last)
	}
}

func roundDur(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func tasksHelp() {
	fmt.Println("Usage: ghost tasks [list] [--json]")
	fmt.Println("       ghost tasks <pause|resume|cancel|delete> <id-prefix>")
	fmt.Println()
	fmt.Println("Show and manage durable routines: plan, progress, and receipts.")
	fmt.Println("Pause/resume/cancel resolve through the routines service —")
	fmt.Println("the same authority the gateway API serves.")
}
