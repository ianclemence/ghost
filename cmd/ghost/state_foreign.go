package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// foreignForgetReason says why, honestly: this is housekeeping, not the owner
// asking to forget, so it is not kept out of the notes as if they had.
const foreignForgetReason = personalcontext.HousekeepingReasonPrefix + "recorded by a session that is not the owner's conversation"

// statePurgeForeignCmd lists the memories learned in conversations that are not
// the owner's, and with --apply forgets them through the full pipeline (erasing
// their content and scrubbing the notes derived from them).
func statePurgeForeignCmd(cfg *config.Config) {
	apply := false
	var keep []string
	for _, a := range os.Args[3:] {
		switch {
		case a == "--apply":
			apply = true
		case strings.HasPrefix(a, "--keep="):
			keep = append(keep, strings.ToLower(strings.TrimPrefix(a, "--keep=")))
		case a == "--help" || a == "-h":
			stateHelp()
			return
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			stateHelp()
			return
		}
	}
	ws := cfg.WorkspacePath()
	store, err := personalcontext.Open(ws)
	if err != nil {
		fmt.Printf("Error opening memory: %v\n", err)
		os.Exit(1)
	}
	foreign := dropKept(store.ForeignCurrent(func(session string) bool { return session == channels.SharedConversationKey }), keep)
	if len(foreign) == 0 {
		fmt.Println("Every current memory was learned in your own conversation.")
		return
	}
	fmt.Printf("%d current memories were learned in sessions that are not your conversation:\n", len(foreign))
	for _, e := range foreign {
		fmt.Printf("  %-22s %s  %s\n", personalcontext.SessionOf(e), e.Predicate, strings.TrimSpace(personalcontext.Value(e)))
	}
	if !apply {
		fmt.Println("\nNothing changed. Run again with --apply to forget them.")
		return
	}
	forgotten := 0
	for _, e := range foreign {
		if _, err := personalcontext.ForgetPipelineWith(store, ws, e.ID, foreignForgetReason); err != nil {
			fmt.Printf("  could not forget %s: %v\n", e.ID, err)
			continue
		}
		forgotten++
	}
	fmt.Printf("\nForgotten: %d of %d.\n", forgotten, len(foreign))
}

// dropKept removes the entries the owner chose to spare: any whose key
// (predicate) contains one of the --keep= words.
func dropKept(entries []personalcontext.Entry, keep []string) []personalcontext.Entry {
	if len(keep) == 0 {
		return entries
	}
	var out []personalcontext.Entry
next:
	for _, e := range entries {
		for _, k := range keep {
			if k != "" && strings.Contains(strings.ToLower(e.Predicate), k) {
				continue next
			}
		}
		out = append(out, e)
	}
	return out
}
