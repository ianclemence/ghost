package film

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/browser/native"
)

// Options are what the owner or the model can change about how a film is
// looked at, without editing it: they are the page's query string.
type Options struct {
	AR    string // aspect ratio override, "16:9"
	Width int    // output width override in pixels
	Look  string // ink, pencil, riso, screen, doodle, for films that support it
}

// Meta is what a film says about itself once it has loaded.
type Meta struct {
	Frames int `json:"n"`
	FPS    int `json:"fps"`
	// size holds the page's window.__size, whose keys differ only by case
	// (w, h in output pixels; W, H in frame units), so it is read as a map.
	size  map[string]float64
	Error string `json:"error"`
}

// OutW and OutH are the rendered video's size in pixels.
func (m Meta) OutW() int { return int(m.size["w"]) }
func (m Meta) OutH() int { return int(m.size["h"]) }

// Seconds is the film's running time.
func (m Meta) Seconds() float64 {
	if m.FPS == 0 {
		return 0
	}
	return float64(m.Frames) / float64(m.FPS)
}

// MaxFrames bounds one render: a Pod has one browser's worth of room, and a
// film this long is a project to plan, not an afternoon's render.
const MaxFrames = 24 * 180

// page is a film open in the Pod's browser.
type page struct {
	sess   *native.Session
	errors func() []string
	Meta   Meta
}

func (p *page) close() { p.sess.Close() }

func filmURL(abs string, o Options) string {
	q := url.Values{"bare": {"1"}, "frame": {"0"}}
	if o.AR != "" {
		q.Set("ar", o.AR)
	}
	if o.Width > 0 {
		q.Set("w", strconv.Itoa(o.Width))
	}
	if o.Look != "" {
		q.Set("look", o.Look)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: q.Encode()}
	return u.String()
}

