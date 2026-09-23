package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// ideasCmd implements `ghost ideas`: the ideas-with-evidence loop for a
// normal owner. Lists show title, evidence excerpts, and decision state;
// accept/dismiss record receipts. Accepting a pause-type idea runs the safe
// reversible op through the routines service; anything creative prints the
// exact next step instead of automating silently.
func ideasCmd() {
	args := os.Args[2:]
	asJSON := false
	var positional []string
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case a == "--help" || a == "-h":
			ideasHelp()
			return
		default:
			positional = append(positional, a)
		}
	}
	ws, err := ideasWorkspace()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	store, err := ideas.New(ws)
	if err != nil {
		fmt.Printf("Error opening ideas: %v\n", err)
		os.Exit(1)
	}
	if len(positional) == 0 {
		listIdeas(store, ideas.StatusPending, asJSON)
		return
	}
	switch positional[0] {
	case "list":
		status := ideas.StatusPending
		if len(positional) > 1 {
			switch strings.ToLower(positional[1]) {
			case "accepted":
				status = ideas.StatusAccepted
			case "dismissed":
				status = ideas.StatusDismissed
			case "all":
				status = ""
			case "pending":
				status = ideas.StatusPending
			default:
				ideasHelp()
				os.Exit(1)
			}
		}
		listIdeas(store, status, asJSON)
	case "refresh":
		refreshIdeas(ws, store, asJSON)
	case "accept", "dismiss":
		if len(positional) != 2 {
			ideasHelp()
			os.Exit(1)
		}
		decideIdea(ws, store, positional[1], positional[0] == "accept", asJSON)
	case "draft":
		draftIdeas(ws, store, asJSON)
	default:
		ideasHelp()
		os.Exit(1)
	}
}

func ideasWorkspace() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	return cfg.WorkspacePath(), nil
}

func listIdeas(store *ideas.Store, status ideas.Status, asJSON bool) {
	all, err := store.List(status, 50)
	if err != nil {
		fmt.Printf("Error listing ideas: %v\n", err)
		os.Exit(1)
	}
	if asJSON {
		raw, _ := json.MarshalIndent(map[string]interface{}{"ideas": all}, "", "  ")
		fmt.Println(string(raw))
		return
	}
	if len(all) == 0 {
		fmt.Println("No ideas right now. Ghost will suggest some when it notices something — each one says why.")
		return
	}
	for i, idea := range all {
		mark := "○"
		if idea.Status == ideas.StatusAccepted {
			mark = "●"
		} else if idea.Status == ideas.StatusDismissed {
			mark = "─"
		}
		unver := ""
		if idea.Unverified {
			unver = " [needs checking]"
		}
		fmt.Printf("%2d  %s %s%s\n", i+1, mark, idea.Title, unver)
		fmt.Printf("    %s\n", idea.Body)
		for _, s := range idea.Sources {
			fmt.Printf("    why: %s\n", s.Excerpt)
		}
	}
}

// collectSignals assembles Phase A evidence from the real stores.
func collectSignals(ws string) (ideas.Signals, error) {
	sig := ideas.Signals{Now: time.Now().UTC()}
	if st, err := personalcontext.Open(ws); err == nil {
		for _, e := range st.Current() {
			if e.Predicate != "goal/primary" && !strings.HasPrefix(e.Predicate, "preference/") && !strings.HasPrefix(e.Predicate, "fact/") {
				continue
			}
			sig.Memories = append(sig.Memories, ideas.MemoryFact{
				ID: e.ID, Predicate: e.Predicate,
				Value:     personalcontext.Value(e),
				UpdatedAt: e.CreatedAt,
			})
		}
	}
	database, err := db.NewDB(ws)
	if err != nil {
		return sig, nil
	}
	defer database.Close()
	schedStore := scheduled.NewStore(database.DB)
	if err := schedStore.InitSchema(); err != nil {
		return sig, nil
	}
	svc, err := routines.New(database.DB, schedStore)
	if err != nil {
		return sig, nil
	}
	gid := "ghost-local"
	for _, r := range svc.List(gid, 100) {
		var last *time.Time
		if r.LastRun != nil {
			last = r.LastRun
		}
		sig.Routines = append(sig.Routines, ideas.RoutineMeta{
			ID: r.ID, Name: r.Name, Status: string(r.Status),
			LastRun: last, NextRun: r.NextRun, Timezone: r.Timezone,
		})
		// Recent execution history per routine for failure detection.
		hist, herr := schedStore.GetExecutionHistory(r.ID, 10)
		if herr != nil {
			continue
		}
		for _, h := range hist {
			sig.Runs = append(sig.Runs, ideas.RoutineRun{
				RunID: h.ID, RoutineID: r.ID, RoutineName: r.Name,
				Status: h.Status, Error: h.Error, At: h.StartedAt,
			})
		}
	}
	return sig, nil
}

