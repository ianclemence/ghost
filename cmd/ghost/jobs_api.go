package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/ianclemence/ghost/pkg/tools"
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
		tz := tools.DeviceLocation().String()
		if al != nil {
			tz = al.OwnerLocation().String()
		}
		set, next, err := applyJob(apiDB, apiWorkspaceDir, tz, j, req.Enabled, req.Time, req.Topic)
		switch {
		case errors.Is(err, errJobsUnavailable):
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "routines are unavailable right now")
			return
		case errors.Is(err, errJobInvalid):
			jsonError(w, http.StatusBadRequest, "invalid_request", strings.TrimPrefix(err.Error(), errJobInvalid.Error()+": "))
			return
		case err != nil:
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		if !req.Enabled {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "enabled": false})
			return
		}
		at := ""
		if next != nil {
			at = next.Format(time.RFC3339)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "enabled": true, "settings": set, "next_run_at": at})
	}))
}

var (
	errJobsUnavailable = errors.New("routines are unavailable")
	errJobInvalid      = errors.New("invalid")
)

// applyJob turns a job on (or changes its time) or off: the one way to do it,
// for the API and the CLI alike. Whatever was running for the job stops first,
// so changing the time is a new routine, never two. A job that is on reports
// to the owner's conversation on the phone.
func applyJob(database *sql.DB, workspace, tz string, j jobs.Job, enabled bool, at, topic string) (jobs.Settings, *time.Time, error) {
	if database == nil {
		return jobs.Settings{}, nil, errJobsUnavailable
	}
	st := scheduled.NewStore(database)
	if err := st.InitSchema(); err != nil {
		return jobs.Settings{}, nil, errJobsUnavailable
	}
	svc, err := routines.New(database, st)
	if err != nil {
		return jobs.Settings{}, nil, errJobsUnavailable
	}
	store := jobs.Open(workspace)
	// Settings are checked before anything stops, so a bad time never
	// leaves a job that was on turned off.
	var set jobs.Settings
	if enabled {
		if set, err = j.Check(jobs.Settings{Time: at, Topic: topic}); err != nil {
			return set, nil, fmt.Errorf("%w: %s", errJobInvalid, err.Error())
		}
	}
	states, _ := store.All()
	if prev := states[j.ID]; prev.RoutineID != "" {
		_ = svc.Cancel(prev.RoutineID)
	}
	if !enabled {
		return set, nil, store.Set(j.ID, jobs.State{Enabled: false})
	}
	ghostID, owner := "ghost-local", "owner"
	if ident, err := ghoststate.LoadIdentity(workspace); err == nil && ident != nil {
		ghostID, owner = ident.GhostID, ident.OwnerName
	}
	rt, err := svc.Create(ghostID, owner, j.Title, j.Instruction(set), tz, scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: j.Cron(set)}, nil)
	if err != nil {
		return set, nil, fmt.Errorf("%w: couldn't schedule it: %s", errJobInvalid, err.Error())
	}
	if item, err := st.Get(rt.ID); err == nil {
		item.Channel, item.ChatID = "mobile", "default"
		_ = st.Update(item)
	}
	if err := store.Set(j.ID, jobs.State{Enabled: true, RoutineID: rt.ID, Settings: set, Since: time.Now().UTC()}); err != nil {
		_ = svc.Cancel(rt.ID)
		return set, nil, err
	}
	return set, rt.NextRun, nil
}

// isJobRoutine reports whether a routine is the one carrying a job.
func isJobRoutine(routineID, job string) bool {
	if apiWorkspaceDir == "" || routineID == "" {
		return false
	}
	states, err := jobs.Open(apiWorkspaceDir).All()
	return err == nil && states[job].Enabled && states[job].RoutineID == routineID
}
