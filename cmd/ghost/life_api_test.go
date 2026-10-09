package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/life"
)

func TestLifeRoutesEditAndForget(t *testing.T) {
	old := apiWorkspaceDir
	apiWorkspaceDir = t.TempDir()
	defer func() { apiWorkspaceDir = old }()
	mux := http.NewServeMux()
	registerLifeRoutes(mux, func() *time.Location { return time.UTC })

	p, _, err := life.PeopleFor(apiWorkspaceDir).Remember("", life.PersonUpdate{Name: "Sam", Birthday: "10-10", Source: life.Source{Kind: "conversation"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, map[string]interface{}) {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18790"+path, nil)
		req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, decode(t, rec.Body.Bytes())
	}
	code, out := get("/v1/life/people")
	people, _ := out["people"].([]interface{})
	if code != 200 || len(people) != 1 || people[0].(map[string]interface{})["next_birthday"] == nil {
		t.Fatalf("people: %d %v", code, out)
	}
	code, out = postJSON(t, mux, "/v1/life/people/"+p.ID, map[string]interface{}{"name": "Sam Otieno", "birthday": "not a date"})
	if code != http.StatusBadRequest {
		t.Fatalf("a bad edit: %d %v", code, out)
	}
	code, out = postJSON(t, mux, "/v1/life/people/"+p.ID, map[string]interface{}{"name": "Sam Otieno", "relation": "friend", "birthday": "1990-10-10"})
	if code != 200 {
		t.Fatalf("edit: %d %v", code, out)
	}
	req := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:18790/v1/life/people/"+p.ID, nil)
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("forget: %d", rec.Code)
	}
	if all, _ := life.PeopleFor(apiWorkspaceDir).All(); len(all) != 0 {
		t.Fatal("forgotten person is still there")
	}
	if code, out := get("/v1/life/money?month=2026-10"); code != 200 || out["summary"] == nil {
		t.Fatalf("money: %d %v", code, out)
	}
	if code, _ := get("/v1/life/money?month=october"); code != http.StatusBadRequest {
		t.Fatalf("a bad month: %d", code)
	}
}

func decode(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func TestKnowledgeRoutes(t *testing.T) {
	old := apiWorkspaceDir
	apiWorkspaceDir = t.TempDir()
	defer func() { apiWorkspaceDir = old }()
	mux := http.NewServeMux()
	registerLifeRoutes(mux, func() *time.Location { return time.UTC })
	code, out := postJSON(t, mux, "/v1/life/knowledge", map[string]interface{}{"title": "Sapiens", "kind": "book", "author": "Yuval Noah Harari", "current": 40, "total": 443})
	if code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	item := out["item"].(map[string]interface{})
	id := item["id"].(string)
	if item["status"] != "active" || item["unit"] != "page" {
		t.Fatalf("item: %v", item)
	}
	code, out = postJSON(t, mux, "/v1/life/knowledge/"+id, map[string]interface{}{"title": "Sapiens", "kind": "book", "unit": "page", "current": 443, "total": 443, "status": "done"})
	if code != 200 || out["item"].(map[string]interface{})["finished"] == nil {
		t.Fatalf("finish: %d %v", code, out)
	}
	if code, _ := postJSON(t, mux, "/v1/life/knowledge", map[string]interface{}{"title": "x", "kind": "movie"}); code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d", code)
	}
	req := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:18790/v1/life/knowledge/"+id, nil)
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:18790"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("forget: %d", rec.Code)
	}
}
