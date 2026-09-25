package main

import (
	"strings"
	"testing"
)

// Browser actions must read as user language on both surfaces (the TUI's
// collapsed rows and the mobile app's tool_status stream) — raw
// browser_* names are machinery, never user copy. Non-browser tools keep
// their existing labels and the default fallback.
func TestToolStatusLabelsNeverLeakMachinery(t *testing.T) {
	// Streams (TUI + mobile) show phases only: no tool names, file names,
	// commands, queries, or URLs. Browser actions keep their specific copy;
	// everything unknown collapses to one calm phrase.
	cases := []struct {
		name string
		args string
		want string
	}{
		{"browser_navigate", `{"url":"https://nairobiunwind.com/some/long/path/x"}`, "Browsing the web…"},
		{"browser_navigate", `{}`, "Browsing the web…"},
		{"browser_snapshot", `{}`, "Reading the page…"},
		{"browser_wait", `{}`, "Waiting for the page…"},
		{"browser_find", `{"text":"Log in"}`, "Searching the page…"},
		{"browser_screenshot", `{}`, "Capturing the page…"},
		{"browser_scroll", `{}`, "Scrolling…"},
		{"browser_console", `{}`, "Checking the page…"},
		{"browser_network", `{}`, "Checking the page…"},
		{"browser_a11y", `{}`, "Checking the page…"},
		{"browser_click", `{"ref":"@e10"}`, "Clicking…"},
		{"browser_click", `{}`, "Clicking…"},
		{"browser_fill_form", `{}`, "Filling in a form…"},
		{"browser_fill", `{}`, "Filling a field…"},
		{"browser_type", `{}`, "Typing…"},
		{"browser_press", `{}`, "Pressing a key…"},
		{"browser_select", `{}`, "Choosing an option…"},
		{"browser_check", `{}`, "Toggling a setting…"},
		{"browser_hover", `{}`, "Hovering…"},
		{"browser_drag", `{}`, "Dragging…"},
		{"browser_dialog", `{}`, "Handling a dialog…"},
		{"browser_submit", `{}`, "Submitting…"},
		{"browser_upload", `{}`, "Uploading…"},
		{"browser_download", `{}`, "Downloading…"},
		// Unknown future action: calm phrase, never the raw name.
		{"browser_frames", `{}`, "Working on it…"},
		{"exec", `{"command":"ls -la"}`, "Working on it…"},
		{"read_file", `{"path":"/home/x/notes.md"}`, "Reading…"},
		{"web_search", `{"query":"secret plans"}`, "Searching the web…"},
		{"web_fetch", `{"url":"https://example.com/a/b"}`, "Reading the page…"},
		{"some_new_tool", `{}`, "Working on it…"},
	}
	for _, c := range cases {
		if got := toolStatusLabel(c.name, c.args); got != c.want {
			t.Errorf("toolStatusLabel(%s) = %q, want %q", c.name, got, c.want)
		}
	}
	// Hard guarantee: no label may contain the tool's own name or a path.
	for _, c := range cases {
		got := toolStatusLabel(c.name, c.args)
		if strings.Contains(got, c.name) || strings.Contains(got, "/") {
			t.Errorf("label %q leaks machinery for %s", got, c.name)
		}
	}
}

// The TUI's collapsed-row icon reads browser actions as the browse glyph.
func TestToolIconBrowser(t *testing.T) {
	for _, name := range []string{
		"browser_navigate", "browser_snapshot", "browser_click",
		"browser_fill_form", "browser_screenshot", "browser_a11y",
	} {
		if got := toolIcon(name); got != "%" {
			t.Errorf("toolIcon(%s) = %q, want %%", name, got)
		}
	}
	// Existing mappings unchanged.
	if toolIcon("exec") != "$" || toolIcon("read_file") != "→" || toolIcon("web_search") != "◈" {
		t.Fatal("existing icon mapping drifted")
	}
}
