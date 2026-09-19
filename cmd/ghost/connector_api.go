package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/connector"
)

// connectorWorkspace resolves the workspace that holds installed connectors.
func connectorWorkspace(al *agent.AgentLoop) string {
	if al == nil || al.Config() == nil {
		return ""
	}
	return al.Config().Agents.Defaults.Workspace
}

// connectorView renders an installed manifest for the directory.
func connectorView(m *connector.Manifest, source string) map[string]interface{} {
	caps := make([]map[string]interface{}, 0, len(m.Capabilities))
	for _, c := range m.Capabilities {
		entry := map[string]interface{}{"id": c.ID}
		if c.Title != "" {
			entry["title"] = c.Title
		}
		entry["risk"] = string(c.Risk)
		caps = append(caps, entry)
	}
	return map[string]interface{}{
		"id":           m.ID,
		"display_name": m.DisplayName,
		"description":  m.Description,
		"kind":         string(m.Kind),
		"version":      m.Version,
		"source":       source,
		"auth":         map[string]interface{}{"kind": string(m.Auth.Kind), "setup": string(m.Auth.Setup)},
		"capabilities": caps,
	}
}

// firstPartyView renders a built-in connected app as a connector entry so the
// directory is one list, not two.
func firstPartyView(c connectedapp.Connector) map[string]interface{} {
	caps := make([]map[string]interface{}, 0, len(c.Capabilities))
	for _, id := range c.Capabilities {
		caps = append(caps, map[string]interface{}{"id": id})
	}
	return map[string]interface{}{
		"id":           c.ID,
		"display_name": c.DisplayName,
		"description":  c.Help,
		"kind":         "native",
		"version":      "first-party",
		"source":       "first-party",
		"auth":         map[string]interface{}{"kind": string(c.AuthKind), "setup": string(c.Setup)},
		"capabilities": caps,
	}
}

// connectorsListHandler lists installed and first-party connectors, optionally
// filtered by capability (or id/name) substring.
func connectorsListHandler(w http.ResponseWriter, r *http.Request, al *agent.AgentLoop) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	out := []map[string]interface{}{}
	installed, err := connector.LoadInstalled(connectorWorkspace(al))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "unavailable", "could not read installed connectors")
		return
	}
	for _, m := range installed {
		out = append(out, connectorView(m, "installed"))
	}
	for _, c := range connectedapp.FirstParty() {
		out = append(out, firstPartyView(c))
	}

	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("capability")))
	if filter != "" {
		out = filterConnectors(out, filter)
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "connectors": out})
}

func filterConnectors(in []map[string]interface{}, filter string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(in))
	for _, c := range in {
		if connectorMatches(c, filter) {
			out = append(out, c)
		}
	}
	return out
}

func connectorMatches(c map[string]interface{}, filter string) bool {
	if id, _ := c["id"].(string); strings.Contains(strings.ToLower(id), filter) {
		return true
	}
	if name, _ := c["display_name"].(string); strings.Contains(strings.ToLower(name), filter) {
		return true
	}
	caps, _ := c["capabilities"].([]map[string]interface{})
	for _, cap := range caps {
		if id, _ := cap["id"].(string); strings.Contains(strings.ToLower(id), filter) {
			return true
		}
	}
	return false
}

// connectorsInstallHandler installs a connector from a server-side path
// ({"source": "..."}) or a provided manifest ({"manifest": {...}}).
func connectorsInstallHandler(w http.ResponseWriter, r *http.Request, al *agent.AgentLoop) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ws := connectorWorkspace(al)
	if ws == "" {
		jsonError(w, http.StatusInternalServerError, "unavailable", "no workspace configured")
		return
	}
	var req struct {
		Source   string              `json:"source"`
		Manifest *connector.Manifest `json:"manifest"`
		Force    bool                `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	dir := filepath.Join(ws, connector.InstalledDir)

	if req.Manifest != nil {
		if verrs := req.Manifest.Validate(); len(verrs) > 0 {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": false, "errors": verrs})
			return
		}
		dest, err := connector.InstallManifest(req.Manifest, dir, req.Force)
		if err != nil {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "installed": dest, "id": req.Manifest.ID})
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		jsonError(w, http.StatusBadRequest, "invalid_request", "provide a manifest or a source path")
		return
	}
	dest, err := connector.Install(req.Source, dir, req.Force)
	if err != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "installed": dest})
}

// connectorsUninstallHandler removes an installed connector by id.
func connectorsUninstallHandler(w http.ResponseWriter, r *http.Request, al *agent.AgentLoop) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ws := connectorWorkspace(al)
	if ws == "" {
		jsonError(w, http.StatusInternalServerError, "unavailable", "no workspace configured")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		jsonError(w, http.StatusBadRequest, "invalid_request", "id required")
		return
	}
	if err := connector.Uninstall(filepath.Join(ws, connector.InstalledDir), req.ID); err != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// connectorsReviewHandler runs the review pipeline on a provided manifest.
func connectorsReviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	var req struct {
		Manifest *connector.Manifest `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Manifest == nil {
		jsonError(w, http.StatusBadRequest, "invalid_request", "manifest required")
		return
	}
	findings := connector.Review(req.Manifest)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"findings":   findings,
		"has_errors": connector.HasErrors(findings),
	})
}
