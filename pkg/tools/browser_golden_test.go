package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/browser/native"
)

// The golden set.
//
// Ghost's browser is currently the agent-browser CLI; pkg/browser/native is
// Ghost's own Go engine speaking Chrome's protocol directly. "Supported" and
// "works on a real site" are different questions, and only the second one
// matters, so the golden set asks it the only way it can be asked: one page,
// one script, both engines, and the contract the tool layer actually depends
// on — a page its parsers can read, refs its ledger can validate, and acts
// that change what the page says.
//
// The core loop is gated on real browsers being present
// (GHOST_NATIVE_BROWSER=1 and agent-browser on PATH). The readiness report
// below it always runs: how far the native engine is from being able to take
// over is a fact about the code, not about the machine.

// goldenPage is one engine's view of the page: the same fields the wire
// format carries, which the tool layer reads through browserPageViewOf and
// ParseRefs.
type goldenPage struct {
	URL     string
	Title   string
	Outline string
}

// goldenEngine is the contract both engines are held to. It is the shape
// the tool layer needs, not the shape either implementation happens to
// have.
type goldenEngine interface {
	Name() string
	Navigate(ctx context.Context, url string) (goldenPage, error)
	Snapshot(ctx context.Context) (goldenPage, error)
	Click(ctx context.Context, ref string) error
	Fill(ctx context.Context, ref, text string) error
	Type(ctx context.Context, ref, text string) error
	Press(ctx context.Context, key string) error
	Wait(ctx context.Context, text string) error
	Screenshot(ctx context.Context, path string) error
	Close()
}

func goldenGate(t *testing.T) {
	t.Helper()
	if os.Getenv("GHOST_NATIVE_BROWSER") != "1" {
		t.Skip("golden set needs GHOST_NATIVE_BROWSER=1 (real Chrome + agent-browser)")
	}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Skip("golden set needs agent-browser on PATH")
	}
}

