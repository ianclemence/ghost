// Package documents turns what Ghost writes into a document the owner can
// keep, print, send or sign: a CV, a letter, an invoice, an itinerary, a
// one-pager. Ghost writes Markdown; the Pod lays it out as a printed page
// (pandoc to HTML in a fixed, quiet print style, then Chromium to PDF) and
// keeps the Markdown beside it, so the same document can also be had as Word.
//
// What the model writes is never trusted as markup: raw HTML in the Markdown
// is not passed through, the page carries a policy that loads and runs
// nothing, and Chromium is pointed at a proxy that does not exist, so a
// document cannot reach the network while it is being printed.
package documents

import (
	"bytes"
	"context"
	"crypto/sha1"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dir is the workspace folder documents are kept in.
const Dir = "documents"

// MaxMarkdown bounds one document's source.
const MaxMarkdown = 200 * 1024

// ErrUnavailable means this Pod is missing a program documents need.
var ErrUnavailable = errors.New("documents are not set up on this Pod")

// Missing names the programs this Pod lacks to make documents, or nil.
func Missing() []string {
	var out []string
	for _, p := range []string{"pandoc", chromiumBinary(), "pdftoppm", "pdfinfo"} {
		if _, err := exec.LookPath(p); err != nil {
			out = append(out, p)
		}
	}
	return out
}

func chromiumBinary() string {
	for _, b := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	return "chromium"
}

// Result is a document made from Markdown.
type Result struct {
	// Workspace-relative paths of the source, the laid-out page and the PDF.
	Markdown, HTML, PDF string
	Pages               int
	Version             int
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is the file-safe form of a title.
func Slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-"), "-")
	if s == "" {
		return "document"
	}
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

func nextVersion(dir, slug string) int {
	matches, _ := filepath.Glob(filepath.Join(dir, slug+"-v*.md"))
	re := regexp.MustCompile(`-v(\d+)\.md$`)
	max := 0
	for _, m := range matches {
		if sub := re.FindStringSubmatch(m); sub != nil {
			if n, _ := strconv.Atoi(sub[1]); n > max {
				max = n
			}
		}
	}
	return max + 1
}

// Render lays out markdown as a printed document titled title and saves it as
// the next version of that title.
func Render(ctx context.Context, workspace, title, markdown string) (Result, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Result{}, errors.New("a document needs a title")
	}
	if strings.TrimSpace(markdown) == "" {
		return Result{}, errors.New("a document needs some words")
	}
	if len(markdown) > MaxMarkdown {
		return Result{}, fmt.Errorf("that document is %d KB; the limit is %d KB", len(markdown)/1024, MaxMarkdown/1024)
	}
	if m := Missing(); len(m) > 0 {
		return Result{}, fmt.Errorf("%w (missing: %s)", ErrUnavailable, strings.Join(m, ", "))
	}
	dir := filepath.Join(workspace, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	slug := Slug(title)
	v := nextVersion(dir, slug)
	base := fmt.Sprintf("%s-v%d", slug, v)
	mdPath := filepath.Join(dir, base+".md")
	htmlPath := filepath.Join(dir, base+".html")
	pdfPath := filepath.Join(dir, base+".pdf")
	if err := os.WriteFile(mdPath, []byte(markdown), 0o644); err != nil {
		return Result{}, err
	}
	body, err := pandoc(ctx, markdown, "html5")
	if err != nil {
		return Result{}, err
	}
	page := Page(title, string(body))
	if err := os.WriteFile(htmlPath, []byte(page), 0o644); err != nil {
		return Result{}, err
	}
	if err := printPDF(ctx, htmlPath, pdfPath); err != nil {
		return Result{}, err
	}
	pages, err := PageCount(ctx, pdfPath)
	if err != nil {
		return Result{}, err
	}
	rel := func(p string) string { return Dir + "/" + filepath.Base(p) }
	return Result{Markdown: rel(mdPath), HTML: rel(htmlPath), PDF: rel(pdfPath), Pages: pages, Version: v}, nil
}

// markdownReader is pandoc's own Markdown with raw HTML, raw attributes and
// raw TeX turned off: anything that looks like a tag is printed as text. A
// single line break is a line break, the way an address or a letter is written.
// (Its
// gfm and commonmark readers pass raw HTML through even when told not to.)
const markdownReader = "markdown+hard_line_breaks-raw_html-raw_attribute-raw_tex"

// pandoc converts Markdown (raw HTML refused) to format.
func pandoc(ctx context.Context, markdown, format string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pandoc", "-f", markdownReader, "-t", format)
	cmd.Stdin = strings.NewReader(markdown)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not lay out the document: %s", firstLine(errb.String(), err))
	}
	return out.Bytes(), nil
}

// Word converts a saved document's Markdown to a .docx.
func Word(ctx context.Context, workspace, mdRel string) ([]byte, error) {
	md, err := readInside(workspace, mdRel)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("pandoc"); err != nil {
		return nil, fmt.Errorf("%w (missing: pandoc)", ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tmp, err := os.CreateTemp("", "ghost-doc-*.docx")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	cmd := exec.CommandContext(ctx, "pandoc", "-f", markdownReader, "-t", "docx", "-o", tmp.Name())
	cmd.Stdin = bytes.NewReader(md)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not make the Word file: %s", firstLine(errb.String(), err))
	}
	return os.ReadFile(tmp.Name())
}

func printPDF(ctx context.Context, htmlPath, pdfPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	profile, err := os.MkdirTemp("", "ghost-doc-profile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	args := func(noSandbox bool) []string {
		a := []string{
			"--headless", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
			"--disable-extensions", "--disable-background-networking", "--disable-sync",
			// Nothing the document says can reach the network while printing,
			// and its policy (default-src 'none') runs no script at all.
			"--proxy-server=127.0.0.1:9", "--proxy-bypass-list=<-loopback>",
			"--user-data-dir=" + profile,
			"--no-pdf-header-footer",
			"--print-to-pdf=" + pdfPath,
		}
		if noSandbox {
			a = append(a, "--no-sandbox")
		}
		return append(a, "file://"+htmlPath)
	}
	run := func(noSandbox bool) (string, error) {
		cmd := exec.CommandContext(ctx, chromiumBinary(), args(noSandbox)...)
		var errb bytes.Buffer
		cmd.Stderr = &errb
		cmd.Stdout = &errb
		err := cmd.Run()
		return errb.String(), err
	}
	_ = os.Remove(pdfPath)
	out, err := run(os.Geteuid() == 0)
	if (err != nil || !nonEmpty(pdfPath)) && os.Geteuid() != 0 && strings.Contains(strings.ToLower(out), "sandbox") {
		// Some small machines cannot start Chromium's sandbox. The page is
		// inert (no scripts, no network), so it is printed without one.
		out, err = run(true)
	}
	if err != nil || !nonEmpty(pdfPath) {
		return fmt.Errorf("could not print the document: %s", firstLine(out, err))
	}
	return nil
}

func nonEmpty(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

var pagesRE = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

// PageCount reads how many pages a PDF has.
func PageCount(ctx context.Context, pdfPath string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pdfinfo", pdfPath).Output()
	if err != nil {
		return 0, fmt.Errorf("could not read the document: %v", err)
	}
	m := pagesRE.FindSubmatch(out)
	if m == nil {
		return 0, errors.New("could not count the document's pages")
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n, nil
}

// PageImage is page n (from 1) of a PDF in the workspace as a PNG about
// width pixels wide, made once and kept.
func PageImage(ctx context.Context, workspace, pdfRel string, n, width int) ([]byte, int, error) {
	abs, err := inside(workspace, pdfRel)
	if err != nil {
		return nil, 0, err
	}
	pages, err := PageCount(ctx, abs)
	if err != nil {
		return nil, 0, err
	}
	if n < 1 || n > pages {
		return nil, pages, fmt.Errorf("the document has %d pages", pages)
	}
	if width < 200 || width > 1600 {
		width = 900
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, pages, err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%d", pdfRel, st.ModTime().UnixNano(), n, width)))
	cache := filepath.Join(workspace, ".cache", "document-pages")
	_ = os.MkdirAll(cache, 0o755)
	cached := filepath.Join(cache, hex.EncodeToString(sum[:8])+".png")
	if b, err := os.ReadFile(cached); err == nil && len(b) > 0 {
		return b, pages, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := strings.TrimSuffix(cached, ".png")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-f", strconv.Itoa(n), "-l", strconv.Itoa(n), "-png", "-singlefile",
		"-scale-to-x", strconv.Itoa(width), "-scale-to-y", "-1", abs, prefix)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, pages, fmt.Errorf("could not draw the page: %s", firstLine(errb.String(), err))
	}
	b, err := os.ReadFile(cached)
	return b, pages, err
}

// inside resolves a workspace-relative path and refuses anything outside it.
func inside(workspace, rel string) (string, error) {
	clean := filepath.Clean("/" + rel)
	abs := filepath.Join(workspace, clean)
	ws, _ := filepath.Abs(workspace)
	full, _ := filepath.Abs(abs)
	if !strings.HasPrefix(full, ws+string(os.PathSeparator)) {
		return "", errors.New("that file is not in the workspace")
	}
	return full, nil
}

func readInside(workspace, rel string) ([]byte, error) {
	abs, err := inside(workspace, rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// SourceOf is the Markdown a document's PDF was made from, if it is one of
// Ghost's documents ("documents/cv-v2.pdf" -> "documents/cv-v2.md").
func SourceOf(pdfRel string) (string, bool) {
	if !strings.HasPrefix(pdfRel, Dir+"/") || !strings.HasSuffix(pdfRel, ".pdf") {
		return "", false
	}
	return strings.TrimSuffix(pdfRel, ".pdf") + ".md", true
}

func firstLine(s string, err error) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 200 {
				l = l[:200]
			}
			return l
		}
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

// The faces every document is set in, the same as Ghost's own: Instrument
// Serif for headings, Inter for reading (OFL; licences beside them). They are
// inlined, so a document looks the same on any Pod and needs nothing fetched.
//
//go:embed fonts/instrument-serif.woff2
var serifFont []byte

//go:embed fonts/inter.woff2
var sansFont []byte

func fontFaces() string {
	face := func(name, weight string, b []byte) string {
		return "@font-face{font-family:\"" + name + "\";font-weight:" + weight + ";font-style:normal;src:url(data:font/woff2;base64," + base64.StdEncoding.EncodeToString(b) + ") format(\"woff2\");}"
	}
	return face("Instrument Serif", "400", serifFont) + face("Inter", "100 900", sansFont)
}

// Page wraps a document's body in the print style: a light page, a serif for
// headings and a clean sans for reading, quiet tables, generous margins. The
// policy at the top lets nothing load or run.
func Page(title, body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:">
<title>` + html.EscapeString(title) + `</title><style>` + fontFaces() + printCSS + `</style></head><body><main>` + body + `</main></body></html>`
}

const printCSS = `
@page { size: A4; margin: 20mm 18mm 22mm; }
:root { --ink:#16151c; --muted:#5d5b68; --line:#e4e2ea; --accent:#4b3fd6; --soft:#f5f4fa; }
* { box-sizing: border-box; }
html { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
body { margin: 0; color: var(--ink); font: 10.5pt/1.55 "Inter","Helvetica Neue","Liberation Sans","DejaVu Sans",Arial,sans-serif; font-weight: 400; }
main { max-width: 100%; }
h1, h2, h3 { font-family: "Instrument Serif","Iowan Old Style","Palatino Linotype","Liberation Serif","DejaVu Serif",Georgia,serif; font-weight: 400; color: var(--ink); line-height: 1.15; break-after: avoid; }
h1 { font-size: 34pt; letter-spacing: -0.02em; margin: 0 0 4pt; }
h1 + p { color: var(--muted); font-size: 11pt; margin-top: 0; }
h1::after { content: ""; display: block; width: 36pt; height: 2pt; background: var(--accent); margin: 10pt 0 14pt; border-radius: 1pt; }
h2 { font-size: 20pt; margin: 18pt 0 6pt; padding-bottom: 4pt; border-bottom: 0.75pt solid var(--line); }
h3 { font-size: 15pt; margin: 14pt 0 4pt; }
h4, h5, h6 { font-size: 10.5pt; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); margin: 12pt 0 4pt; }
p { margin: 0 0 8pt; }
a { color: var(--accent); text-decoration: none; }
strong { font-weight: 600; }
ul, ol { margin: 0 0 8pt; padding-left: 16pt; }
li { margin: 2pt 0; }
li > p { margin: 0; }
hr { border: 0; border-top: 0.75pt solid var(--line); margin: 14pt 0; }
blockquote { margin: 10pt 0; padding: 8pt 12pt; border-left: 2pt solid var(--accent); background: var(--soft); color: #34323d; border-radius: 0 4pt 4pt 0; }
blockquote > :last-child { margin-bottom: 0; }
code { font: 9pt/1.4 "DejaVu Sans Mono","Liberation Mono",Menlo,monospace; background: var(--soft); padding: 1pt 3pt; border-radius: 3pt; }
pre { background: var(--soft); padding: 8pt 10pt; border-radius: 5pt; overflow: hidden; white-space: pre-wrap; break-inside: avoid; }
pre code { background: none; padding: 0; }
table { width: 100%; border-collapse: collapse; margin: 8pt 0 12pt; font-size: 9.5pt; break-inside: auto; }
thead th { text-align: left; font-weight: 600; color: var(--muted); font-size: 8.5pt; text-transform: uppercase; letter-spacing: 0.06em; border-bottom: 1pt solid var(--ink); padding: 5pt 6pt; }
td { padding: 5pt 6pt; border-bottom: 0.5pt solid var(--line); vertical-align: top; }
tr { break-inside: avoid; }
/* A wide table (an invoice, a budget) lines its last column up on the right. */
tr > :last-child:nth-child(n+3) { text-align: right; font-variant-numeric: tabular-nums; }
img { max-width: 100%; }
`

// FontFaces is Ghost's type as inline @font-face rules, for anything else the
// Pod lays out (a motion, a page) that must look the same with nothing fetched.
func FontFaces() string { return fontFaces() }
