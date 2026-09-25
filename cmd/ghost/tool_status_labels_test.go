package main

import "testing"

// Browser actions must read as user language on both surfaces (the TUI's
// collapsed rows and the mobile app's tool_status stream) — raw
// browser_* names are machinery, never user copy. Non-browser tools keep
// their existing labels and the default fallback.
func TestBrowserToolLabels(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"browser_navigate", `{"url":"https://nairobiunwind.com/some/long/path/x"}`, "Opening page: https://nairobiunwind.com/some/long/path…"},
		{"browser_navigate", `{}`, "Opening page…"},
		{"browser_snapshot", `{}`, "Reading the page…"},
		{"browser_wait", `{}`, "Waiting for the page…"},
		{"browser_find", `{"text":"Log in"}`, "Finding: Log in"},
		{"browser_screenshot", `{}`, "Capturing the page…"},
		{"browser_scroll", `{}`, "Scrolling the page…"},
		{"browser_console", `{}`, "Checking page console…"},
		{"browser_network", `{}`, "Checking network requests…"},
		{"browser_a11y", `{}`, "Checking accessibility…"},
		{"browser_click", `{"ref":"@e10"}`, "Clicking @e10"},
		{"browser_click", `{}`, "Clicking…"},
		{"browser_fill_form", `{}`, "Filling form…"},
		{"browser_fill", `{}`, "Filling field…"},
		{"browser_type", `{}`, "Typing…"},
		{"browser_press", `{}`, "Pressing key…"},
		{"browser_select", `{}`, "Selecting option…"},
		{"browser_check", `{}`, "Toggling checkbox…"},
		{"browser_hover", `{}`, "Hovering…"},
		{"browser_drag", `{}`, "Dragging…"},
		{"browser_dialog", `{}`, "Handling dialog…"},
		{"browser_submit", `{}`, "Submitting…"},
		{"browser_upload", `{}`, "Uploading file…"},
		{"browser_download", `{}`, "Downloading file…"},
		// Unknown future action: honest raw fallback, still one line.
		{"browser_frames", `{}`, "Using browser_frames…"},
		// Non-browser labels untouched.
		{"exec", `{"command":"ls -la"}`, "Running: ls -la"},
		{"some_new_tool", `{}`, "Using some_new_tool…"},
	}
	for _, c := range cases {
		if got := toolStatusLabel(c.name, c.args); got != c.want {
			t.Errorf("toolStatusLabel(%s) = %q, want %q", c.name, got, c.want)
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
