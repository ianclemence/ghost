package personalcontext

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// OnForget, when set by the runtime, removes forgotten values from stores
// this package cannot reach (the vector index). It receives the values and
// the distinctive names in them.
var OnForget func(values, names []string)

// Workspace is the workspace directory this store lives in.
func (s *Store) Workspace() string {
	return filepath.Dir(filepath.Dir(s.path))
}

// chainOf returns id and every earlier version it replaced (entries whose
// SupersededBy leads to it). "Forget my dentist" means every version Ghost
// held, not only the last: the old one stayed in history and came back when
// Ghost searched.
func (s *Store) chainOf(id string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, e := range s.byID {
			if !in[e.ID] && e.SupersededBy != nil && in[*e.SupersededBy] {
				in[e.ID] = true
				changed = true
			}
		}
	}
	var out []Entry
	for eid := range in {
		if e, ok := s.byID[eid]; ok {
			out = append(out, *e)
		}
	}
	return out
}

// forgetChain retracts an entry and every version it replaced, returning the
// values that were forgotten.
func (s *Store) forgetChain(id, reason string) ([]string, error) {
	chain := s.chainOf(id)
	if len(chain) == 0 {
		if _, err := s.ForgetWithReason(id, reason); err != nil {
			return nil, err
		}
		return nil, nil
	}
	var values []string
	for _, e := range chain {
		if e.Status != StatusRejected {
			if _, err := s.ForgetWithReason(e.ID, reason); err != nil {
				return values, err
			}
		}
		if v := strings.TrimSpace(entryValueString(e)); v != "" {
			values = append(values, v)
		}
	}
	return values, nil
}

// forgetNames picks the words in forgotten values that identify them: names
// and places ("Somchai", "Bumrungrad", "Samitivej"), never common words. A
// note line is only removed when it carries these, so forgetting a dentist
// does not wipe every line that says "dentist".
func forgetNames(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		words := strings.FieldsFunc(v, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		for i, w := range words {
			r := []rune(w)
			if len(r) < 4 || !unicode.IsUpper(r[0]) || i == 0 {
				continue // too short, not a name, or capitalised only as the first word
			}
			k := strings.ToLower(w)
			if forgetStopwords[k] || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, w)
		}
	}
	return out
}

var forgetStopwords = map[string]bool{
	"their": true, "they": true, "user": true, "owner": true, "ghost": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true, "friday": true,
	"saturday": true, "sunday": true, "january": true, "february": true, "march": true,
	"april": true, "june": true, "july": true, "august": true, "september": true,
	"october": true, "november": true, "december": true, "hospital": true, "clinic": true,
}

// topicWords are what a fact is about, from its key ("fact/dentist" →
// "dentist", "event/shenzhen-trip" → "shenzhen", "trip").
func topicWords(entries []Entry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		p := e.Predicate
		if i := strings.LastIndex(p, "/"); i >= 0 {
			p = p[i+1:]
		}
		for _, w := range strings.FieldsFunc(p, func(r rune) bool { return r == '-' || r == '_' }) {
			w = strings.ToLower(w)
			if len(w) >= 4 && !seen[w] && !genericTopic[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out
}

var genericTopic = map[string]bool{"general": true, "current": true, "primary": true, "other": true, "favorite": true, "prefers": true, "likes": true}

// mentions reports whether a line still carries a forgotten fact: the whole
// value, two of its names, or one name together with what the fact is about.
// One name alone is not enough: forgetting "departs from Bangkok" must not
// erase every note that mentions Bangkok.
func mentions(line string, values, names, topics []string) bool {
	l := strings.ToLower(line)
	for _, v := range values {
		if len([]rune(v)) >= 6 && strings.Contains(l, strings.ToLower(v)) {
			return true
		}
	}
	hits := 0
	for _, n := range names {
		if strings.Contains(l, strings.ToLower(n)) {
			hits++
		}
	}
	if hits >= 2 {
		return true
	}
	if hits == 1 {
		for _, t := range topics {
			if strings.Contains(l, t) {
				return true
			}
		}
	}
	return false
}

// scrubNotes removes lines carrying a forgotten fact from Ghost's own
// derived notes: the daily journal (memory/YYYY-MM-DD.md) and MEMORY.md. These
// are Ghost's summaries of the conversation, not the conversation itself, and
// they are what Ghost searched when it recited a "forgotten" dentist.
// Returns how many lines went.
func scrubNotes(workspace string, values, names, topics []string) int {
	if len(values) == 0 && len(names) == 0 {
		return 0
	}
	files, _ := filepath.Glob(filepath.Join(workspace, "memory", "*.md"))
	removed := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		kept := lines[:0]
		n := 0
		for _, ln := range lines {
			if strings.TrimSpace(ln) != "" && !strings.HasPrefix(strings.TrimSpace(ln), "#") && mentions(ln, values, names, topics) {
				n++
				continue
			}
			kept = append(kept, ln)
		}
		if n == 0 {
			continue
		}
		info, _ := os.Stat(f)
		mode := os.FileMode(0644)
		if info != nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(f, []byte(strings.Join(kept, "\n")), mode); err == nil {
			removed += n
		}
	}
	return removed
}
