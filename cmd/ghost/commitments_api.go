package main

import (
	"net/http"
	"strings"

	"github.com/ianclemence/ghost/pkg/commitments"
)

// The promise ledger surface: what Ghost is holding on the owner's behalf, and
// the owner's ability to close one. Cancelling a promise is an owner decision,
// so it is explicit here rather than something a suggestion could do.
func registerCommitmentRoutes(mux *http.ServeMux, apiWorkspaceDir string, agentLoop interface {
	Commitments() ([]commitments.Commitment, error)
	CancelCommitment(id, note string) (commitments.Commitment, error)
}, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/v1/commitments", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if agentLoop == nil || apiWorkspaceDir == "" {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "promises are unavailable right now")
			return
		}
		list, err := agentLoop.Commitments()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "unavailable", "promises are unavailable right now")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "commitments": list})
	}))

	mux.HandleFunc("/v1/commitments/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if agentLoop == nil || apiWorkspaceDir == "" {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "promises are unavailable right now")
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/commitments/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) != 2 || parts[1] != "cancel" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "use /v1/commitments/{id}/cancel")
			return
		}
		updated, err := agentLoop.CancelCommitment(parts[0], "the owner closed this one")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "cancel_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "commitment": updated})
	}))
}
