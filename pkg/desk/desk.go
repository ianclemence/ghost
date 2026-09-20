// Package desk is Ghost's single product-facing model for "the work Ghost
// has done on your machine".
//
// It is deliberately NOT a new storage layer, not a VM, and not an execution
// surface. The Pod is the computer; the Desk is the honest name for the
// workspace Ghost already uses. This package is a READ-ONLY projection over
// authorities that already exist:
//
//	workspace filesystem  → documents
//	pkg/artifacts         → artifacts
//	skills (workspace)    → tools Ghost built/installed
//	pkg/live              → live surfaces Ghost acted on
//
// It owns no tables, no execution, no authority, and no credentials. Acting
// on a Desk item is a new governed capability through the Permission Broker —
// the Desk grants nothing.
//
// The estate boundary is inherited unchanged: anything the runtime protects
// (personal-context/, state/, events/, knowledge/self/, databases) is never a
// Desk item. Ghost's internal memory is not the owner's document shelf.
//
// See docs/DESK.md for the canonical architecture.
package desk

import (
	"path"
	"sort"
	"strings"
	"time"
)

// Kind is the shape of a Desk item. Exactly four exist.
type Kind string

const (
	KindDocument Kind = "document"
	KindArtifact Kind = "artifact"
	KindTool     Kind = "tool"
	KindSurface  Kind = "surface"
)

// Source is where an item came from. Provenance, not navigation.
type Source string

const (
	SourceWorkspace Source = "workspace"
	SourceArtifact  Source = "artifact"
	SourceSkill     Source = "skill"
	SourceLive      Source = "live"
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

// Item is one durable thing on the owner's Desk. Every field is derived from
// a backing authority; nothing here is a claim the runtime has not validated.
type Item struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary,omitempty"`
	Source    Source    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Size      int64     `json:"size,omitempty"` // documents only
	Render    []Render  `json:"render"`
	// Protected is always false for a surfaced item. Protected paths are
	// filtered before an item is constructed; the field exists so a future
	// caller cannot silently surface one.
	Protected bool `json:"protected"`
}

