package film

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skillTree copies the repo's skill into a scratch workspace, the way an
// installed Pod has it.
func skillTree(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	src := filepath.Join("..", "..", "workspace", filepath.FromSlash(SkillDir))
	if _, err := os.Stat(src); err != nil {
		t.Skip("the skill is not in this checkout")
	}
	dst := filepath.Join(ws, filepath.FromSlash(SkillDir))
	if err := exec.Command("cp", "-r", src, dst+"/").Run(); err != nil {
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := exec.Command("cp", "-r", src, dst).Run(); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"A Bird's Day!": "a-bird-s-day", "  ": "film", "Ünï": "n"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewMakesAProjectThatStandsAlone(t *testing.T) {
	ws := skillTree(t)
	dir, file, err := New(ws, "Paper Boat", "sketchbook-bird", []string{"sand"})
	if err != nil {
		t.Fatal(err)
	}
	if dir != "films/paper-boat" || file != "films/paper-boat/paper-boat.html" {
		t.Fatalf("dir %q file %q", dir, file)
	}
	for _, f := range []string{"core.js", "studio.js", "cels.js", "materials.js", "sand.js", "paper-boat.html"} {
		if _, err := os.Stat(filepath.Join(ws, dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	html, _ := os.ReadFile(filepath.Join(ws, file))
	if strings.Contains(string(html), "../assets/") || !strings.Contains(string(html), `src="core.js"`) {
		t.Error("script paths must be local to the project")
	}
	if _, _, err := New(ws, "Paper Boat", "starter", nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a project must never be overwritten: %v", err)
	}
	if _, _, err := New(ws, "Other", "nope", nil); err == nil {
		t.Error("an unknown template must be refused")
	}
	if _, _, err := New(ws, "Other", "starter", []string{"webgl"}); err == nil {
		t.Error("an unknown module must be refused")
	}
	if _, err := os.Stat(filepath.Join(ws, "films", "other")); err == nil {
		t.Error("a refused project must leave nothing behind")
	}
}

func TestResolveFindsTheFilm(t *testing.T) {
	ws := skillTree(t)
	_, file, err := New(ws, "Paper Boat", "starter", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"paper-boat", "Paper Boat", "films/paper-boat", "films/paper-boat/", file} {
		got, err := Resolve(ws, name)
		if err != nil || got != file {
			t.Errorf("Resolve(%q) = %q, %v", name, got, err)
		}
	}
	if _, err := Resolve(ws, "../etc/passwd"); err == nil {
		t.Error("a path out of the workspace must be refused")
	}
	if _, err := Resolve(ws, "nothing"); err == nil {
		t.Error("an unknown film must be refused")
	}
}

func e2e(t *testing.T) {
	t.Helper()
	if os.Getenv("GHOST_FILM_E2E") != "1" {
		t.Skip("set GHOST_FILM_E2E=1 to render real films in the Pod's browser")
	}
	for _, bin := range []string{"ffmpeg", "chromium"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " is not installed")
		}
	}
}

func TestEveryTemplateLoadsAndDraws(t *testing.T) {
	e2e(t)
	ws := skillTree(t)
	for _, tpl := range TemplateNames() {
		t.Run(tpl, func(t *testing.T) {
			_, file, err := New(ws, "T "+tpl, tpl, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			r, err := Check(ctx, ws, file, Options{Width: 480})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Notes) > 0 {
				t.Errorf("notes: %v", r.Notes)
			}
			t.Logf("%s: %d frames @ %d fps, %dx%d", tpl, r.Meta.Frames, r.Meta.FPS, r.Meta.OutW(), r.Meta.OutH())
		})
	}
}

func TestAFilmWithAnErrorIsReportedWithItsMessage(t *testing.T) {
	e2e(t)
	ws := skillTree(t)
	_, file, err := New(ws, "Broken", "starter", nil)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(ws, file)
	b, _ := os.ReadFile(abs)
	// Break the real call, which is the last one in the file (comments mention it too).
	src := string(b)
	i := strings.LastIndex(src, "defineFilm({")
	if i < 0 {
		t.Fatal("no defineFilm call in the starter")
	}
	_ = os.WriteFile(abs, []byte(src[:i]+"thisDoesNotExist(); "+src[i:]), 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err = Check(ctx, ws, file, Options{})
	if err == nil || !strings.Contains(err.Error(), "thisDoesNotExist") {
		t.Fatalf("the page's own error must reach the model, got: %v", err)
	}
}

func TestRenderMakesAVideoThatDecodes(t *testing.T) {
	e2e(t)
	ws := skillTree(t)
	_, file, err := New(ws, "Starter Render", "starter", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	start := time.Now()
	res, err := Render(ctx, ws, file, Options{Width: 480}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d frames in %s -> %s (audio=%v)", res.Meta.Frames, time.Since(start).Round(time.Second), res.Video, res.Audio)
	fi, err := os.Stat(filepath.Join(ws, res.Video))
	if err != nil || fi.Size() < 1000 {
		t.Fatalf("no video: %v", err)
	}
	if res.Contact == "" {
		t.Error("a contact sheet should ship beside the video")
	}
}

func TestAFilmCannotReachTheNetwork(t *testing.T) {
	e2e(t)
	ws := skillTree(t)
	_, file, err := New(ws, "Net", "starter", nil)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(ws, file)
	b, _ := os.ReadFile(abs)
	probe := `window.__net = "unset"; fetch("http://1.1.1.1/").then(()=>{window.__net="reached"}).catch(()=>{window.__net="blocked"});` +
		`fetch("https://example.com/").then(()=>{window.__net2="reached"}).catch(()=>{window.__net2="blocked"});` + "\n"
	_ = os.WriteFile(abs, []byte(strings.Replace(string(b), "const LOOK", probe+"const LOOK", 1)), 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	p, err := open(ctx, abs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	time.Sleep(2 * time.Second)
	raw, err := p.sess.EvalValue(ctx, `window.__net + "," + window.__net2`)
	if err != nil || string(raw) != `"blocked,blocked"` {
		t.Fatalf("a film must not reach the network: %s %v", raw, err)
	}
}

func TestPhotoIsCutFromItsBackdropAndRegistered(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ws := skillTree(t)
	_, _, err := New(ws, "Doodle", "starter", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A red disc on a white, evenly lit backdrop.
	img := image.NewNRGBA(image.Rect(0, 0, 200, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 200; x++ {
			c := color.NRGBA{250, 250, 250, 255}
			if (x-100)*(x-100)+(y-80)*(y-80) < 50*50 {
				c = color.NRGBA{200, 30, 30, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	_ = os.MkdirAll(filepath.Join(ws, "uploads"), 0o755)
	f, _ := os.Create(filepath.Join(ws, "uploads", "disc.png"))
	_ = png.Encode(f, img)
	f.Close()

	sheet, w, h, err := AddPhoto(context.Background(), ws, "doodle", "uploads/disc.png", "disc", PhotoOptions{Credit: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if w < 95 || w > 105 || h < 95 || h > 105 {
		t.Errorf("the cutout should be cropped to the disc, got %dx%d", w, h)
	}
	js, _ := os.ReadFile(filepath.Join(ws, "films", "doodle", "photos.js"))
	if !strings.Contains(string(js), `registerPhoto("disc"`) || !strings.Contains(string(js), "data:image/png;base64,") {
		t.Errorf("photos.js: %.80s", js)
	}
	if _, err := os.Stat(filepath.Join(ws, sheet)); err != nil {
		t.Errorf("no check sheet: %v", err)
	}
	// Registering the same name again replaces it instead of piling up.
	if _, _, _, err := AddPhoto(context.Background(), ws, "doodle", "uploads/disc.png", "disc", PhotoOptions{}); err != nil {
		t.Fatal(err)
	}
	js, _ = os.ReadFile(filepath.Join(ws, "films", "doodle", "photos.js"))
	if strings.Count(string(js), `registerPhoto("disc"`) != 1 {
		t.Error("the same name must replace its earlier entry")
	}
	if _, _, _, err := AddPhoto(context.Background(), ws, "doodle", "../secret.png", "disc", PhotoOptions{}); err == nil {
		t.Error("a path outside the workspace must be refused")
	}
}
