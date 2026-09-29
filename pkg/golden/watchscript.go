package golden

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/watch"
)

// The scripted half of a watch conversation: time passing and the world
// changing, played deterministically between the last turn and the
// assertions. The runtime does the real work — every step goes through the
// production poll path (PollWatches → probe → diff → noticer); the script
// only supplies the two inputs a live run would wait for: a due check and a
// changed world.
//
// Steps (one per line; "#" lines are comments):
//
//	set <key>=<value>  merge one field into every watch's sandbox state
//	error <message>    inject a probe failure marker (class-prefixed)
//	clear-error        remove failure markers
//	poll               advance to the next due check, run one poll cycle
//	past               push every live watch past its horizon (expiry)
//
// There is no step that calls a model: the golden runner never starts the
// proactive eval loop, so polling happens exactly when the script asks.

// runWatchScript applies the steps in order against one person's workspace.
func runWatchScript(loop *agent.AgentLoop, ws string, steps []string) error {
	if loop == nil {
		return fmt.Errorf("no loop to poll with")
	}
	for i, raw := range steps {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := runWatchStep(loop, ws, line); err != nil {
			return fmt.Errorf("step %d (%q): %w", i+1, line, err)
		}
	}
	return nil
}

func runWatchStep(loop *agent.AgentLoop, ws, line string) error {
	now := time.Now().UTC()
	switch {
	case strings.HasPrefix(line, "set "):
		eq := strings.Index(line, "=")
		if eq < 0 {
			return fmt.Errorf("want set key=value")
		}
		key := strings.TrimSpace(line[len("set "):eq])
		value := strings.TrimSpace(line[eq+1:])
		if key == "" {
			return fmt.Errorf("empty state key")
		}
		return forEachWatch(ws, func(w watch.Watch) error {
			state, err := readSandboxState(ws, w.Kind, w.Entity)
			if err != nil {
				return err
			}
			state[key] = value
			return watch.SetState(ws, w.Kind, w.Entity, state)
		})
	case strings.HasPrefix(line, "error "):
		msg := strings.TrimSpace(strings.TrimPrefix(line, "error "))
		if msg == "" {
			return fmt.Errorf("empty error message")
		}
		return forEachWatch(ws, func(w watch.Watch) error {
			return watch.SetError(ws, w.Kind, w.Entity, msg)
		})
	case line == "clear-error":
		return forEachWatch(ws, func(w watch.Watch) error {
			return watch.SetError(ws, w.Kind, w.Entity, "")
		})
	case line == "past":
		return mutateWatchList(ws, func(list []watch.Watch) (bool, error) {
			changed := false
			for i := range list {
				if !list[i].Live() {
					continue
				}
				past := now.Add(-time.Minute)
				list[i].ExpiresAt = &past
				changed = true
			}
			return changed, nil
		})
	case line == "poll":
		if err := mutateWatchList(ws, func(list []watch.Watch) (bool, error) {
			changed := false
			for i := range list {
				w := &list[i]
				if !w.Live() {
					continue
				}
				if w.SnoozeUntil != nil && now.Before(*w.SnoozeUntil) {
					continue // genuinely not due yet
				}
				if w.NextCheck != nil {
					w.NextCheck = nil // the clock reached the next check
					changed = true
				}
			}
			return changed, nil
		}); err != nil {
			return err
		}
		loop.PollWatches(now)
		return nil
	default:
		return fmt.Errorf("unknown watch script step")
	}
}

// forEachWatch applies fn once per watch in the workspace ledger.
func forEachWatch(ws string, fn func(w watch.Watch) error) error {
	list, err := readWatchList(ws)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return fmt.Errorf("no watch exists to act on")
	}
	for _, w := range list {
		if err := fn(w); err != nil {
			return err
		}
	}
	return nil
}

// mutateWatchList rewrites the ledger in place when fn reports a change.
// It is the harness's time machine: NextCheck/horizon edits model the clock
// passing, exactly the state a real run would reach on its own.
func mutateWatchList(ws string, fn func(list []watch.Watch) (bool, error)) error {
	list, err := readWatchList(ws)
	if err != nil {
		return err
	}
	changed, err := fn(list)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return writeWatchList(ws, list)
}

func watchListPath(ws string) string {
	return filepath.Join(ws, watch.Dir, "watches.json")
}

func readWatchList(ws string) ([]watch.Watch, error) {
	raw, err := os.ReadFile(watchListPath(ws))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []watch.Watch
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("watch ledger unreadable: %w", err)
		}
	}
	return list, nil
}

func writeWatchList(ws string, list []watch.Watch) error {
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	path := watchListPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readSandboxState loads one entity's sandbox state, starting from an empty
// map when the world has no record yet.
func readSandboxState(ws string, kind watch.Kind, entity string) (map[string]string, error) {
	state := map[string]string{}
	raw, err := os.ReadFile(watch.StatePath(ws, kind, entity))
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return state, nil
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("sandbox state unreadable: %w", err)
	}
	return state, nil
}

// goldenWatchRow mirrors the on-disk watch ledger row as assertions read it.
type goldenWatchRow struct {
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	Entity       string        `json:"entity"`
	Status       string        `json:"status"`
	Fingerprints []string      `json:"notified,omitempty"`
	Evidence     []interface{} `json:"evidence,omitempty"`
}

// readWatchRows loads the watch ledger from a workspace (nil when absent).
func readWatchRows(ws string) ([]goldenWatchRow, error) {
	if ws == "" {
		return nil, fmt.Errorf("no workspace")
	}
	data, err := os.ReadFile(watchListPath(ws))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []goldenWatchRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// matchWatch checks one expected watch spec against a ledger row. An empty
// spec field means "don't care"; a non-empty one must match exactly.
func matchWatch(w goldenWatchRow, spec WatchExpect) (bool, string) {
	if spec.Kind != "" && w.Kind != spec.Kind {
		return false, "kind=" + w.Kind + " want " + spec.Kind
	}
	if spec.Entity != "" && !strings.EqualFold(w.Entity, spec.Entity) {
		return false, "entity=" + w.Entity + " want " + spec.Entity
	}
	if spec.Status != "" && w.Status != spec.Status {
		return false, "status=" + w.Status + " want " + spec.Status
	}
	for _, f := range spec.NotifiedFields {
		found := false
		for _, fp := range w.Fingerprints {
			if strings.Contains(fp, ":"+f+":") {
				found = true
				break
			}
		}
		if !found {
			return false, "no notified fingerprint for field " + f
		}
	}
	if spec.MinProbes > 0 && len(w.Evidence) < spec.MinProbes {
		return false, fmt.Sprintf("%d probes < %d", len(w.Evidence), spec.MinProbes)
	}
	if spec.MaxProbes > 0 && len(w.Evidence) > spec.MaxProbes {
		return false, fmt.Sprintf("%d probes > %d", len(w.Evidence), spec.MaxProbes)
	}
	return true, ""
}

// countHeldNotices counts lines in the held-notice outbox (absent = 0).
func countHeldNotices(ws string) int {
	if ws == "" {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(ws, "proactive", "outbox.jsonl"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
