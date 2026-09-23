package ideas

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func newID() string {
	return fmt.Sprintf("idea-%d", time.Now().UTC().UnixNano())
}

// Add stores new ideas, filling IDs and timestamps.
func (s *Store) Add(ideas []Idea) error {
	if s == nil || len(ideas) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, idea := range ideas {
		if idea.ID == "" {
			idea.ID = newID()
		}
		if idea.CreatedAt.IsZero() {
			idea.CreatedAt = time.Now().UTC()
		}
		if idea.Status == "" {
			idea.Status = StatusPending
		}
		raw, err := json.Marshal(idea)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// all reads every idea, newest last.
func (s *Store) all() ([]Idea, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Idea
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var idea Idea
		if err := json.Unmarshal(sc.Bytes(), &idea); err != nil {
			continue
		}
		out = append(out, idea)
	}
	return out, sc.Err()
}

// List returns ideas, newest first, optionally filtered by status (""
// means all), capped by limit (<=0 means all).
func (s *Store) List(status Status, limit int) ([]Idea, error) {
	all, err := s.all()
	if err != nil {
		return nil, err
	}
	var out []Idea
	for i := len(all) - 1; i >= 0; i-- {
		if status != "" && all[i].Status != status {
			continue
		}
		out = append(out, all[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Get returns one idea by id or unique prefix.
func (s *Store) Get(ref string) (Idea, error) {
	all, err := s.all()
	if err != nil {
		return Idea{}, err
	}
	var hit Idea
	matches := 0
	for _, idea := range all {
		if idea.ID == ref || (len(ref) >= 4 && len(idea.ID) >= len(ref) && idea.ID[:len(ref)] == ref) {
			hit = idea
			matches++
		}
	}
	if matches != 1 {
		return Idea{}, fmt.Errorf("no single idea matches %q", ref)
	}
	return hit, nil
}

// Decide records accept (true) or dismiss (false) with a timestamp. It
// rewrites the store file; only status/decided fields change.
func (s *Store) Decide(id string, accept bool) (Idea, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		return Idea{}, err
	}
	var all []Idea
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var idea Idea
		if err := json.Unmarshal(sc.Bytes(), &idea); err != nil {
			continue
		}
		all = append(all, idea)
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return Idea{}, err
	}
	var decided Idea
	found := false
	now := time.Now().UTC()
	for i := range all {
		if all[i].ID == id {
			if accept {
				all[i].Status = StatusAccepted
			} else {
				all[i].Status = StatusDismissed
			}
			all[i].DecidedAt = &now
			decided = all[i]
			found = true
		}
	}
	if !found {
		return Idea{}, fmt.Errorf("no idea %q", id)
	}
	tmp := s.path + ".tmp"
	w, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return Idea{}, err
	}
	for _, idea := range all {
		raw, err := json.Marshal(idea)
		if err != nil {
			w.Close()
			return Idea{}, err
		}
		if _, err := w.Write(append(raw, '\n')); err != nil {
			w.Close()
			return Idea{}, err
		}
	}
	w.Close()
	if err := os.Rename(tmp, s.path); err != nil {
		return Idea{}, err
	}
	return decided, nil
}
