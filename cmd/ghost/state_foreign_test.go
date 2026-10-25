package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func TestDropKeptSparesWhatTheOwnerNames(t *testing.T) {
	in := []personalcontext.Entry{
		{ID: "a", Predicate: "preference/communication.style"},
		{ID: "b", Predicate: "project/lumen-pricing-page"},
		{ID: "c", Predicate: "routine/vitamin-routine"},
	}
	got := dropKept(in, []string{"communication"})
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("--keep must spare only the named entry: %+v", got)
	}
	if len(dropKept(in, nil)) != 3 {
		t.Fatal("no --keep spares nothing")
	}
}
