package personalcontext

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm
}

func declaredSource() Source {
	return Source{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "telegram:42:msg-9", Timestamp: fixedTime}
}

func inferredSource() Source {
	return Source{Type: SourceAgentInference, Kind: SourceInferred, Ref: "cli:7:msg-3", Timestamp: fixedTime}
}

// mkEntry builds a fully valid preference entry with explicit timestamps so
// serialization is deterministic.
func mkEntry(id, subject, predicate string, value interface{}) Entry {
	raw, _ := RawValue(value)
	return Entry{
		ID:         id,
		Kind:       KindPreference,
		Subject:    subject,
		Predicate:  predicate,
		Value:      raw,
		Status:     StatusCurrent,
		Confidence: 1,
		Sources:    []Source{declaredSource()},
		CreatedAt:  fixedTime,
		UpdatedAt:  fixedTime,
	}
}

func mustOpen(t *testing.T, ws string) *Store {
	t.Helper()
	s, err := Open(ws)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func marshalJSON(e Entry) string {
	b, err := json.Marshal(e)
	if err != nil {
		return "<marshal error>"
	}
	return string(b)
}

func marshalEqual(t *testing.T, a, b Entry) bool {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	return bytes.Equal(ab, bb)
}

func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	return strings.Count(string(fileBytes(t, path)), "\n")
}

func entriesPath(ws string) string {
	return filepath.Join(ws, EntriesDir, EntriesFile)
}

