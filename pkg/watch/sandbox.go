package watch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The sandbox is Ghost's local, network-free watch source: a file per
// watched thing under <workspace>/state/watch-source/. It exists so watch
// behavior is observable, testable and demonstrable without any external
// provider — the same state a real probe would return, written down.
//
// The source answers honestly in both directions:
//   - state file present → its fields, verbatim.
//   - state file absent   → FailUnavailable ("I have no record of this"),
//     retried with backoff and reported as a failure, never invented.
//   - <slug>.error marker present → that failure, with whatever class the
//     marker states. This is the failure-injection seam golden cases and
//     live demos use to prove the failure path.
const (
	// WatchSourceDir is the workspace-relative directory of sandbox state.
	WatchSourceDir = "state/watch-source"
)

// Sandbox is the file-backed probe.
type Sandbox struct {
	workspace string
}

// NewSandbox builds the sandbox probe for a workspace.
func NewSandbox(workspace string) *Sandbox {
	return &Sandbox{workspace: strings.TrimSpace(workspace)}
}

// Name implements Probe.
func (s *Sandbox) Name() string { return "sandbox" }

// dir is the state directory.
func (s *Sandbox) dir() string {
	return filepath.Join(s.workspace, WatchSourceDir)
}

// Available implements Probe: the sandbox can answer once its directory
// exists — i.e. once someone has actually put a watch source on this
// machine. Without it Ghost refuses to create a watch honestly rather than
// "watching" a source that is not there.
func (s *Sandbox) Available() bool {
	if s == nil || s.workspace == "" {
		return false
	}
	st, err := os.Stat(s.dir())
	return err == nil && st.IsDir()
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns kind+entity into a stable file name: flight BA123 →
// flight-ba123. The same entity always maps to the same file, so two
// watches of one thing read one state.
func Slug(kind Kind, entity string) string {
	k := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(string(kind)), "-"), "-")
	e := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(entity), "-"), "-")
	if e == "" {
		e = "watch"
	}
	return k + "-" + e
}

// StatePath returns the state file for a watch.
func StatePath(workspace string, kind Kind, entity string) string {
	return filepath.Join(workspace, WatchSourceDir, Slug(kind, entity)+".json")
}

// ErrorPath returns the failure-injection marker for a watch.
func ErrorPath(workspace string, kind Kind, entity string) string {
	return filepath.Join(workspace, WatchSourceDir, Slug(kind, entity)+".error")
}

// SetState writes sandbox state for an entity, creating the directory. It
// is how tests, golden fixtures and live demonstrations place a
// "change in the world" where the runtime will read it.
func SetState(workspace string, kind Kind, entity string, state map[string]string) error {
	path := StatePath(workspace, kind, entity)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ClearState removes sandbox state (the entity stops being observable).
func ClearState(workspace string, kind Kind, entity string) {
	_ = os.Remove(StatePath(workspace, kind, entity))
}

// SetError writes the failure-injection marker. An empty message removes
// it. A class prefix ("unavailable:", "notfound:", "invalid:") selects the
// failure class; anything else is transient.
func SetError(workspace string, kind Kind, entity string, message string) error {
	path := ErrorPath(workspace, kind, entity)
	if strings.TrimSpace(message) == "" {
		_ = os.Remove(path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(message), 0600)
}

// Fetch implements Probe.
func (s *Sandbox) Fetch(ctx context.Context, w Watch) (map[string]string, string, error) {
	if s == nil || s.workspace == "" {
		return nil, "", failf(FailUnavailable, "no workspace for sandbox source")
	}
	base := filepath.Join(s.dir(), Slug(w.Kind, w.Entity))
	// Failure injection first: the marker wins over any state file, so a
	// golden case can make a probe fail even while state exists.
	if raw, err := os.ReadFile(base + ".error"); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		return nil, "", classedError(string(raw))
	}
	raw, err := os.ReadFile(base + ".json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", failf(FailUnavailable, "no watch source record for %s", w.Entity)
		}
		return nil, "", failf(FailTransient, "read watch source: %v", err)
	}
	state, err := decodeState(raw)
	if err != nil {
		return nil, "", err
	}
	state["_source"] = "sandbox"
	state["_checked_at"] = time.Now().UTC().Format(time.RFC3339)
	return state, Excerpt(state), nil
}

// classedError interprets a failure-injection marker.
func classedError(msg string) error {
	msg = strings.TrimSpace(msg)
	lower := strings.ToLower(msg)
	switch {
	case strings.HasPrefix(lower, "unavailable:"):
		return &ProbeError{Class: FailUnavailable, Err: errors.New(strings.TrimSpace(msg[len("unavailable:"):]))}
	case strings.HasPrefix(lower, "notfound:"), strings.HasPrefix(lower, "not found:"):
		return &ProbeError{Class: FailNotFound, Err: errors.New(msg)}
	case strings.HasPrefix(lower, "invalid:"):
		return &ProbeError{Class: FailInvalid, Err: errors.New(msg)}
	default:
		return &ProbeError{Class: FailTransient, Err: errors.New(strings.TrimPrefix(lower, "transient:"))}
	}
}

// decodeState reads the sandbox JSON: a flat object of values. Numbers and
// booleans are stringified so a fixture can write {"gate": 12} and a notice
// reads "12".
func decodeState(raw []byte) (map[string]string, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, failf(FailInvalid, "empty watch source file")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var obj map[string]interface{}
	if err := dec.Decode(&obj); err != nil {
		return nil, failf(FailInvalid, "watch source is not a JSON object: %v", err)
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		out[k] = scalarString(v)
	}
	return out, nil
}

func scalarString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
