// Package film makes hand-drawn short films from the skill's Canvas engine:
// scaffolds a project, looks at it in the Pod's own browser, and renders it to
// an MP4. A film is one HTML file that draws every frame with JavaScript
// strokes; this package never draws anything itself, it only drives the page's
// frame hooks (window.__frame, __grid, __strip, __wav) and packs the result.
package film

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SkillDir is where the skill's engine, templates and examples live in the
// workspace.
const SkillDir = "skills/hand-drawn-animation"

// Dir is where film projects live in the workspace.
const Dir = "films"

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into a folder name.
func Slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "film"
	}
	return s
}

// engine modules every project gets; the rest are added on request.
var baseModules = []string{"core.js", "studio.js", "cels.js", "materials.js"}

var optionalModules = map[string]string{"sand": "sand.js", "paper3d": "paper3d.js"}

// Templates are the starting points `new` can copy.
var Templates = map[string]string{
	"starter":         "assets/starter.html",
	"still":           "assets/still.html",
	"sketchbook-bird": "examples/sketchbook-bird.html",
	"four-looks":      "examples/four-looks.html",
	"fly-style":       "examples/fly-style.html",
	"one-year":        "examples/one-year.html",
	"film-template":   "assets/film-template.html",
}

// TemplateNames lists them in a stable order.
func TemplateNames() []string {
	var out []string
	for k := range Templates {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var srcRe = regexp.MustCompile(`(<script[^>]+src=")(?:\.\./)+assets/([^"]+)(")`)

// New creates films/<slug>/ with the engine modules beside a copy of the
// chosen template, its script paths made local so the folder stands alone. It
// refuses to overwrite a project that exists. extra names optional modules
// ("sand", "paper3d"). It returns the project folder and the film's HTML file,
// both relative to the workspace.
func New(workspace, title, template string, extra []string) (dir, file string, err error) {
	if template == "" {
		template = "starter"
	}
	tplRel, ok := Templates[template]
	if !ok {
		return "", "", fmt.Errorf("no template %q (choose one of: %s)", template, strings.Join(TemplateNames(), ", "))
	}
	skill := filepath.Join(workspace, filepath.FromSlash(SkillDir))
	if _, err := os.Stat(filepath.Join(skill, "assets", "core.js")); err != nil {
		return "", "", errors.New("the hand-drawn animation skill isn't installed on this Pod")
	}
	slug := Slug(title)
	dir = Dir + "/" + slug
	abs := filepath.Join(workspace, filepath.FromSlash(dir))
	if _, err := os.Stat(abs); err == nil {
		return "", "", fmt.Errorf("%s already exists: edit its files, or choose another title", dir)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", "", err
	}
	fail := func(e error) (string, string, error) {
		_ = os.RemoveAll(abs)
		return "", "", e
	}
	mods := append([]string(nil), baseModules...)
	for _, e := range extra {
		m, ok := optionalModules[strings.ToLower(strings.TrimSpace(e))]
		if !ok {
			return fail(fmt.Errorf("no module %q (optional modules: sand, paper3d)", e))
		}
		mods = append(mods, m)
	}
	for _, m := range mods {
		b, err := os.ReadFile(filepath.Join(skill, "assets", m))
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(filepath.Join(abs, m), b, 0o644); err != nil {
			return fail(err)
		}
	}
	tpl, err := os.ReadFile(filepath.Join(skill, filepath.FromSlash(tplRel)))
	if err != nil {
		return fail(err)
	}
	html := srcRe.ReplaceAllString(string(tpl), "${1}${2}${3}")
	// A template that wants a module the project doesn't have gets it too.
	for _, m := range regexp.MustCompile(`src="([a-z0-9]+\.js)"`).FindAllStringSubmatch(html, -1) {
		if _, err := os.Stat(filepath.Join(abs, m[1])); err != nil {
			if b, err := os.ReadFile(filepath.Join(skill, "assets", m[1])); err == nil {
				_ = os.WriteFile(filepath.Join(abs, m[1]), b, 0o644)
			}
		}
	}
	file = dir + "/" + slug + ".html"
	if err := os.WriteFile(filepath.Join(workspace, filepath.FromSlash(file)), []byte(html), 0o644); err != nil {
		return fail(err)
	}
	return dir, file, nil
}

// Resolve turns what the model named (a project folder, a slug, a title or an
// HTML path) into the film's HTML file, relative to the workspace.
func Resolve(workspace, name string) (string, error) {
	name = strings.TrimSpace(strings.TrimPrefix(filepath.ToSlash(name), "./"))
	if name == "" {
		return "", errors.New("name the film project")
	}
	if strings.Contains(name, "..") {
		return "", errors.New("that path leaves the workspace")
	}
	try := func(rel string) (string, bool) {
		fi, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(rel)))
		return rel, err == nil && !fi.IsDir()
	}
	if strings.HasSuffix(name, ".html") && strings.HasPrefix(name, Dir+"/") {
		if r, ok := try(name); ok {
			return r, nil
		}
	}
	slug := strings.TrimPrefix(strings.TrimSuffix(name, "/"), Dir+"/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	for _, s := range []string{slug, Slug(slug)} {
		if r, ok := try(Dir + "/" + s + "/" + s + ".html"); ok {
			return r, nil
		}
		// A project whose main file was named differently: its only HTML file.
		if m, _ := filepath.Glob(filepath.Join(workspace, Dir, s, "*.html")); len(m) == 1 {
			return Dir + "/" + s + "/" + filepath.Base(m[0]), nil
		}
	}
	return "", fmt.Errorf("no film project %q (start one with action new)", name)
}

// OutDir is where a film's renders land, relative to the workspace.
func OutDir(file string) string { return filepath.ToSlash(filepath.Dir(file)) + "/out" }

// Name is the film's base name (its HTML file without the extension).
func Name(file string) string { return strings.TrimSuffix(filepath.Base(file), ".html") }