func refreshIdeas(ws string, store *ideas.Store, asJSON bool) {
	sig, err := collectSignals(ws)
	if err != nil {
		fmt.Printf("Error collecting signals: %v\n", err)
		os.Exit(1)
	}
	existing, err := store.List("", 200)
	if err != nil {
		fmt.Printf("Error reading ideas: %v\n", err)
		os.Exit(1)
	}
	fresh := ideas.Generate(sig, existing)
	if err := store.Add(fresh); err != nil {
		fmt.Printf("Error storing ideas: %v\n", err)
		os.Exit(1)
	}
	if asJSON {
		raw, _ := json.MarshalIndent(map[string]interface{}{"new": fresh}, "", "  ")
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("%d new idea(s).\n", len(fresh))
	listIdeas(store, ideas.StatusPending, false)
}

func decideIdea(ws string, store *ideas.Store, ref string, accept, asJSON bool) {
	idea, err := store.Get(ref)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if idea.Status != ideas.StatusPending {
		fmt.Printf("Idea is already %s.\n", idea.Status)
		return
	}
	decided, err := store.Decide(idea.ID, accept)
	if err != nil {
		fmt.Printf("Error recording decision: %v\n", err)
		os.Exit(1)
	}
	acted := ""
	if accept && strings.HasPrefix(decided.Action, "pause:") {
		rid := strings.TrimPrefix(decided.Action, "pause:")
		if perr := pauseRoutineByID(ws, rid); perr != nil {
			fmt.Printf("Accepted, but pausing failed: %v\n", perr)
			return
		}
		acted = " Paused " + rid + " (resume anytime with ghost tasks)."
	}
	if asJSON {
		raw, _ := json.MarshalIndent(map[string]interface{}{"idea": decided, "acted": acted}, "", "  ")
		fmt.Println(string(raw))
		return
	}
	verb := "Dismissed"
	if accept {
		verb = "Accepted"
	}
	fmt.Printf("%s: %s.%s\n", verb, decided.Title, acted)
}

func pauseRoutineByID(ws, rid string) error {
	database, err := db.NewDB(ws)
	if err != nil {
		return err
	}
	defer database.Close()
	schedStore := scheduled.NewStore(database.DB)
	if err := schedStore.InitSchema(); err != nil {
		return err
	}
	svc, err := routines.New(database.DB, schedStore)
	if err != nil {
		return err
	}
	return svc.Pause(rid)
}

// draftIdeas runs Phase B: provider-drafted suggestions with verified
// citations. Unverified drafts are stored and shown as such.
func draftIdeas(ws string, store *ideas.Store, asJSON bool) {
	sig, err := collectSignals(ws)
	if err != nil {
		fmt.Printf("Error collecting signals: %v\n", err)
		os.Exit(1)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	p, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider (configure a model first): %v\n", err)
		os.Exit(1)
	}
	model := cfg.Agents.Defaults.Model
	ev, evidenceText := buildDraftEvidence(sig)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	text, err := ideas.DraftIdeas(ctx, p, model, evidenceText)
	if err != nil {
		fmt.Printf("Error drafting ideas: %v\n", err)
		os.Exit(1)
	}
	existing, err := store.List("", 200)
	if err != nil {
		fmt.Printf("Error reading ideas: %v\n", err)
		os.Exit(1)
	}
	var fresh []ideas.Idea
	for _, d := range ideas.ParseDrafts(text) {
		fresh = append(fresh, ideas.VerifyDraft(d, ev, time.Now().UTC()))
	}
	// Dedupe against cited sources already tracked.
	kept := fresh[:0]
	for _, idea := range fresh {
		dup := false
		for _, e := range existing {
			if e.Status == ideas.StatusDismissed {
				continue
			}
			for _, s := range idea.Sources {
				for _, es := range e.Sources {
					if s.Kind == es.Kind && s.Ref == es.Ref {
						dup = true
					}
				}
			}
		}
		if !dup {
			kept = append(kept, idea)
		}
	}
	if err := store.Add(kept); err != nil {
		fmt.Printf("Error storing ideas: %v\n", err)
		os.Exit(1)
	}
	if asJSON {
		raw, _ := json.MarshalIndent(map[string]interface{}{"new": kept}, "", "  ")
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("%d new drafted idea(s).\n", len(kept))
	listIdeas(store, ideas.StatusPending, false)
}

// buildDraftEvidence serializes citable rows with stable marker ids.
func buildDraftEvidence(sig ideas.Signals) (ideas.Evidence, string) {
	ev := ideas.Evidence{Memories: map[string]ideas.MemoryFact{}, Runs: map[string]ideas.RoutineRun{}}
	var b strings.Builder
	b.WriteString("Evidence (cite these ids exactly):\n")
	mem := sig.Memories
	sort.Slice(mem, func(i, j int) bool { return mem[i].UpdatedAt.After(mem[j].UpdatedAt) })
	if len(mem) > 20 {
		mem = mem[:20]
	}
	for _, m := range mem {
		ev.Memories[m.ID] = m
		fmt.Fprintf(&b, "- [memory:%s] %s: %s\n", m.ID, m.Predicate, m.Value)
	}
	runs := sig.Runs
	sort.Slice(runs, func(i, j int) bool { return runs[i].At.After(runs[j].At) })
	if len(runs) > 20 {
		runs = runs[:20]
	}
	for _, r := range runs {
		ev.Runs[r.RunID] = r
		fmt.Fprintf(&b, "- [routine:%s] routine %q run status=%s at %s\n",
			r.RunID, r.RoutineName, r.Status, r.At.Local().Format("2006-01-02 15:04"))
	}
	return ev, b.String()
}

func ideasHelp() {
	fmt.Println("Usage: ghost ideas [list [pending|accepted|dismissed|all]] [--json]")
	fmt.Println("       ghost ideas refresh [--json]")
	fmt.Println("       ghost ideas accept <id> [--json]")
	fmt.Println("       ghost ideas dismiss <id> [--json]")
	fmt.Println("       ghost ideas draft [--json]")
	fmt.Println()
	fmt.Println("Ideas with evidence: suggestions that cite their sources,")
	fmt.Println("with accept/dismiss receipts. refresh derives them")
	fmt.Println("deterministically; draft asks the model (citations verified).")
}
