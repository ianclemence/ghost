package computer

import (
	"fmt"
	"strconv"
	"strings"
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
			fmt.Fprintf(&b, "  %d. %s\n     role: %s\n", i+1, e.Label, role)
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
