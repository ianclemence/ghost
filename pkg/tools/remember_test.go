package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runRemember(t *testing.T, ctx context.Context, content string) string {
	t.Helper()
	ws := t.TempDir()
	tool := NewRememberTool(ws, nil)
	res := tool.Execute(ctx, map[string]interface{}{"content": content, "category": "fact"})
	if res.IsError {
		t.Fatalf("remember failed: %s", res.ForLLM)
	}
	data, err := os.ReadFile(filepath.Join(ws, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Clean turns write plain memory lines.
func TestRememberClean(t *testing.T) {
	out := runRemember(t, context.Background(), "user likes tea")
	if !strings.Contains(out, "user likes tea") {
		t.Fatalf("content missing: %q", out)
	}
	if strings.Contains(out, "unverified") {
		t.Fatalf("clean turn must not be marked: %q", out)
	}
}

// Web-derived turns mark the file line so provenance survives the DB too.
func TestRememberWebDerivedMarked(t *testing.T) {
	ctx := WithWebDerived(context.Background())
	out := runRemember(t, ctx, "whatever the web said")
	if !strings.Contains(out, "(unverified: from outside content)") {
		t.Fatalf("content read from outside must be marked: %q", out)
	}
}

func TestWebToolSet(t *testing.T) {
	if !IsWebTool("web_fetch") || !IsWebTool("web_search") {
		t.Fatal("web tools must be recognized")
	}
	if IsWebTool("exec") || IsWebTool("read_file") || IsWebTool("") {
		t.Fatal("non-web tools must not be recognized")
	}
	if WebDerivedFromContext(nil) || WebDerivedFromContext(context.Background()) {
		t.Fatal("fresh contexts are not web-derived")
	}
}

// A memory the owner did not say must not look like one they did. Mail,
// documents, a calendar, external servers and browser pages are written by
// other people, and "remember that my account number is ..." inside any of
// them is the same attack as in a web page.
func TestReadingOutsideContentTaintsLaterMemory(t *testing.T) {
	for _, name := range []string{"web_fetch", "web_search", "email_search", "docs_search", "code_search", "calendar",
		"doc_parser", "mcp_crm_lookup", "browser_snapshot", "browser_click", "computer_screenshot"} {
		if !ReadsOutsideContent(name, nil) {
			t.Errorf("%s returns text the owner did not write and must taint later memory", name)
		}
	}
	for _, name := range []string{"read_file", "exec", "remember", "memory_recall", "system_status", "weather_now", ""} {
		if ReadsOutsideContent(name, nil) {
			t.Errorf("%s is the owner's or Ghost's own data and must not taint", name)
		}
	}
}