// goldenWorld is a two-step sign-in whose first step is removed from the
// DOM when it is completed. That is the condition this set exists to
// exercise: a page that re-renders under the model, invalidating every
// ref it was handed, in the middle of a multi-step flow.
func goldenWorld(t *testing.T) *httptest.Server {
	t.Helper()
	page := `<!doctype html><html><head><title>Golden Set — sign in</title></head><body>
<h1>Sign in</h1>
<div id="step1">
  <label for="user">Username</label>
  <input id="user" aria-label="Username">
  <button id="next" onclick="showStep2()">Next</button>
</div>
<div id="step2" style="display:none">
  <label for="pass">Password</label>
  <input id="pass" type="password" aria-label="Password"
         onkeydown="if(event.key==='Enter')welcome()">
  <button id="signin" onclick="welcome()">Sign in</button>
</div>
<div id="out"></div>
<script>
var held = '';
function showStep2() {
  held = document.getElementById('user').value;
  document.getElementById('step1').remove();
  document.getElementById('step2').style.display = 'block';
}
function welcome() {
  document.getElementById('out').textContent = 'Welcome back, ' + held;
}
</script>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- the CLI engine -------------------------------------------------------

type cliEngine struct {
	t    *testing.T
	done bool
}

func (e *cliEngine) Name() string { return "cli" }

func (e *cliEngine) Close() {
	if e.done {
		return
	}
	e.done = true
	e.run("close")
}

func (e *cliEngine) run(args ...string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agent-browser", append(args, "--json")...)
	cmd.Env = browserEnvironment("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("agent-browser %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	var env struct {
		Data map[string]interface{} `json:"data"`
		Err  interface{}            `json:"error"`
	}
	if json.Unmarshal(out, &env) != nil {
		return nil, fmt.Errorf("agent-browser %s: unparseable output %q", args[0], boundedBrowserText(string(out)))
	}
	if env.Err != nil {
		return nil, fmt.Errorf("agent-browser %s: %v", args[0], env.Err)
	}
	return env.Data, nil
}

func (e *cliEngine) Navigate(ctx context.Context, url string) (goldenPage, error) {
	nav, err := e.run("navigate", url)
	if err != nil {
		return goldenPage{}, err
	}
	// navigate carries the page identity and snapshot carries the tree —
	// the tool layer merges the two, so the golden set must too.
	page, err := e.Snapshot(ctx)
	if err != nil {
		return goldenPage{}, err
	}
	if page.Title == "" {
		if s, _ := nav["title"].(string); s != "" {
			page.Title = s
		}
	}
	if page.URL == "" {
		if s, _ := nav["url"].(string); s != "" {
			page.URL = s
		}
	}
	return page, nil
}

func (e *cliEngine) Snapshot(ctx context.Context) (goldenPage, error) {
	data, err := e.run("snapshot")
	if err != nil {
		return goldenPage{}, err
	}
	str := func(k string) string { s, _ := data[k].(string); return s }
	p := goldenPage{URL: str("origin"), Title: str("title"), Outline: str("snapshot")}
	if p.URL == "" {
		p.URL = str("url")
	}
	if p.Outline == "" {
		p.Outline = str("text")
	}
	return p, nil
}

func (e *cliEngine) act(args ...string) error {
	_, err := e.run(args...)
	return err
}

func (e *cliEngine) Click(ctx context.Context, ref string) error { return e.act("click", ref) }
func (e *cliEngine) Fill(ctx context.Context, ref, text string) error {
	return e.act("fill", ref, text)
}
func (e *cliEngine) Type(ctx context.Context, ref, text string) error {
	return e.act("type", ref, text)
}
func (e *cliEngine) Press(ctx context.Context, key string) error { return e.act("press", key) }
func (e *cliEngine) Wait(ctx context.Context, text string) error {
	return e.act("wait", "--text", text)
}
func (e *cliEngine) Screenshot(ctx context.Context, path string) error {
	return e.act("screenshot", path)
}

// --- the native engine ----------------------------------------------------

type nativeEngine struct {
	s *native.Session
	// cancel releases the context Chrome was launched under: the process
	// is bound to it, so dropping it early would kill the browser the
	// engine is about to drive.
	cancel context.CancelFunc
	closed bool
}

func (e *nativeEngine) Name() string { return "native" }

func (e *nativeEngine) Close() {
	if e.closed {
		return
	}
	e.closed = true
	e.s.Close()
	if e.cancel != nil {
		e.cancel()
	}
}

func (e *nativeEngine) Navigate(ctx context.Context, url string) (goldenPage, error) {
	if _, err := e.s.Navigate(ctx, url); err != nil {
		return goldenPage{}, err
	}
	return e.Snapshot(ctx)
}

func (e *nativeEngine) Snapshot(ctx context.Context) (goldenPage, error) {
	outline, err := e.s.Outline(ctx)
	if err != nil {
		return goldenPage{}, err
	}
	u, _ := e.s.URL(ctx)
	title, _ := e.s.Title(ctx)
	return goldenPage{URL: u, Title: title, Outline: outline}, nil
}

func (e *nativeEngine) Click(ctx context.Context, ref string) error { return e.s.Click(ctx, ref) }
func (e *nativeEngine) Fill(ctx context.Context, ref, text string) error {
	return e.s.Fill(ctx, ref, text)
}
func (e *nativeEngine) Type(ctx context.Context, ref, text string) error {
	return e.s.Type(ctx, ref, text, false)
}
func (e *nativeEngine) Press(ctx context.Context, key string) error { return e.s.Press(ctx, key) }
func (e *nativeEngine) Wait(ctx context.Context, text string) error {
	return e.s.Wait(ctx, text, "", 10*time.Second)
}
func (e *nativeEngine) Screenshot(ctx context.Context, path string) error {
	_, err := e.s.Screenshot(ctx, path)
	return err
}

func goldenEngines(t *testing.T) []goldenEngine {
	t.Helper()
	goldenGate(t)
	engines := []goldenEngine{&cliEngine{t: t}}
	// Chrome is bound to the context it was launched under; this one
	// outlives the helper and is released by Close. No shared profile
	// dir: this set exercises the action contract, not persistence, and
	// a profile tied to t.TempDir() races its own removal when the
	// browser's children outlive it by a beat.
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := native.Launch(ctx, native.Options{Headless: true})
	if err != nil {
		cancel()
		t.Fatalf("native launch: %v", err)
	}
	engines = append(engines, &nativeEngine{s: sess, cancel: cancel})
	for _, e := range engines {
		t.Cleanup(e.Close)
	}
	return engines
}

// --- the set --------------------------------------------------------------

// refFor returns the ref of the snapshot line containing marker, or fails
// the step: a model would read the same line and reach the same ref.
func goldenRef(t *testing.T, page goldenPage, marker string) string {
	t.Helper()
	for _, line := range strings.Split(page.Outline, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		if i := strings.Index(line, "ref="); i >= 0 {
			rest := line[i+len("ref="):]
			if j := strings.IndexByte(rest, ']'); j >= 0 {
				return "@" + rest[:j]
			}
		}
	}
	t.Fatalf("%s: no ref on a line containing %q in:\n%s", marker, marker, page.Outline)
	return ""
}

// TestGoldenSetCoreLoop drives both engines through the same multi-step
// flow and holds each to the contract the tools rely on: a page its parsers
// can read, refs its ledger can validate, and acts that change what the
// page says — including a layout change in the middle, which is what real
// sites do.
func TestGoldenSetCoreLoop(t *testing.T) {
	world := goldenWorld(t)
	for _, eng := range goldenEngines(t) {
		e := eng
		t.Run(e.Name(), func(t *testing.T) {
			defer e.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			// 1. Navigate: the page must arrive as something the tool
			//    layer's own parsers can read, not just as text.
			page, err := e.Navigate(ctx, world.URL)
			if err != nil {
				t.Fatalf("navigate: %v", err)
			}
			if !strings.Contains(page.URL, world.URL) {
				t.Fatalf("navigate landed on %q, want %q", page.URL, world.URL)
			}
			if !strings.Contains(page.Title, "Golden Set") {
				t.Fatalf("title = %q, want the page's", page.Title)
			}
			raw, _ := json.Marshal(map[string]string{
				"url": page.URL, "title": page.Title, "snapshot": page.Outline,
			})
			view, ok := browserPageViewOf(string(raw))
			if !ok || view.Text == "" {
				t.Fatalf("browserPageViewOf cannot read this engine's page: ok=%v %+v", ok, view)
			}
			if refs := browser.ParseRefs(string(raw)); len(refs) < 2 {
				t.Fatalf("ParseRefs found %d refs, want at least 2: %v", len(refs), refs)
			}

			// 2. Snapshot: the username field is addressable.
			page, err = e.Snapshot(ctx)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			user := goldenRef(t, page, "Username")

			// 3. Fill and click: the first step completes.
			if err := e.Fill(ctx, user, "ian"); err != nil {
				t.Fatalf("fill username: %v", err)
			}
			if err := e.Click(ctx, goldenRef(t, page, "Next")); err != nil {
				t.Fatalf("click next: %v", err)
			}

			// 4. The layout changed under us: the field the model was
			//    working from is gone from the DOM.
			if err := e.Wait(ctx, "Password"); err != nil {
				t.Fatalf("wait for step two: %v", err)
			}
			page, err = e.Snapshot(ctx)
			if err != nil {
				t.Fatalf("snapshot after the re-render: %v", err)
			}
			if strings.Contains(page.Outline, "Username") {
				t.Fatal("step one should be gone from the DOM; the layout did not change")
			}
			pass := goldenRef(t, page, "Password")

			// 5. Type and press: the second step completes and the page
			//    reports what the flow actually accomplished.
			if err := e.Type(ctx, pass, "hunter2"); err != nil {
				t.Fatalf("type password: %v", err)
			}
			if err := e.Press(ctx, "Enter"); err != nil {
				t.Fatalf("press enter: %v", err)
			}
			if err := e.Wait(ctx, "Welcome back, ian"); err != nil {
				t.Fatalf("the flow did not complete: %v", err)
			}

			// 6. Evidence: a screenshot the owner can be shown.
			path := filepath.Join(t.TempDir(), e.Name()+".png")
			if err := e.Screenshot(ctx, path); err != nil {
				t.Fatalf("screenshot: %v", err)
			}
			if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
				t.Fatalf("screenshot missing or empty: %v", err)
			}
		})
	}
}

// --- readiness ------------------------------------------------------------

// nativeEngineActions is what pkg/browser/native can be asked to do, named
// in the tool layer's vocabulary. Adding an engine action without adding it
// here (and to native.Supported) fails the report below, so the list can
// never drift from the engine it describes.
var nativeEngineActions = []string{
	"navigate", "snapshot", "click", "fill", "type", "press", "wait", "screenshot",
}

// toolEngineActions is every action executeBare can hand to the browser
// engine. It is the surface an engine must cover before it can replace the
// CLI rather than sit behind a switch.
var toolEngineActions = []string{
	"navigate", "snapshot", "click", "fill", "type", "press", "wait",
	"find", "screenshot", "scroll", "console", "network", "a11y",
	"select", "check", "hover", "drag", "dialog", "upload", "download",
}

// TestGoldenSetReadiness is the honest answer to "can the native engine
// drive the browser yet". It fails when either list changes without the
// other being reconsidered, so the gap is a decision nobody can quietly
// widen — and it prints what is missing, because the point of the golden
// set is to say out loud how far there is to go.
func TestGoldenSetReadiness(t *testing.T) {
	fromEngine := native.Supported()
	if len(fromEngine) != len(nativeEngineActions) {
		t.Fatalf("pkg/browser/native.Supported() reports %v; nativeEngineActions says %v.\n"+
			"Update both, deliberately: this is the engine's claim about itself.",
			fromEngine, nativeEngineActions)
	}
	have := map[string]bool{}
	for _, a := range nativeEngineActions {
		have[a] = true
	}
	for _, a := range fromEngine {
		if !have[a] {
			t.Fatalf("native.Supported() claims %q, which nativeEngineActions does not: %v", a, nativeEngineActions)
		}
	}

	var missing []string
	for _, a := range toolEngineActions {
		if !have[a] {
			missing = append(missing, a)
		}
	}
	t.Logf("native engine covers %d/%d tool actions", len(nativeEngineActions), len(toolEngineActions))
	if len(missing) > 0 {
		t.Logf("NOT YET IMPLEMENTED (%d): %s", len(missing), strings.Join(missing, ", "))
	}
	if len(missing) > 0 && len(missing) == len(toolEngineActions) {
		t.Fatal("the native engine covers none of the tool surface")
	}
}
