package main

import (
	"net/http"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/desk"
)

// registerDeskRoutes wires the Desk: a read-only feed of the things Ghost has
// made FOR the owner. It is a projection over the artifacts authority Ghost
// already validates — no new storage, no execution, no authority.
//
// Scope is deliberately narrow. An earlier version surfaced every workspace
// file, which meant the owner saw internal files (proactive outboxes, run
// logs) and file sizes — a file manager, not a product. The Desk shows only
// what Ghost produced on the owner's behalf, which is what a person actually
// wants to look at.
func registerDeskRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	mux.HandleFunc("/v1/desk", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		items := desk.List(gatherDeskInputs())
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":    true,
			"items": items,
		})
	}))
}

// gatherDeskInputs reads the artifacts authority and returns the raw inputs
// the Desk projection normalizes. A missing backend degrades to an empty feed
// rather than failing, so a partially-available Ghost still shows what it can.
func gatherDeskInputs() desk.Inputs {
	return desk.Inputs{Artifacts: gatherDeskArtifacts()}
}

// gatherDeskArtifacts reads validated handoffs from the artifacts store. The
// store already bounds what may become an artifact, so no extra filtering is
// needed here. The Desk is cross-conversation by design: the owner's work is
// one shelf, not one shelf per chat, so we use ListAll.
func gatherDeskArtifacts() []desk.ArtifactInput {
	if apiDB == nil {
		return nil
	}
	st, err := artifacts.NewStore(apiDB, apiWorkspaceDir)
	if err != nil {
		return nil
	}
	items, err := st.ListAll(100)
	if err != nil {
		return nil
	}
	out := make([]desk.ArtifactInput, 0, len(items))
	for _, a := range items {
		out = append(out, desk.ArtifactInput{
			ID:        a.ID,
			Kind:      a.Kind,
			Title:     a.Title,
			Summary:   a.Summary,
			Path:      a.Path,
			URL:       a.URL,
			State:     a.State,
			CreatedAt: a.CreatedAt,
		})
	}
	return out
}
