package ghoststate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// pcSource builds a realistic provenance source. Refs use the store's
// session_id:message_id convention.
func pcSource(kind personalcontext.SourceKind, ref string, ts time.Time) personalcontext.Source {
	return personalcontext.Source{
		Type:      personalcontext.SourceConversation,
		Kind:      kind,
		Ref:       ref,
		Timestamp: ts,
	}
}

// pcEntry builds a current entry with deterministic provenance.
func pcEntry(t *testing.T, id, kind, subject, predicate string, value interface{}, src personalcontext.Source) personalcontext.Entry {
	t.Helper()
	raw, err := personalcontext.RawValue(value)
	if err != nil {
		t.Fatalf("RawValue(%v): %v", value, err)
	}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return personalcontext.Entry{
		ID:         id,
		Kind:       personalcontext.Kind(kind),
		Subject:    subject,
		Predicate:  predicate,
		Value:      raw,
		Status:     personalcontext.StatusCurrent,
		Confidence: 0.95,
		Sources:    []personalcontext.Source{src},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// mustCreatePC persists an entry, failing the test on error.
func mustCreatePC(t *testing.T, store *personalcontext.Store, e personalcontext.Entry) {
	t.Helper()
	if _, err := store.Create(e); err != nil {
		t.Fatalf("Create(%s): %v", e.ID, err)
	}
}

// richPCWorkspace builds a workspace whose Personal Context store carries a
// deliberately complete lifecycle: a current entry, a supersession chain, a
// forgotten entry, a conflicting pair, an entry with temporal validity, and a
// multi-source current entry.
func richPCWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}

	// 1. A normal current declaration, with realistic provenance.
	mustCreatePC(t, store, pcEntry(t, "e-name", "identity", "user", "identity/name", "Ian",
		pcSource(personalcontext.SourceUserDeclared, "s1:m1", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))))

	// 2. A supersession: blue -> green. blue must stay superseded with
	//    superseded_by pointing at green; green becomes current.
	mustCreatePC(t, store, pcEntry(t, "e-blue", "preference", "user", "preference/favorite_color", "blue",
		pcSource(personalcontext.SourceUserDeclared, "s1:m2", time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC))))
	green := pcEntry(t, "e-green", "preference", "user", "preference/favorite_color", "green",
		pcSource(personalcontext.SourceUserCorrected, "s1:m3", time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)))
	if _, err := store.Supersede("user", "preference/favorite_color", green); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	// 3. A forgotten entry: pizza is retired to rejected, exactly as /forget
	//    does. Its record and provenance must travel.
	mustCreatePC(t, store, pcEntry(t, "e-pizza", "preference", "user", "preference/food", "pizza",
		pcSource(personalcontext.SourceUserDeclared, "s1:m4", time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC))))
	if err := store.Forget("e-pizza"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	// 4. A conflicting pair: two current values for one belief, both marked
	//    conflicting and never silently resolved.
	mustCreatePC(t, store, pcEntry(t, "e-ex-a", "preference", "user", "preference/example", "A",
		pcSource(personalcontext.SourceUserDeclared, "s1:m5", time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))))
	mustCreatePC(t, store, pcEntry(t, "e-ex-b", "preference", "user", "preference/example", "B",
		pcSource(personalcontext.SourceUserDeclared, "s1:m6", time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC))))
	if err := store.DeclareConflict("user", "preference/example", "e-ex-a", "e-ex-b"); err != nil {
		t.Fatalf("DeclareConflict: %v", err)
	}

	// 5. Temporal validity: current only within a window.
	seasonal := pcEntry(t, "e-season", "fact", "user", "fact/season", "winter",
		pcSource(personalcontext.SourceUserDeclared, "s1:m7", time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)))
	from := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	seasonal.ValidFrom = &from
	seasonal.ValidUntil = &until
	mustCreatePC(t, store, seasonal)

	return ws
}

// canonicalEntries renders entries as canonical JSON lines for a
// deterministic logical comparison that does not weaken correctness.
func canonicalEntries(t *testing.T, entries []personalcontext.Entry) string {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal entry %s: %v", e.ID, err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	return buf.String()
}

func assertSameEntries(t *testing.T, label string, want, got []personalcontext.Entry) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: got %d entries, want %d\nwant: %s\ngot:  %s", label, len(got), len(want),
			canonicalEntries(t, want), canonicalEntries(t, got))
	}
	if a, b := canonicalEntries(t, want), canonicalEntries(t, got); a != b {
		t.Fatalf("%s: entries differ\nwant:\n%s\ngot:\n%s", label, a, b)
	}
}

// exportPCAndImport round-trips a workspace's Ghost State into a fresh target
// workspace and returns both manifests.
func exportPCAndImport(t *testing.T, ws string) (string, *Manifest, *Manifest) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	m, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	targetWS := t.TempDir()
	im, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return targetWS, m, im
}

