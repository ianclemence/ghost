package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/film"
)

// FilmTool is Ghost's animation studio for hand-drawn films and drawings. A
// film is one HTML file whose every frame is drawn with JavaScript strokes (the
// hand-drawn-animation skill explains how to author one). The tool sets a
// project up, looks at it in the Pod's own browser, and renders it to an MP4,
// so the model can work the way an animator does: draw, look, correct, then
// commit to the render.
type FilmTool struct {
	workspace string
	jobs      *film.Jobs
	mu        sync.Mutex
	pub       CanvasPublisher
	// onVideo tells the owner a render is ready (or failed), in their conversation.
	onVideo func(ctx context.Context, title string, j film.Job)
	// onProgress says what a long tool call is doing, so a film that takes a
	// minute is not silence. Optional: unwired in tests and the CLI.
	onProgress func(ctx context.Context, text string)
	// locks serializes work on one film. Two turns can arrive at once (a
	// resent message, a second device); without this they write the same grid
	// and edit the same file in parallel.
	locks sync.Map // project file → *sync.Mutex
	// progressAt throttles progress notes per film.
	progressAt map[string]time.Time
}

// filmCheatSheet is the part of the engine's API a new film needs, so the
// model does not spend its tool calls reading the 80 KB engine.
const filmCheatSheet = `FILM CHEAT SHEET
Shape: the film file loads core.js, studio.js, cels.js (script tags, already there), then your own <script> ending in defineFilm(...).
Cels are strokes only: for a filled shape use c.fill(curvePath(pts,true,2.4)) first (2.4 rounds corners, .8 keeps them), then draw strokes on top; the still template shows form() (fill, hatched shade, wobbling ink line).
Drawings are strokes: cel = {strokes:[{id, points:[[x,y],...], width, opacity, pressure, close}]}. Same ids and point counts in two cels lets inbetweenCel(a,b,u) blend them. Make every drawing in code: a function(params) that returns a cel.
  compileCel(cel,{id}) once per drawing; exposureSheet([{id,frames},...], {id:compiledCel}, 24); SHEET.at(tau).drawing is the cel to show; drawCel(c, cel, {material:'pencil'|'ink', color}).
  Hold each drawing 2 frames (on twos), 1 for fast action. A held drawing keeps its marks: never reseed per frame.
defineFilm({palette:{...PALETTES.pencilMinimal, paper:'#f5f0e5'}, format:{ar:'9:16'|'16:9'|'1:1', width:1080}, fps:24, timeline:[{name, dur: seconds, fn:(c,tau,i)=>{...}}]}).
Scene fn(c,tau): paper(c,'#f5f0e5',null,seed) FIRST (it resets the transform), then c.save(); c.translate(CX, CY+y); drawCel(...); c.restore(). tau = seconds since the scene started.
COORDINATES: CX, CY, W, H are only valid INSIDE scene functions. At file scope they are still a 1:1 frame, so drawings built there from CX land off-screen. Author in local units (origin = a point you choose) and translate in the scene. The frame's short side is 1080 units (9:16 is 1080x1920).
Helpers: ellPts(cx,cy,rx,ry,rot,n), blob(cx,cy,rx,ry,seed), smoothPts(pts), rng(seed) (never Math.random), key(t,[[t,v],...]), sm(a,b,t), lerp, clamp, easeIO, easeOutQuint, easeOutBack, mix, alpha, hatch, wob, handText, PALETTES, usePalette.
A long line needs pressure:[[0,.55],[.08,.95],[.92,.95],[1,.45]] or it fades at both ends.
FRAMING: the subject should fill a quarter or more of the frame width and sit on a thirds line; use the whole frame (in 9:16, three depths from top to bottom) so no half of it is blank paper. Check the grid for empty areas and clipping.
Weight comes from spacing (fast fall, slow apex), squash on contact, and anticipation, not from texture. Exaggerate: a hop clears at least one body height, a squash is a quarter to a third of the body, anticipation is 2-4 frames; a move under a tenth of the frame does not read on a phone.
LOOP: edit -> check -> preview grid 24 -> look with vision -> preview strip around the hardest action -> fix -> render. A short film is about a dozen tool calls: do not spend more than three reading.`