// open loads the film and waits until it says it is ready, failing with the
// page's own error (and the line it came from) instead of waiting out a timeout.
func open(ctx context.Context, abs string, o Options) (*page, error) {
	sess, err := native.Launch(ctx, native.Options{
		Headless: true,
		Viewport: [2]int{1280, 720},
		Env:      []string{"DBUS_SESSION_BUS_ADDRESS=disabled:"},
		// A film is model-written JavaScript. It draws; it has no business on
		// the network, so every request that is not a local file goes to a
		// proxy that is not there (this also stops requests by IP address,
		// which name-resolution rules would not).
		ExtraArgs: []string{"--proxy-server=socks5://127.0.0.1:1", "--host-resolver-rules=MAP * ~NOTFOUND"},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't start the browser to draw it: %w", err)
	}
	p := &page{sess: sess}
	if p.errors, err = sess.WatchErrors(ctx); err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.Open(ctx, filmURL(abs, o), 40*time.Second); err != nil {
		sess.Close()
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if errs := p.errors(); len(errs) > 0 {
			sess.Close()
			return nil, pageErr("the film has an error", errs)
		}
		raw, err := sess.EvalValue(ctx, `window.__ready===true?"ready":(window.__error?"error:"+window.__error:"")`)
		if err == nil {
			var state string
			_ = json.Unmarshal(raw, &state)
			if state == "ready" {
				break
			}
			if strings.HasPrefix(state, "error:") {
				sess.Close()
				return nil, errors.New("the film has an error: " + strings.TrimPrefix(state, "error:"))
			}
		}
		if time.Now().After(deadline) {
			sess.Close()
			return nil, errors.New("the film never became ready. Usually defineFilm was not reached: a script failed to load, a module is loaded twice, or the film redefines a name core.js already defines (key, arc, drift, spring, hash, W, H, PAL)")
		}
		select {
		case <-ctx.Done():
			sess.Close()
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	raw, err := sess.EvalValue(ctx, `JSON.stringify({n:window.__NDRAW,fps:window.__fps,size:window.__size,error:window.__error})`)
	if err != nil {
		sess.Close()
		return nil, err
	}
	var js string
	_ = json.Unmarshal(raw, &js)
	var sz struct {
		Size map[string]float64 `json:"size"`
	}
	_ = json.Unmarshal([]byte(js), &sz)
	if err := json.Unmarshal([]byte(js), &p.Meta); err != nil || p.Meta.Frames <= 0 || (p.Meta.FPS != 12 && p.Meta.FPS != 24) {
		sess.Close()
		return nil, errors.New("the film did not report a valid frame count and fps (it must call defineFilm)")
	}
	p.Meta.size = sz.Size
	if p.Meta.Error != "" {
		sess.Close()
		return nil, errors.New("the film has an error: " + p.Meta.Error)
	}
	return p, nil
}

func pageErr(prefix string, errs []string) error {
	return fmt.Errorf("%s:\n- %s", prefix, strings.Join(errs, "\n- "))
}

// dataURL decodes a "data:<mime>;base64,..." string the page returned.
func dataURL(raw json.RawMessage) ([]byte, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	i := strings.IndexByte(s, ',')
	if i < 0 {
		return nil, errors.New("the page returned no image")
	}
	return base64.StdEncoding.DecodeString(s[i+1:])
}

// frame draws frame i and returns it as PNG bytes. A script error while
// drawing it is reported with the frame number and the page's own message.
func (p *page) frame(ctx context.Context, i int) ([]byte, error) {
	raw, err := p.sess.EvalValue(ctx, fmt.Sprintf("window.__frame(%d)", i))
	if err != nil {
		return nil, p.frameErr(i, err)
	}
	if errs := p.errors(); len(errs) > 0 {
		return nil, pageErr(fmt.Sprintf("frame %d (%.2fs) has an error", i, float64(i)/float64(p.Meta.FPS)), errs)
	}
	b, err := dataURL(raw)
	if err != nil {
		return nil, p.frameErr(i, err)
	}
	return b, nil
}

func (p *page) frameErr(i int, err error) error {
	if errs := p.errors(); len(errs) > 0 {
		return pageErr(fmt.Sprintf("frame %d (%.2fs) has an error", i, float64(i)/float64(p.Meta.FPS)), errs)
	}
	return fmt.Errorf("frame %d (%.2fs): %w", i, float64(i)/float64(p.Meta.FPS), err)
}

// blank reports whether a PNG is one flat colour: a frame that drew nothing.
func blank(b []byte) bool {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return false
	}
	bd := img.Bounds()
	r0, g0, b0, a0 := img.At(bd.Min.X, bd.Min.Y).RGBA()
	stepX, stepY := max(1, bd.Dx()/40), max(1, bd.Dy()/40)
	for y := bd.Min.Y; y < bd.Max.Y; y += stepY {
		for x := bd.Min.X; x < bd.Max.X; x += stepX {
			r, g, b, a := img.At(x, y).RGBA()
			if r != r0 || g != g0 || b != b0 || a != a0 {
				return false
			}
		}
	}
	return true
}

// Report is what Check found.
type Report struct {
	Meta    Meta
	Notes   []string  // things to fix before rendering
	Sample  []int     // the frames that were drawn to find them
	Quality []Quality // the measurements behind the notes, one per sample
}

// Worst returns the least legible sampled frame: the one that would make a
// film look blank.
func (r Report) Worst() Quality {
	var worst Quality
	for i, q := range r.Quality {
		if i == 0 || q.Contrast < worst.Contrast || q.DrawnCells < worst.DrawnCells {
			worst = q
		}
	}
	return worst
}

// Check opens the film and draws a spread of frames, including the first and
// last, to see that it loads, draws without error and does not show a blank.
func Check(ctx context.Context, workspace, file string, o Options) (Report, error) {
	abs := filepath.Join(workspace, filepath.FromSlash(file))
	p, err := open(ctx, abs, o)
	if err != nil {
		return Report{}, err
	}
	defer p.close()
	r := Report{Meta: p.Meta}
	if p.Meta.Frames > MaxFrames {
		r.Notes = append(r.Notes, fmt.Sprintf("it is %.0f s long; one render is limited to %d s", p.Meta.Seconds(), MaxFrames/24))
	}
	n := p.Meta.Frames
	seen := map[int]bool{}
	for _, f := range []int{0, n / 8, n / 4, 3 * n / 8, n / 2, 5 * n / 8, 3 * n / 4, 7 * n / 8, n - 1} {
		if f < 0 || f >= n || seen[f] {
			continue
		}
		seen[f] = true
		r.Sample = append(r.Sample, f)
		b, err := p.frame(ctx, f)
		if err != nil {
			return r, err
		}
		if blank(b) {
			r.Notes = append(r.Notes, fmt.Sprintf("frame %d (%.1fs) is a single flat colour: nothing was drawn there", f, float64(f)/float64(p.Meta.FPS)))
			continue
		}
		// A frame that drew something can still be too faint to see on a
		// phone; measure it rather than hope the model notices.
		if q, err := AnalyzePNG(b, f); err == nil {
			r.Quality = append(r.Quality, q)
			r.Notes = append(r.Notes, q.Notes()...)
		}
	}
	return r, nil
}

func write(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Grid writes a contact sheet of n evenly spaced frames and returns its
// workspace path.
func Grid(ctx context.Context, workspace, file string, o Options, n int) (string, error) {
	return sheet(ctx, workspace, file, o, fmt.Sprintf("window.__grid(%d)", n), "grid")
}

// Strip writes count consecutive frames from start, for checking a fast
// action or a contact, and returns its workspace path.
func Strip(ctx context.Context, workspace, file string, o Options, start, count int) (string, error) {
	return sheet(ctx, workspace, file, o, fmt.Sprintf("window.__strip(%d,%d)", start, count), fmt.Sprintf("strip-%d", start))
}

func sheet(ctx context.Context, workspace, file string, o Options, expr, tag string) (string, error) {
	p, err := open(ctx, filepath.Join(workspace, filepath.FromSlash(file)), o)
	if err != nil {
		return "", err
	}
	defer p.close()
	raw, err := p.sess.EvalValue(ctx, expr)
	if err != nil {
		return "", p.frameErr(0, err)
	}
	if errs := p.errors(); len(errs) > 0 {
		return "", pageErr("the film has an error", errs)
	}
	b, err := dataURL(raw)
	if err != nil {
		return "", err
	}
	rel := OutDir(file) + "/" + Name(file) + "-" + tag + ".jpg"
	return rel, write(filepath.Join(workspace, filepath.FromSlash(rel)), b)
}

// Frames writes full-size PNGs of the named frames and returns their
// workspace paths. With one frame this is a still: the picture itself.
func Frames(ctx context.Context, workspace, file string, o Options, frames []int) ([]string, Meta, error) {
	p, err := open(ctx, filepath.Join(workspace, filepath.FromSlash(file)), o)
	if err != nil {
		return nil, Meta{}, err
	}
	defer p.close()
	var out []string
	for _, f := range frames {
		if f < 0 || f >= p.Meta.Frames {
			return nil, p.Meta, fmt.Errorf("frame %d is outside the film (0 to %d)", f, p.Meta.Frames-1)
		}
		b, err := p.frame(ctx, f)
		if err != nil {
			return nil, p.Meta, err
		}
		rel := fmt.Sprintf("%s/%s-frames/%04d.png", OutDir(file), Name(file), f)
		if err := write(filepath.Join(workspace, filepath.FromSlash(rel)), b); err != nil {
			return nil, p.Meta, err
		}
		out = append(out, rel)
	}
	return out, p.Meta, nil
}

// Progress reports frames done of all, while a render runs.
type Progress func(done, total int)

// Result is a finished render.
type Result struct {
	Video   string // workspace path of the MP4
	Contact string // workspace path of the contact sheet
	Meta    Meta
	Audio   bool
}

// Render draws every frame of the film in the Pod's browser, straight from
// its canvas, and packs them into an H.264 MP4 at 24 fps (a 12 fps film has
// each drawing shown twice). If the film defines a score, it is rendered by the
// page and muxed in as AAC. A contact sheet with two tiles per second lands
// beside it. Outputs replace the previous ones only after everything succeeded.
func Render(ctx context.Context, workspace, file string, o Options, progress Progress) (Result, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return Result{}, errors.New("ffmpeg is not installed on this Pod, so it can't make videos")
	}
	abs := filepath.Join(workspace, filepath.FromSlash(file))
	p, err := open(ctx, abs, o)
	if err != nil {
		return Result{}, err
	}
	defer p.close()
	if p.Meta.Frames > MaxFrames {
		return Result{}, fmt.Errorf("the film is %.0f s; one render is limited to %d s. Shorten it or render it in parts", p.Meta.Seconds(), MaxFrames/24)
	}
	// A render is a deliverable. Measure three frames before encoding: a film
	// that draws nothing visible is refused here, with what to fix, instead of
	// arriving in the owner's chat as a blank video.
	if err := p.qualityGate(ctx); err != nil {
		return Result{}, err
	}

	name := Name(file)
	outRel := OutDir(file)
	outAbs := filepath.Join(workspace, filepath.FromSlash(outRel))
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return Result{}, err
	}
	work, err := os.MkdirTemp(outAbs, "."+name+"-render-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(work)

	silent := filepath.Join(work, name+".mp4")
	enc := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "image2pipe", "-framerate", strconv.Itoa(p.Meta.FPS), "-c:v", "png", "-i", "-",
		// libx264 wants even sizes; 24 fps out whatever the film's drawing rate.
		"-vf", "fps=24,scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-movflags", "+faststart", silent)
	var stderr strings.Builder
	enc.Stderr = &stderr
	stdin, err := enc.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	if err := enc.Start(); err != nil {
		return Result{}, err
	}
	werr := func() error {
		defer stdin.Close()
		for i := 0; i < p.Meta.Frames; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			b, err := p.frame(ctx, i)
			if err != nil {
				return err
			}
			if _, err := stdin.Write(b); err != nil {
				return err
			}
			if progress != nil && (i%24 == 0 || i == p.Meta.Frames-1) {
				progress(i+1, p.Meta.Frames)
			}
		}
		return nil
	}()
	if err := enc.Wait(); err != nil && werr == nil {
		werr = fmt.Errorf("encoding failed: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	if werr != nil {
		return Result{}, werr
	}

	final := silent
	hasAudio := false
	if raw, err := p.sess.EvalValue(ctx, `window.__wav ? window.__wav() : null`); err == nil && string(raw) != "null" {
		var b64 string
		if json.Unmarshal(raw, &b64) == nil && b64 != "" {
			if wav, err := base64.StdEncoding.DecodeString(b64); err == nil && len(wav) > 44 {
				wavPath := filepath.Join(work, name+"-score.wav")
				_ = os.WriteFile(wavPath, wav, 0o644)
				withAudio := filepath.Join(work, name+"-final.mp4")
				secs := fmt.Sprintf("%.3f", p.Meta.Seconds())
				mux := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
					"-i", silent, "-i", wavPath, "-map", "0:v:0", "-map", "1:a:0", "-af", "apad", "-t", secs,
					"-c:v", "copy", "-c:a", "aac", "-b:a", "192k", withAudio)
				if mux.Run() == nil {
					final, hasAudio = withAudio, true
				}
			}
		}
	}
	// A contact sheet with two tiles a second: the whole film at a glance.
	contact := filepath.Join(work, name+"-contact.jpg")
	rows := max(1, int((p.Meta.Seconds()*2+5)/6))
	_ = exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", final,
		"-vf", fmt.Sprintf("fps=2,scale=240:-2,tile=6x%d", rows), "-frames:v", "1", contact).Run()

	// The film must decode, and be as long as it says, before it replaces anything.
	if err := verifyVideo(ctx, final, p.Meta); err != nil {
		return Result{}, err
	}
	meta, _ := json.MarshalIndent(map[string]interface{}{
		"file": file, "fps": p.Meta.FPS, "frames": p.Meta.Frames, "outputFps": 24,
		"duration": p.Meta.Seconds(), "width": p.Meta.OutW(), "height": p.Meta.OutH(), "look": o.Look, "audio": hasAudio,
	}, "", " ")
	_ = os.WriteFile(filepath.Join(work, name+"-render.json"), append(meta, '\n'), 0o644)

	res := Result{Video: outRel + "/" + name + ".mp4", Meta: p.Meta, Audio: hasAudio}
	if err := os.Rename(final, filepath.Join(outAbs, name+".mp4")); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(contact); err == nil {
		_ = os.Rename(contact, filepath.Join(outAbs, name+"-contact.jpg"))
		res.Contact = outRel + "/" + name + "-contact.jpg"
	}
	_ = os.Rename(filepath.Join(work, name+"-render.json"), filepath.Join(outAbs, name+"-render.json"))
	// Full-size preview frames are working files, not deliverables: keep the
	// latest week's and drop the rest so a small card does not fill with PNGs.
	PruneFrames(workspace, 7*24*time.Hour)
	return res, nil
}

