// Package desk is Ghost's read-only feed of the things Ghost has made FOR the
// owner — the artifacts it produced on their behalf.
//
// It owns no tables, no execution, no authority, and no credentials. The
// artifact authority has already validated existence and provenance; this
// package only normalizes and orders. See docs/DESK.md.
//
// Scope note: an earlier version also surfaced workspace files, built tools,
// and live surfaces. That made the Desk a file manager — the owner saw
// internal files and sizes they had no reason to care about. A person wants
// to look at what Ghost made for them, so the Desk is exactly that.
package desk

import (
	"sort"
	"strings"
	"time"
)

// Kind is the shape of a Desk item. Every item is something Ghost made.
type Kind string

const (
	KindFile Kind = "file" // a file Ghost produced
	KindText Kind = "text" // a written result
	KindLink Kind = "link" // a link Ghost handed over
)

// Render is a backend-declared affordance. It is a display hint, never an
// execution grant: "open" opens a preview, it does not run anything.
type Render string

const (
	RenderPreview  Render = "preview"
	RenderOpen     Render = "open"
	RenderDownload Render = "download"
	RenderVisit    Render = "visit"
)

// Item is one thing Ghost made for the owner.
type Item struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary,omitempty"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	Render    []Render  `json:"render"`
}

// ArtifactInput mirrors the fields of pkg/artifacts.Artifact the Desk needs,
// avoiding an import cycle and keeping the projection pure.
type ArtifactInput struct {
	ID        string
	Kind      string
	Title     string
	Summary   string
	Path      string
	URL       string
	State     string
	CreatedAt time.Time
}

// Inputs is everything the projection reads.
type Inputs struct {
	Artifacts []ArtifactInput
}

// maxTitle bounds a rendered title so a model string can never dominate the
// surface.
const maxTitle = 200

// List normalizes and orders the feed: most recent first, then by id, so the
// surface is stable across calls.
func List(in Inputs) []Item {
	out := make([]Item, 0, len(in.Artifacts))
	for _, a := range in.Artifacts {
		if item, ok := artifactItem(a); ok {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// artifactItem builds a Desk item from a runtime-validated artifact.
func artifactItem(a ArtifactInput) (Item, bool) {
	id := strings.TrimSpace(a.ID)
	title := strings.TrimSpace(a.Title)
	if id == "" || title == "" {
		return Item{}, false
	}
	kind := KindText
	render := []Render{RenderPreview}
	switch strings.ToLower(strings.TrimSpace(a.Kind)) {
	case "file":
		kind = KindFile
		render = append(render, RenderOpen, RenderDownload)
	case "link":
		kind = KindLink
		render = append(render, RenderVisit)
	default:
		kind = KindText
		render = append(render, RenderOpen)
	}
	summary := strings.TrimSpace(a.Summary)
	if a.State == "unavailable" && summary == "" {
		summary = "No longer available"
	}
	return Item{
		ID:        "art:" + id,
		Kind:      kind,
		Title:     bound(title),
		Summary:   summary,
		State:     state(a.State),
		CreatedAt: a.CreatedAt,
		Render:    render,
	}, true
}

// state normalizes the artifact lifecycle for the owner.
func state(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "unavailable") {
		return "unavailable"
	}
	return "available"
}

func bound(s string) string {
	if len(s) <= maxTitle {
		return s
	}
	return strings.TrimSpace(s[:maxTitle-1]) + "\u2026"
}