func NewFilmTool(workspace string, jobs *film.Jobs) *FilmTool {
	return &FilmTool{workspace: workspace, jobs: jobs}
}

// SetPublisher wires the artifact store once it exists.
func (t *FilmTool) SetPublisher(p CanvasPublisher) { t.mu.Lock(); t.pub = p; t.mu.Unlock() }

// OnVideo sets what happens when a render finishes.
func (t *FilmTool) OnVideo(fn func(ctx context.Context, title string, j film.Job)) {
	t.mu.Lock()
	t.onVideo = fn
	t.mu.Unlock()
}

// OnProgress sets where a one-line progress note goes while a film is being
// looked at or rendered.
func (t *FilmTool) OnProgress(fn func(ctx context.Context, text string)) {
	t.mu.Lock()
	t.onProgress = fn
	t.mu.Unlock()
}

// lock serializes tool calls that touch one film. The returned function
// releases it; callers defer it.
func (t *FilmTool) lock(key string) func() {
	v, _ := t.locks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// progress publishes at most one note per film every 15 seconds: enough to
// break a long silence, not enough to become noise.
func (t *FilmTool) progress(ctx context.Context, key, text string) {
	t.mu.Lock()
	fn := t.onProgress
	if t.progressAt == nil {
		t.progressAt = map[string]time.Time{}
	}
	if last, ok := t.progressAt[key]; ok && time.Since(last) < 15*time.Second {
		fn = nil
	}
	if fn != nil {
		t.progressAt[key] = time.Now()
	}
	t.mu.Unlock()
	if fn != nil {
		fn(ctx, text)
	}
}

func (t *FilmTool) Name() string { return "film" }

func (t *FilmTool) Description() string {
	return `Make hand-drawn animations, cartoons, motion films and drawings in code (beta): every drawing is JavaScript strokes on a canvas, rendered by the Pod to an MP4 (with sound if the film has a score) or to a still picture. A render is gated on measured contrast and coverage, so a film that would look blank is refused rather than shipped. Use it whenever the owner asks for an animation, a cartoon, a motion video, a flipbook, a drawn film, or to draw something. Draw in code unless they ask you to generate an image or photo. Not for explainers made from the owner's numbers or reports (that is the motion tool).

Read skills/hand-drawn-animation/SKILL.md first; it is the method (brief, drawings, timing, marks, QA). Then work like an animator, in this order:
1. new: start a project from a template (default "starter", a small animation; "still" for a drawing or illustration; also sketchbook-bird, four-looks, fly-style, one-year, film-template). It copies the engine beside the film file. Then edit the film's HTML with write_file/edit_file.
2. check: load the film in the Pod's browser and draw a spread of frames. Reports page errors with their message and line, blank frames and the film's length. Run it after every edit.
3. preview: grid (N evenly spaced frames), strip ("start,count" of consecutive frames, for a fast action or a contact) or frames (full-size PNGs). Then LOOK at the image with the vision tool and fix what is wrong: silhouette, spacing, contacts, line weight. Do not render a film you have not looked at.
4. render: the whole film to an MP4 in the background; the owner is told when it is in their chat. Render as soon as check is clean and a grid looks right, then keep refining and render again: a render can be redone, and running out of tool calls before the first render is the usual failure.
still: one frame as a full-size picture shown in the owner's chat: for a drawing, an illustration or a poster, drawn with strokes in code.
status: how a render is going.
photo: prepare a picture for the doodle look (drawings on photos): scale it, cut a plain backdrop away, register it in the film's photos.js and write a check sheet to look at. A film's page has no network and cannot load pictures from files, so photos are embedded this way.`
}

func (t *FilmTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"action", "project"},
		"properties": map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "enum": []string{"new", "check", "preview", "still", "render", "status", "photo"}},
			"project":  map[string]interface{}{"type": "string", "description": "The film's name. For new, its title (the folder is films/<slug>/). Afterwards the slug, the title or the folder."},
			"template": map[string]interface{}{"type": "string", "description": "new: starting point. starter (default), sketchbook-bird, four-looks, fly-style, one-year or film-template."},
			"modules":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "new: optional engine modules to include: sand, paper3d."},
			"image":    map[string]interface{}{"type": "string", "description": "photo: workspace path of the picture (an owner's upload, or a file you downloaded)."},
			"name":     map[string]interface{}{"type": "string", "description": "photo: short lowercase id the film uses for it, e.g. teapot."},
			"tol":      map[string]interface{}{"type": "integer", "description": "photo: how different from the backdrop a pixel must be to stay (default 34); raise it for a noisy backdrop."},
			"keep":     map[string]interface{}{"type": "boolean", "description": "photo: the picture already has transparency; only scale and register it."},
			"credit":   map[string]interface{}{"type": "string", "description": "photo: title, holder and licence, recorded with the photo."},
			"grid":     map[string]interface{}{"type": "integer", "description": "preview: a sheet of this many evenly spaced frames (default 24)."},
			"strip":    map[string]interface{}{"type": "string", "description": "preview: \"start,count\" consecutive frames, count up to 24."},
			"frames":   map[string]interface{}{"type": "string", "description": "preview: comma-separated frame numbers, saved as full-size PNGs."},
			"frame":    map[string]interface{}{"type": "integer", "description": "still: which frame to show (default 0)."},
			"look":     map[string]interface{}{"type": "string", "description": "ink, pencil, riso, screen or doodle, for films that support looks."},
			"ar":       map[string]interface{}{"type": "string", "description": "aspect ratio override, e.g. 16:9, 9:16, 1:1."},
			"width":    map[string]interface{}{"type": "integer", "description": "output width in pixels. Previews are quick at 960; render at the film's own width for delivery."},
		},
	}
}

