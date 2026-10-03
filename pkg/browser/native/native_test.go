package native

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

// hasBrowser reports whether a real Chrome is available for the e2e tests.
func hasBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("GHOST_NATIVE_BROWSER") != "1" {
		t.Skip("native browser e2e needs GHOST_NATIVE_BROWSER=1 (real Chrome)")
	}
	if _, err := chromeBinary(); err != nil {
		t.Skip("no Chrome/Chromium on PATH")
	}
}

func dataURL(html string) string {
	return "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
}

// pullRef returns the ref for the first snapshot line containing marker.
func pullRef(snapshot, marker string) string {
	for _, line := range strings.Split(snapshot, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		i := strings.Index(line, "[ref=")
		if i < 0 {
			continue
		}
		rest := line[i+len("[ref="):]
		if j := strings.IndexByte(rest, ']'); j >= 0 {
			return rest[:j]
		}
	}
	return ""
}

// The engine drives a real page end to end: navigate, read the accessibility
// tree into refs, fill a field, click a button, and observe the result in the
// page. This is the Go port of the upstream e2e coverage for the core actions.
func TestNativeNavigateFillClick(t *testing.T) {
	hasBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := Launch(ctx, Options{Headless: true})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer s.Close()

	html := `<html><body><input id="q" aria-label="Search"><button onclick="document.getElementById('out').textContent=document.getElementById('q').value">Go</button><div id="out"></div></body></html>`
	snap, err := s.Navigate(ctx, dataURL(html))
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	field := pullRef(snap, "textbox")
	button := pullRef(snap, "button")
	if field == "" || button == "" {
		t.Fatalf("snapshot must expose a textbox and a button:\n%s", snap)
	}
	if err := s.Fill(ctx, field, "hello native"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if err := s.Click(ctx, button); err != nil {
		t.Fatalf("click: %v", err)
	}
	// The click handler runs on the click; give it a beat, then read.
	time.Sleep(200 * time.Millisecond)
	page, err := s.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(page, "hello native") {
		t.Fatalf("the click did not carry the field value through: %q", page)
	}
}

// Durable refs: the same element keeps its ref across a re-snapshot within one
// document, so an agent can read once and act many times.
func TestNativeDurableRefs(t *testing.T) {
	hasBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Launch(ctx, Options{Headless: true})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer s.Close()
	html := `<html><body><button>Stable</button></body></html>`
	first, err := s.Navigate(ctx, dataURL(html))
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	second, err := s.Snapshot(ctx, snapshotOptions{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if a, b := pullRef(first, "button"), pullRef(second, "button"); a == "" || a != b {
		t.Fatalf("ref must be durable across snapshots: %q vs %q", a, b)
	}
}

// TestNativeSetValueFromScript proves the vault-fill hook: a value set through
// the native setter lands in the field and is observable, and the value need
// never be a command argument.
func TestNativeSetValueFromScript(t *testing.T) {
	hasBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Launch(ctx, Options{Headless: true})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer s.Close()
	html := `<html><body><input id="pw" type="password" aria-label="Password"></body></html>`
	snap, err := s.Navigate(ctx, dataURL(html))
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	ref := pullRef(snap, "textbox")
	if ref == "" {
		t.Fatalf("no textbox in snapshot:\n%s", snap)
	}
	if err := s.Focus(ctx, ref); err != nil {
		t.Fatalf("focus: %v", err)
	}
	script := `(() => { const el = document.activeElement; const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set; set.call(el, 's3cret'); el.dispatchEvent(new Event('input',{bubbles:true})); return el.value; })()`
	if err := s.SetValueFromScript(ctx, script); err != nil {
		t.Fatalf("set value: %v", err)
	}
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := s.eval(ctx, "document.getElementById('pw').value", &res); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if res.Result.Value != "s3cret" {
		t.Fatalf("value not set: %q", res.Result.Value)
	}
}
