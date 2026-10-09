package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/jobs"
	"github.com/ianclemence/ghost/pkg/life"
	calprov "github.com/ianclemence/ghost/pkg/providers/calendar"
	gmailprov "github.com/ianclemence/ghost/pkg/providers/gmail"
	"github.com/ianclemence/ghost/pkg/providers/hass"
	outlookprov "github.com/ianclemence/ghost/pkg/providers/outlook"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// registerJobRoutes offers what Ghost can take on, as jobs.
//
//	GET  /v1/jobs        every job, whether it is on, and what it still needs
//	POST /v1/jobs/{id}   {enabled, time?, topic?}  turn a job on (or change it) or off
//
// A job that is on is a routine: the time the owner chose and the job's
// instruction, run as a turn and delivered to the conversation. Turning it
// off cancels that routine.
func registerJobRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	store := func() *jobs.Store { return jobs.Open(apiWorkspaceDir) }
	svcOf := func() (*routines.Service, error) {
		if apiDB == nil {
			return nil, errNoDB()
		}
		st := scheduled.NewStore(apiDB)
		if err := st.InitSchema(); err != nil {
			return nil, err
		}
		return routines.New(apiDB, st)
	}
	// What each need means, checked now.
	ready := func(need string) (bool, string) {
		switch need {
		case "email":
			if gmailprov.New(gmailprov.Config{}).Configured() || outlookprov.New(outlookprov.Config{}).Configured() {
				return true, ""
			}
			return false, "Connect Gmail or Outlook under Connected apps"
		case "calendar":
			if calprov.New(calprov.Config{}).Configured() {
				return true, ""
			}
			return false, "Connect Google Calendar under Connected apps"
		case "home":
			u, tok := credentials.HassEndpoint()
			if (hass.Config{Base: u, Token: tok}).Configured() {
				return true, ""
			}
			return false, "Connect Home Assistant under Connected apps"
		case "health":
			now := time.Now()
			days, _ := life.DeviceFor(apiWorkspaceDir).Health(now.AddDate(0, 0, -14).Format("2006-01-02"), now.Format("2006-01-02"))
			if len(days) > 0 {
				return true, ""
			}
			return false, "Turn on Health in Settings → Phone"
		}
		return true, ""
	}

	mux.HandleFunc("/v1/jobs", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		states, err := store().All()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		svc, _ := svcOf()
		type view struct {
			jobs.Job
			Enabled  bool          `json:"enabled"`
			Settings jobs.Settings `json:"settings"`
			Missing  []string      `json:"missing,omitempty"`
		}
		out := make([]view, 0, len(jobs.Catalog))
		for _, j := range jobs.Catalog {
			v := view{Job: j}
			if st, ok := states[j.ID]; ok && st.Enabled {
				v.Enabled, v.Settings = true, st.Settings
				// A routine cancelled elsewhere (the Routines screen) ends the job.
				if svc != nil && st.RoutineID != "" {
					if rt, err := svc.Get(st.RoutineID); err != nil || rt == nil || rt.Status == routines.StatusCancelled {
						v.Enabled = false
						_ = store().Set(j.ID, jobs.State{Enabled: false})
					}
				}
			}
			if j.Time == "" {
				v.Enabled = true
			}
			if v.Settings.Time == "" {
				v.Settings.Time = j.Time
			}
			for _, n := range j.Needs {
				if ok, why := ready(n); !ok {
					v.Missing = append(v.Missing, why)
				}
			}
			out = append(out, v)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "jobs": out})
	}))

	mux.HandleFunc("/v1/jobs/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/jobs/"), "/")
		j, ok := jobs.Find(id)
		if !ok {
			jsonError(w, http.StatusNotFound, "not_found", "no such job")
			return
		}
		var req struct {
			Enabled bool   `json:"enabled"`
			Time    string `json:"time"`
			Topic   string `json:"topic"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
			return
		}
		svc, err := svcOf()
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "routines are unavailable right now")
			return
		}
		states, _ := store().All()
		prev := states[id]
		// Whatever was running for this job stops first: changing the time is
		// a new routine, never two.
		if prev.RoutineID != "" {
			_ = svc.Cancel(prev.RoutineID)
		}
		if !req.Enabled {
			if err := store().Set(id, jobs.State{Enabled: false}); err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "enabled": false})
			return
		}
		set, err := j.Check(jobs.Settings{Time: req.Time, Topic: req.Topic})
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		ghostID, owner := "ghost-local", "owner"
		if ident, err := ghoststate.LoadIdentity(apiWorkspaceDir); err == nil && ident != nil {
			ghostID, owner = ident.GhostID, ident.OwnerName
		}
		tz := "UTC"
		if al != nil {
			tz = al.OwnerLocation().String()
		}
		rt, err := svc.Create(ghostID, owner, j.Title, j.Instruction(set), tz, scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: j.Cron(set)}, nil)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "couldn't schedule it: "+err.Error())
			return
		}
		// The job reports to the owner's conversation on the phone.
		st := scheduled.NewStore(apiDB)
		if item, err := st.Get(rt.ID); err == nil {
			item.Channel, item.ChatID = "mobile", "default"
			_ = st.Update(item)
		}
		if err := store().Set(id, jobs.State{Enabled: true, RoutineID: rt.ID, Settings: set, Since: time.Now().UTC()}); err != nil {
			_ = svc.Cancel(rt.ID)
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		next := ""
		if rt.NextRun != nil {
			next = rt.NextRun.Format(time.RFC3339)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "enabled": true, "settings": set, "next_run_at": next})
	}))
}
