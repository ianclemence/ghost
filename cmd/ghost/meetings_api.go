package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/meetings"
	"github.com/ianclemence/ghost/pkg/voice"
)

// registerMeetingRoutes receives recordings from the phone and transcribes
// them on the Pod.
//
//	POST /v1/meetings                 {title, mime}  start a recording upload
//	POST /v1/meetings/{id}/chunk      {seq, data}    one piece (base64, up to 2 MB)
//	POST /v1/meetings/{id}/finish     {chunks}       all pieces sent: transcribe
//	GET  /v1/meetings                                recordings, newest first
//	GET  /v1/meetings/{id}                           one recording and its progress
func registerMeetingRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	transcriber := func() (meetings.Transcriber, error) {
		gcfg := al.Config()
		engine := voice.EngineFromEnv(voice.SelectConfig{
			Engine:      gcfg.STT.Engine,
			LocalURL:    voice.LocalBaseURL(gcfg.STT.Port),
			MoonshotKey: gcfg.Providers.Moonshot.APIKey,
			GroqKey:     gcfg.Providers.Groq.APIKey,
		}, voice.SynthConfig{Engine: gcfg.TTS.Engine, Speed: gcfg.TTS.Speed})
		if !engine.InputAvailable() {
			return nil, errors.New("Speech recognition isn't set up on your Pod yet. Turn it on under Voice in the console.")
		}
		return func(ctx context.Context, audio []byte, mime string) (string, error) {
			tr, err := engine.Transcribe(ctx, audio, mime)
			if err != nil {
				return "", err
			}
			return tr.Text, nil
		}, nil
	}
	publisher := func(title, summary, path string) (string, error) {
		if apiDB == nil {
			return "", errNoDB()
		}
		st, err := artifacts.NewStore(apiDB, apiWorkspaceDir)
		if err != nil {
			return "", err
		}
		a, err := st.Publish(artifacts.Input{SessionKey: ownerConversation, Kind: artifacts.KindFile, Title: title, Summary: summary, Path: path})
		if err != nil {
			return "", err
		}
		return a.ID, nil
	}
	// Recordings a restart interrupted carry on.
	go func() {
		time.Sleep(20 * time.Second)
		if tr, err := transcriber(); err == nil && apiWorkspaceDir != "" {
			meetings.For(apiWorkspaceDir).Resume(tr, publisher)
		}
	}()

	store := func() *meetings.Store { return meetings.For(apiWorkspaceDir) }

	mux.HandleFunc("/v1/meetings", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list, err := store().List()
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", err.Error())
				return
			}
			if list == nil {
				list = []meetings.Meeting{}
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "meetings": list})
		case http.MethodPost:
			if err := meetings.Available(); err != nil {
				jsonError(w, http.StatusServiceUnavailable, "unavailable", err.Error())
				return
			}
			if _, err := transcriber(); err != nil {
				jsonError(w, http.StatusServiceUnavailable, "voice_unavailable", err.Error())
				return
			}
			var req struct {
				Title string `json:"title"`
				Mime  string `json:"mime"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			m, err := store().Begin(req.Title, req.Mime, time.Now())
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "meeting": m})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET or POST")
		}
	}))

	mux.HandleFunc("/v1/meetings/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/meetings/"), "/"), "/")
		id := parts[0]
		sub := ""
		if len(parts) > 1 {
			sub = parts[1]
		}
		switch {
		case r.Method == http.MethodGet && sub == "":
			m, err := store().Get(id)
			if err != nil {
				jsonError(w, http.StatusNotFound, "not_found", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "meeting": m})
		case r.Method == http.MethodPost && sub == "chunk":
			var req struct {
				Seq  int    `json:"seq"`
				Data string `json:"data"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, (meetings.MaxChunkBytes*4)/3+4096)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid piece")
				return
			}
			data, err := base64.StdEncoding.DecodeString(req.Data)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "a piece is base64")
				return
			}
			m, err := store().Chunk(id, req.Seq, data)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "meeting": m})
		case r.Method == http.MethodPost && sub == "finish":
			var req struct {
				Chunks int `json:"chunks"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			tr, err := transcriber()
			if err != nil {
				jsonError(w, http.StatusServiceUnavailable, "voice_unavailable", err.Error())
				return
			}
			m, err := store().Finish(id, req.Chunks, tr, publisher)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "meeting": m})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "not something a recording does")
		}
	}))
}
