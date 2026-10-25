package personalcontext

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// erasedValue stands where a forgotten belief's value was. It is valid JSON for
// any reader that expects a string, and says what happened.
var erasedValue = json.RawMessage(`"[forgotten]"`)

// eraseContentLocked removes what a forgotten belief said from every revision of
// it in the log: the value, and the verbatim quote it was read from. The record
// stays, with its id, subject, predicate, status, timestamps and sources, so the
// audit trail shows that something was forgotten and when; only the content
// goes. The log is append-only, so without this a retraction is a new line
// saying "rejected" beside the original line still saying the value.
//
// The file is rewritten line by line from disk rather than from memory, so a
// record another process appended since this store loaded is carried over
// untouched. It replaces the file atomically.
func (s *Store) eraseContentLocked(ids map[string]bool) error {
	if len(ids) == 0 {
		return nil
	}
	in, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("open entries log: %w", err)
	}
	defer in.Close()

	tmp := s.path + ".erase"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	cleanup := func() { out.Close(); os.Remove(tmp) }

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	w := bufio.NewWriter(out)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		line := []byte(raw)
		var e Entry
		if err := json.Unmarshal(line, &e); err == nil && ids[e.ID] {
			e.Value = erasedValue
			e.Quote = ""
			if line, err = json.Marshal(e); err != nil {
				cleanup()
				return fmt.Errorf("marshal entry %s: %w", e.ID, err)
			}
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		cleanup()
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if err := w.Flush(); err != nil {
		cleanup()
		return err
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace entries log: %w", err)
	}
	for i := range s.log {
		if ids[s.log[i].ID] {
			s.log[i].Value = erasedValue
			s.log[i].Quote = ""
		}
	}
	for id := range ids {
		if e, ok := s.byID[id]; ok {
			e.Value = erasedValue
			e.Quote = ""
		}
	}
	return nil
}

// scrubForgottenLocked erases the content of beliefs the owner forgot before
// forgetting erased it. Retiring an entry without a retraction (a correction,
// an extractor's own rejection) keeps its record readable, as documented.
func (s *Store) scrubForgottenLocked() error {
	ids := map[string]bool{}
	for id, e := range s.byID {
		if e.Status == StatusRejected && e.RetractedAt != nil && (string(e.Value) != string(erasedValue) || e.Quote != "") {
			ids[id] = true
		}
	}
	for _, e := range s.log {
		if ids[e.ID] || e.RetractedAt != nil {
			continue
		}
		// Earlier revisions of a forgotten belief carry no retraction themselves.
		if final, ok := s.byID[e.ID]; ok && final.Status == StatusRejected && final.RetractedAt != nil &&
			(string(e.Value) != string(erasedValue) || e.Quote != "") {
			ids[e.ID] = true
		}
	}
	s.recordFingerprints(ids)
	return s.eraseContentLocked(ids)
}

// saltPath holds the install's random salt for forgotten-word hashes. It is
// what stops a hash from being looked up in a table of common names.
func (s *Store) saltPath() string {
	return filepath.Join(filepath.Dir(s.path), "forget.salt")
}

func (s *Store) salt() []byte {
	if b, err := os.ReadFile(s.saltPath()); err == nil && len(b) >= 16 {
		return b
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil
	}
	_ = os.WriteFile(s.saltPath(), b, 0o600)
	return b
}

func (s *Store) wordHash(word string) string {
	sum := sha256.Sum256(append(append([]byte{}, s.salt()...), []byte(strings.ToLower(word))...))
	return hex.EncodeToString(sum[:8])
}

// fingerprint hashes the words that identify a belief: its names, and for a
// short single-word value the word itself. The words are not kept.
func (s *Store) fingerprint(e Entry) (names []string, whole string) {
	v := strings.TrimSpace(entryValueString(e))
	if v == "" {
		return nil, ""
	}
	for _, n := range forgetNames([]string{v}) {
		names = append(names, s.wordHash(n))
	}
	if !strings.ContainsAny(v, " -_") && len([]rune(v)) < 6 {
		whole = s.wordHash(v)
	}
	return names, whole
}

// matchesForgotten is the hashed counterpart of mentions: two of a forgotten
// belief's names, one name with what the belief was about, or its one short
// word, found in a line.
func (s *Store) matchesForgotten(line string, t Tombstone) bool {
	l := strings.ToLower(line)
	words := strings.FieldsFunc(l, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	known := map[string]bool{}
	for _, n := range t.Names {
		known[n] = true
	}
	hits, seen := 0, map[string]bool{}
	for _, w := range words {
		if t.Whole != "" && s.wordHash(w) == t.Whole {
			return true
		}
		if len([]rune(w)) < 4 || seen[w] {
			continue
		}
		seen[w] = true
		if known[s.wordHash(w)] {
			hits++
		}
	}
	if hits >= 2 {
		return true
	}
	if hits == 1 {
		for _, topic := range topicWords([]Entry{{Predicate: t.Predicate}}) {
			if strings.Contains(l, topic) {
				return true
			}
		}
	}
	return false
}

// recordFingerprints writes the hashed words for beliefs forgotten before
// forgetting kept them that way, from the values still on disk, just before
// those values are erased.
func (s *Store) recordFingerprints(ids map[string]bool) {
	have := map[string]bool{}
	for _, t := range s.Tombstones() {
		if len(t.Names) > 0 || t.Whole != "" {
			have[t.ID] = true
		}
	}
	for id := range ids {
		e, ok := s.byID[id]
		if !ok || have[id] || !strings.Contains(strings.ToLower(e.RetractReason), "owner asked") {
			continue
		}
		names, whole := s.fingerprint(*e)
		if len(names) == 0 && whole == "" {
			continue
		}
		at := time.Now().UTC()
		if e.RetractedAt != nil {
			at = *e.RetractedAt
		}
		_ = s.appendTombstone(Tombstone{ID: id, Subject: e.Subject, Predicate: e.Predicate,
			ValueHash: valueHash(e.Value), Reason: e.RetractReason, At: at, Names: names, Whole: whole})
	}
}
