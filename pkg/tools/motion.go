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
	return `Make a short animated explainer (a motion) from a report, numbers, a chart or a walkthrough, for the owner to watch, change and export as an MP4 video. It is written as scenes, not filmed: no video model, every word, number and timing can be changed. Use it when they ask to animate something, make a video or explainer of their data, a short clip for a presentation or social media, or to explain steps visually. Use real numbers only (from what they gave you, a file, their finances, a dashboard query); never invent data.

action:
- make: title, spec. The spec is JSON: {"title", "size": "landscape" (16:9, the default) | "portrait" (9:16, only when they want it for a phone or a story) | "square", "accent": "#RRGGBB" (one leading colour, optional), "scenes": [{"duration": seconds 1-20, "caption"?: a source or date at the foot, "elements": [...]}]}. At most 12 scenes, 90 seconds in all, 6 elements a scene. Elements (each may have "at": seconds into its scene when it enters, and "stay"):
  {"type":"title","text","sub"?} · {"type":"text","text"} · {"type":"quote","text","sub"?: who}
  {"type":"number","from","to","prefix"? ("KES "),"suffix"? ("%"),"decimals"?,"label"?}: counts up
  {"type":"bars","labels":[...],"values":[...],"unit"?,"label"?}: horizontal bars growing in turn, the biggest lit
  {"type":"line","labels":[first,…,last],"values":[...],"unit"?,"label"?}: a line drawing itself
  {"type":"donut","labels","values","label"?}: shares of a whole
  {"type":"compare","labels":[a,b],"values":[a,b],"prefix"?,"label"?}: before and after
  {"type":"list","items":[...],"label"?} · {"type":"steps","items":[...],"label"?}: a walkthrough, each step lit in turn
  {"type":"flow","items":[2-6 short boxes],"loop"?:true,"label"?}: a diagram of a process or a chain (A → B → C), arrows drawing in turn; loop returns to the start
  {"type":"hub","text":"the centre","items":[2-6 parts],"label"?}: a diagram of one thing and what connects to it (your Pod and the phone, web and terminal around it)
  {"type":"gauge","to":value,"max"?:100,"suffix"?:"%","label"?}: a ring filling to a share or a score
  Use flow and hub for diagrams: they are the motion's diagrams.
  Good motions are short (15-40 s), one idea per scene, a title first, few words on screen, the number that matters big.
- change: title, spec: the whole updated spec, saved as the next version (read it first with show).
- show: title → the current spec, to change it.
- export: title → makes the MP4 on the Pod (about three times its length); the owner is told when it is ready, and it appears in the chat and on their shelf.
Do not paste the spec into your reply; say in a sentence what you made.`
}

func (t *MotionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"action", "title"},
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"make", "change", "show", "export"}},
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
	return ErrorResult("action is make, change, show or export")
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
