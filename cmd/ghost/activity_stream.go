package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/cevents"
)

// registerActivityStream exposes a live, read-only activity stream over SSE.
// It reuses the canonical event store (no new bus): the handler polls
// Stream.Since with the client's last seen seq and projects only user-visible
// chips. Reconnect with ?since_seq=<last> resumes without duplicates.
func registerActivityStream(mux *http.ServeMux) {
	mux.HandleFunc("/v1/activity/stream", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		st, err := eventStream()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "unavailable", "activity is unavailable right now")
			return
		}
		var cursor int64
		if raw := strings.TrimSpace(r.URL.Query().Get("since_seq")); raw != "" {
			cursor, _ = strconv.ParseInt(raw, 10, 64)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		emit := func(v interface{}) {
			raw, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", string(raw))
			flusher.Flush()
		}
		// retention/keep-alive markers
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		// Emit the live snapshot immediately so a fresh client renders fast.
		last := cursor
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			default:
			}
			f := cevents.Filter{GhostID: ghostID(), UserVisibleOnly: true}
			evs := st.Since(last, 100, f)
			for _, e := range evs {
				chip, okp := activity.Project(e)
				if !okp {
					continue
				}
				// user-visible only; never internal event types/ids/correlation.
				emit(map[string]interface{}{
					"seq":     e.Seq,
					"type":    "activity",
					"title":   chip.Title,
					"kind":    chip.Kind,
					"state":   chip.State,
					"time":    chip.Timestamp,
					"summary": chip.Summary,
				})
				last = e.Seq
			}
			time.Sleep(700 * time.Millisecond)
		}
	}))
}
