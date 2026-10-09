// Package life is what Ghost knows about the owner's life beyond the
// conversation: the people in it, the documents that matter (a passport, a
// lease, a warranty), and their finances (what they spend, what renews, what is
// due). It is not memory: memory holds facts in the owner's words; these are
// structured records Ghost can count, date and remind from. Every record says
// where it came from (the conversation, a photo, an email, an import), and the
// owner can see, change and forget any of it.
//
// Each store is one JSON file under the workspace's data/ folder (which Ghost
// State carries as the owner's own data), written atomically, guarded by a
// mutex, and small enough to read whole.
package life

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Dir is the workspace folder the stores live in.
const Dir = "data/life"

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// Source is where a record or a fact came from.
type Source struct {
	// Kind: conversation, photo, document, email, calendar, import, phone, owner
	Kind string `json:"kind"`
	// Ref points at it: a message id, a workspace path, an email id.
	Ref string    `json:"ref,omitempty"`
	At  time.Time `json:"at"`
	// Quote is the owner's own words, when it came from what they said.
	Quote string `json:"quote,omitempty"`
}

var sourceKinds = map[string]bool{"conversation": true, "photo": true, "document": true, "email": true, "calendar": true, "import": true, "phone": true, "owner": true}

func (s Source) check() error {
	if !sourceKinds[s.Kind] {
		return fmt.Errorf("a source is one of conversation, photo, document, email, calendar, import, phone or owner, not %q", s.Kind)
	}
	if len(s.Quote) > 500 || len(s.Ref) > 300 {
		return errors.New("a source's quote or reference is too long")
	}
	return nil
}

// file is one JSON store.
type file[T any] struct {
	mu   sync.Mutex
	path string
	data T
	load bool
}

func newFile[T any](workspace, name string, empty T) *file[T] {
	return &file[T]{path: filepath.Join(workspace, Dir, name), data: empty}
}

// with runs fn on the loaded data under the lock; when write is true the data
// is saved afterwards (only if fn succeeded).
func (f *file[T]) with(write bool, fn func(d *T) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.load {
		if b, err := os.ReadFile(f.path); err == nil {
			if err := json.Unmarshal(b, &f.data); err != nil {
				return fmt.Errorf("%s is unreadable: %w", filepath.Base(f.path), err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		f.load = true
	}
	if err := fn(&f.data); err != nil {
		return err
	}
	if !write {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f.data, "", " ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func newID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b)
}

// text cleans one line of text: control characters out, spaces collapsed, at
// most max runes. An over-long value is an error, not silently cut.
func text(name, s string, max int, required bool) (string, error) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > max {
		return "", fmt.Errorf("%s is longer than %d characters", name, max)
	}
	if required && s == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return s, nil
}

// key is the form names are matched in: lowercase, spaces collapsed.
func key(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// day parses a calendar date the owner gave: 2026-10-12, or a month and day
// without a year (10-12) for things that come round every year.
func day(s string) (time.Time, bool, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true, nil
	}
	if t, err := time.Parse("01-02", s); err == nil {
		return t, false, nil
	}
	return time.Time{}, false, fmt.Errorf("%q is not a date (2026-10-12, or 10-12 for every year)", s)
}

// One store of each kind per workspace, shared by the tools, the API and the
// reminders, so each has one lock and one copy in memory and no writer can
// overwrite another's change with a stale copy.
var (
	sharedMu       sync.Mutex
	sharedPeople   = map[string]*People{}
	sharedVault    = map[string]*Vault{}
	sharedFinances = map[string]*Finances{}
)

// PeopleFor is the people store of a workspace.
func PeopleFor(workspace string) *People {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s, ok := sharedPeople[workspace]; ok {
		return s
	}
	s := OpenPeople(workspace)
	sharedPeople[workspace] = s
	return s
}

// VaultFor is the vault of a workspace.
func VaultFor(workspace string) *Vault {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s, ok := sharedVault[workspace]; ok {
		return s
	}
	s := OpenVault(workspace)
	sharedVault[workspace] = s
	return s
}

// FinancesFor is the finances store of a workspace.
func FinancesFor(workspace string) *Finances {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s, ok := sharedFinances[workspace]; ok {
		return s
	}
	s := OpenFinances(workspace)
	sharedFinances[workspace] = s
	return s
}
