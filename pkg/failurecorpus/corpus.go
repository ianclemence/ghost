// Package failurecorpus records interesting failures locally so they can be
// turned into regression tests. It is deliberately local-first: nothing is
// sent anywhere. Records are redacted before they are written, and the corpus
// is a plain append-only JSONL under the workspace state directory.
package failurecorpus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/redact"
)

// Category classifies where a failure occurred (the brief's failure taxonomy).
type Category string

const (
	CatMemory       Category = "memory"
	CatContext      Category = "context"
	CatPlanning     Category = "planning"
	CatToolSelect   Category = "tool_selection"
	CatToolArgs     Category = "tool_arguments"
	CatVerification Category = "verification"
	CatRecovery     Category = "recovery"
	CatRouting      Category = "routing"
	CatModel        Category = "model"
	CatHardware     Category = "hardware"
	CatOther        Category = "other"
)

// Record is one captured failure. It carries enough to reconstruct the
// important conditions (task, environment, expected vs observed) without
// private payloads.
type Record struct {
	ID           string    `json:"id"`
	At           time.Time `json:"at"`
	Category     Category  `json:"category"`
	Task         string    `json:"task,omitempty"`
	Environment  string    `json:"environment,omitempty"`
	Expected     string    `json:"expected,omitempty"`
	Observed     string    `json:"observed,omitempty"`
	Model        string    `json:"model,omitempty"`
	Effort       string    `json:"effort,omitempty"`
	TrajectoryID string    `json:"trajectory_id,omitempty"`
	GoldenCase   string    `json:"golden_case,omitempty"`
	Regression   bool      `json:"regression"`
}

// Store is a workspace-scoped failure corpus.
type Store struct {
	path string
	mu   sync.Mutex
}

// New opens (creating the directory if needed) the corpus under
// workspace/state/failure-corpus.jsonl.
func New(workspace string) (*Store, error) {
	dir := filepath.Join(workspace, "state")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, "failure-corpus.jsonl")}, nil
}

// Path returns the corpus file path.
func (s *Store) Path() string { return s.path }

// Append redacts and writes a record. It fills ID/At when absent.
func (s *Store) Append(r Record) error {
	if s == nil {
		return nil
	}
	if r.At.IsZero() {
		r.At = time.Now().UTC()
	}
	if r.ID == "" {
		r.ID = fmt.Sprintf("fail-%d", r.At.UnixNano())
	}
	// Redact every free-text field: a failure note can accidentally contain
	// a credential or private fact from the task.
	r.Task = safeString(r.Task)
	r.Environment = safeString(r.Environment)
	r.Expected = safeString(r.Expected)
	r.Observed = safeString(r.Observed)
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// List returns up to limit records, newest first.
func (s *Store) List(limit int) ([]Record, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var all []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if json.Unmarshal(line, &r) == nil {
			all = append(all, r)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// Newest first.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func safeString(s string) string {
	if s == "" {
		return ""
	}
	if v, ok := redact.Any(s).(string); ok {
		return v
	}
	return s
}