// Rendering and preview each open a browser and can take a while.
func (t *FilmTool) Timeout() time.Duration { return 5 * time.Minute }

// look attaches workspace images (previews, stills) to a result so the model
// actually sees them: the tool's text cannot carry pixels. An image that is too
// large to attach is left out with a note, never silently.
func (t *FilmTool) look(res *ToolResult, caption string, rels ...string) *ToolResult {
	var skipped []string
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(t.workspace, filepath.FromSlash(rel)))
		if err != nil || len(b) == 0 {
			continue
		}
		if len(b) > maxResultImageBytes {
			skipped = append(skipped, rel)
			continue
		}
		res.Images = append(res.Images, ResultImage{Mime: http.DetectContentType(b), Data: b, Caption: caption + " (" + rel + ")"})
	}
	if len(skipped) > 0 {
		res.ForLLM += " (Too large to attach: " + strings.Join(skipped, ", ") + "; ask for a smaller width.)"
	}
	return res
}

// maxResultImageBytes bounds one attached image; base64 inflates it by a third.
const maxResultImageBytes = 1 << 20

func (t *FilmTool) options(args map[string]interface{}, action string) film.Options {
	o := film.Options{AR: strings.TrimSpace(sarg(args, "ar")), Look: strings.TrimSpace(sarg(args, "look"))}
	// Ghost lives on a phone. A still that defaults to a landscape frame is a
	// mismatch with the surface it arrives on, so a drawing is portrait unless
	// the film or the owner says otherwise.
	if o.AR == "" && action == "still" {
		o.AR = "9:16"
	}
	if n, ok := args["width"].(float64); ok && n > 0 {
		o.Width = int(n)
	}
	return o
}

