// Package browser makes web browsing a controlled capability of the
// personal AI runtime. One Ghost drives the browser through sessions
// scoped to owner+context+task; page content is always untrusted data,
// never instructions; credentials never enter prompts, logs, or events.
package browser

import (
	"context"
	"fmt"
)

// Element is one interactive node of a page's semantic tree. Refs are
// driver-assigned handles stable for the life of one snapshot —
// coordinates are never exposed to the model.
type Element struct {
	Ref   string
	Role  string
	Name  string
	Value string
}

// Page is what the runtime sees after navigation or inspection: a
// semantic tree plus text, never raw DOM, scripts, or cookies.
type Page struct {
	URL      string
	Title    string
	Text     string
	Elements []Element
}

// Driver executes browser operations. Production drivers shell out to a
// browser backend; tests use FakeDriver. Every method must fail closed:
// unknown sessions, unknown refs, and unreachable pages are errors,
// never silent success.
type Driver interface {
	Navigate(ctx context.Context, sessionID, url string) (Page, error)
	Snapshot(ctx context.Context, sessionID string) (Page, error)
	Click(ctx context.Context, sessionID, ref string) error
	Type(ctx context.Context, sessionID, ref, text string, enter bool) error
	Press(ctx context.Context, sessionID, key string) error
	Screenshot(ctx context.Context, sessionID string) ([]byte, error)
	Close(sessionID string) error
}

// FakeDriver is a scripted in-memory browser for deterministic scenario
// tests. No network, no processes: pages are declared up front, and an
// optional hook inspects every operation (used to assert, e.g., that a
// purchase never executes without approval).
type FakeDriver struct {
	Pages    map[string]Page
	OnOp     func(op, sessionID, arg string)
	Closed   []string
	current  map[string]string
}

// NewFakeDriver creates a driver serving the given URL→page map.
func NewFakeDriver(pages map[string]Page) *FakeDriver {
	return &FakeDriver{Pages: pages, current: map[string]string{}}
}

func (f *FakeDriver) note(op, sessionID, arg string) {
	if f.OnOp != nil {
		f.OnOp(op, sessionID, arg)
	}
}

// Navigate loads a scripted page. Unknown URLs fail like an unreachable
// site would — the runtime must handle that honestly.
func (f *FakeDriver) Navigate(ctx context.Context, sessionID, url string) (Page, error) {
	f.note("navigate", sessionID, url)
	p, ok := f.Pages[url]
	if !ok {
		return Page{}, fmt.Errorf("browser: cannot reach %s", url)
	}
	p.URL = url
	f.current[sessionID] = url
	return p, nil
}

// Snapshot returns the current page's semantic tree.
func (f *FakeDriver) Snapshot(ctx context.Context, sessionID string) (Page, error) {
	f.note("snapshot", sessionID, "")
	url, ok := f.current[sessionID]
	if !ok {
		return Page{}, fmt.Errorf("browser: no page loaded in session %s", sessionID)
	}
	p := f.Pages[url]
	p.URL = url
	return p, nil
}

// Click requires a ref present in the current snapshot.
func (f *FakeDriver) Click(ctx context.Context, sessionID, ref string) error {
	f.note("click", sessionID, ref)
	p, err := f.Snapshot(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, e := range p.Elements {
		if e.Ref == ref {
			return nil
		}
	}
	return fmt.Errorf("browser: no such element %q", ref)
}

// Type requires a ref present in the current snapshot.
func (f *FakeDriver) Type(ctx context.Context, sessionID, ref, text string, enter bool) error {
	f.note("type", sessionID, ref)
	p, err := f.Snapshot(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, e := range p.Elements {
		if e.Ref == ref {
			return nil
		}
	}
	return fmt.Errorf("browser: no such element %q", ref)
}

// Press accepts a small allowlist; anything else (notably raw
// JavaScript-ish payloads) is rejected — the model gets keys, not code.
func (f *FakeDriver) Press(ctx context.Context, sessionID, key string) error {
	f.note("press", sessionID, key)
	allowed := map[string]bool{
		"Enter": true, "Escape": true, "Tab": true,
		"ArrowUp": true, "ArrowDown": true, "ArrowLeft": true, "ArrowRight": true,
		"Backspace": true, "Delete": true,
	}
	if !allowed[key] {
		return fmt.Errorf("browser: key %q not allowed", key)
	}
	return nil
}

// Screenshot returns a deterministic placeholder.
func (f *FakeDriver) Screenshot(ctx context.Context, sessionID string) ([]byte, error) {
	f.note("screenshot", sessionID, "")
	if _, ok := f.current[sessionID]; !ok {
		return nil, fmt.Errorf("browser: no page loaded in session %s", sessionID)
	}
	return []byte("fake-png-bytes"), nil
}

// Close forgets the session's current page.
func (f *FakeDriver) Close(sessionID string) error {
	f.note("close", sessionID, "")
	delete(f.current, sessionID)
	f.Closed = append(f.Closed, sessionID)
	return nil
}
