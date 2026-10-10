package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/dashboards"
	"github.com/ianclemence/ghost/pkg/documents"
	"github.com/ianclemence/ghost/pkg/motion"
	"github.com/ianclemence/ghost/pkg/tools"
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

	//	GET  /v1/artifacts/{id}/page?n=1&w=900   one page of a PDF, as a picture
	//	GET  /v1/artifacts/{id}/export?format=pdf|docx  the file, to share or keep
	//	POST /v1/artifacts/{id}/pin {pinned}      keep it at the top of the shelf
	mux.HandleFunc("/v1/artifacts/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		st, err := storeOf()
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "artifacts are unavailable right now")
			return
		}
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/artifacts/"), "/"), "/")
		id := parts[0]
		if id == "" || len(parts) > 2 {
			jsonError(w, http.StatusBadRequest, "invalid_request", "artifact id is required")
			return
		}
		a, err := st.Get(id)
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "that artifact does not exist")
			return
		}
		sub := ""
		if len(parts) == 2 {
			sub = parts[1]
		}
		switch {
		case sub == "" && r.Method == http.MethodGet:
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "artifact": a})
		case sub == "" && r.Method == http.MethodDelete:
			// The owner deletes what Ghost made: this version, or with all=1
			// the whole thing. Uploads stay in Files.
			var n int
			var err error
			if r.URL.Query().Get("all") == "1" {
				n, err = st.DeleteAll(a.ID)
			} else {
				n, err = st.Delete(a.ID)
			}
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", "couldn't delete it")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": n})
		case sub == "versions" && r.Method == http.MethodGet:
			vs, err := st.Versions(a.ID)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", "couldn't list its versions")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "versions": vs})
		case sub == "page" && r.Method == http.MethodGet:
			servePage(w, r, a)
		case sub == "export" && r.Method == http.MethodGet:
			serveExport(w, r, a)
		case sub == "motion" && r.Method == http.MethodGet:
			// A motion with its player, for the phone to play as it is. Its
			// video opens the motion it was made from (motion_id says which).
			src, motionID := a.Path, a.ID
			if m, ok := motion.MotionOfVideo(a.Path); ok {
				src = m
				if ma, err := st.ByPath(m); err == nil {
					motionID = ma.ID
				}
			}
			s, err := motion.Load(apiWorkspaceDir, src)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "this is not a motion")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "spec": s, "duration": s.Duration(), "motion_id": motionID,
				"html": motion.Page(s, false), "video": motion.ExportsFor(apiWorkspaceDir).Status(src)})
		case sub == "motion" && r.Method == http.MethodPut:
			// The owner changed a word, a number or a timing: the next version.
			old, err := motion.Load(apiWorkspaceDir, a.Path)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "this is not a motion")
				return
			}
			var req struct {
				Spec json.RawMessage `json:"spec"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			s, err := motion.Parse(req.Spec)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			s.Title = old.Title // the same motion, so the same name
			rel, version, err := motion.Save(apiWorkspaceDir, s)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", err.Error())
				return
			}
			na, err := st.Publish(artifacts.Input{SessionKey: a.SessionKey, Kind: "file", Title: s.Title,
				Summary: fmt.Sprintf("Motion · %.0f s · version %d", s.Duration(), version), Path: rel})
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "artifact": na})
		case sub == "dashboard" && r.Method == http.MethodGet:
			// The dashboard with every chart's query run now.
			d, err := dashboards.Load(apiWorkspaceDir, a.Path)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "this is not a dashboard")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "title": d.Title,
				"tiles": dashboards.Data(r.Context(), apiWorkspaceDir, d), "ran_at": time.Now().UTC().Format(time.RFC3339)})
		case sub == "video" && r.Method == http.MethodGet:
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "video": motion.ExportsFor(apiWorkspaceDir).Status(a.Path)})
		case sub == "video" && r.Method == http.MethodPost:
			// Make the MP4 in the background; the phone asks how it is going.
			session, title := a.SessionKey, a.Title
			j, err := motion.ExportsFor(apiWorkspaceDir).Start(apiWorkspaceDir, a.Path, func(j motion.Job) {
				if j.State == "done" {
					_, _ = st.Publish(artifacts.Input{SessionKey: session, Kind: "file", Title: title, Summary: "Video", Path: j.Video})
				}
			})
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "video": j})
		case sub == "saved" && r.Method == http.MethodGet:
			// What a canvas kept with ghost.save, for the page to start from.
			raw, err := tools.ReadCanvasSaved(apiWorkspaceDir, a.Path)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "only a canvas keeps things")
				return
			}
			if raw == nil {
				raw = json.RawMessage("null")
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "saved": raw})
		case sub == "saved" && r.Method == http.MethodPut:
			var req struct {
				Saved json.RawMessage `json:"saved"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, tools.MaxCanvasSaved+1024)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "too large, or not JSON")
				return
			}
			if len(req.Saved) == 0 {
				req.Saved = json.RawMessage("null")
			}
			if err := tools.WriteCanvasSaved(apiWorkspaceDir, a.Path, req.Saved); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		case sub == "pin" && r.Method == http.MethodPost:
			var req struct {
				Pinned bool `json:"pinned"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "say whether it is pinned")
				return
			}
			if ok, err := st.SetPinned(a.ID, req.Pinned); !ok || err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", "could not change the pin")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "pinned": req.Pinned})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "not something an artifact does")
		}
	}))

	// GET /v1/shelf?q=&kind=pages|documents|pictures|links|notes&limit=
	// Everything Ghost has made, in every conversation: pinned first, then
	// newest, versions of one thing as one entry.
	mux.HandleFunc("/v1/shelf", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		st, err := storeOf()
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "the shelf is unavailable right now")
			return
		}
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		switch kind {
		case "", artifacts.ShelfPages, artifacts.ShelfDocuments, artifacts.ShelfPictures, artifacts.ShelfLinks, artifacts.ShelfNotes, artifacts.ShelfMotion, artifacts.ShelfDashboards:
		default:
			jsonError(w, http.StatusBadRequest, "invalid_request", "unknown kind")
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := st.Shelf(r.URL.Query().Get("q"), kind, limit)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", "could not read the shelf")
			return
		}
		if items == nil {
			items = []artifacts.ShelfItem{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "items": items})
	}))
}

// servePage draws one page of a PDF artifact as a PNG (as base64 in JSON, the
// way every file reaches the phone, so it travels the relay too).
func servePage(w http.ResponseWriter, r *http.Request, a *artifacts.Artifact) {
	if a.Kind != artifacts.KindFile || !strings.HasSuffix(strings.ToLower(a.Path), ".pdf") || a.State != artifacts.StateAvailable {
		jsonError(w, http.StatusBadRequest, "invalid_request", "only a PDF has pages")
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n < 1 {
		n = 1
	}
	width, _ := strconv.Atoi(r.URL.Query().Get("w"))
	png, pages, err := documents.PageImage(r.Context(), apiWorkspaceDir, a.Path, n, width)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, documents.ErrUnavailable) || pages == 0 {
			status = http.StatusServiceUnavailable
		}
		jsonResponse(w, status, map[string]interface{}{"ok": false, "pages": pages, "error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"ok": true, "page": n, "pages": pages, "mime_type": "image/png",
		"image_base64": base64.StdEncoding.EncodeToString(png),
	})
}

// serveExport hands over a file artifact to share: the PDF itself, or a Word
// copy made from a document's Markdown.
func serveExport(w http.ResponseWriter, r *http.Request, a *artifacts.Artifact) {
	if a.Kind != artifacts.KindFile || a.State != artifacts.StateAvailable {
		jsonError(w, http.StatusBadRequest, "invalid_request", "only a file can be exported")
		return
	}
	base := strings.TrimSuffix(filepath.Base(a.Path), filepath.Ext(a.Path))
	switch r.URL.Query().Get("format") {
	case "", "pdf":
		if !strings.HasSuffix(strings.ToLower(a.Path), ".pdf") {
			jsonError(w, http.StatusBadRequest, "invalid_request", "this file is not a PDF")
			return
		}
		b, err := os.ReadFile(filepath.Join(apiWorkspaceDir, filepath.Clean("/"+a.Path)))
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "the file is gone")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": base + ".pdf", "mime_type": "application/pdf", "base64": base64.StdEncoding.EncodeToString(b)})
	case "docx":
		src, ok := documents.SourceOf(a.Path)
		if !ok {
			jsonError(w, http.StatusBadRequest, "invalid_request", "only Ghost's own documents come as Word")
			return
		}
		b, err := documents.Word(r.Context(), apiWorkspaceDir, src)
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": base + ".docx",
			"mime_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "base64": base64.StdEncoding.EncodeToString(b)})
	case "mp4":
		// A motion's video, once made: from the motion, or the video itself.
		video := ""
		if motion.IsMotion(a.Path) {
			video = motion.VideoPath(a.Path)
		} else if _, ok := motion.MotionOfVideo(a.Path); ok {
			video = a.Path
		} else {
			jsonError(w, http.StatusBadRequest, "invalid_request", "only a motion comes as a video")
			return
		}
		b, err := os.ReadFile(filepath.Join(apiWorkspaceDir, filepath.Clean("/"+video)))
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "the video hasn't been made yet")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": base + ".mp4", "mime_type": "video/mp4", "base64": base64.StdEncoding.EncodeToString(b)})
	default:
		jsonError(w, http.StatusBadRequest, "invalid_request", "format is pdf, docx or mp4")
	}
}

func errNoDB() error {
	return errors.New("no database")
}