// The exported entries.jsonl is byte-identical to the canonical log, recorded
// as portable, and deterministic across exports.
func TestPersonalContextExportIsCanonicalAndDeterministic(t *testing.T) {
	ws := richPCWorkspace(t)
	sourceLog, err := os.ReadFile(filepath.Join(ws, "personal-context", "entries.jsonl"))
	if err != nil {
		t.Fatalf("read source log: %v", err)
	}

	archive1 := filepath.Join(t.TempDir(), "a.ghost")
	m1, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive1,
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	archive2 := filepath.Join(t.TempDir(), "b.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive2,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("second Export: %v", err)
	}

	f := m1.File(personalContextEntriesLogical)
	if f == nil {
		t.Fatalf("%s missing from manifest", personalContextEntriesLogical)
	}
	if f.Category != CategoryPortable {
		t.Fatalf("category = %q, want portable", f.Category)
	}
	if f.Digest != digestBytes(sourceLog) {
		t.Fatalf("manifest digest does not match the canonical log bytes")
	}
	if f.Size != int64(len(sourceLog)) {
		t.Fatalf("manifest size %d, want %d", f.Size, len(sourceLog))
	}

	readFiles := func(archive string) map[string][]byte {
		t.Helper()
		blob, err := os.ReadFile(archive)
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		plain, err := decryptBytes(blob, testPassphrase)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		_, files, err := readArchive(plain)
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		return files
	}
	a, b := readFiles(archive1), readFiles(archive2)
	got, ok := a[personalContextEntriesLogical]
	if !ok {
		t.Fatalf("archive missing %s", personalContextEntriesLogical)
	}
	if !bytes.Equal(got, sourceLog) {
		t.Fatal("exported Personal Context is not byte-identical to the canonical log")
	}
	if !bytes.Equal(got, b[personalContextEntriesLogical]) {
		t.Fatal("Personal Context export is not deterministic across exports")
	}
}

// The complete lifecycle round-trips: after export/import the store
// reconstructs exactly the same state with no conversion step.
func TestPersonalContextRoundTripRichLifecycle(t *testing.T) {
	srcWS := richPCWorkspace(t)
	srcStore, err := personalcontext.Open(srcWS)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}

	targetWS, m, im := exportPCAndImport(t, srcWS)
	if m.File(personalContextEntriesLogical) == nil {
		t.Fatal("Personal Context missing from export manifest")
	}
	if im.File(personalContextEntriesLogical) == nil {
		t.Fatal("Personal Context missing from import manifest")
	}

	// The imported file is directly loadable by the store — no conversion.
	dstStore, err := personalcontext.Open(targetWS)
	if err != nil {
		t.Fatalf("open imported store: %v", err)
	}

	// Full-log equality is the strongest guarantee: every revision, including
	// the superseded and rejected ones, survived in order.
	assertSameEntries(t, "All()", srcStore.All(), dstStore.All())

	now := time.Now()
	assertSameEntries(t, "Current()", srcStore.Current(), dstStore.Current())
	assertSameEntries(t, "CurrentAt(now)", srcStore.CurrentAt(now), dstStore.CurrentAt(now))

	// Temporal validity: outside the window the seasonal entry is absent on
	// both sides; inside it, present.
	outside := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	assertSameEntries(t, "CurrentAt(outside window)", srcStore.CurrentAt(outside), dstStore.CurrentAt(outside))
	for _, store := range []*personalcontext.Store{srcStore, dstStore} {
		for _, e := range store.CurrentAt(outside) {
			if e.ID == "e-season" {
				t.Fatalf("e-season leaked into CurrentAt outside its validity window")
			}
		}
	}

	// Lifecycle state is preserved per entry.
	for _, id := range []string{"e-name", "e-blue", "e-green", "e-pizza", "e-ex-a", "e-ex-b", "e-season"} {
		se, ok := srcStore.Get(id)
		if !ok {
			t.Fatalf("source missing %s", id)
		}
		ie, ok := dstStore.Get(id)
		if !ok {
			t.Fatalf("imported missing %s", id)
		}
		if se.Status != ie.Status {
			t.Errorf("%s status = %q, want %q", id, ie.Status, se.Status)
		}
		if se.Confidence != ie.Confidence {
			t.Errorf("%s confidence = %v, want %v", id, ie.Confidence, se.Confidence)
		}
		if len(se.Sources) != len(ie.Sources) {
			t.Errorf("%s sources = %d, want %d", id, len(ie.Sources), len(se.Sources))
		}
		for i := range se.Sources {
			if se.Sources[i] != ie.Sources[i] {
				t.Errorf("%s source %d = %+v, want %+v", id, i, ie.Sources[i], se.Sources[i])
			}
		}
		if (se.ValidFrom == nil) != (ie.ValidFrom == nil) ||
			(se.ValidUntil == nil) != (ie.ValidUntil == nil) {
			t.Errorf("%s validity pointers differ: src %v/%v, dst %v/%v",
				id, se.ValidFrom, se.ValidUntil, ie.ValidFrom, ie.ValidUntil)
		} else if se.ValidFrom != nil && !se.ValidFrom.Equal(*ie.ValidFrom) {
			t.Errorf("%s valid_from = %v, want %v", id, *ie.ValidFrom, *se.ValidFrom)
		} else if se.ValidUntil != nil && !se.ValidUntil.Equal(*ie.ValidUntil) {
			t.Errorf("%s valid_until = %v, want %v", id, *ie.ValidUntil, *se.ValidUntil)
		}
		if (se.SupersededBy == nil) != (ie.SupersededBy == nil) ||
			(se.SupersededBy != nil && *se.SupersededBy != *ie.SupersededBy) {
			t.Errorf("%s superseded_by = %v, want %v", id, ie.SupersededBy, se.SupersededBy)
		}
		if !se.CreatedAt.Equal(ie.CreatedAt) || !se.UpdatedAt.Equal(ie.UpdatedAt) {
			t.Errorf("%s timestamps differ: src %v/%v, dst %v/%v", id, se.CreatedAt, se.UpdatedAt, ie.CreatedAt, ie.UpdatedAt)
		}
	}

	// Query surfaces agree.
	assertSameEntries(t, "ByPredicate(favorite_color)", srcStore.ByPredicate("preference/favorite_color"), dstStore.ByPredicate("preference/favorite_color"))
	assertSameEntries(t, "BySubject(user)", srcStore.BySubject("user"), dstStore.BySubject("user"))
	assertSameEntries(t, "ByKind(preference)", srcStore.ByKind(personalcontext.KindPreference), dstStore.ByKind(personalcontext.KindPreference))

	// History (full revision chains) survives: blue has two records, the
	// rejected pizza has two, the current name has one.
	assertSameEntries(t, "History(e-blue)", srcStore.History("e-blue"), dstStore.History("e-blue"))
	assertSameEntries(t, "History(e-pizza)", srcStore.History("e-pizza"), dstStore.History("e-pizza"))
	assertSameEntries(t, "History(e-name)", srcStore.History("e-name"), dstStore.History("e-name"))

	// The complete append-only log survives line-for-line: the imported file
	// is byte-identical to the source's canonical log.
	srcBytes, _ := os.ReadFile(filepath.Join(srcWS, "personal-context", "entries.jsonl"))
	dstBytes, err := os.ReadFile(filepath.Join(targetWS, "personal-context", "entries.jsonl"))
	if err != nil {
		t.Fatalf("read imported log: %v", err)
	}
	if !bytes.Equal(srcBytes, dstBytes) {
		t.Fatal("imported entries.jsonl differs from the source log")
	}
}

