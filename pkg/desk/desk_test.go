package desk

import (
	"testing"
	"time"
)

func ts(min int) time.Time {
	return time.Date(2026, 3, 1, 9, min, 0, 0, time.UTC)
}

func TestListEmpty(t *testing.T) {
	if got := List(Inputs{}); len(got) != 0 {
		t.Fatalf("empty inputs must yield empty desk, got %d", len(got))
	}
}

func TestDocumentItem(t *testing.T) {
	items := List(Inputs{Documents: []DocumentInput{
		{RelPath: "reports/q1.md", Size: 120, UpdatedAt: ts(1)},
	}})
	if len(items) != 1 {
		t.Fatalf("want 1, got %d", len(items))
	}
	it := items[0]
	if it.Kind != KindDocument || it.ID != "doc:reports/q1.md" {
		t.Errorf("identity wrong: %+v", it)
	}
	if it.Title != "q1.md" || it.Summary != "reports/q1.md" {
		t.Errorf("title/summary wrong: %+v", it)
	}
	if it.Size != 120 || it.Protected {
		t.Errorf("size/protected wrong: %+v", it)
	}
	if len(it.Render) == 0 {
		t.Errorf("document must declare render affordances")
	}
}

// The estate boundary is inherited: internal paths are never Desk items,
// even if a caller passes them directly.
func TestProtectedPathsNeverSurfaced(t *testing.T) {
	protected := []string{
		"personal-context/entries.jsonl",
		"state/turns/x.json",
		"events/2026-09-01.ndjson",
		"knowledge/self/user-profile.md",
		"memory/2026-09-20.md",
		"journal/note.md",
		"ghost.db",
		"ghost.db-wal",
		"tmp/browser/page.png",
		"../escape.txt",
		"/etc/passwd",
	}
	items := List(Inputs{Documents: func() []DocumentInput {
		var out []DocumentInput
		for _, p := range protected {
			out = append(out, DocumentInput{RelPath: p, UpdatedAt: ts(1)})
		}
		return out
	}()})
	if len(items) != 0 {
		t.Fatalf("protected paths must never surface, got %d: %+v", len(items), items)
	}
}

func TestArtifactItem(t *testing.T) {
	items := List(Inputs{Artifacts: []ArtifactInput{
		{ID: "a1", Kind: "file", Title: "Trip itinerary", Path: "reports/trip.md", State: "available", CreatedAt: ts(5)},
		{ID: "a2", Kind: "link", Title: "Booking", URL: "https://example.com", State: "available", CreatedAt: ts(4)},
		{ID: "a3", Kind: "text", Title: "Notes", Summary: "A summary", State: "available", CreatedAt: ts(3)},
	}})
	if len(items) != 3 {
		t.Fatalf("want 3 artifacts, got %d", len(items))
	}
	// Newest first.
	if items[0].ID != "art:a1" {
		t.Errorf("ordering wrong, first = %s", items[0].ID)
	}
	// Link artifacts declare visit; file artifacts declare download.
	byID := map[string]Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if !hasRender(byID["art:a2"].Render, RenderVisit) {
		t.Errorf("link artifact must declare visit")
	}
	if !hasRender(byID["art:a1"].Render, RenderDownload) {
		t.Errorf("file artifact must declare download")
	}
}

func TestArtifactUnavailableIsHonest(t *testing.T) {
	items := List(Inputs{Artifacts: []ArtifactInput{
		{ID: "a1", Kind: "file", Title: "Gone", State: "unavailable", CreatedAt: ts(1)},
	}})
	if len(items) != 1 {
		t.Fatalf("unavailable artifacts must still list (their existence was real)")
	}
	if items[0].Summary == "" {
		t.Errorf("unavailable artifact must carry an honest summary")
	}
}

// Only skills Ghost built/installed into the workspace are "tools" on the
// Desk; bundled skills are Ghost's own abilities, not the owner's artifacts.
func TestToolItemOnlyBuilt(t *testing.T) {
	items := List(Inputs{Tools: []ToolInput{
		{Name: "my-tracker", Description: "Tracks spending", Built: true, UpdatedAt: ts(2)},
		{Name: "weather", Description: "Bundled", Built: false, UpdatedAt: ts(2)},
	}})
	if len(items) != 1 || items[0].ID != "tool:my-tracker" {
		t.Fatalf("only built tools should surface, got %+v", items)
	}
}

func TestToolItemFallbackSummary(t *testing.T) {
	items := List(Inputs{Tools: []ToolInput{{Name: "helper", Built: true, UpdatedAt: ts(1)}}})
	if len(items) != 1 || items[0].Summary == "" {
		t.Fatalf("a tool with no description still needs an honest summary")
	}
}

func TestSurfaceItem(t *testing.T) {
	items := List(Inputs{Surfaces: []SurfaceInput{
		{ID: "s1", Kind: "browser", State: "active", Title: "Example", URL: "https://example.com", UpdatedAt: ts(3)},
		{ID: "s2", Kind: "computer", State: "active", UpdatedAt: ts(2)},
	}})
	if len(items) != 2 {
		t.Fatalf("want 2 surfaces, got %d", len(items))
	}
	if items[0].ID != "surf:browser:s1" || items[0].Summary != "https://example.com" {
		t.Errorf("browser surface wrong: %+v", items[0])
	}
	if items[1].Title == "" {
		t.Errorf("computer surface needs a fallback title")
	}
}

func TestDeterministicOrdering(t *testing.T) {
	in := Inputs{
		Documents: []DocumentInput{
			{RelPath: "b.md", UpdatedAt: ts(1)},
			{RelPath: "a.md", UpdatedAt: ts(1)},
		},
		Artifacts: []ArtifactInput{
			{ID: "z", Kind: "file", Title: "Z", CreatedAt: ts(9)},
		},
	}
	first := List(in)
	second := List(in)
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("non-deterministic at %d: %s vs %s", i, first[i].ID, second[i].ID)
		}
	}
	// Newest updated first.
	if first[0].ID != "art:z" {
		t.Errorf("most recently updated should sort first, got %s", first[0].ID)
	}
	// Tie-break by id within same timestamp+kind.
	if first[1].ID != "doc:a.md" || first[2].ID != "doc:b.md" {
		t.Errorf("tie-break by id failed: %s, %s", first[1].ID, first[2].ID)
	}
}

func TestNoMutationOfInput(t *testing.T) {
	docs := []DocumentInput{{RelPath: "./a.md", UpdatedAt: ts(1)}}
	in := Inputs{Documents: docs}
	_ = List(in)
	if docs[0].RelPath != "./a.md" {
		t.Errorf("List must not mutate its input")
	}
}

func hasRender(rs []Render, want Render) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}
