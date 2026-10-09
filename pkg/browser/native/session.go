package native

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Options configures a native browser session.
type Options struct {
	// Binary is an explicit Chrome path; empty auto-discovers.
	Binary string
	// Headless runs without a display. The Pod runs headless; a desktop surface
	// may want a visible window.
	Headless bool
	// Profile is a persistent user-data-dir; empty gets a temp one that is
	// removed on Close.
	Profile string
	// CDPURL connects to an already-running browser instead of launching one.
	CDPURL string
	// Viewport, when set, sizes the window.
	Viewport [2]int
	// StartURL opens as the first tab.
	StartURL string
	// Env adds to the browser's environment (KEY=VALUE).
	Env []string
}

// Session is a live page: a Chrome process (or an attached target), a CDP
// connection, and the refs from the most recent snapshot. It is the Go
// equivalent of the upstream daemon's browser state, scoped to one page.
type Session struct {
	chrome    *chromeProcess
	cdp       *cdpClient
	refs      *refMap
	owned     bool // true when this session launched the browser
	pageWSURL string
}

// Launch starts a Chrome and attaches to its first page. Callers own the
// returned session and must Close it.
func Launch(ctx context.Context, opts Options) (*Session, error) {
	if opts.CDPURL != "" {
		return attach(ctx, opts.CDPURL)
	}
	proc, err := launchChrome(ctx, launchOptions{
		Binary:   opts.Binary,
		Headless: opts.Headless,
		Profile:  opts.Profile,
		Viewport: opts.Viewport,
		StartURL: opts.StartURL,
		Env:      opts.Env,
	})
	if err != nil {
		return nil, err
	}
	target, err := firstPageTarget(ctx, proc.port)
	if err != nil {
		proc.stop()
		return nil, err
	}
	s, err := attach(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		proc.stop()
		return nil, err
	}
	s.chrome = proc
	s.owned = true
	return s, nil
}

// attach dials a page websocket and enables the domains the engine uses.
func attach(ctx context.Context, wsURL string) (*Session, error) {
	cdp, err := dialCDP(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	s := &Session{cdp: cdp, refs: newRefMap(), pageWSURL: wsURL}
	for _, d := range []string{"Page.enable", "Runtime.enable", "DOM.enable", "Accessibility.enable"} {
		if err := cdp.send(ctx, d, nil, nil); err != nil {
			cdp.close()
			return nil, fmt.Errorf("enable %s: %w", d, err)
		}
	}
	return s, nil
}

// Close detaches and, when this session launched the browser, stops it.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.cdp != nil {
		s.cdp.close()
	}
	if s.owned && s.chrome != nil {
		s.chrome.stop()
	}
}

// Navigate opens a URL and waits for the load to settle, returning the fresh
// snapshot the agent needs next. A navigation that never settles still returns
// a snapshot of wherever the page got to rather than failing the whole call.
func (s *Session) Navigate(ctx context.Context, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !hasScheme(rawURL) {
		rawURL = "https://" + rawURL
	}
	loaded := s.cdp.on("Page.loadEventFired")
	if err := s.cdp.send(ctx, "Page.navigate", map[string]interface{}{"url": rawURL}, nil); err != nil {
		return "", err
	}
	// Wait for the load event, but do not hang on pages that keep loading
	// (SSE, websockets, polling): fall through to a snapshot after the timeout.
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	// A short settle for late client-side rendering.
	select {
	case <-time.After(400 * time.Millisecond):
	case <-ctx.Done():
	}
	return s.Snapshot(ctx, snapshotOptions{})
}

// Snapshot returns the accessibility outline with @eN refs.
func (s *Session) Snapshot(ctx context.Context, opts snapshotOptions) (string, error) {
	return s.takeSnapshot(ctx, opts)
}

// Outline is Snapshot for callers outside this package: the full
// accessibility tree with @eN refs, which is what a model reads and what
// the tool layer's ref ledger parses. Options are internal because the
// flags they mirror are a CLI detail; a snapshot to a model is always the
// whole tree.
func (s *Session) Outline(ctx context.Context) (string, error) {
	return s.takeSnapshot(ctx, snapshotOptions{})
}

// Supported lists the actions this engine implements, named in the tool
// layer's vocabulary so the two can be compared directly.
//
// It is a claim, not a capability probe: the golden set asserts that
// everything in here is actually driven end to end, and that the tool
// actions outside it are named too. An engine that silently answered for
// actions it could not perform would fail on a real site in the worst
// possible way — mid-flow, on somebody's account — so routing consults
// this list and refuses honestly rather than trying.
func Supported() []string {
	return []string{
		"navigate", "snapshot", "click", "fill", "type", "press", "wait", "screenshot",
	}
}