// Workspaces that never used Personal Context export and import cleanly: the
// target store opens as an empty store with no fake context file.
func TestPersonalContextEmptyMissingRoundTrip(t *testing.T) {
	ws := t.TempDir()
	targetWS, m, _ := exportPCAndImport(t, ws)
	if m.File(personalContextEntriesLogical) != nil {
		t.Fatalf("empty workspace should not export %s", personalContextEntriesLogical)
	}
	if _, err := os.Stat(filepath.Join(targetWS, "personal-context", "entries.jsonl")); err == nil {
		t.Fatal("import must not fabricate a Personal Context file for a workspace that never had one")
	}
	store, err := personalcontext.Open(targetWS)
	if err != nil {
		t.Fatalf("open empty imported store: %v", err)
	}
	if cur := store.Current(); len(cur) != 0 {
		t.Fatalf("imported empty store has %d current entries, want 0", len(cur))
	}
}

// Malformed Personal Context in an archive fails the import loudly instead of
// silently becoming an empty context or dropping records.
func TestPersonalContextMalformedImportFails(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "personal-context")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bad := `{"id":"e1","kind":"preference","subject":"user","predicate":"preference/favorite_color","value":"blue","status":"current","confidence":0.95,"sources":[],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}` + "\n" + `NOT JSON`
	if err := os.WriteFile(filepath.Join(dir, "entries.jsonl"), []byte(bad), 0644); err != nil {
		t.Fatalf("write malformed log: %v", err)
	}

	// Export copies the canonical bytes without interpretation.
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Import validates the Personal Context before writing it and fails loudly.
	_, err := Import(ImportOptions{
		Workspace:  t.TempDir(),
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	})
	if err == nil {
		t.Fatal("import of malformed Personal Context must fail")
	}
	if !strings.Contains(err.Error(), "Personal Context") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error should name the artifact and the bad line, got: %v", err)
	}
}

// ValidateEntries is the store's own integrity gate: it accepts exactly what
// Open accepts (including an empty log) and rejects malformed lines loudly.
func TestPersonalContextValidateEntries(t *testing.T) {
	if err := personalcontext.ValidateEntries(nil); err != nil {
		t.Fatalf("empty log should validate: %v", err)
	}
	store, err := personalcontext.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	e := pcEntry(t, "e1", "preference", "user", "preference/favorite_color", "green",
		pcSource(personalcontext.SourceUserDeclared, "s1:m1", time.Now().UTC()))
	if _, err := store.Create(e); err != nil {
		t.Fatalf("create: %v", err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if err := personalcontext.ValidateEntries(raw); err != nil {
		t.Fatalf("valid log should validate: %v", err)
	}
	if err := personalcontext.ValidateEntries([]byte("{\"id\":\"e2\"}\nthis is not json\n")); err == nil {
		t.Fatal("malformed line should fail validation")
	}
}
