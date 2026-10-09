package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/motion"

	_ "modernc.org/sqlite"
)

func TestMotionRoutesPlayChangeAndQueueAVideo(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	spec := motion.Spec{Title: "Q3", Scenes: []motion.Scene{{Duration: 2, Elements: []motion.Element{{Type: "title", Text: "Q3"}}}}}
	rel, _, err := motion.Save(ws, spec)
	if err != nil {
		t.Fatal(err)
	}
	st, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Publish(artifacts.Input{SessionKey: "main", Kind: "file", Title: "Q3", Path: rel})
	if err != nil {
		t.Fatal(err)
	}
	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()
	mux := http.NewServeMux()
	registerArtifactRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18790/v1/artifacts/"+a.ID+"/motion", nil)
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	got := decode(t, rec.Body.Bytes())
	if rec.Code != 200 || !strings.Contains(got["html"].(string), "ghostMotion") {
		t.Fatalf("motion: %d %v", rec.Code, got["error"])
	}
	// A change is the next version, under the same name.
	body, _ := json.Marshal(map[string]interface{}{"spec": map[string]interface{}{"title": "renamed", "scenes": []interface{}{
		map[string]interface{}{"duration": 3, "elements": []interface{}{map[string]interface{}{"type": "title", "text": "Q3, in numbers"}}}}}})
	req = httptest.NewRequest(http.MethodPut, "http://127.0.0.1:18790/v1/artifacts/"+a.ID+"/motion", strings.NewReader(string(body)))
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	got = decode(t, rec.Body.Bytes())
	if rec.Code != 200 || got["artifact"].(map[string]interface{})["path"] != "motion/q3-v2.json" {
		t.Fatalf("change: %d %v", rec.Code, got)
	}
	// A bad change is refused and nothing is saved.
	req = httptest.NewRequest(http.MethodPut, "http://127.0.0.1:18790/v1/artifacts/"+a.ID+"/motion", strings.NewReader(`{"spec":{"title":"x","scenes":[]}}`))
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad change: %d", rec.Code)
	}
	// No video yet: export says so.
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18790/v1/artifacts/"+a.ID+"/export?format=mp4", nil)
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no video yet: %d", rec.Code)
	}
}