// qualityGate draws a few frames and refuses a film that is too faint or too
// empty to be seen. The measurements are the same ones Check reports, so the
// model can fix a film before rendering instead of after.
func (p *page) qualityGate(ctx context.Context) error {
	n := p.Meta.Frames
	seen := map[int]bool{}
	var fatal []Quality
	flat := 0
	for _, f := range []int{0, n / 2, n - 1} {
		if f < 0 || f >= n || seen[f] {
			continue
		}
		seen[f] = true
		b, err := p.frame(ctx, f)
		if err != nil {
			return err
		}
		// A single flat frame is a cut to white or a fade, which is a
		// deliberate part of a film. A film made only of flat frames is not.
		if blank(b) {
			flat++
			continue
		}
		q, err := AnalyzePNG(b, f)
		if err != nil {
			continue // a frame that cannot be measured is not a reason to refuse
		}
		if q.Fatal() {
			fatal = append(fatal, q)
		}
	}
	if flat == len(seen) {
		return fmt.Errorf("refusing to render a film that would look blank: every sampled frame is a single flat colour. Draw the subject, then run check again. Nothing was encoded")
	}
	if len(fatal) == 0 {
		return nil
	}
	worst := fatal[0]
	for _, q := range fatal[1:] {
		if q.Contrast < worst.Contrast || q.DrawnCells < worst.DrawnCells {
			worst = q
		}
	}
	return fmt.Errorf("refusing to render a film that would look blank: frame %d has contrast %.2f and marks in %d of %d areas (needs contrast %.2f+ and %d+ areas). Darken the strokes, add tone, and let the subject fill more of the frame; then run check again. Nothing was encoded",
		worst.Frame, worst.Contrast, worst.DrawnCells, gridCells, fatalContrast, fatalDrawnCells)
}

// verifyVideo decodes the whole file and checks it is about as long as the
// film. A video that cannot be decoded is not a deliverable.
func verifyVideo(ctx context.Context, path string, m Meta) error {
	ffmpeg, _ := exec.LookPath("ffmpeg")
	out, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", path, "-f", "null", "-").CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) > 0 {
		return fmt.Errorf("the encoded video does not decode cleanly: %s", strings.TrimSpace(string(out)))
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil
	}
	o, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return nil
	}
	got, _ := strconv.ParseFloat(strings.TrimSpace(string(o)), 64)
	if want := m.Seconds(); got < want-0.5 || got > want+0.5 {
		return fmt.Errorf("the video is %.1f s but the film is %.1f s", got, want)
	}
	return nil
}
