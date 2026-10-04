package agent

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// A forgetting must reach the canonical stream, because until this existed
// memory.created and memory.updated did and memory.deleted did not: a belief
// Ghost removed simply vanished from the record the owner can read.
func TestForgetPublishesMemoryDeletedEvent(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/cevents.db?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stream, err := cevents.Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	al := &AgentLoop{governance: &Governance{Events: stream, GhostID: "ghost-1", AgentID: "agent-main"}}
	al.wireMemoryAudit()
	t.Cleanup(func() { personalcontext.OnForgetReceipt = nil })

	ws := t.TempDir()
	pc, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	src := []personalcontext.Source{{
		Type: personalcontext.SourceConversation, Kind: personalcontext.SourceUserDeclared,
		Ref: "m1", Timestamp: time.Now(),
	}}
	e, err := pc.Create(personalcontext.Entry{
		ID: personalcontext.NewEntryID(), Kind: personalcontext.KindPreference,
		Subject: "user", Predicate: "preference/favorite_color", Status: personalcontext.StatusCurrent,
		Value: json.RawMessage(`"blue"`), Confidence: 0.9, Sources: src,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := personalcontext.ForgetPipelineWith(pc, ws, e.ID, "the owner asked"); err != nil {
		t.Fatal(err)
	}

	var deleted *cevents.Event
	for _, ev := range stream.Recent(50, cevents.Filter{UserVisibleOnly: true}) {
		if ev.Type == cevents.MemoryDeleted {
			deleted = ev
		}
	}
	if deleted == nil {
		t.Fatal("forgetting published no memory.deleted event; it is invisible in the activity feed")
	}
	if got, _ := deleted.Payload["title"].(string); got != "preference/favorite_color" {
		t.Fatalf("event title = %q, want the removed belief named", got)
	}
	if got, _ := deleted.Payload["reason"].(string); got != "the owner asked" {
		t.Fatalf("event reason = %q, want the recorded reason", got)
	}
	if got, _ := deleted.Payload["summary"].(string); got == "" {
		t.Fatal("event summary is empty; the owner cannot see what was rebuilt")
	}
}
