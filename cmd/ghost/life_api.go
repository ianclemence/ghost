package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/life"
)

// registerLifeRoutes serves the owner's own records to their devices: the
// people in their life, their important documents, their finances. Reading is
// open to a paired device; every change is the owner's own edit on their
// phone, checked by the same rules the tools use.
//
//	GET    /v1/life/people              everyone (with next birthday)
//	POST   /v1/life/people/{id}         edit someone
//	DELETE /v1/life/people/{id}         forget someone
//	GET    /v1/life/vault               the documents, soonest due first
//	POST   /v1/life/vault/{id}          edit a document
//	DELETE /v1/life/vault/{id}          remove a document
//	GET    /v1/life/finances?month=2026-10  the month at a glance, recent entries, subscriptions and bills
//	GET    /v1/life/trips               trips, upcoming first, each leg with when to leave
//	DELETE /v1/life/trips/{id}          remove a trip
//	DELETE /v1/life/finances/{id}          remove an entry, a subscription or a bill
//	POST   /v1/life/finances/{id}/active   {active} start or stop tracking a subscription or bill
//	GET    /v1/life/knowledge           what the owner reads and studies, current first
//	POST   /v1/life/knowledge           add something to read or learn
//	POST   /v1/life/knowledge/{id}      edit it (progress, status, notes dropped)
//	DELETE /v1/life/knowledge/{id}      forget it
func registerLifeRoutes(mux *http.ServeMux, zone func() *time.Location) {
	ws := func() string { return apiWorkspaceDir }
	idOf := func(r *http.Request, prefix string) (string, string) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
		parts := strings.Split(rest, "/")
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return rest, ""
	}
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, life.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "not_found", "that is no longer there")
			return
		}
		jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
	}

	mux.HandleFunc("/v1/life/people", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		all, err := life.PeopleFor(ws()).All()
		if err != nil {
			fail(w, err)
			return
		}
		now, loc := time.Now(), zone()
		type view struct {
			life.Person
			NextBirthday string `json:"next_birthday,omitempty"`
			Turning      int    `json:"turning,omitempty"`
			DaysToBirth  *int   `json:"days_to_birthday,omitempty"`
		}
		out := make([]view, 0, len(all))
		today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
		for _, p := range all {
			v := view{Person: p}
			if next, age, ok := life.NextBirthday(p, now, loc); ok {
				d := int(next.Sub(today).Hours() / 24)
				v.NextBirthday, v.Turning, v.DaysToBirth = next.Format("2006-01-02"), age, &d
			}
			out = append(out, v)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "people": out})
	}))
	mux.HandleFunc("/v1/life/people/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, _ := idOf(r, "/v1/life/people/")
		store := life.PeopleFor(ws())
		switch r.Method {
		case http.MethodDelete:
			if err := store.Forget(id); err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		case http.MethodPost:
			var req struct {
				Name        string   `json:"name"`
				Relation    string   `json:"relation"`
				Birthday    string   `json:"birthday"`
				Phone       string   `json:"phone"`
				Email       string   `json:"email"`
				Likes       []string `json:"likes"`
				KeepInTouch int      `json:"keep_in_touch_days"`
				DropNotes   []int    `json:"drop_notes"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			p, err := store.Edit(id, req.Name, req.Relation, req.Birthday, req.Phone, req.Email, req.Likes, req.KeepInTouch, req.DropNotes, time.Now())
			if err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "person": p})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST or DELETE")
		}
	}))

	mux.HandleFunc("/v1/life/vault", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		all, err := life.VaultFor(ws()).All()
		if err != nil {
			fail(w, err)
			return
		}
		type view struct {
			life.Paper
			DaysLeft *int   `json:"days_left,omitempty"`
			What     string `json:"what,omitempty"`
		}
		out := make([]view, 0, len(all))
		for _, p := range all {
			v := view{Paper: p}
			if d, what, ok := life.DaysLeft(p, time.Now(), zone()); ok {
				v.DaysLeft, v.What = &d, what
			}
			out = append(out, v)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "papers": out, "kinds": life.PaperKinds})
	}))
	mux.HandleFunc("/v1/life/vault/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, _ := idOf(r, "/v1/life/vault/")
		store := life.VaultFor(ws())
		switch r.Method {
		case http.MethodDelete:
			if err := store.Forget(id); err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		case http.MethodPost:
			var req struct {
				Kind    string      `json:"kind"`
				Title   string      `json:"title"`
				Holder  string      `json:"holder"`
				Facts   []life.Fact `json:"facts"`
				Expires string      `json:"expires"`
				Renews  string      `json:"renews"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			p, err := store.Update(id, life.PaperInput{Kind: req.Kind, Title: req.Title, Holder: req.Holder, Facts: req.Facts, Expires: req.Expires, Renews: req.Renews,
				Source: life.Source{Kind: "owner", At: time.Now()}}, time.Now())
			if err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "paper": p})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST or DELETE")
		}
	}))

	mux.HandleFunc("/v1/life/trips", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		loc := zone()
		now := time.Now().In(loc)
		all, err := life.TripsFor(ws()).All(now)
		if err != nil {
			fail(w, err)
			return
		}
		type legView struct {
			life.Leg
			LeaveBy string `json:"leave_by,omitempty"`
		}
		type view struct {
			life.Trip
			Legs  []legView `json:"legs"`
			State string    `json:"state"` // upcoming | now | past
		}
		today := now.Format("2006-01-02")
		out := make([]view, 0, len(all))
		for _, tr := range all {
			v := view{Trip: tr, State: "upcoming", Legs: []legView{}}
			if tr.End < today {
				v.State = "past"
			} else if tr.Start <= today {
				v.State = "now"
			}
			for _, l := range tr.Legs {
				lv := legView{Leg: l}
				if by, ok := life.LeaveBy(l, loc); ok {
					lv.LeaveBy = by.Format(life.LegTime)
				}
				v.Legs = append(v.Legs, lv)
			}
			out = append(out, v)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "trips": out})
	}))
	mux.HandleFunc("/v1/life/trips/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, _ := idOf(r, "/v1/life/trips/")
		if r.Method != http.MethodDelete {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use DELETE")
			return
		}
		if err := life.TripsFor(ws()).Forget(id); err != nil {
			fail(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	}))

	type learningBody struct {
		Kind      string   `json:"kind"`
		Title     string   `json:"title"`
		Author    string   `json:"author"`
		Status    string   `json:"status"`
		Current   *int     `json:"current"`
		Total     *int     `json:"total"`
		Unit      string   `json:"unit"`
		Goal      string   `json:"goal"`
		Due       string   `json:"due"`
		Tags      []string `json:"tags"`
		Note      string   `json:"note"`
		DropNotes []int    `json:"drop_notes"`
	}
	learningIn := func(b learningBody) life.LearningInput {
		in := life.LearningInput{Kind: b.Kind, Title: b.Title, Author: b.Author, Status: b.Status, Unit: b.Unit, Goal: b.Goal, Due: b.Due,
			Tags: b.Tags, Note: b.Note, Current: -1, Total: -1}
		if b.Current != nil {
			in.Current = *b.Current
		}
		if b.Total != nil {
			in.Total = *b.Total
		}
		return in
	}
	mux.HandleFunc("/v1/life/knowledge", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		store := life.KnowledgeFor(ws())
		switch r.Method {
		case http.MethodGet:
			all, err := store.All()
			if err != nil {
				fail(w, err)
				return
			}
			if all == nil {
				all = []life.Learning{}
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "items": all})
		case http.MethodPost:
			var b learningBody
			if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&b); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			in := learningIn(b)
			in.Source = life.Source{Kind: "owner"}
			l, _, err := store.Save(in, time.Now().In(zone()))
			if err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "item": l})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET or POST")
		}
	}))
	mux.HandleFunc("/v1/life/knowledge/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, _ := idOf(r, "/v1/life/knowledge/")
		store := life.KnowledgeFor(ws())
		switch r.Method {
		case http.MethodPost:
			var b learningBody
			if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&b); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			l, err := store.Edit(id, learningIn(b), b.DropNotes, time.Now().In(zone()))
			if err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "item": l})
		case http.MethodDelete:
			if err := store.Forget(id); err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST or DELETE")
		}
	}))

	mux.HandleFunc("/v1/life/finances", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		loc := zone()
		now := time.Now().In(loc)
		month := strings.TrimSpace(r.URL.Query().Get("month"))
		if month == "" {
			month = now.Format("2006-01")
		}
		store := life.FinancesFor(ws())
		_, _ = store.Advance(now, loc)
		sum, err := store.Summary(month, now)
		if err != nil {
			fail(w, err)
			return
		}
		start, _ := time.Parse("2006-01", month)
		entries, err := store.Entries(start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02"))
		if err != nil {
			fail(w, err)
			return
		}
		if len(entries) > 200 {
			entries = entries[:200]
		}
		recs, err := store.Recurrings()
		if err != nil {
			fail(w, err)
			return
		}
		if entries == nil {
			entries = []life.Entry{}
		}
		if recs == nil {
			recs = []life.Recurring{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "summary": sum, "entries": entries, "recurring": recs, "categories": life.Categories})
	}))
	mux.HandleFunc("/v1/life/finances/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, sub := idOf(r, "/v1/life/finances/")
		store := life.FinancesFor(ws())
		switch {
		case r.Method == http.MethodDelete && sub == "":
			if err := store.Forget(id); err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && sub == "active":
			var req struct {
				Active bool `json:"active"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			rec, err := store.SetActive(id, req.Active)
			if err != nil {
				fail(w, err)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "recurring": rec})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "not something finances does")
		}
	}))
}
