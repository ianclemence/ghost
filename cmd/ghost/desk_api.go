package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/desk"
	"github.com/ianclemence/ghost/pkg/skills"
)

// registerDeskRoutes wires the Desk: one read-only feed of the work Ghost has
// done on the owner's own machine. It is a projection over authorities Ghost
// already has (workspace files, artifacts, workspace tools, live surfaces) —
// no new storage, no execution, no authority. Acting on an item is a new
// governed conversation turn; the Desk grants nothing.
//
// See docs/DESK.md.
func registerDeskRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	mux.HandleFunc("/v1/desk", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		items := desk.List(gatherDeskInputs(al))
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":    true,
			"items": items,
		})
	}))
}

// gatherDeskInputs reads every authority and returns the raw inputs the Desk
// projection normalizes. Each authority is read defensively: a missing or
// unavailable source contributes nothing rather than failing the feed, so a
// partially-available Ghost still shows what it honestly can.
func gatherDeskInputs(al *agent.AgentLoop) desk.Inputs {
	var in desk.Inputs
	in.Documents = gatherWorkspaceDocuments()
	in.Artifacts = gatherDeskArtifacts()
	in.Tools = gatherDeskTools()
	in.Surfaces = gatherDeskSurfaces(al)
	return in
}

// gatherWorkspaceDocuments lists workspace files the owner may see. It uses
// the same estate boundary as the workspace files API; protected internal
// paths are filtered here and re-checked inside the projection.
func gatherWorkspaceDocuments() []desk.DocumentInput {
	if apiWorkspaceDir == "" {
		return nil
	}
	var out []desk.DocumentInput
	if _, err := os.Stat(apiWorkspaceDir); err != nil {
		return nil
	}
	_ = filepath.Walk(apiWorkspaceDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(apiWorkspaceDir, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if workspaceFileProtected(rel) {
			return nil
		}
		// Files inside skills/ are represented once, as a tool (or not at
		// all when bundled). Showing SKILL.md as both a tool and a raw
		// document would double-count the same thing on the owner's Desk.
		if rel == "skills" || strings.HasPrefix(rel, "skills/") {
			return nil
		}
		out = append(out, desk.DocumentInput{
			RelPath:   rel,
			Size:      info.Size(),
			UpdatedAt: info.ModTime(),
		})
		return nil
	})
	return out
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

// gatherDeskTools lists workspace tools — skills that live in the owner's
// workspace and are not bundled. These are the tools Ghost (or the owner)
// built or installed, as opposed to Ghost's built-in abilities.
func gatherDeskTools() []desk.ToolInput {
	skillsDir := filepath.Join(apiWorkspaceDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}
	manifest, _ := skills.LoadManifest(skillsDir)
	var out []desk.ToolInput
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		skillPath := filepath.Join(skillsDir, name)
		var info os.FileInfo
		var desc string
		if b, rerr := os.ReadFile(filepath.Join(skillPath, "SKILL.md")); rerr == nil {
			desc = skillSummaryMD(string(b))
			info, _ = os.Stat(filepath.Join(skillPath, "SKILL.md"))
		} else if b, rerr := os.ReadFile(filepath.Join(skillPath, "SKILL.md.disabled")); rerr == nil {
			desc = skillSummaryMD(string(b))
			info, _ = os.Stat(filepath.Join(skillPath, "SKILL.md.disabled"))
		} else {
			continue
		}
		_, bundled := manifest.Skills[name]
		if bundled {
			continue // a built-in ability, not an owner tool
		}
		updated := time.Now()
		if info != nil {
			updated = info.ModTime()
		}
		out = append(out, desk.ToolInput{
			Name:        name,
			Description: desc,
			Built:       true,
			UpdatedAt:   updated,
		})
	}
	return out
}

// gatherDeskSurfaces maps live browser/computer surfaces the runtime acted on
// into Desk items. Only owner-safe observation fields are carried.
func gatherDeskSurfaces(al *agent.AgentLoop) []desk.SurfaceInput {
	if al == nil {
		return nil
	}
	plane := al.LivePlane()
	if plane == nil {
		return nil
	}
	var out []desk.SurfaceInput
	for _, s := range plane.List() {
		out = append(out, desk.SurfaceInput{
			ID:        s.ID,
			Kind:      string(s.Kind),
			State:     string(s.State),
			Title:     s.Obs.Title,
			URL:       s.Obs.URL,
			UpdatedAt: s.Updated,
		})
	}
	return out
}
