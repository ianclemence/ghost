package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The web console's markdown renderer (GhostUI.md in web/js/components.js)
// serves the memory/skills/about content. Its safety invariants are
// structural: escaping happens before any markup is recognized, and
// rendered fragments are stashed behind placeholders so a later rule can
// never re-match them.
func TestConsoleMarkdownSafetyInvariants(t *testing.T) {
	data, err := webFiles.ReadFile("web/js/components.js")
	if err != nil {
		t.Fatalf("components.js: %v", err)
	}
	src := string(data)

	// Escape-first: the escaped string is what every rule operates on.
	if !strings.Contains(src, "let s = esc(t);") {
		t.Fatalf("md() must escape the input before recognizing markup")
	}
	// Rendered fragments are stashed: code spans, images, links, autolinks.
	for _, want := range []string{
		"const keep = (frag) =>",
		"stash.push(frag)",
		"return s.replace(/\\u0000(\\d+)\\u0000/g",
		"class=\"md-image\"",
		"rel=\"noopener\"",
		"class=\"task",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("md() is missing expected safety/structure marker %q", want)
		}
	}
	// No dynamic code execution anywhere in the console scripts.
	for _, bad := range []string{"eval(", "new Function(", "document.write("} {
		if strings.Contains(src, bad) {
			t.Errorf("components.js must not use %s", bad)
		}
	}
}

// Behavioral check of the real renderer. It needs a JS runtime; skipped
// when none is installed rather than silently passing an unrun test.
func TestConsoleMarkdownBehavior(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed; skipping executable markdown checks")
	}
	cmd := exec.Command(bun, "web/js/markdown.test.mjs")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("web markdown checks failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "all checks passed") {
		t.Fatalf("unexpected test output:\n%s", out)
	}
}

// The wording helpers decide what a routine shows twice or once.
func TestConsoleWordingBehavior(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed; skipping executable wording checks")
	}
	cmd := exec.Command(bun, "web/js/wording.test.mjs")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("web wording checks failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "all checks passed") {
		t.Fatalf("unexpected test output:\n%s", out)
	}
}
