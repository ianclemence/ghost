package motion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `{"title":"Q3 at the bakery","size":"portrait","accent":"#FFB547","scenes":[
 {"duration":3,"elements":[{"type":"title","text":"Q3 at the bakery","sub":"July to September, in numbers"}]},
 {"duration":4,"elements":[{"type":"number","from":0,"to":1240500,"prefix":"KES ","label":"Sales this quarter"},{"type":"text","text":"Up 18% on the quarter before.","at":1.6}],"caption":"From the till exports"},
 {"duration":4.5,"elements":[{"type":"bars","label":"Best sellers","labels":["Croissants","Sourdough","Cinnamon rolls","Coffee"],"values":[412,380,265,198],"unit":"sold"}]},
 {"duration":4,"elements":[{"type":"line","label":"Weekly sales","labels":["Jul","Aug","Sep"],"values":[80,92,88,101,97,110,118,115,122,130,128,141],"unit":"k"}]},
 {"duration":3.5,"elements":[{"type":"compare","label":"Average order","labels":["Q2","Q3"],"values":[540,610],"prefix":"KES "}]},
 {"duration":4,"elements":[{"type":"steps","label":"Next quarter","items":["Open on Sundays","Add a lunch menu","Loyalty card"]}]}
]}`

func TestSpecIsCheckedAndThePageIsSealed(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Duration() != 23 {
		t.Fatalf("duration %v", s.Duration())
	}
	if w, h := s.Frame(); w != 720 || h != 1280 {
		t.Fatalf("frame %dx%d", w, h)
	}
	bad := []string{
		`{"title":"x","scenes":[]}`,
		`{"title":"x","scenes":[{"duration":0.5,"elements":[{"type":"title","text":"a"}]}]}`,
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"video","text":"a"}]}]}`,
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"bars","labels":["a"],"values":[1,2]}]}]}`,
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"title","text":"a","at":5}]}]}`,
		`{"title":"x","accent":"red","scenes":[{"duration":3,"elements":[{"type":"title","text":"a"}]}]}`,
		`{"title":"x","script":"alert(1)","scenes":[{"duration":3,"elements":[{"type":"title","text":"a"}]}]}`,
	}
	for _, b := range bad {
		if _, err := Parse([]byte(b)); err == nil {
			t.Fatalf("accepted: %s", b)
		}
	}
	s.Scenes[0].Elements[0].Text = "</script><script>alert(1)</script>"
	page := Page(s, false)
	if strings.Contains(page, "</script><script>alert(1)") {
		t.Fatal("the spec can close its script tag")
	}
	if dir := os.Getenv("MOTION_DUMP"); dir != "" {
		s, _ := Parse([]byte(sample))
		_ = os.WriteFile(filepath.Join(dir, "preview.html"), []byte(Page(s, false)), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "render.html"), []byte(Page(s, true)), 0o644)
	}
}

// TestRenderMakesAVideo renders a short motion to an MP4 for real. It needs a
// browser and ffmpeg, so it runs only when asked (MOTION_RENDER=1).
func TestRenderMakesAVideo(t *testing.T) {
	if os.Getenv("MOTION_RENDER") == "" {
		t.Skip("set MOTION_RENDER=1 to render a real video")
	}
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "q3.mp4")
	if d := os.Getenv("MOTION_DUMP"); d != "" {
		out = filepath.Join(d, "q3.mp4")
	}
	start := time.Now()
	last := 0
	if err := Render(context.Background(), s, out, func(done, total int) { last = done }); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil || fi.Size() < 20000 || last == 0 {
		t.Fatalf("video: %v %v", fi, err)
	}
	t.Logf("rendered %.0fs of motion in %s (%d KB)", s.Duration(), time.Since(start).Round(time.Second), fi.Size()/1024)
}

const diagrams = `{"title":"How Ghost works","accent":"#8FB8FF","scenes":[
 {"duration":5,"elements":[{"type":"flow","label":"From words to done","items":["You ask","Ghost plans","You approve","It acts","It shows proof"]}]},
 {"duration":5,"elements":[{"type":"hub","label":"One Ghost, everywhere","text":"Your Pod","items":["Phone","Web console","Terminal","Home Assistant","Your files"]}]},
 {"duration":4,"elements":[{"type":"gauge","to":100,"suffix":"%","label":"Of your data stays at home"}]},
 {"duration":4,"elements":[{"type":"flow","items":["Notice","Remind","Follow up"],"loop":true}]}
]}`

func TestDiagramsAreChecked(t *testing.T) {
	s, err := Parse([]byte(diagrams))
	if err != nil {
		t.Fatal(err)
	}
	if s.Size != "landscape" {
		t.Fatalf("default size %q", s.Size)
	}
	if s.Scenes[2].Elements[0].Max != 100 {
		t.Fatal("a gauge's max defaults to 100")
	}
	for _, b := range []string{
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"flow","items":["only one"]}]}]}`,
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"hub","items":["a","b"]}]}]}`,
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"gauge","to":120,"max":100}]}]}`,
	} {
		if _, err := Parse([]byte(b)); err == nil {
			t.Fatalf("accepted: %s", b)
		}
	}
	if dir := os.Getenv("MOTION_DUMP"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "diagrams-land.html"), []byte(Page(s, true)), 0o644)
		s.Size = "portrait"
		_ = os.WriteFile(filepath.Join(dir, "diagrams-port.html"), []byte(Page(s, true)), 0o644)
	}
}
