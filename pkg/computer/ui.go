package computer

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// UIElement is one interactive element of the current UI, described in the
// way "a normal user could reasonably see and interact with it" — never a
// raw accessibility dump, never hidden/credential fields.
type UIElement struct {
	// Label is the human-visible element label ("Name", "Save").
	Label string
	// Role is the element role (textbox, button, ...).
	Role string
	// Value is the current user-visible value ("" when none).
	Value string
	// Enabled reports whether the element is interactive.
	Enabled bool
	// Focused reports whether the element currently has keyboard focus.
	Focused bool
	// X,Y is the clickable center of the element on the screen (0,0 when
	// the element has no stable click target). computer_click operates on
	// these coordinates, so the observation must make them available.
	X, Y int
}

// UISnapshot is the bounded, deterministic, model-useful rendering of the
// current UI state.
type UISnapshot struct {
	WindowTitle  string
	VisibleText  string
	FocusedLabel string
	Elements     []UIElement
}

// FormatUISnapshot renders a snapshot as structured text for the model.
// Deterministic, bounded, and compact: it exposes what a user sees and can
// interact with, and nothing else.
func FormatUISnapshot(s UISnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Window:\n  title: %q\n", s.WindowTitle)
	if s.VisibleText != "" {
		b.WriteString("Visible text:\n")
		for _, line := range strings.Split(strings.TrimSpace(s.VisibleText), "\n") {
			fmt.Fprintf(&b, "  %q\n", line)
		}
	}
	if len(s.Elements) > 0 {
		b.WriteString("Elements:\n")
		for i, e := range s.Elements {
			role := e.Role
			if role == "" {
				role = "element"
			}
			// Refs are positional within one snapshot: click by element
			// with ref "eN" while the snapshot is live. A ref dies with
			// the next observation or any action after it — clicking a
			// stale ref errors and says to inspect again. Elements with
			// no click target carry no click line: screenshot the screen
			// and click raw coordinates instead.
			fmt.Fprintf(&b, "  %d. %s\n     ref: e%d\n     role: %s\n", i+1, e.Label, i+1, role)
			if e.Value != "" {
				fmt.Fprintf(&b, "     value: %q\n", e.Value)
			}
			fmt.Fprintf(&b, "     enabled: %s\n", strconv.FormatBool(e.Enabled))
			if e.Focused {
				fmt.Fprintf(&b, "     focused: true\n")
			}
			if e.X != 0 || e.Y != 0 {
				fmt.Fprintf(&b, "     click: (%d, %d)\n", e.X, e.Y)
			}
		}
	}
	if s.FocusedLabel != "" {
		fmt.Fprintf(&b, "Focused element:\n  %s\n", s.FocusedLabel)
	}
	return strings.TrimRight(b.String(), "\n")
}

// refRetainer holds one inspect snapshot's elements so a later click can
// name them by ref ("e3") instead of coordinates. Refs live for exactly
// one screen: any state-changing action expires them, and the next click
// by ref fails closed telling the model to inspect again. Screenshots and
// clipboard reads do not expire refs — they change nothing on screen.
type refRetainer struct {
	mu        sync.Mutex
	epoch     uint64
	window    string
	els       []UIElement
	inspected bool
}

// retain stores a fresh snapshot and returns its epoch.
func (r *refRetainer) retain(window string, els []UIElement) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	r.window = window
	r.els = els
	r.inspected = true
	return r.epoch
}

// expire drops the retained elements: the screen may have changed.
func (r *refRetainer) expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	r.els = nil
}

// resolve maps "eN" to its element against the live snapshot.
func (r *refRetainer) resolve(ref string) (UIElement, uint64, error) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(ref), "e%d", &n); err != nil || n < 1 {
		return UIElement{}, 0, fmt.Errorf("element ref looks like e3 (the ref beside an element in the latest inspect_ui)")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.els) == 0 {
		if !r.inspected {
			return UIElement{}, 0, fmt.Errorf("no live element snapshot: call inspect_ui first, then click one of its refs")
		}
		return UIElement{}, 0, fmt.Errorf("elements expired after the last action: inspect_ui again for live refs")
	}
	if n > len(r.els) {
		return UIElement{}, 0, fmt.Errorf("ref %s is outside the 1..%d live elements: inspect_ui again", strings.TrimSpace(ref), len(r.els))
	}
	e := r.els[n-1]
	if e.X == 0 && e.Y == 0 {
		return UIElement{}, 0, fmt.Errorf("element %s has no click target: screenshot the screen and click raw coordinates", strings.TrimSpace(ref))
	}
	return e, r.epoch, nil
}