// URL returns the current page URL.
func (s *Session) URL(ctx context.Context) (string, error) {
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := s.eval(ctx, "location.href", &res); err != nil {
		return "", err
	}
	return res.Result.Value, nil
}

// Title returns the document title.
func (s *Session) Title(ctx context.Context) (string, error) {
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := s.eval(ctx, "document.title", &res); err != nil {
		return "", err
	}
	return res.Result.Value, nil
}

// Read returns the page's visible text, bounded.
func (s *Session) Read(ctx context.Context) (string, error) {
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := s.eval(ctx, "document.body ? document.body.innerText : ''", &res); err != nil {
		return "", err
	}
	text := strings.TrimSpace(res.Result.Value)
	if r := []rune(text); len(r) > 20000 {
		text = string(r[:20000]) + "…"
	}
	return text, nil
}

// resolveObject turns a ref into a Runtime object handle on the live node.
func (s *Session) resolveObject(ctx context.Context, ref string) (string, error) {
	ref = cleanRef(ref)
	e, ok := s.refs.get(ref)
	if !ok {
		return "", fmt.Errorf("unknown ref @%s — take a fresh snapshot", ref)
	}
	if !e.HasBackend {
		return "", fmt.Errorf("ref @%s has no DOM node to act on", ref)
	}
	var res struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := s.cdp.send(ctx, "DOM.resolveNode", map[string]interface{}{"backendNodeId": e.BackendNodeID}, &res); err != nil {
		return "", err
	}
	if res.Object.ObjectID == "" {
		return "", fmt.Errorf("ref @%s resolved to no node (stale — take a fresh snapshot)", ref)
	}
	return res.Object.ObjectID, nil
}

// scrollIntoView makes sure a node is on screen before it is clicked.
func (s *Session) scrollIntoView(ctx context.Context, ref string) {
	if e, ok := s.refs.get(ref); ok && e.HasBackend {
		_ = s.cdp.send(ctx, "DOM.scrollIntoViewIfNeeded", map[string]interface{}{"backendNodeId": e.BackendNodeID}, nil)
	}
}

// boxCenter returns the clickable center of a node in CSS pixels.
func (s *Session) boxCenter(ctx context.Context, ref string) (float64, float64, error) {
	ref = cleanRef(ref)
	e, ok := s.refs.get(ref)
	if !ok || !e.HasBackend {
		return 0, 0, fmt.Errorf("unknown ref @%s", ref)
	}
	var res struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := s.cdp.send(ctx, "DOM.getBoxModel", map[string]interface{}{"backendNodeId": e.BackendNodeID}, &res); err != nil {
		return 0, 0, fmt.Errorf("no box for @%s: %w", ref, err)
	}
	q := res.Model.Content
	if len(q) < 8 {
		return 0, 0, fmt.Errorf("no visible box for @%s", ref)
	}
	// Quad order is top-left, top-right, bottom-right, bottom-left.
	x := (q[0] + q[2] + q[4] + q[6]) / 4
	y := (q[1] + q[3] + q[5] + q[7]) / 4
	return x, y, nil
}

// Click acts on a ref: scroll it in, then dispatch a real mouse press/release
// at its center. Coordinates (not element.click()) mean the event path is the
// same one a person produces, which frameworks and hit-testing depend on.
func (s *Session) Click(ctx context.Context, ref string) error {
	if _, err := s.resolveObject(ctx, ref); err != nil {
		return err
	}
	s.scrollIntoView(ctx, ref)
	x, y, err := s.boxCenter(ctx, ref)
	if err != nil {
		return err
	}
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		if err := s.cdp.send(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
			"type": eventType, "x": x, "y": y, "button": "left", "clickCount": 1,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// Fill clears a field and sets it to text, the way a person retyping would.
func (s *Session) Fill(ctx context.Context, ref, text string) error {
	objectID, err := s.resolveObject(ctx, ref)
	if err != nil {
		return err
	}
	if _, err := s.callFunction(ctx, objectID, `function(){ this.focus(); if (this.select) this.select(); this.value=''; this.dispatchEvent(new Event('input',{bubbles:true})); }`); err != nil {
		return err
	}
	return s.insertText(ctx, text)
}

// Type focuses a field and inserts text without clearing what is there.
func (s *Session) Type(ctx context.Context, ref, text string, pressEnter bool) error {
	objectID, err := s.resolveObject(ctx, ref)
	if err != nil {
		return err
	}
	if _, err := s.callFunction(ctx, objectID, `function(){ this.focus(); }`); err != nil {
		return err
	}
	if err := s.insertText(ctx, text); err != nil {
		return err
	}
	if pressEnter {
		return s.Press(ctx, "Enter")
	}
	return nil
}

// SetValueFromScript sets a field's value through the native value setter, so
// React and other controlled inputs observe the change. It targets whatever
// element currently has focus. This is the hook the vault-fill path uses: a
// secret can be delivered as the script (on stdin) without ever becoming a
// command argument, a log line, or model context.
func (s *Session) SetValueFromScript(ctx context.Context, script string) error {
	return s.cdp.send(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": script, "returnByValue": true, "awaitPromise": false,
	}, nil)
}

// Focus focuses a ref without typing, so SetValueFromScript can target it.
func (s *Session) Focus(ctx context.Context, ref string) error {
	objectID, err := s.resolveObject(ctx, ref)
	if err != nil {
		return err
	}
	_, err = s.callFunction(ctx, objectID, `function(){ this.focus(); }`)
	return err
}

func (s *Session) callFunction(ctx context.Context, objectID, fn string) (string, error) {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	err := s.cdp.send(ctx, "Runtime.callFunctionOn", map[string]interface{}{
		"functionDeclaration": fn,
		"objectId":            objectID,
		"returnByValue":       true,
		"awaitPromise":        false,
	}, &res)
	return string(res.Result.Value), err
}

func (s *Session) insertText(ctx context.Context, text string) error {
	return s.cdp.send(ctx, "Input.insertText", map[string]interface{}{"text": text}, nil)
}

func (s *Session) eval(ctx context.Context, expr string, out interface{}) error {
	return s.cdp.send(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": expr, "returnByValue": true, "awaitPromise": false,
	}, out)
}