// A. Create + reload: an entry persists and is reconstructed identically by a
// fresh store.
func TestCreateAndReload(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	created, err := s.Create(mkEntry("pc-1", "user", "favorite_color", "blue"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "pc-1" || created.Status != StatusCurrent {
		t.Fatalf("created entry not normalized: %+v", created)
	}
	if got := lineCount(t, s.Path()); got != 1 {
		t.Fatalf("log lines = %d, want 1", got)
	}

	s2 := mustOpen(t, ws)
	got, ok := s2.Get("pc-1")
	if !ok {
		t.Fatal("reloaded store missing pc-1")
	}
	if !marshalEqual(t, got, created) {
		t.Fatalf("reloaded entry differs:\n got %s\nwant %s", marshalJSON(got), marshalJSON(created))
	}
}

// B. Deterministic serialization: the same logical entries written into two
// separate stores produce byte-identical logs, including whitespace-varied
// JSON values (compacted to canonical form).
func TestDeterministicSerialization(t *testing.T) {
	ws1, ws2 := t.TempDir(), t.TempDir()
	s1 := mustOpen(t, ws1)
	s2 := mustOpen(t, ws2)

	entries := []Entry{
		mkEntry("pc-1", "user", "favorite_color", "blue"),
		mkEntry("pc-2", "user", "address", json.RawMessage(" {\n\t\"street\": \"x\"\n} ")),
		mkEntry("pc-3", "user", "age", 42),
	}
	for i := range entries {
		if _, err := s1.Create(entries[i]); err != nil {
			t.Fatalf("create in s1: %v", err)
		}
		if _, err := s2.Create(entries[i]); err != nil {
			t.Fatalf("create in s2: %v", err)
		}
	}

	a := fileBytes(t, s1.Path())
	b := fileBytes(t, s2.Path())
	if !bytes.Equal(a, b) {
		t.Fatalf("logs are not deterministic:\n%s\n---\n%s", a, b)
	}
}

// C. Supersession: a new explicitly declared value for the same subject and
// predicate replaces the current one; the old entry is marked superseded and
// points at the new entry; both remain in history.
func TestSupersession(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	if _, err := s.Create(mkEntry("pc-blue", "user", "favorite_color", "blue")); err != nil {
		t.Fatalf("create blue: %v", err)
	}

	sup, err := s.Supersede("user", "favorite_color", mkEntry("pc-green", "user", "favorite_color", "green"))
	if err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	if sup.ID != "pc-green" || sup.Status != StatusCurrent {
		t.Fatalf("superseding entry = %+v, want pc-green current", sup)
	}

	cur := s.Current()
	if len(cur) != 1 || cur[0].ID != "pc-green" {
		t.Fatalf("Current = %+v, want only green", cur)
	}

	old, ok := s.Get("pc-blue")
	if !ok {
		t.Fatal("pc-blue disappeared")
	}
	if old.Status != StatusSuperseded {
		t.Fatalf("pc-blue status = %q, want superseded", old.Status)
	}
	if old.SupersededBy == nil || *old.SupersededBy != "pc-green" {
		t.Fatalf("pc-blue.superseded_by = %v, want pc-green", old.SupersededBy)
	}

	hist := s.History("pc-blue")
	if len(hist) != 2 {
		t.Fatalf("pc-blue history = %d records, want 2", len(hist))
	}
	if hist[0].Status != StatusCurrent || hist[1].Status != StatusSuperseded {
		t.Fatalf("pc-blue history statuses = %q,%q", hist[0].Status, hist[1].Status)
	}

	if all := s.All(); len(all) != 2 {
		t.Fatalf("All = %d entries, want 2 (both must remain)", len(all))
	}
	if got := lineCount(t, s.Path()); got != 3 {
		t.Fatalf("log lines = %d, want 3 (blue + green + blue revision)", got)
	}

	// No current entry to supersede is an explicit error, not a silent create.
	if _, err := s.Supersede("user", "nonexistent_predicate", mkEntry("pc-x", "user", "nonexistent_predicate", "v")); err == nil {
		t.Fatal("Supersede with no current entry should fail")
	}
}

// D. Provenance: supersession never destroys the original source.
func TestProvenancePreserved(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	src := declaredSource()
	blue := mkEntry("pc-blue", "user", "favorite_color", "blue")
	blue.Sources = []Source{src}
	if _, err := s.Create(blue); err != nil {
		t.Fatalf("create blue: %v", err)
	}

	green := mkEntry("pc-green", "user", "favorite_color", "green")
	green.Sources = []Source{{Type: SourceConversation, Kind: SourceUserCorrected, Ref: "telegram:42:msg-11", Timestamp: fixedTime}}
	if _, err := s.Supersede("user", "favorite_color", green); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	old, _ := s.Get("pc-blue")
	if !reflect.DeepEqual(old.Sources, []Source{src}) {
		t.Fatalf("pc-blue sources after supersession = %+v, want original", old.Sources)
	}
	for i, rec := range s.History("pc-blue") {
		if !reflect.DeepEqual(rec.Sources, []Source{src}) {
			t.Fatalf("history[%d] sources = %+v, want original", i, rec.Sources)
		}
	}
	newEntry, _ := s.Get("pc-green")
	if len(newEntry.Sources) != 1 || newEntry.Sources[0].Kind != SourceUserCorrected {
		t.Fatalf("pc-green sources = %+v, want the correcting source", newEntry.Sources)
	}
}

func TestResetClearsMemoryAndFile(t *testing.T) {
	ws := t.TempDir()
	store, err := Open(ws)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.Create(mkEntry("e1", "user", "identity/name", "Ian")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(store.Current()) != 1 {
		t.Fatalf("expected 1 current entry")
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if len(store.Current()) != 0 {
		t.Fatalf("stale beliefs visible after reset")
	}
	data, err := os.ReadFile(filepath.Join(ws, EntriesDir, EntriesFile))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("log not truncated")
	}
	// Store stays usable after reset.
	if _, err := store.Create(mkEntry("e2", "user", "identity/name", "Lan")); err != nil {
		t.Fatalf("create after reset: %v", err)
	}
	if len(store.Current()) != 1 {
		t.Fatalf("expected 1 current entry after re-create")
	}
}

func TestCurrentInScope(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	mk := func(pred, val string, scopes []string) {
		raw, err := RawValue(val)
		if err != nil {
			t.Fatal(err)
		}
		e := Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: pred,
			Value: raw, Status: StatusCurrent, Scopes: scopes,
			Sources: []Source{{Type: SourceCommand, Kind: SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}}}
		if _, err := s.Create(e); err != nil {
			t.Fatal(err)
		}
	}
	mk("likes", "tea", nil)
	mk("project", "Ghost", []string{"context:work"})
	mk("hobby", "sailing", []string{"context:home"})
	global := s.CurrentInScope(nil)
	if len(global) != 1 {
		t.Fatalf("scopeless sees global only, got %d", len(global))
	}
	work := s.CurrentInScope([]string{"context:work"})
	if len(work) != 2 {
		t.Fatalf("work sees global+own, got %d", len(work))
	}
	home := s.CurrentInScope([]string{"context:home"})
	for _, e := range home {
		if e.Predicate == "project" {
			t.Fatal("home must not see work-scoped fact")
		}
	}
}

func TestCurrentInScopePrecedence(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	mk := func(pred, val string, scopes []string) {
		raw, err := RawValue(val)
		if err != nil {
			t.Fatal(err)
		}
		e := Entry{ID: "test-" + pred + "-", Kind: KindFact, Subject: "user", Predicate: pred,
			Value: raw, Status: StatusCurrent, Scopes: scopes,
			Sources: []Source{{Type: SourceCommand, Kind: SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}}}
		// unique ids per scope
		if len(scopes) > 0 {
			e.ID += scopes[0]
		}
		if _, err := s.Create(e); err != nil {
			t.Fatal(err)
		}
	}
	mk("likes", "tea", nil)
	mk("likes", "coffee", []string{"context:work"})
	work := s.CurrentInScope([]string{"context:work"})
	count, sawCoffee, sawTea := 0, false, false
	for _, e := range work {
		if e.Predicate == "likes" {
			count++
			var v string
			json.Unmarshal(e.Value, &v)
			if v == "coffee" {
				sawCoffee = true
			}
			if v == "tea" {
				sawTea = true
			}
		}
	}
	if count != 1 || !sawCoffee || sawTea {
		t.Fatalf("scoped must shadow global (count=%d coffee=%v tea=%v)", count, sawCoffee, sawTea)
	}
	// Personal still sees the global one.
	personal := s.CurrentInScope(nil)
	found := false
	for _, e := range personal {
		if e.Predicate == "likes" {
			found = true
		}
	}
	if !found {
		t.Fatal("personal must see global")
	}
}

func TestHasCurrentDedup(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	raw, _ := RawValue("tea")
	raw2, _ := RawValue("coffee")
	e1 := Entry{ID: newEntryID(), Kind: KindPreference, Subject: "user", Predicate: "likes", Value: raw,
		Status: StatusCurrent, Sources: []Source{{Type: SourceCommand, Kind: SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}}}
	if _, err := s.Create(e1); err != nil {
		t.Fatal(err)
	}
	dup := Entry{ID: newEntryID(), Kind: KindPreference, Subject: "user", Predicate: "likes", Value: raw,
		Status: StatusCurrent, Sources: []Source{{Type: SourceCommand, Kind: SourceUserDeclared, Ref: "t:2", Timestamp: time.Now().UTC()}}}
	if !HasCurrent(s.Current(), dup) {
		t.Fatal("identical restatement must be detected as duplicate")
	}
	diff := dup
	diff.Value = raw2
	if HasCurrent(s.Current(), diff) {
		t.Fatal("different value must not be considered duplicate")
	}
}
