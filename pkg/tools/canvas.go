package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/artifacts"
)

// CanvasPublisher records a canvas as a handoff in the conversation, so it is
// there on every device and after a reload. *artifacts.Store satisfies it.
type CanvasPublisher interface {
	Publish(in artifacts.Input) (*artifacts.Artifact, error)
}

// CanvasTool lets Ghost show the owner something they can see and use: a page,
// a chart, a game, a calculator, a mock-up. It is one self-contained HTML
// document. Each call is saved as a numbered version of that canvas in the
// workspace and published as an artifact, so the app can run it in the chat and
// the owner can ask for changes and step back through earlier versions.
type CanvasTool struct {
	workspace string
	onPresent func(html string)
	mu        sync.Mutex
	pub       CanvasPublisher
}

// MaxCanvasBytes bounds one canvas. A phone has to load it inside a chat.
const MaxCanvasBytes = 256 * 1024

func NewCanvasTool(workspace string, onPresent func(html string), pub CanvasPublisher) *CanvasTool {
	return &CanvasTool{workspace: workspace, onPresent: onPresent, pub: pub}
}

// SetPublisher wires the artifact store once it exists.
func (t *CanvasTool) SetPublisher(p CanvasPublisher) {
	t.mu.Lock()
	t.pub = p
	t.mu.Unlock()
}

func (t *CanvasTool) Name() string {
	return "canvas"
}

func (t *CanvasTool) Description() string {
	return "Show the owner something they can see and use, running in their chat: a web page, " +
		"interactive tool, game, calculator, chart or dashboard, UI mock-up, diagram, animation. " +
		"Use it when they ask you to build, draw, prototype or visualize something, or to show what " +
		"code you wrote does. Give ONE complete, self-contained HTML document with inline CSS and JS. " +
		"It runs sandboxed on a phone: it cannot make network requests (fetch, XHR and WebSocket fail), " +
		"cannot use localStorage or cookies (keep state in memory), and links do not navigate. " +
		"Scripts and styles may be loaded only from cdnjs.cloudflare.com, cdn.jsdelivr.net, unpkg.com " +
		"or fonts.googleapis.com (Tailwind, Chart.js, D3, Three.js, React, Alpine all work from those). " +
		"Design for a phone: a viewport meta tag, touch targets of at least 44px, no hover-only controls. " +
		"In the chat it runs in a window as wide as the conversation (about 360px) at its own height, up " +
		"to about 520px; the owner taps Open to see all of it full screen. So the page IS the window: let " +
		"it use the full width, do not wrap everything in one outer card, and do not centre it in a " +
		"min-height of 100vh. The window already has a frame and a title bar, so do not repeat the title " +
		"as a big heading unless the page needs one. " +
		"The page already has a dark base, a clean font and these CSS variables, so use them instead of " +
		"inventing colours: --bg --surface --fg --muted --accent --ok --warn --bad --line --radius. " +
		"To remember what the owner does between opens (a score, where they are in a deck of " +
		"flashcards and when each card is due again, what they ticked), call ghost.save(value) with any " +
		"JSON value (up to 64 KB) and read it back as ghost.saved when the page loads (null the first " +
		"time). It is kept for this canvas across its versions; read it yourself with read_saved: true " +
		"and the title (no html), for instance to build the next version from where they got to. " +
		"To change a canvas, call canvas again with the WHOLE updated document and the same title: it " +
		"becomes the next version. Do not paste the HTML into your reply; say in a sentence or two what " +
		"you made and how to use it."
}

func (t *CanvasTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"html": map[string]interface{}{
				"type":        "string",
				"description": "The complete HTML document, with inline <style> and <script>. Under 256 KB.",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "A short name for what this is (\"Click counter\"). Reuse the same title for a new version of the same canvas.",
			},
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "Optional: one line on what changed in this version.",
			},
			"read_saved": map[string]interface{}{
				"type":        "boolean",
				"description": "Read what the canvas with this title saved with ghost.save, instead of showing a new version.",
			},
		},
		"required": []string{"title"},
	}
}

// canvasProblems names what in a document cannot work inside the canvas
// sandbox, so the model can fix it before the owner ever sees it fail.
var canvasProblems = []struct {
	re   *regexp.Regexp
	said string
}{
	{regexp.MustCompile(`\b(fetch\s*\(|XMLHttpRequest|new\s+WebSocket|EventSource\s*\()`), "it makes network requests, which the canvas blocks"},
	{regexp.MustCompile(`\b(localStorage|sessionStorage|indexedDB|document\.cookie)\b`), "it uses browser storage, which the canvas does not have (keep state in memory)"},
}

var allowedCanvasHosts = []string{"cdnjs.cloudflare.com", "cdn.jsdelivr.net", "unpkg.com", "fonts.googleapis.com", "fonts.gstatic.com"}

