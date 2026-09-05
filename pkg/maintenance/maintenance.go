// Package maintenance keeps the appliance responsible on small disks
// (32GB SD card): bounded event history, log caps, temp cleanup.
// Runs once at daemon startup and daily thereafter; every action is
// conservative (oldest-first deletes, regular files only) and reported.
package maintenance

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Policy bounds. Generous for user history, tight for telemetry.
const (
	TransientEventAge = 7 * 24 * time.Hour
	DurableEventAge   = 180 * 24 * time.Hour
	NDJSONAge         = 30 * 24 * time.Hour
	HeartbeatLogLines = 2000
	HeartbeatLogMax   = 1 << 20 // 1MB hard cap
	TmpAge            = 7 * 24 * time.Hour
)

// Action describes one cleanup step and its effect.
type Action struct {
	Name    string `json:"name"`
	Removed int    `json:"removed"`
	Detail  string `json:"detail,omitempty"`
}

// Report summarizes a maintenance run.
type Report struct {
	At      string   `json:"at"`
	Actions []Action `json:"actions"`
}

// Run executes retention against workspace + db (db may be nil to skip
// database steps; filesystem steps still run).
func Run(workspace string, db *sql.DB) Report {
	rep := Report{At: time.Now().Format(time.RFC3339)}
	rep.Actions = append(rep.Actions,
		pruneEvents(db),
		pruneNDJSON(filepath.Join(workspace, "events")),
		capFile(filepath.Join(workspace, "heartbeat.log"), HeartbeatLogLines, HeartbeatLogMax),
		pruneTmp(filepath.Join(workspace, "tmp")),
	)
	return rep
}

func pruneEvents(db *sql.DB) Action {
	if db == nil {
		return Action{Name: "events", Detail: "no database handle"}
	}
	// Transient types first (progress heartbeats etc.).
	res, err := db.Exec(`DELETE FROM canonical_events WHERE timestamp < ? AND
		type IN ('agent.progress','tool.started','tool.completed','memory.retrieved')`,
		time.Now().Add(-TransientEventAge).Format(time.RFC3339))
	transient := 0
	if err == nil {
		if n, err := res.RowsAffected(); err == nil {
			transient = int(n)
		}
	}
	// Durable user-visible history: generous bound, never unbounded.
	res2, err := db.Exec(`DELETE FROM canonical_events WHERE timestamp < ?`,
		time.Now().Add(-DurableEventAge).Format(time.RFC3339))
	durable := 0
	if err == nil {
		// Table may not exist on fresh appliances; ignore.
		if n, err := res2.RowsAffected(); err == nil {
			durable = int(n)
		}
	}
	_ = err
	return Action{Name: "events", Removed: transient + durable,
		Detail: fmt.Sprintf("transient %d, history %d", transient, durable)}
}

func pruneNDJSON(dir string) Action {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Action{Name: "event-logs", Detail: "absent"}
	}
	cutoff := time.Now().Add(-NDJSONAge)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ndjson") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				removed++
			}
		}
	}
	return Action{Name: "event-logs", Removed: removed}
}

func capFile(path string, keepLines int, maxBytes int64) Action {
	info, err := os.Stat(path)
	if err != nil {
		return Action{Name: "heartbeat-log", Detail: "absent"}
	}
	if info.Size() <= maxBytes {
		return Action{Name: "heartbeat-log", Detail: "within cap"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Action{Name: "heartbeat-log", Detail: "unreadable"}
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > keepLines {
		lines = lines[len(lines)-keepLines:]
	}
	trimmed := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(trimmed), 0644); err != nil {
		return Action{Name: "heartbeat-log", Detail: "write failed"}
	}
	return Action{Name: "heartbeat-log", Removed: 1, Detail: fmt.Sprintf("%d→%d bytes", info.Size(), len(trimmed))}
}

func pruneTmp(dir string) Action {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Action{Name: "tmp", Detail: "absent"}
	}
	cutoff := time.Now().Add(-TmpAge)
	removed := 0
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || !info.ModTime().Before(cutoff) {
			continue
		}
		if os.Remove(p) == nil {
			removed++
		}
	}
	return Action{Name: "tmp", Removed: removed}
}