// keyDef is the CDP key description for one key name.
type keyDef struct {
	key, code string
	keyCode   int
	text      string
}

var keyTable = map[string]keyDef{
	"Enter":      {"Enter", "Enter", 13, "\r"},
	"Tab":        {"Tab", "Tab", 9, ""},
	"Escape":     {"Escape", "Escape", 27, ""},
	"Backspace":  {"Backspace", "Backspace", 8, ""},
	"Delete":     {"Delete", "Delete", 46, ""},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38, ""},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40, ""},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39, ""},
	"Home":       {"Home", "Home", 36, ""},
	"End":        {"End", "End", 35, ""},
	"PageUp":     {"PageUp", "PageUp", 33, ""},
	"PageDown":   {"PageDown", "PageDown", 34, ""},
	"Space":      {" ", "Space", 32, " "},
}

// Press dispatches one key down and up. An unknown single character types that
// character; an unknown named key is refused rather than guessed.
func (s *Session) Press(ctx context.Context, key string) error {
	def, ok := keyTable[key]
	if !ok {
		if len([]rune(key)) == 1 {
			def = keyDef{key: key, code: "", keyCode: 0, text: key}
		} else {
			return fmt.Errorf("unknown key %q", key)
		}
	}
	base := map[string]interface{}{
		"key": def.key, "code": def.code, "windowsVirtualKeyCode": def.keyCode,
		"nativeVirtualKeyCode": def.keyCode,
	}
	down := map[string]interface{}{"type": "keyDown"}
	for k, v := range base {
		down[k] = v
	}
	if def.text != "" {
		down["text"] = def.text
	}
	if err := s.cdp.send(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	up := map[string]interface{}{"type": "keyUp"}
	for k, v := range base {
		up[k] = v
	}
	return s.cdp.send(ctx, "Input.dispatchKeyEvent", up, nil)
}

// Wait waits for a condition: text, a URL substring, or a fixed time. It
// returns an honest timeout rather than hanging the turn.
func (s *Session) Wait(ctx context.Context, text, urlContains string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		if text != "" {
			page, _ := s.Read(ctx)
			if strings.Contains(page, text) {
				return nil
			}
		}
		if urlContains != "" {
			u, _ := s.URL(ctx)
			if strings.Contains(u, urlContains) {
				return nil
			}
		}
		if text == "" && urlContains == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waited %.0fs for %q and it did not appear", timeout.Seconds(), firstNonEmpty(text, urlContains))
		}
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Screenshot captures the page to a PNG file and returns its path.
func (s *Session) Screenshot(ctx context.Context, path string) (string, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := s.cdp.send(ctx, "Page.captureScreenshot", map[string]interface{}{"format": "png"}, &res); err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil {
		return "", err
	}
	if path == "" {
		f, err := os.CreateTemp("", "ghost-shot-*.png")
		if err != nil {
			return "", err
		}
		path = f.Name()
		_, werr := f.Write(data)
		_ = f.Close()
		return path, werr
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// hasScheme reports whether a URL already carries a scheme (http, data, file,
// about, ...), so the engine never rewrites "data:..." into "https://data:...".
func hasScheme(s string) bool {
	for i, r := range s {
		switch {
		case r == ':':
			return i > 0
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case i > 0 && ((r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return false
}

// looksLikeURL reports whether a string is a web address the engine can open.
func looksLikeURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}
