package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"

	_ "modernc.org/sqlite"
)

func TestJobsTurnOnAsRoutinesAndOff(t *testing.T) {
	ws := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()
	mux := http.NewServeMux()
	registerJobRoutes(mux, nil)

	code, out := postJSON(t, mux, "/v1/jobs/morning_brief", map[string]interface{}{"enabled": true, "time": "06:45"})
	if code != 200 || out["next_run_at"] == "" {
		t.Fatalf("on: %d %v", code, out)
	}
	st := scheduled.NewStore(db)
	svc, _ := routines.New(db, st)
	list := svc.List("ghost-local", 50)
	if len(list) != 1 || list[0].ScheduleExpr != "45 6 * * *" || list[0].Name != "Morning brief" {
		t.Fatalf("routine: %+v", list)
	}
	item, _ := st.Get(list[0].ID)
	if item.Channel != "mobile" || item.ChatID != "default" {
		t.Fatalf("delivery: %q %q", item.Channel, item.ChatID)
	}
	// Changing the time is a new routine, never two running.
	if code, _ := postJSON(t, mux, "/v1/jobs/morning_brief", map[string]interface{}{"enabled": true, "time": "07:15"}); code != 200 {
		t.Fatal("change")
	}
	active := 0
	for _, r := range svc.List("ghost-local", 50) {
		if r.Status != routines.StatusCancelled {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active routines after a change: %d", active)
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18790/v1/jobs", nil)
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	got := decode(t, rec.Body.Bytes())
	jobsList, _ := got["jobs"].([]interface{})
	if len(jobsList) != 10 {
		t.Fatalf("catalog: %d", len(jobsList))
	}
	for _, j := range jobsList {
		m := j.(map[string]interface{})
		if m["id"] == "morning_brief" && (m["enabled"] != true || m["settings"].(map[string]interface{})["time"] != "07:15") {
			t.Fatalf("brief: %v", m)
		}
		if m["id"] == "inbox" && m["missing"] == nil {
			t.Fatalf("inbox needs a mailbox: %v", m)
		}
	}
	if code, _ := postJSON(t, mux, "/v1/jobs/morning_brief", map[string]interface{}{"enabled": false}); code != 200 {
		t.Fatal("off")
	}
	for _, r := range svc.List("ghost-local", 50) {
		if r.Status != routines.StatusCancelled {
			t.Fatalf("still running: %+v", r)
		}
	}
	if code, _ := postJSON(t, mux, "/v1/jobs/learn", map[string]interface{}{"enabled": true}); code != http.StatusBadRequest {
		t.Fatalf("learn without a topic: %d", code)
	}
	if code, _ := postJSON(t, mux, "/v1/jobs/nope", map[string]interface{}{"enabled": true}); code != http.StatusNotFound {
		t.Fatalf("unknown job: %d", code)
	}
}

func TestJobRefsByIDOrTitle(t *testing.T) {
	for ref, want := range map[string]string{"morning": "morning_brief", "Inbox": "inbox", "life_admin": "life_admin", "health weekly": "health"} {
		if j, ok := findJobRef(ref); !ok || j.ID != want {
			t.Fatalf("%q: %v %q", ref, ok, j.ID)
		}
	}
	if _, ok := findJobRef("nothing"); ok {
		t.Fatal("no such job")
	}
}
