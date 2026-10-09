package documents

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugAndSource(t *testing.T) {
	if got := Slug("  My CV — 2026!  "); got != "my-cv-2026" {
		t.Fatalf("slug = %q", got)
	}
	if got := Slug("!!!"); got != "document" {
		t.Fatalf("empty slug = %q", got)
	}
	if src, ok := SourceOf("documents/cv-v2.pdf"); !ok || src != "documents/cv-v2.md" {
		t.Fatalf("source = %q %v", src, ok)
	}
	if _, ok := SourceOf("canvas/x.pdf"); ok {
		t.Fatal("a PDF outside documents is not one of Ghost's documents")
	}
}

func TestPageIsInert(t *testing.T) {
	p := Page(`A "title" <b>`, "<p>hi</p>")
	if !strings.Contains(p, "default-src 'none'") || strings.Contains(p, "<b>") {
		t.Fatalf("page must carry the policy and escape the title: %s", p[:300])
	}
}

func TestInsideRefusesEscapes(t *testing.T) {
	ws := t.TempDir()
	for _, rel := range []string{"../etc/passwd", "documents/../../x"} {
		if abs, err := inside(ws, rel); err == nil && !strings.HasPrefix(abs, ws) {
			t.Fatalf("%s resolved outside: %s", rel, abs)
		}
	}
}

// The whole path on a real Pod: Markdown to a PDF with pages, a page image and
// a Word file. Skipped where the programs are not installed.
func TestRenderEndToEnd(t *testing.T) {
	if m := Missing(); len(m) > 0 {
		t.Skipf("missing %v", m)
	}
	ws := t.TempDir()
	md := "# Ian Clemence\n\nProduct engineer, Nairobi\n\n## Experience\n\n- Built Ghost\n- Shipped things\n\n| Year | Role |\n|---|---|\n| 2026 | Founder |\n\n<script>alert(1)</script>\n"
	r, err := Render(context.Background(), ws, "Ian's CV", md)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages < 1 || r.Version != 1 || r.PDF != "documents/ian-s-cv-v1.pdf" {
		t.Fatalf("result = %+v", r)
	}
	html, _ := os.ReadFile(filepath.Join(ws, r.HTML))
	if bytes.Contains(html, []byte("<script>")) {
		t.Fatal("raw HTML from the model reached the page")
	}
	png, pages, err := PageImage(context.Background(), ws, r.PDF, 1, 600)
	if err != nil || pages != r.Pages || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("page image: %v pages=%d len=%d", err, pages, len(png))
	}
	if _, _, err := PageImage(context.Background(), ws, r.PDF, r.Pages+1, 600); err == nil {
		t.Fatal("a page past the end was drawn")
	}
	docx, err := Word(context.Background(), ws, r.Markdown)
	if err != nil || !bytes.HasPrefix(docx, []byte("PK")) {
		t.Fatalf("word: %v", err)
	}
	again, err := Render(context.Background(), ws, "Ian's CV", md+"\nMore.\n")
	if err != nil || again.Version != 2 {
		t.Fatalf("second version: %+v %v", again, err)
	}
}
