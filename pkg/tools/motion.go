package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/motion"
)

// MotionTool lets Ghost turn a report, numbers or a walkthrough into a short
// animated explainer: a spec of scenes the phone plays and the Pod renders to
// an MP4. Each change is the next version, so the owner can change any word,
// number or timing and make the video again.
type MotionTool struct {
	workspace string
	exports   *motion.Exports
	mu        sync.Mutex
	pub       CanvasPublisher
	// onVideo tells the owner a video is ready (or failed), in their conversation.
	onVideo func(ctx context.Context, title string, j motion.Job)
}

func NewMotionTool(workspace string, exports *motion.Exports) *MotionTool {
	return &MotionTool{workspace: workspace, exports: exports}
}

// SetPublisher wires the artifact store once it exists.
func (t *MotionTool) SetPublisher(p CanvasPublisher) { t.mu.Lock(); t.pub = p; t.mu.Unlock() }

// OnVideo sets what happens when a video is made.
func (t *MotionTool) OnVideo(fn func(ctx context.Context, title string, j motion.Job)) {
	t.mu.Lock()
	t.onVideo = fn
	t.mu.Unlock()
}

func (t *MotionTool) Name() string { return "motion" }

func (t *MotionTool) Description() string {
	return `Make a short animated explainer (a motion) from a report, numbers, a chart or a walkthrough, for the owner to watch, change and export as an MP4 video with sound. It is written as scenes, not filmed: no video model, every word, number and timing can be changed. Use it when they ask to animate something, make a video or explainer of their data, a short clip for a presentation or social media, or to explain steps visually. Use real numbers only (from what they gave you, a file, their finances, a dashboard query); never invent data. Every number has a source or it goes.

Captions are opt-in: a clean picture unless the owner asks for captions or subtitles. Only then set spec captions to "on" (voice/source line burns on screen and a .srt ships beside the MP4). Never turn captions on unasked.

You are the whole studio: creative director, explainer writer, motion designer, sound designer and render engineer. When the brief is thin, make the call and say what you assumed in one sentence; keep moving.

Work shape-before-polish:
- Settle the one idea and the viewer's mental model first; polish is the last pass.
- Ask before designing: who is this for, what are we communicating, does it need a name or can it stay invisible?
- Never start from nothing: anchor to their brand, product and real screenshots/data, or it looks generic.
- Restraint is taste: cut whatever does not serve the idea. Banned look: neon glow, stock 3D blobs, gradient title cards, floating particles, fake metrics.

Discovery (do this first, in one pass):
- Write the one question the film answers, in the viewer's own words (spec question).
- Split facts into must-know and cut list; the cut list stays out.
- Find the hook: the wrong belief most viewers hold (spec hook). Open on it as an image, not a title card.
- Metaphor only if it makes the idea more accurate; otherwise show the real thing.

Story arc (map scenes to it; 15-40 s is usually right):
- Question 0-10%: the thing people get wrong, as an image.
- Model 10-35%: smallest correct picture of how it works, one element per beat.
- Proof 35-75%: run the model on a real case; cause and effect play out on screen.
- Turn 75-90%: change one variable, show what follows.
- Payoff 90-100%: opening image again, now read correctly, plus the next step.

Motion language:
- Motion carries meaning (grouping, order, cause, scale). One focal action at a time: set focus:true on it; everything else holds still and dims.
- Keep objects alive across scenes with the same element id (a dot becomes a node, a label becomes an axis) instead of replacing them.
- Easing settles without bounce; the camera reframes between ideas and stays still while text is read.
- Prefer real UI, real data and clean diagrams over illustration.

Copy:
- On-screen words are anchors (at most 3 lines, 12 words a line); the sentence lives in voice.
- Max two ideas on screen at once; plain words before jargon; one name per thing, used every time.
- Never put the full voice line on screen. Every data scene carries a caption with its source.

Sound and access:
- Every scene may carry a voice line for when captions are on; with captions on they burn on screen and ship as .srt, and the video muxes a soft procedural bed with ticks only where the scene names a sound. With captions off the picture stays clean (the bed still muxes; it carries no words).
- Keep contrast high, never colour-only (the lead bar/step is also labelled/lit in text), no flashes; reduced-motion (?reduced=1) keeps the same ideas with hard cuts.

action:
- make: title, spec. The spec is JSON: {"title", "size": "landscape" (16:9, default) | "portrait" (phone/story) | "square", "accent": "#RRGGBB" (one leading colour), "question"?, "audience"?, "outcome"?, "hook"?, "sources"?: ["outlet or file, date"], "captions"?: "on" (only when they ask for captions or subtitles; otherwise leave it out for a clean picture), "scenes": [{"id"?: "q|model-1|proof-1|turn|payoff", "duration": seconds 1-20, "teaches"?: "the one thing this scene adds (required with question)", "voice"?: "spoken line, at most 48 words", "sound"?: "cue marking the key action", "check"?: "what the viewer can now say", "caption"?: "source or date at the foot (required on data scenes in studio mode)", "elements": [...]}]}. At most 12 scenes, 90 seconds in all, 6 elements a scene, one focus:true at most per scene. Elements (each may have "id" to persist across scenes, "at": seconds in when it enters, and "stay"):
  {"type":"title","text","sub"?} · {"type":"text","text"} · {"type":"quote","text","sub"?: who}
  {"type":"number","from","to","prefix"? ("KES "),"suffix"? ("%"),"decimals"?,"label"?}: counts up
  {"type":"bars","labels":[...],"values":[...],"unit"?,"label"?}: horizontal bars growing in turn, the biggest lit
  {"type":"line","labels":[first,…,last],"values":[...],"unit"?,"label"?}: a line drawing itself
  {"type":"donut","labels","values","label"?}: shares of a whole
  {"type":"compare","labels":[a,b],"values":[a,b],"prefix"?,"label"?}: before and after
  {"type":"list","items":[...],"label"?} · {"type":"steps","items":[...],"label"?}: a walkthrough, each step lit in turn
  {"type":"flow","items":[2-6 short boxes],"loop"?:true,"label"?}: a process or chain (A → B → C), arrows drawing in turn; loop returns to the start
  {"type":"sketch","items":[2-6 short boxes],"loop"?:true,"label"?}: like flow but hand-drawn for processes with character; prefer it when the subject has a physical shape or story beat
  {"type":"hub","text":"the centre","items":[2-6 parts],"label"?}: one thing and what connects to it
  {"type":"timeline","items":[2-8 milestones],"label"?}: lit in turn with a travelling marker
  {"type":"table","labels":[2-4 headers],"items":["cell | cell | cell", ...]}: one row per item, cells split by |
  {"type":"gauge","to":value,"max"?:100,"suffix"?:"%","label"?}: a ring filling to a share or score
  Use flow and hub for diagrams. Make it about THIS subject, never generic: pick the diagram the subject asks for (a match gets a timeline + table, a gut gets a sketch, a system gets a hub), open on the hook image, close on it re-read, and put the source on screen for researched numbers.
- change: title, spec: the whole updated spec, saved as the next version (read it first with show).
- show: title → the current spec, to change it.
- verify: title → run the studio gate (deterministic frames, cue at every beat and 390 px lines when captions are on, muted/audio-only parity with them, sources, one focus, no flashes) and report what failed.
- captions: title → write the .srt beside the motion and say where it is (an explicit ask counts as opting in, even when the spec leaves captions off).
- export: title → makes the MP4 on the Pod (about three times its length) with bed + .srt beside it only when captions are on; the owner is told when it is ready, and it appears in the chat and on their shelf.
Do not paste the spec into your reply; say in a sentence what you made, what it teaches, and what you assumed.`
}