// DocumentInput is one workspace file the runtime has already vetted.
// The caller (the gateway) is responsible for estate filtering; the Desk
// trusts the input and re-checks the path defensively.
type DocumentInput struct {
	RelPath   string
	Size      int64
	UpdatedAt time.Time
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

// ToolInput is one skill present in the workspace skills directory.
type ToolInput struct {
	Name        string
	Description string
	// Built marks a skill Ghost itself created (workspace-local), as
	// opposed to a bundled or globally-installed one. The Desk presents
	// "tools Ghost built" distinctly.
	Built     bool
	UpdatedAt time.Time
}

// SurfaceInput is one live browser/computer surface the runtime acted on.
type SurfaceInput struct {
	ID        string
	Kind      string // "browser" | "computer"
	State     string
	Title     string
	URL       string
	UpdatedAt time.Time
}

// Inputs is everything the projection reads. Every field is optional; a
// missing authority degrades to an empty contribution rather than a failure,
// so a partially-available Ghost still shows what it honestly can.
type Inputs struct {
	Documents []DocumentInput
	Artifacts []ArtifactInput
	Tools     []ToolInput
	Surfaces  []SurfaceInput
}

// maxTitle bounds a rendered title so a filename or model string can never
// dominate the surface.
const maxTitle = 200

// List merges every authority into one normalized, sorted Desk. Ordering is
// deterministic: most recently updated first, then by kind, then by id, so
// the surface is stable across calls.
func List(in Inputs) []Item {
	out := make([]Item, 0, len(in.Documents)+len(in.Artifacts)+len(in.Tools)+len(in.Surfaces))

	for _, d := range in.Documents {
		if item, ok := documentItem(d); ok {
			out = append(out, item)
		}
	}
	for _, a := range in.Artifacts {
		if item, ok := artifactItem(a); ok {
			out = append(out, item)
		}
	}
	for _, t := range in.Tools {
		if item, ok := toolItem(t); ok {
			out = append(out, item)
		}
	}
	for _, s := range in.Surfaces {
		if item, ok := surfaceItem(s); ok {
			out = append(out, item)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

func less(a, b Item) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

// documentItem builds a document item from a vetted workspace path. It
// re-checks the estate boundary defensively: a protected path is never
// surfaced, even if the caller forgot to filter it.
func documentItem(d DocumentInput) (Item, bool) {
	rel := cleanRel(d.RelPath)
	if rel == "" || protectedPath(rel) {
		return Item{}, false
	}
	title := path.Base(rel)
	if title == "" || title == "." || title == "/" {
		return Item{}, false
	}
	return Item{
		ID:        "doc:" + rel,
		Kind:      KindDocument,
		Title:     bound(title),
		Summary:   rel,
		Source:    SourceWorkspace,
		CreatedAt: d.UpdatedAt,
		UpdatedAt: d.UpdatedAt,
		Size:      d.Size,
		Render:    []Render{RenderPreview, RenderOpen, RenderDownload},
	}, true
}

// artifactItem builds an artifact item from a runtime-validated artifact.
// Unavailable artifacts are still shown (honestly marked) because their
// historical existence is real; their summary carries the reason.
func artifactItem(a ArtifactInput) (Item, bool) {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Title) == "" {
		return Item{}, false
	}
	render := []Render{RenderPreview}
	switch a.Kind {
	case "link":
		render = append(render, RenderVisit)
	case "file":
		render = append(render, RenderOpen, RenderDownload)
	case "text":
		render = append(render, RenderOpen)
	}
	summary := strings.TrimSpace(a.Summary)
	if a.State == "unavailable" && summary == "" {
		summary = "No longer available"
	}
	return Item{
		ID:        "art:" + a.ID,
		Kind:      KindArtifact,
		Title:     bound(a.Title),
		Summary:   summary,
		Source:    SourceArtifact,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.CreatedAt,
		Render:    render,
	}, true
}

// toolItem builds a tool item from a workspace skill. Only skills that Ghost
// itself authored or installed into the workspace are "tools" on the Desk;
// bundled skills are Ghost's built-in abilities, not the owner's artifacts.
func toolItem(t ToolInput) (Item, bool) {
	name := strings.TrimSpace(t.Name)
	if name == "" || !t.Built {
		return Item{}, false
	}
	summary := strings.TrimSpace(t.Description)
	if summary == "" {
		summary = "A tool Ghost built"
	}
	return Item{
		ID:        "tool:" + name,
		Kind:      KindTool,
		Title:     bound(name),
		Summary:   bound(summary),
		Source:    SourceSkill,
		CreatedAt: t.UpdatedAt,
		UpdatedAt: t.UpdatedAt,
		Render:    []Render{RenderPreview},
	}, true
}

// surfaceItem builds a live-surface item. It carries only what a client may
// safely render (title, url/domain); never credentials or internals.
func surfaceItem(s SurfaceInput) (Item, bool) {
	id := strings.TrimSpace(s.ID)
	kind := strings.TrimSpace(s.Kind)
	if id == "" || (kind != "browser" && kind != "computer") {
		return Item{}, false
	}
	title := strings.TrimSpace(s.Title)
	if title == "" {
		if kind == "browser" {
			title = "Web session"
		} else {
			title = "Computer session"
		}
	}
	summary := strings.TrimSpace(s.URL)
	render := []Render{RenderPreview}
	if kind == "browser" {
		render = append(render, RenderVisit)
	}
	return Item{
		ID:        "surf:" + kind + ":" + id,
		Kind:      KindSurface,
		Title:     bound(title),
		Summary:   bound(summary),
		Source:    SourceLive,
		CreatedAt: s.UpdatedAt,
		UpdatedAt: s.UpdatedAt,
		Render:    render,
	}, true
}

// protectedPath mirrors the gateway's workspace estate boundary
// (workspaceFileProtected) plus the artifacts protected prefixes. Kept here so
// the projection is safe even when called directly in a test or a future
// surface.
//
// memory/ is included on purpose: the daily journal is Ghost's internal
// memory and already has its own Memory surface. The Desk is the curated
// shelf of Ghost's WORK, not a dump of its recall.
func protectedPath(rel string) bool {
	r := cleanRel(rel)
	lower := strings.ToLower(r)
	for _, p := range []string{"personal-context", "state", "events", "knowledge/self", "memory", "journal", "tmp"} {
		if r == p || strings.HasPrefix(r, p+"/") {
			return true
		}
	}
	for _, ext := range []string{".db", ".db-wal", ".db-shm", ".sqlite", ".sqlite3"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// cleanRel normalizes a workspace-relative path: forward slashes, no leading
// "./", no traversal, no absolute paths.
func cleanRel(rel string) string {
	r := strings.TrimSpace(rel)
	r = strings.ReplaceAll(r, "\\", "/")
	r = strings.TrimPrefix(r, "./")
	if r == "" || strings.HasPrefix(r, "/") {
		return ""
	}
	r = path.Clean(r)
	if r == "." || strings.HasPrefix(r, "../") || r == ".." {
		return ""
	}
	return r
}

func bound(s string) string {
	if len(s) <= maxTitle {
		return s
	}
	return strings.TrimSpace(s[:maxTitle-1]) + "\u2026"
}
