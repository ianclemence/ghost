package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ianclemence/ghost/pkg/artifacts"
)

// registerArtifactRoutes exposes runtime-validated handoffs ("things Ghost
// hands you") to authenticated devices. The store is authoritative: the
// model proposes via the publish_artifact tool, validation happens in the
// artifacts package, and mobile renders what these endpoints return.
//
//	GET  /v1/artifacts?conversation_id=&since=  list a conversation's artifacts
//	GET  /v1/artifacts/{id}                     one artifact (revalidated)
//
// File bytes are NOT served here: clients preview file-backed artifacts
// through the existing bounded /v1/workspace/file endpoint. Text content
// for text-kind artifacts is included inline (bounded at creation).
func registerArtifactRoutes(mux *http.ServeMux) {
	storeOf := func() (*artifacts.Store, error) {
		if apiDB == nil {
			return nil, errNoDB()
		}
		return artifacts.NewStore(apiDB, apiWorkspaceDir)
	}

	mux.HandleFunc("/v1/artifacts", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		st, err := storeOf()
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "artifacts are unavailable right now")
			return
		}
		conversation := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
		if conversation == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "conversation_id is required")
			return
		}
		limit := 50
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		items, err := st.List(conversation, limit)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "could not list artifacts")
			return
		}
		if items == nil {
			items = []artifacts.Artifact{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "artifacts": items})
	}))

	mux.HandleFunc("/v1/artifacts/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		st, err := storeOf()
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "artifacts are unavailable right now")
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/artifacts/"), "/")
		if id == "" || strings.Contains(id, "/") {
			jsonError(w, http.StatusBadRequest, "invalid_request", "artifact id is required")
			return
		}
		a, err := st.Get(id)
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "that artifact does not exist")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "artifact": a})
	}))
}

func errNoDB() error {
	return errors.New("no database")
}