func (t *MotionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"action", "title"},
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"make", "change", "show", "verify", "captions", "export"}},
			"title":  map[string]interface{}{"type": "string"},
			"spec":   map[string]interface{}{"type": "object", "description": "The motion spec (make, change)."},
		},
	}
}

func (t *MotionTool) Timeout() time.Duration { return 30 * time.Second }

func (t *MotionTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	title := strings.TrimSpace(sarg(args, "title"))
	switch sarg(args, "action") {
	case "make", "change":
		raw, err := json.Marshal(args["spec"])
		if err != nil || args["spec"] == nil {
			if s, ok := args["spec"].(string); ok {
				raw = []byte(s)
			} else {
				return ErrorResult("spec is required: the motion as JSON")
			}
		}
		if s, ok := args["spec"].(string); ok {
			raw = []byte(s)
		}
		spec, err := motion.Parse(raw)
		if err != nil {
			return ErrorResult("That motion can't be made: " + err.Error() + ". Fix it and call again.")
		}
		if title != "" {
			spec.Title = title
		}
		rel, version, err := motion.Save(t.workspace, spec)
		if err != nil {
			return ErrorResult("Couldn't save it: " + err.Error())
		}
		t.mu.Lock()
		pub := t.pub
		t.mu.Unlock()
		if pub == nil {
			return NewToolResult(fmt.Sprintf("Saved %q (version %d) but the chat can't show it right now.", spec.Title, version))
		}
		a, err := pub.Publish(artifacts.Input{SessionKey: SessionKeyFromContext(ctx), Kind: "file", Title: spec.Title,
			Summary: fmt.Sprintf("Motion · %.0f s · version %d", spec.Duration(), version), Path: rel})
		if err != nil {
			return ErrorResult("Saved, but it couldn't be shown in the chat: " + err.Error())
		}
		return &ToolResult{ForLLM: fmt.Sprintf("Showing %q (version %d, %.0f seconds, artifact %s) in the owner's chat; it plays there, and they can change any word, number or timing on it or export the video. Say in a sentence what it shows.", spec.Title, version, spec.Duration(), a.ID), Silent: true}
	case "show":
		rel, ok := motion.Latest(t.workspace, title)
		if !ok {
			return ErrorResult(fmt.Sprintf("No motion called %q.", title))
		}
		s, err := motion.Load(t.workspace, rel)
		if err != nil {
			return ErrorResult(err.Error())
		}
		b, _ := json.MarshalIndent(s, "", " ")
		return NewToolResult(string(b))
	case "verify":
		rel, ok := motion.Latest(t.workspace, title)
		if !ok {
			return ErrorResult(fmt.Sprintf("No motion called %q.", title))
		}
		s, err := motion.Load(t.workspace, rel)
		if err != nil {
			return ErrorResult(err.Error())
		}
		v := s.Verify()
		if v.OK {
			return NewToolResult(fmt.Sprintf("%q verifies: %s.", title, strings.Join(v.Checks, "; ")))
		}
		return NewToolResult(fmt.Sprintf("%q does not verify yet. Failed: %s. Passed: %s. Fix the spec with change and verify again.", title, strings.Join(v.Failed, "; "), strings.Join(v.Checks, "; ")))
	case "captions":
		rel, ok := motion.Latest(t.workspace, title)
		if !ok {
			return ErrorResult(fmt.Sprintf("No motion called %q.", title))
		}
		crel, err := motion.WriteCaptions(t.workspace, rel)
		if err != nil {
			return ErrorResult("Couldn't write its captions: " + err.Error())
		}
		return NewToolResult(fmt.Sprintf("Captions for %q are at %s (%d cues). They burn on screen and ship beside the MP4.", title, crel, len(mustLoadCues(t.workspace, rel))))
	case "export":
		rel, ok := motion.Latest(t.workspace, title)
		if !ok {
			return ErrorResult(fmt.Sprintf("No motion called %q.", title))
		}
		t.mu.Lock()
		fn := t.onVideo
		t.mu.Unlock()
		session := SessionKeyFromContext(ctx)
		j, err := t.exports.Start(t.workspace, rel, func(j motion.Job) {
			t.publishVideo(session, title, j)
			if fn != nil {
				fn(context.Background(), title, j)
			}
		})
		if err != nil {
			return ErrorResult(err.Error())
		}
		if j.State == "done" {
			return NewToolResult(fmt.Sprintf("The video of %q is already made (%s).", title, j.Video))
		}
		return NewToolResult(fmt.Sprintf("Making the video of %q on the Pod now (%d frames). The owner will be told when it's ready; say so in a sentence.", title, j.Total))
	}
	return ErrorResult("action is make, change, show, verify, captions or export")
}

func mustLoadCues(workspace, rel string) []motion.Cue {
	s, err := motion.Load(workspace, rel)
	if err != nil {
		return nil
	}
	return s.Cues()
}

// publishVideo puts a finished video in the conversation and on the shelf.
func (t *MotionTool) publishVideo(session, title string, j motion.Job) {
	if j.State != "done" {
		return
	}
	t.mu.Lock()
	pub := t.pub
	t.mu.Unlock()
	if pub == nil {
		return
	}
	_, _ = pub.Publish(artifacts.Input{SessionKey: session, Kind: "file", Title: title, Summary: "Video", Path: j.Video})
}
