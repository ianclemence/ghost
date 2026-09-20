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

func TestArtifactItem(t *testing.T) {
	items := List(Inputs{Artifacts: []ArtifactInput{
		{ID: "a1", Kind: "file", Title: "Trip itinerary", Path: "reports/trip.md", State: "available", CreatedAt: ts(5)},
		{ID: "a2", Kind: "link", Title: "Booking", URL: "https://example.com", State: "available", CreatedAt: ts(4)},
		{ID: "a3", Kind: "text", Title: "Notes", Summary: "A summary", State: "available", CreatedAt: ts(3)},
	}})
	if len(items) != 3 {
		t.Fatalf("want 3 artifacts, got %d", len(items))
	}
	if items[0].ID != "art:a1" {
		t.Errorf("newest should sort first, got %s", items[0].ID)
	}
	byID := map[string]Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if byID["art:a1"].Kind != KindFile || !hasRender(byID["art:a1"].Render, RenderDownload) {
		t.Errorf("file artifact wrong: %+v", byID["art:a1"])
	}
	if byID["art:a2"].Kind != KindLink || !hasRender(byID["art:a2"].Render, RenderVisit) {
		t.Errorf("link artifact wrong: %+v", byID["art:a2"])
	}
	if byID["art:a3"].Kind != KindText {
		t.Errorf("text artifact wrong: %+v", byID["art:a3"])
	}
}

func TestMissingIdentityIsDropped(t *testing.T) {
	items := List(Inputs{Artifacts: []ArtifactInput{
		{ID: "", Title: "no id"},
		{ID: "x", Title: "  "},
	}})
	if len(items) != 0 {
		t.Fatalf("items without id/title must be dropped, got %d", len(items))
	}
}

func TestUnavailableIsHonest(t *testing.T) {
	items := List(Inputs{Artifacts: []ArtifactInput{
		{ID: "a1", Kind: "file", Title: "Gone", State: "unavailable", CreatedAt: ts(1)},
	}})
	if len(items) != 1 {
		t.Fatalf("unavailable artifacts must still list")
	}
	if items[0].Summary == "" || items[0].State != "unavailable" {
		t.Errorf("unavailable must be honest: %+v", items[0])
	}
}

func TestDeterministicOrdering(t *testing.T) {
	in := Inputs{Artifacts: []ArtifactInput{
		{ID: "b", Kind: "text", Title: "B", CreatedAt: ts(1)},
		{ID: "a", Kind: "text", Title: "A", CreatedAt: ts(1)},
		{ID: "z", Kind: "text", Title: "Z", CreatedAt: ts(9)},
	}}
	first := List(in)
	second := List(in)
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("non-deterministic at %d", i)
		}
	}
	if first[0].ID != "art:z" {
		t.Errorf("newest first, got %s", first[0].ID)
	}
	if first[1].ID != "art:a" || first[2].ID != "art:b" {
		t.Errorf("tie-break by id failed: %s, %s", first[1].ID, first[2].ID)
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
