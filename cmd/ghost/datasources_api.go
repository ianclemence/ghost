package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/dashboards"
)

// registerDataSourceRoutes is where dashboards draw from.
//
//	GET    /v1/datasources          the Pod's own data and files (with their tables) and connected databases
//	POST   /v1/datasources          {name, url}: connect a Postgres database (tested before it is kept)
//	DELETE /v1/datasources/{name}   forget a database
//
// A database address is kept sealed in the vault and never returned.
func registerDataSourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/datasources", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "sources": dashboards.Sources(ctx, apiWorkspaceDir), "databases": credentials.ListDatabases()})
		case http.MethodPost:
			var req struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			name := credentials.NormalizeDatabaseName(req.Name)
			if name == dashboards.PodSource {
				jsonError(w, http.StatusBadRequest, "invalid_request", "choose another name")
				return
			}
			if _, exists := credentials.DatabaseFor(name); exists {
				jsonError(w, http.StatusConflict, "conflict", "a database by that name is already connected")
				return
			}
			if err := credentials.SaveDatabase(credentials.Database{Name: name, URL: strings.TrimSpace(req.URL)}); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			// Kept only once it answers: a typo is not left behind.
			if err := dashboards.TestConnection(ctx, name); err != nil {
				_ = credentials.DeleteDatabase(name)
				jsonError(w, http.StatusBadRequest, "unreachable", "Couldn't connect: "+err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": name})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET or POST")
		}
	}))
	mux.HandleFunc("/v1/datasources/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use DELETE")
			return
		}
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/datasources/"), "/")
		if _, ok := credentials.DatabaseFor(name); !ok {
			jsonError(w, http.StatusNotFound, "not_found", "no such database")
			return
		}
		if err := credentials.DeleteDatabase(name); err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	}))
}