func (t *FilmTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	project := strings.TrimSpace(sarg(args, "project"))
	action := sarg(args, "action")
	if action == "new" {
		var mods []string
		if l, ok := args["modules"].([]interface{}); ok {
			for _, m := range l {
				if s, ok := m.(string); ok {
					mods = append(mods, s)
				}
			}
		}
		unlock := t.lock("new:" + film.Slug(project))
		defer unlock()
		dir, file, err := film.New(t.workspace, project, strings.TrimSpace(sarg(args, "template")), mods)
		if err != nil {
			return ErrorResult("Couldn't start the film: " + err.Error())
		}
		return NewToolResult(fmt.Sprintf("Started %s. The film is %s; the engine (core.js, studio.js, cels.js, materials.js) sits beside it and is never edited per film. Do not read the engine files or the other references: this sheet is the API you need. Read %s once, then replace its subject with the brief using write_file (or edit_file), keeping the script tags and the defineFilm call, then run check.\n\n%s", dir, file, file, filmCheatSheet))
	}
	file, err := film.Resolve(t.workspace, project)
	if err != nil {
		return ErrorResult(err.Error())
	}
	// One film is worked on by one call at a time. A resent prompt used to
	// start a second pipeline that wrote the same frames and previews.
	unlock := t.lock(file)
	defer unlock()
	opts := t.options(args, action)
	if action == "photo" {
		o := film.PhotoOptions{Credit: strings.TrimSpace(sarg(args, "credit")), Keep: args["keep"] == true}
		if n, ok := args["tol"].(float64); ok {
			o.Tol = int(n)
		}
		sheet, w, h, err := film.AddPhoto(ctx, t.workspace, project, strings.TrimSpace(sarg(args, "image")), strings.TrimSpace(sarg(args, "name")), o)
		if err != nil {
			return ErrorResult("Couldn't prepare that photo: " + err.Error())
		}
		return t.look(NewToolResult(fmt.Sprintf("Registered %q (%dx%d) in photos.js beside the film; load it with <script src=\"photos.js\"></script> after core.js. The check sheet %s is attached: the object must be whole on the magenta, with no halo, and handles empty inside. The grid cells are the u,v anchor units (0 to 1 across and down). If the backdrop is left or the object is eaten, run photo again with a different tol.", sarg(args, "name"), w, h, sheet)), "Photo check sheet", sheet)
	}

	switch action {
	case "check":
		t.progress(ctx, file, "Looking at the film now — drawing a spread of frames to check it.")
		r, err := film.Check(ctx, t.workspace, file, opts)
		if err != nil {
			return ErrorResult("The film doesn't run yet: " + err.Error() + "\nFix it in " + file + " and run check again.")
		}
		msg := fmt.Sprintf("%s loads and draws: %d frames at %d fps (%.1f s), %dx%d. Drew frames %v without a script error.", file, r.Meta.Frames, r.Meta.FPS, r.Meta.Seconds(), r.Meta.OutW(), r.Meta.OutH(), r.Sample)
		if q := r.Worst(); q.InkPct > 0 || q.Contrast > 0 || q.DrawnCells > 0 {
			msg += fmt.Sprintf(" Measured: visible marks cover %.1f%% of the frame, in %d of %d areas; tonal range %.2f.", q.CoveragePct, q.DrawnCells, 36, q.Contrast)
		}
		if len(r.Notes) > 0 {
			msg += "\nFix before going on:\n- " + strings.Join(r.Notes, "\n- ")
		} else {
			msg += "\nNext: preview a grid and look at it with the vision tool; then a strip around the hardest action."
		}
		return NewToolResult(msg)

	case "preview":
		t.progress(ctx, file, "Drawing a preview of the film now.")
		if s := strings.TrimSpace(sarg(args, "strip")); s != "" {
			a, b, ok := strings.Cut(s, ",")
			start, e1 := strconv.Atoi(strings.TrimSpace(a))
			count, e2 := strconv.Atoi(strings.TrimSpace(b))
			if !ok || e1 != nil || e2 != nil || start < 0 || count < 1 || count > 24 {
				return ErrorResult(`strip is "start,count" with count 1 to 24, for example "48,12"`)
			}
			rel, err := film.Strip(ctx, t.workspace, file, opts, start, count)
			if err != nil {
				return ErrorResult(err.Error())
			}
			return t.look(NewToolResult(fmt.Sprintf("Wrote %d consecutive frames from %d to %s; the picture is attached. Look at it: check spacing, arcs, contacts and line stability from frame to frame.", count, start, rel)), "Consecutive frames", rel)
		}
		if fr := strings.TrimSpace(sarg(args, "frames")); fr != "" {
			var list []int
			for _, f := range strings.Split(fr, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(f))
				if err != nil {
					return ErrorResult("frames is a comma-separated list of frame numbers, for example 0,37,74")
				}
				list = append(list, n)
			}
			if len(list) > 12 {
				return ErrorResult("ask for at most 12 frames at a time")
			}
			paths, _, err := film.Frames(ctx, t.workspace, file, opts, list)
			if err != nil {
				return ErrorResult(err.Error())
			}
			if len(paths) > 4 {
				return NewToolResult("Wrote full-size frames: " + strings.Join(paths, ", ") + ". Ask for at most 4 at a time if you want to look at them here.")
			}
			return t.look(NewToolResult("Wrote full-size frames: "+strings.Join(paths, ", ")+"; the pictures are attached. Look at them."), "Frame", paths...)
		}
		n := 24
		if g, ok := args["grid"].(float64); ok && g >= 1 && g <= 120 {
			n = int(g)
		}
		rel, err := film.Grid(ctx, t.workspace, file, opts, n)
		if err != nil {
			return ErrorResult(err.Error())
		}
		return t.look(NewToolResult(fmt.Sprintf("Wrote a sheet of %d evenly spaced frames to %s; the picture is attached. Look at it: is the story readable, is anything blank or clipped, does the action have a clear start and end?", n, rel)), "Contact sheet", rel)

	case "still":
		f := 0
		if v, ok := args["frame"].(float64); ok {
			f = int(v)
		}
		paths, meta, err := film.Frames(ctx, t.workspace, file, opts, []int{f})
		if err != nil {
			return ErrorResult(err.Error())
		}
		// A still is looked at before it is shown: the same measurement that
		// gates a render says whether this picture will read on a phone.
		qualityNote := ""
		if q, err := film.AnalyzeFile(paths[0]); err == nil && len(q.Notes()) > 0 {
			qualityNote = " Measured: " + strings.Join(q.Notes(), "; ") + "."
		}
		t.mu.Lock()
		pub := t.pub
		t.mu.Unlock()
		if pub == nil {
			return NewToolResult(fmt.Sprintf("Drew frame %d at %dx%d to %s, but the chat can't show it right now.", f, meta.OutW(), meta.OutH(), paths[0]))
		}
		a, err := pub.Publish(artifacts.Input{SessionKey: SessionKeyFromContext(ctx), Kind: "file", Title: film.Name(file), Summary: "Drawing", Path: paths[0]})
		if err != nil {
			return ErrorResult("Drew it, but it couldn't be shown in the chat: " + err.Error())
		}
		return t.look(&ToolResult{ForLLM: fmt.Sprintf("Showing the drawing (%dx%d, artifact %s) in the owner's chat, and attached here so you can check it. Say in a sentence what it shows; to change it, edit %s and call still again.%s", meta.OutW(), meta.OutH(), a.ID, file, qualityNote), Silent: true}, "The drawing the owner sees", paths[0])

	case "render":
		title := film.Name(file)
		session := SessionKeyFromContext(ctx)
		t.progress(ctx, file, fmt.Sprintf("Rendering %s now — I'll tell you here when the video is ready.", title))
		j := t.jobs.Start(t.workspace, file, opts, func(j film.Job) {
			t.finish(session, title, j)
		})
		if j.State == "queued" || j.State == "rendering" {
			return NewToolResult(fmt.Sprintf("Rendering %s on the Pod now. The owner will be told when the video is ready; say so in a sentence and don't wait for it.", file))
		}
		return NewToolResult(fmt.Sprintf("Render of %s: %s.", file, j.State))

	case "status":
		j := t.jobs.Status(file)
		switch j.State {
		case "none":
			return NewToolResult("No render of " + file + " has been started.")
		case "failed":
			return NewToolResult("The render failed: " + j.Error)
		case "done":
			return NewToolResult(fmt.Sprintf("Done: %s (%.1f s, sound=%v).", j.Video, j.Seconds, j.Audio))
		}
		return NewToolResult(fmt.Sprintf("%s: %d of %d frames.", j.State, j.Done, j.Total))
	}
	return ErrorResult("action is new, check, preview, still, render or status")
}

// finish puts a finished render in the conversation and tells the owner.
func (t *FilmTool) finish(session, title string, j film.Job) {
	t.mu.Lock()
	pub, fn := t.pub, t.onVideo
	t.mu.Unlock()
	if j.State == "done" && pub != nil {
		_, _ = pub.Publish(artifacts.Input{SessionKey: session, Kind: "file", Title: title, Summary: fmt.Sprintf("Film · %.0f s", j.Seconds), Path: j.Video})
		if j.Contact != "" {
			_, _ = pub.Publish(artifacts.Input{SessionKey: session, Kind: "file", Title: title + " (contact sheet)", Summary: "Every half second of the film", Path: j.Contact})
		}
	}
	if fn != nil {
		fn(context.Background(), title, j)
	}
}