func canvasWarnings(html string) []string {
	var out []string
	for _, p := range canvasProblems {
		if p.re.MatchString(html) {
			out = append(out, p.said)
		}
	}
	// A script or stylesheet from anywhere else will not load.
	ext := regexp.MustCompile(`(?i)<(?:script|link)[^>]+(?:src|href)\s*=\s*["']\s*https?://([^/"']+)`)
	seen := map[string]bool{}
	for _, m := range ext.FindAllStringSubmatch(html, -1) {
		host := strings.ToLower(m[1])
		ok := false
		for _, a := range allowedCanvasHosts {
			if host == a {
				ok = true
			}
		}
		if !ok && !seen[host] {
			seen[host] = true
			out = append(out, "it loads "+host+", which is not an allowed source (use cdnjs.cloudflare.com, cdn.jsdelivr.net or unpkg.com)")
		}
	}
	return out
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// canvasSlug is the file-safe form of a title.
func canvasSlug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-"), "-")
	if s == "" {
		return "canvas"
	}
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// nextCanvasVersion is one more than the highest version saved for the slug.
func nextCanvasVersion(dir, slug string) int {
	matches, _ := filepath.Glob(filepath.Join(dir, slug+"-v*.html"))
	re := regexp.MustCompile(`-v(\d+)\.html$`)
	max := 0
	for _, m := range matches {
		if sub := re.FindStringSubmatch(m); sub != nil {
			var n int
			fmt.Sscanf(sub[1], "%d", &n)
			if n > max {
				max = n
			}
		}
	}
	return max + 1
}

func (t *CanvasTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	html, _ := args["html"].(string)
	title, _ := args["title"].(string)
	summary, _ := args["summary"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Canvas"
	}

	if read, _ := args["read_saved"].(bool); read {
		raw, err := ReadCanvasSaved(t.workspace, "canvas/"+canvasSlug(title)+"-v1.html")
		if err != nil || raw == nil {
			return &ToolResult{ForLLM: fmt.Sprintf("The canvas %q has saved nothing yet.", title), Silent: true}
		}
		return &ToolResult{ForLLM: fmt.Sprintf("What the canvas %q saved (JSON): %s", title, raw), Silent: true}
	}
	if strings.TrimSpace(html) == "" || !strings.Contains(html, "<") {
		return ErrorResult("html content is required: give a complete HTML document.")
	}
	if len(html) > MaxCanvasBytes {
		return ErrorResult(fmt.Sprintf("That canvas is %d KB; the limit is %d KB. Make it smaller (less inline data, simpler styles) and call canvas again.", len(html)/1024, MaxCanvasBytes/1024))
	}

	// The console still follows the latest canvas live.
	if t.onPresent != nil {
		t.onPresent(html)
	}

	slug := canvasSlug(title)
	dir := filepath.Join(t.workspace, "canvas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ErrorResult("Could not save the canvas: " + err.Error())
	}
	version := nextCanvasVersion(dir, slug)
	name := fmt.Sprintf("%s-v%d.html", slug, version)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(html), 0o644); err != nil {
		return ErrorResult("Could not save the canvas: " + err.Error())
	}
	// Kept for anything that still reads the single latest canvas.
	tmp := filepath.Join(t.workspace, "tmp", "canvas")
	if os.MkdirAll(tmp, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(tmp, "last_canvas.html"), []byte(html), 0o644)
	}

	t.mu.Lock()
	pub := t.pub
	t.mu.Unlock()
	if pub == nil {
		return &ToolResult{ForLLM: fmt.Sprintf("Canvas %q (v%d) is shown on the console only: the chat can't show it right now.", title, version), Silent: true}
	}
	if summary = strings.TrimSpace(summary); summary == "" {
		summary = fmt.Sprintf("Version %d", version)
	}
	a, err := pub.Publish(artifacts.Input{
		SessionKey: SessionKeyFromContext(ctx),
		Kind:       "file",
		Title:      title,
		Summary:    summary,
		Path:       "canvas/" + name,
	})
	if err != nil {
		return ErrorResult("The canvas was saved but could not be shown in the chat: " + err.Error())
	}
	msg := fmt.Sprintf("Showing %q (version %d) in the owner's chat as artifact %s (saved as canvas/%s). It runs there now. Do not paste the HTML. Say in a sentence or two what you made and how to use it; to change it, call canvas again with the full updated document and the same title.", title, version, a.ID, name)
	if warn := canvasWarnings(html); len(warn) > 0 {
		msg += " But note: " + strings.Join(warn, "; ") + ". It will not work as written. Fix it and call canvas again with the same title before telling the owner it is done."
	}
	return &ToolResult{ForLLM: msg, Silent: true}
}

// MaxCanvasSaved is the most a canvas may keep with ghost.save.
const MaxCanvasSaved = 64 << 10

var canvasVersioned = regexp.MustCompile(`^canvas/([a-z0-9-]+)-v\d+\.html$`)

// canvasSavedFile is where a canvas keeps what it saved: one file for all of
// its versions (canvas/<slug>.saved.json), so a new version picks up where
// the owner got to. "" for a path that is not a canvas.
func canvasSavedFile(workspace, path string) string {
	m := canvasVersioned.FindStringSubmatch(filepath.ToSlash(path))
	if m == nil {
		return ""
	}
	return filepath.Join(workspace, "canvas", m[1]+".saved.json")
}

// ReadCanvasSaved returns what the canvas at path saved, or nil if nothing.
func ReadCanvasSaved(workspace, path string) (json.RawMessage, error) {
	f := canvasSavedFile(workspace, path)
	if f == "" {
		return nil, errors.New("not a canvas")
	}
	b, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// WriteCanvasSaved keeps a JSON value for the canvas at path; null forgets it.
func WriteCanvasSaved(workspace, path string, value json.RawMessage) error {
	f := canvasSavedFile(workspace, path)
	if f == "" {
		return errors.New("not a canvas")
	}
	if len(value) > MaxCanvasSaved {
		return fmt.Errorf("a canvas can keep up to %d KB", MaxCanvasSaved>>10)
	}
	if !json.Valid(value) {
		return errors.New("not JSON")
	}
	if string(bytes.TrimSpace(value)) == "null" {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, value, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}
