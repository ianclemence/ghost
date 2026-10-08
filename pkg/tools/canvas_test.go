package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/db"
)

func canvasWith(t *testing.T) (*CanvasTool, *artifacts.Store, string, *[]string) {
	t.Helper()
	ws := t.TempDir()
	database, err := db.NewDB(ws)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	store, err := artifacts.NewStore(database.DB, ws)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	var shown []string
	return NewCanvasTool(ws, func(h string) { shown = append(shown, h) }, store), store, ws, &shown
}

func runCanvas(t *testing.T, c *CanvasTool, args map[string]interface{}) *ToolResult {
	t.Helper()
	return c.Execute(WithSessionKey(context.Background(), "main"), args)
}

// Each call is a numbered version of that canvas, saved in the workspace and
// published so it is in the conversation on every device.
func TestCanvasSavesVersionsAndPublishesThem(t *testing.T) {
	c, store, ws, shown := canvasWith(t)
	page := "<!doctype html><html><body><button>Click me</button></body></html>"

	r1 := runCanvas(t, c, map[string]interface{}{"html": page, "title": "Click counter"})
	if r1.IsError {
		t.Fatalf("first: %s", r1.ForLLM)
	}
	r2 := runCanvas(t, c, map[string]interface{}{"html": page + "<!--v2-->", "title": "Click counter", "summary": "Bigger button"})
	if r2.IsError {
		t.Fatalf("second: %s", r2.ForLLM)
	}
	for _, f := range []string{"click-counter-v1.html", "click-counter-v2.html"} {
		if _, err := os.Stat(filepath.Join(ws, "canvas", f)); err != nil {
			t.Fatalf("%s not saved: %v", f, err)
		}
	}
	if !strings.Contains(r2.ForLLM, "version 2") || !strings.Contains(r2.ForLLM, "Do not paste the HTML") {
		t.Fatalf("the model is told what happened and what not to do: %q", r2.ForLLM)
	}
	list, err := store.List("main", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("published %d artifacts, want 2", len(list))
	}
	for _, a := range list {
		if a.Kind != "file" || a.Title != "Click counter" || !strings.HasPrefix(a.Path, "canvas/click-counter-v") {
			t.Fatalf("artifact: %+v", a)
		}
	}
	if len(*shown) != 2 {
		t.Fatal("the console is shown each canvas as well")
	}
}

func TestDifferentTitlesAreDifferentCanvases(t *testing.T) {
	c, _, ws, _ := canvasWith(t)
	runCanvas(t, c, map[string]interface{}{"html": "<p>a</p>", "title": "Todo app"})
	runCanvas(t, c, map[string]interface{}{"html": "<p>b</p>", "title": "Pong"})
	for _, f := range []string{"todo-app-v1.html", "pong-v1.html"} {
		if _, err := os.Stat(filepath.Join(ws, "canvas", f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}

func TestCanvasRefusesWhatCannotRunInAChat(t *testing.T) {
	c, store, _, _ := canvasWith(t)
	if r := runCanvas(t, c, map[string]interface{}{"html": "   ", "title": "x"}); !r.IsError {
		t.Fatal("an empty canvas must be refused")
	}
	if r := runCanvas(t, c, map[string]interface{}{"html": "just words", "title": "x"}); !r.IsError {
		t.Fatal("plain text is not a document")
	}
	big := "<p>" + strings.Repeat("x", MaxCanvasBytes) + "</p>"
	r := runCanvas(t, c, map[string]interface{}{"html": big, "title": "Huge"})
	if !r.IsError || !strings.Contains(r.ForLLM, "limit") {
		t.Fatalf("an oversized canvas must say so: %+v", r)
	}
	if list, _ := store.List("main", 10); len(list) != 0 {
		t.Fatal("a refused canvas must publish nothing")
	}
}

func TestCanvasWithoutAStoreStillShowsOnTheConsole(t *testing.T) {
	ws := t.TempDir()
	var shown int
	c := NewCanvasTool(ws, func(string) { shown++ }, nil)
	r := runCanvas(t, c, map[string]interface{}{"html": "<p>hi</p>", "title": "T"})
	if r.IsError || shown != 1 || !strings.Contains(r.ForLLM, "console only") {
		t.Fatalf("shown=%d result=%+v", shown, r)
	}
}

func TestCanvasSlugsAreFileSafe(t *testing.T) {
	for in, want := range map[string]string{
		"Click counter":         "click-counter",
		"  ../../etc/passwd  ":  "etc-passwd",
		"日本語":                   "canvas",
		"":                      "canvas",
		strings.Repeat("a", 80): strings.Repeat("a", 40),
	} {
		if got := canvasSlug(in); got != want {
			t.Errorf("canvasSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// The ways people ask for something to be built and shown all reach the tool;
// ordinary talk does not carry its schema.
func TestCanvasIsOfferedForBuildRequestsOnly(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(NewCanvasTool(t.TempDir(), nil, nil))
	has := func(msg string) bool {
		_, ok := FilterToolsForTurn(reg, ProfileMobileSafe, msg, false).Get("canvas")
		return ok
	}
	for _, msg := range []string{
		"Write a piece of html code", "make me a pong game", "build me a calculator", "prototype a login screen",
		"visualize my spending as a chart", "show me a landing page for my bar", "can you draw a diagram of this",
		"create a dashboard for the pod",
	} {
		if !has(msg) {
			t.Errorf("%q should offer canvas", msg)
		}
	}
	for _, msg := range []string{"hello", "what's the weather in nairobi", "remind me at 9", "run uname -a", "summarise my week"} {
		if has(msg) {
			t.Errorf("%q should not offer canvas", msg)
		}
	}
}

func TestMobileProfileCanShowACanvas(t *testing.T) {
	if !ProfileMobileSafe.Allows("canvas") {
		t.Fatal("the phone's profile must allow the canvas tool: it could never be used otherwise")
	}
}

// What the sandbox blocks is named to the model while it can still fix it.
func TestCanvasWarnsAboutWhatTheSandboxBlocks(t *testing.T) {
	c, _, _, _ := canvasWith(t)
	bad := `<html><head><script src="https://evil.example/x.js"></script></head><body><script>
fetch('/api'); localStorage.setItem('a','b');</script></body></html>`
	r := runCanvas(t, c, map[string]interface{}{"html": bad, "title": "Bad"})
	if r.IsError {
		t.Fatalf("a flawed canvas is still shown: %s", r.ForLLM)
	}
	for _, want := range []string{"network requests", "browser storage", "evil.example"} {
		if !strings.Contains(r.ForLLM, want) {
			t.Errorf("warning missing %q: %s", want, r.ForLLM)
		}
	}
	good := `<html><head><script src="https://cdn.jsdelivr.net/npm/chart.js"></script></head><body></body></html>`
	r = runCanvas(t, c, map[string]interface{}{"html": good, "title": "Good"})
	if strings.Contains(r.ForLLM, "But note") {
		t.Fatalf("an allowed library must not be flagged: %s", r.ForLLM)
	}
}
