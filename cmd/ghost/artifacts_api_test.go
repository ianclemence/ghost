package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/dashboards"
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

func TestDashboardAndDataSourceRoutes(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	rel, err := dashboards.Save(ws, dashboards.Dashboard{Title: "Mine", Tiles: []dashboards.Tile{{Title: "Trips", Chart: "metric", Query: "SELECT COUNT(*) FROM trips"}}})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := artifacts.NewStore(db, ws)
	a, err := st.Publish(artifacts.Input{SessionKey: "main", Kind: "file", Title: "Mine", Path: rel})
	if err != nil {
		t.Fatal(err)
	}
	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()
	mux := http.NewServeMux()
	registerArtifactRoutes(mux)
	registerDataSourceRoutes(mux)
	get := func(path string) (int, map[string]interface{}) {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18790"+path, nil)
		req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, decode(t, rec.Body.Bytes())
	}
	code, out := get("/v1/artifacts/" + a.ID + "/dashboard")
	tiles, _ := out["tiles"].([]interface{})
	if code != 200 || len(tiles) != 1 || tiles[0].(map[string]interface{})["value"] != 0.0 {
		t.Fatalf("dashboard: %d %v", code, out)
	}
	code, out = get("/v1/datasources")
	if code != 200 || len(out["sources"].([]interface{})) != 1 {
		t.Fatalf("sources: %d %v", code, out)
	}
	// A database that doesn't answer is not kept.
	code, _ = postJSON(t, mux, "/v1/datasources", map[string]interface{}{"name": "shop", "url": "postgres://u:p@127.0.0.1:1/x?connect_timeout=2"})
	if code != http.StatusBadRequest {
		t.Fatalf("unreachable: %d", code)
	}
	if len(credentials.ListDatabases()) != 0 {
		t.Fatal("an unreachable database was kept")
	}
	if code, _ := postJSON(t, mux, "/v1/datasources", map[string]interface{}{"name": "pod", "url": "postgres://u:p@h/x"}); code != http.StatusBadRequest {
		t.Fatalf("reserved name: %d", code)
	}
}

func TestDeleteRoutes(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := artifacts.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	st, _ := artifacts.NewStore(db, ws)
	var ids []string
	for _, v := range []string{"v1", "v2", "v3"} {
		p := filepath.Join(ws, "canvas", "chess-"+v+".html")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("<p>x</p>"), 0o644)
		a, err := st.Publish(artifacts.Input{SessionKey: "main", Kind: "file", Title: "Chess", Path: "canvas/chess-" + v + ".html"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	prevDB, prevWS := apiDB, apiWorkspaceDir
	apiDB, apiWorkspaceDir = db, ws
	defer func() { apiDB, apiWorkspaceDir = prevDB, prevWS }()
	mux := http.NewServeMux()
	registerArtifactRoutes(mux)
	call := func(method, path string) (int, map[string]interface{}) {
		req := httptest.NewRequest(method, "http://127.0.0.1:18790"+path, nil)
		req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, decode(t, rec.Body.Bytes())
	}
	if code, out := call(http.MethodGet, "/v1/artifacts/"+ids[2]+"/versions"); code != 200 || len(out["versions"].([]interface{})) != 3 {
		t.Fatalf("versions: %d %v", code, out)
	}
	if code, out := call(http.MethodDelete, "/v1/artifacts/"+ids[0]); code != 200 || out["deleted"] != 1.0 {
		t.Fatalf("delete one: %d %v", code, out)
	}
	if code, out := call(http.MethodDelete, "/v1/artifacts/"+ids[2]+"?all=1"); code != 200 || out["deleted"] != 2.0 {
		t.Fatalf("delete all: %d %v", code, out)
	}
}
