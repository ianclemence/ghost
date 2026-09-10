package personalcontext

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ProvenanceClass orders trust by strongest source: a user statement outranks
// any number of model inferences, and a legacy entry with no source is
// unknown (never invented as canonical).
func TestProvenanceClass(t *testing.T) {
	cases := []struct {
		name    string
		sources []Source
		want    Trust
	}{
		{"user declared", []Source{{Type: SourceConversation, Kind: SourceUserDeclared}}, TrustCanonical},
		{"user corrected", []Source{{Type: SourceConversation, Kind: SourceUserCorrected}}, TrustCanonical},
		{"manual edit", []Source{{Type: SourceManualEdit, Kind: SourceManual}}, TrustCanonical},
		{"document", []Source{{Type: SourceDocument, Kind: SourceImported}}, TrustObserved},
		{"import", []Source{{Type: SourceImport, Kind: SourceImported}}, TrustObserved},
		{"inferred", []Source{{Type: SourceAgentInference, Kind: SourceInferred}}, TrustInferred},
		{"no provenance", nil, TrustUnknown},
		{"inferred plus declared is canonical", []Source{
			{Type: SourceAgentInference, Kind: SourceInferred},
			{Type: SourceConversation, Kind: SourceUserDeclared},
		}, TrustCanonical},
	}
	for _, c := range cases {
		if got := ProvenanceClass(Entry{Sources: c.sources}); got != c.want {
			t.Fatalf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

// The promotion policy is the guard against self-reinforcing hallucination:
// a weak model inference is held as a candidate, not promoted to truth.
func TestPromotionPolicy(t *testing.T) {
	p := DefaultPromotionPolicy()
	cases := []struct {
		name       string
		entry      Entry
		wantPromot bool
		wantStatus Status
	}{
		{"explicit user fact", Entry{Confidence: 1, Sources: []Source{{Type: SourceConversation, Kind: SourceUserDeclared}}}, true, StatusCurrent},
		{"observed evidence", Entry{Confidence: 0.4, Sources: []Source{{Type: SourceDocument, Kind: SourceImported}}}, true, StatusCurrent},
		{"confident inference", Entry{Confidence: 0.9, Sources: []Source{inferredSource()}}, true, StatusCurrent},
		{"weak inference held back", Entry{Confidence: 0.2, Sources: []Source{inferredSource()}}, false, StatusUncertain},
		{"no provenance held back", Entry{Confidence: 1}, false, StatusUncertain},
	}
	for _, c := range cases {
		d := p.Evaluate(c.entry)
		if d.Promote != c.wantPromot || d.Status != c.wantStatus {
			t.Fatalf("%s: got promote=%v status=%s (%s)", c.name, d.Promote, d.Status, d.Reason)
		}
		if d.Reason == "" {
			t.Fatalf("%s: decision must carry a reason", c.name)
		}
	}
}

// Existing logs written before the lifetime field existed load as durable —
// no rewrite, no data loss, provenance untouched.
func TestLegacyEntryLoadsDurable(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, EntriesDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"pc-old","kind":"fact","subject":"user","predicate":"timezone","value":"\"UTC\"","status":"current","confidence":0.9,` +
		`"sources":[{"type":"conversation","kind":"user_declared","timestamp":"2026-01-01T00:00:00Z"}],` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, EntriesFile), []byte(legacy+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t, ws)
	e, ok := s.Get("pc-old")
	if !ok {
		t.Fatal("legacy entry must load")
	}
	if e.Lifetime != LifetimeDurable {
		t.Fatalf("legacy entry lifetime = %q, want durable", e.Lifetime)
	}
	if ProvenanceClass(e) != TrustCanonical {
		t.Fatalf("legacy provenance must survive: %s", ProvenanceClass(e))
	}
}

// A new entry with no lifetime is stored as durable and validates.
func TestCreateDefaultsLifetime(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	e := mkEntry("pc-lt", "user", "color", "blue")
	e.Lifetime = ""
	got, err := s.Create(e)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifetime != LifetimeDurable {
		t.Fatalf("created lifetime = %q, want durable", got.Lifetime)
	}
}

// An invalid lifetime is rejected rather than silently accepted.
func TestInvalidLifetimeRejected(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	e := mkEntry("pc-bad", "user", "color", "blue")
	e.Lifetime = "eternal"
	if _, err := s.Create(e); err == nil {
		t.Fatal("invalid lifetime must be rejected")
	}
}

// An expired belief is not current, even though its record and provenance
// remain inspectable.
func TestExpiredNotCurrent(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	past := fixedTime.Add(-time.Hour)
	e := mkEntry("pc-exp", "user", "promo", "spring")
	e.ValidUntil = &past
	if _, err := s.Create(e); err != nil {
		t.Fatal(err)
	}
	if cur := s.Current(); len(cur) != 0 {
		t.Fatalf("expired entry must not be current: %+v", cur)
	}
	if _, ok := s.Get("pc-exp"); !ok {
		t.Fatal("expired entry must remain inspectable")
	}
}
