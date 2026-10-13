// Package motion turns reports, numbers and walkthroughs into short animated
// explainers that are code, not footage: a motion is a small declarative spec
// (scenes of titles, counting numbers, bars, lines, lists and steps, each with
// its own timing) that one deterministic player draws at any moment t. The
// same player previews it on the phone and renders it, frame by frame, into an
// MP4 on the Pod, so any word, number or timing can be changed and the video
// made again. No video model is involved.
package motion

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Spec is one motion.
type Spec struct {
	Title string `json:"title"`
	// Size: portrait (9:16, for phones and stories), landscape (16:9) or square.
	Size string `json:"size,omitempty"`
	// Accent is the one colour that leads (#RRGGBB); the rest is the dark ground.
	Accent string `json:"accent,omitempty"`
	// Story is the explainer brief (Choi studio method). Optional, so older
	// motions keep playing; when present, the arc and copy rules apply.
	// Question is the one question the film answers, in the viewer's words.
	// Audience is who it is for; Outcome is what they can now say or do.
	// Hook is the wrong belief most viewers hold, opened on as an image.
	Question string `json:"question,omitempty"`
	Audience string `json:"audience,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Hook     string `json:"hook,omitempty"`
	// Sources lists where every number came from (SOURCES.md lines). When a
	// scene shows data, it must also carry a Caption naming its source.
	Sources []string `json:"sources,omitempty"`
	// Captions is "on" only when the owner asked for captions: the voice or
	// source line burns on screen and a .srt ships beside the MP4. Anything
	// else (including unset) means a clean picture — no burned text, no .srt.
	Captions string `json:"captions,omitempty"`
	Scenes   []Scene `json:"scenes"`
}

// Scene is one beat, shown for Duration seconds.
type Scene struct {
	Duration float64 `json:"duration"`
	// ID is a stable beat id (q, model-1, proof-2, turn, payoff). Optional.
	ID string `json:"id,omitempty"`
	// Teaches is the one thing this scene adds. Required when the spec
	// carries a Question (the studio arc); otherwise optional.
	Teaches string `json:"teaches,omitempty"`
	// Voice is the spoken line for this beat (captions + audio-only check).
	// Never put the full voice line on screen; on-screen words are anchors.
	Voice string `json:"voice,omitempty"`
	// Sound names the cue that marks the key action (e.g. "soft tick on
	// third bar", "motif returns"). Silent when empty.
	Sound string `json:"sound,omitempty"`
	// Check is what the viewer can now say that they could not before.
	Check    string    `json:"check,omitempty"`
	Elements []Element `json:"elements"`
	// Caption is an optional line at the foot of the scene (a source, a date).
	Caption string `json:"caption,omitempty"`
}

// Element is one thing on a scene. Which fields matter depends on Type:
//
//	title   Text (and Sub, a smaller line under it)
//	text    Text
//	number  From → To counting up, with Prefix/Suffix and Decimals; Label under it
//	bars    Labels + Values (horizontal bars that grow in turn); Unit
//	line    Labels + Values (a line that draws itself); Unit
//	donut   Labels + Values (shares of a whole)
//	list    Items, appearing one after another
//	steps   Items as numbered steps (a walkthrough), each lit in turn
//	compare Labels (two) + Values (two): before and after, this and that
//	quote   Text, and Sub for who said it
//	flow    Items as boxes joined by arrows, drawn in turn (Loop: the last
//	        arrow returns to the first): a process, a chain of cause and effect
//	hub     Text at the centre, Items around it, joined by spokes: one thing
//	        and the parts that connect to it
//	gauge   To out of Max (100 if unset) as a ring that fills; Suffix, Label
//	timeline Items as milestones on a line, lit in turn with a travelling
//	        marker: a sequence over time or over steps of a story
//	table   Labels as column headers (2 to 4), Items as rows with cells split
//	        by " | ": a small fact grid, rows appearing in turn
//	sketch  Items as boxes (2 to 6, Loop allowed) joined by arrows like a
//	        flow, but drawn hand: wobbling strokes that draw themselves, for
//	        processes with character (a formation, a gut, a route)
//
// At is when it enters, in seconds from the start of its scene; Stay, when set,
// is how long it stays (otherwise to the end of the scene).
type Element struct {
	Type string `json:"type"`
	// ID keeps an object alive across scenes: an element with the same ID in
	// the next scene transforms instead of being replaced (a dot becomes a
	// node, a label becomes an axis). Optional.
	ID       string  `json:"id,omitempty"`
	// Focus marks the one focal action: when set, everything else in the
	// scene holds still and dims while this moves. At most one per scene.
	Focus    bool    `json:"focus,omitempty"`
	Text     string  `json:"text,omitempty"`
	Sub      string  `json:"sub,omitempty"`
	Label    string  `json:"label,omitempty"`
	From     float64 `json:"from,omitempty"`
	To       float64 `json:"to,omitempty"`
	Prefix   string  `json:"prefix,omitempty"`
	Suffix   string  `json:"suffix,omitempty"`
	Decimals int     `json:"decimals,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Labels   []string  `json:"labels,omitempty"`
	Values   []float64 `json:"values,omitempty"`
	Items    []string  `json:"items,omitempty"`
	Loop     bool      `json:"loop,omitempty"`
	Max      float64   `json:"max,omitempty"`
	At       float64   `json:"at,omitempty"`
	Stay     float64   `json:"stay,omitempty"`
}

// Limits keep a motion short enough to watch and to render on a small Pod.
const (
	MaxScenes   = 12
	MaxDuration = 90.0 // seconds in all
	MaxElements = 6    // per scene
	MaxPoints   = 24
	MaxItems    = 8
)

var (
	hexRE        = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	idRE         = regexp.MustCompile(`^[a-z0-9-]+$`)
	elementTypes = map[string]bool{"title": true, "text": true, "number": true, "bars": true, "line": true, "donut": true, "list": true, "steps": true, "compare": true, "quote": true, "flow": true, "hub": true, "gauge": true, "timeline": true, "table": true, "sketch": true}
)

// Parse reads a spec and checks it.
func Parse(b []byte) (Spec, error) {
	var s Spec
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("not a motion spec: %w", err)
	}
	return s, s.Check()
}

// Check validates the spec in place, filling defaults.
func (s *Spec) Check() error {
	s.Title = strings.TrimSpace(s.Title)
	if s.Title == "" || len([]rune(s.Title)) > 80 {
		return errors.New("a motion needs a title of at most 80 characters")
	}
	for _, f := range []*string{&s.Question, &s.Audience, &s.Outcome, &s.Hook} {
		*f = strings.TrimSpace(*f)
	}
	if len([]rune(s.Question)) > 140 || len([]rune(s.Audience)) > 120 || len([]rune(s.Outcome)) > 140 || len([]rune(s.Hook)) > 140 {
		return errors.New("question, audience, outcome and hook are each one short line (at most 140 characters)")
	}
	for i := range s.Sources {
		s.Sources[i] = strings.TrimSpace(s.Sources[i])
		if len([]rune(s.Sources[i])) > 160 {
			return errors.New("a source is one short line (at most 160 characters)")
		}
		if s.Sources[i] == "" {
			return errors.New("a source is never empty; drop it instead")
		}
	}
	if len(s.Sources) > 12 {
		return errors.New("at most 12 sources; one per data scene is plenty")
	}
	switch s.Captions {
	case "", "on", "off":
	default:
		return fmt.Errorf("captions is \"on\" or \"off\", not %q", s.Captions)
	}
	switch s.Size {
	case "":
		s.Size = "landscape"
	case "portrait", "landscape", "square":
	default:
		return fmt.Errorf("size is portrait, landscape or square, not %q", s.Size)
	}
	if s.Accent == "" {
		s.Accent = "#9C95FF"
	}
	if !hexRE.MatchString(s.Accent) {
		return fmt.Errorf("accent is a colour like #9C95FF, not %q", s.Accent)
	}
	if len(s.Scenes) == 0 || len(s.Scenes) > MaxScenes {
		return fmt.Errorf("a motion has 1 to %d scenes", MaxScenes)
	}
	// A studio arc needs room: question, model, proof, turn, payoff.
	if s.Question != "" && len(s.Scenes) < 3 {
		return errors.New("a motion with a question needs at least 3 scenes (open it, show it, close it)")
	}
	total := 0.0
	seenIDs := map[string]bool{}
	for i := range s.Scenes {
		sc := &s.Scenes[i]
		if sc.Duration < 1 || sc.Duration > 20 || math.IsNaN(sc.Duration) {
			return fmt.Errorf("scene %d: a scene lasts 1 to 20 seconds", i+1)
		}
		total += sc.Duration
		sc.ID = strings.TrimSpace(sc.ID)
		sc.Teaches = strings.TrimSpace(sc.Teaches)
		sc.Voice = strings.TrimSpace(sc.Voice)
		sc.Sound = strings.TrimSpace(sc.Sound)
		sc.Check = strings.TrimSpace(sc.Check)
		sc.Caption = strings.TrimSpace(sc.Caption)
		if len(sc.ID) > 24 {
			return fmt.Errorf("scene %d: id is at most 24 characters", i+1)
		}
		if sc.ID != "" {
			if seenIDs[sc.ID] {
				return fmt.Errorf("scene %d: id %q is used twice", i+1, sc.ID)
			}
			seenIDs[sc.ID] = true
		}
		if s.Question != "" && sc.Teaches == "" {
			return fmt.Errorf("scene %d: with a question set, every scene says what it teaches (one line)", i+1)
		}
		if len([]rune(sc.Teaches)) > 140 || len([]rune(sc.Voice)) > 280 || len([]rune(sc.Sound)) > 120 || len([]rune(sc.Check)) > 140 {
			return fmt.Errorf("scene %d: teaches/check are at most 140, voice 280, sound 120 characters", i+1)
		}
		if sc.Voice != "" {
			if n := len(strings.Fields(sc.Voice)); n > 48 {
				return fmt.Errorf("scene %d: the voice line is spoken, so at most 48 words (has %d)", i+1, n)
			}
		}
		if len([]rune(sc.Caption)) > 120 {
			return fmt.Errorf("scene %d: the caption is longer than 120 characters", i+1)
		}
		if len(sc.Elements) == 0 || len(sc.Elements) > MaxElements {
			return fmt.Errorf("scene %d: a scene has 1 to %d elements", i+1, MaxElements)
		}
		focused := 0
		for j := range sc.Elements {
			if err := sc.Elements[j].check(sc.Duration); err != nil {
				return fmt.Errorf("scene %d, element %d: %w", i+1, j+1, err)
			}
			if sc.Elements[j].Focus {
				focused++
			}
		}
		if focused > 1 {
			return fmt.Errorf("scene %d: one focal action at a time (one focus:true at most)", i+1)
		}
		// Every number has a source or it goes: a studio data scene names it.
		// Older motions without a question keep playing as they did.
		if s.Question != "" && hasData(sc) && sc.Caption == "" && len(s.Sources) == 0 {
			return fmt.Errorf("scene %d: it shows numbers, so it needs a caption with the source (or spec sources)", i+1)
		}
	}
	if total > MaxDuration {
		return fmt.Errorf("a motion is at most %.0f seconds; this one is %.0f", MaxDuration, total)
	}
	return nil
}

// hasData reports whether a scene shows numbers that need a source.
func hasData(sc *Scene) bool {
	for _, e := range sc.Elements {
		switch e.Type {
		case "number", "bars", "line", "donut", "compare", "gauge", "table":
			return true
		}
	}
	return false
}

func (e *Element) check(sceneDur float64) error {
	if e.Type != "title" && e.Type != "text" && e.Type != "number" && e.Type != "bars" && e.Type != "line" && e.Type != "donut" && e.Type != "list" && e.Type != "steps" && e.Type != "compare" && e.Type != "quote" && e.Type != "flow" && e.Type != "hub" && e.Type != "gauge" && e.Type != "timeline" && e.Type != "table" && e.Type != "sketch" {
		return fmt.Errorf("type is title, text, number, bars, line, donut, list, steps, compare, quote, flow, hub, gauge, timeline, table or sketch, not %q", e.Type)
	}
	e.ID = strings.TrimSpace(e.ID)
	if len(e.ID) > 32 {
		return errors.New("id is at most 32 characters (a-z, 0-9, -)")
	}
	if e.ID != "" && !idRE.MatchString(e.ID) {
		return fmt.Errorf("id is lowercase letters, numbers and -, not %q", e.ID)
	}
	// A prefix or suffix keeps its spacing ("KES " before, " sold" after).
	for _, f := range []*string{&e.Text, &e.Sub, &e.Label, &e.Unit} {
		*f = strings.TrimSpace(*f)
	}
	if len([]rune(e.Text)) > 220 || len([]rune(e.Sub)) > 140 || len([]rune(e.Label)) > 80 || len(e.Prefix) > 12 || len(e.Suffix) > 12 || len(e.Unit) > 12 {
		return errors.New("a word field is too long")
	}
	// On-screen words are anchors, never paragraphs: at most 3 lines, each
	// line at most 12 words. The voice line carries the sentence.
	for _, field := range []string{e.Text, e.Sub, e.Label} {
		if field == "" {
			continue
		}
		lines := strings.Split(field, "\n")
		if len(lines) > 3 {
			return errors.New("on-screen text is at most 3 lines; the rest belongs in voice")
		}
		for _, ln := range lines {
			if n := len(strings.Fields(ln)); n > 12 {
				return fmt.Errorf("on-screen text is at most 12 words a line (has %d): %q", n, ln)
			}
		}
	}
	if len([]rune(e.Text)) > 220 || len([]rune(e.Sub)) > 140 || len([]rune(e.Label)) > 80 || len(e.Prefix) > 12 || len(e.Suffix) > 12 || len(e.Unit) > 12 {
		return errors.New("a word field is too long")
	}
	if e.At < 0 || e.At >= sceneDur || e.Stay < 0 {
		return fmt.Errorf("it enters at %.1fs, which is outside its %.1fs scene", e.At, sceneDur)
	}
	if e.Decimals < 0 || e.Decimals > 3 {
		return errors.New("decimals is 0 to 3")
	}
	for _, v := range append([]float64{e.From, e.To, e.Max}, e.Values...) {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e15 {
			return errors.New("a number is out of range")
		}
	}
	if len(e.Labels) > MaxPoints || len(e.Values) > MaxPoints || len(e.Items) > MaxItems {
		return fmt.Errorf("at most %d points and %d items", MaxPoints, MaxItems)
	}
	for _, l := range append(append([]string{}, e.Labels...), e.Items...) {
		if len([]rune(l)) > 120 {
			return errors.New("a label or item is longer than 120 characters")
		}
	}
	switch e.Type {
	case "title", "text", "quote":
		if e.Text == "" {
			return fmt.Errorf("a %s needs text", e.Type)
		}
	case "line":
		// A line is labelled at its ends (and between, if given), not at every point.
		if len(e.Values) < 2 || len(e.Labels) < 2 || len(e.Labels) > len(e.Values) {
			return errors.New("a line needs at least two values and two labels (its first and last)")
		}
	case "bars", "donut":
		if len(e.Values) < 2 || len(e.Labels) != len(e.Values) {
			return fmt.Errorf("a %s needs at least two values, each with a label", e.Type)
		}
		if e.Type == "donut" {
			for _, v := range e.Values {
				if v < 0 {
					return errors.New("a donut's values are shares, so not negative")
				}
			}
		}
	case "flow", "sketch":
		n := len(e.Items)
		if n < 2 || n > 6 {
			return fmt.Errorf("a %s has 2 to 6 boxes", e.Type)
		}
		for _, it := range e.Items {
			if len([]rune(it)) > 40 {
				return fmt.Errorf("a %s's box says at most 40 characters", e.Type)
			}
		}
	case "hub":
		if e.Text == "" || len(e.Items) < 2 || len(e.Items) > 6 {
			return errors.New("a hub needs text at its centre and 2 to 6 items around it")
		}
		for _, it := range e.Items {
			if len([]rune(it)) > 32 {
				return errors.New("a hub's item says at most 32 characters")
			}
		}
	case "gauge":
		if e.Max == 0 {
			e.Max = 100
		}
		if e.Max < 0 || e.To < 0 || e.To > e.Max {
			return errors.New("a gauge's value is between 0 and its max")
		}
	case "compare":
		if len(e.Values) != 2 || len(e.Labels) != 2 {
			return errors.New("a compare has two labels and two values")
		}
	case "list", "steps", "timeline":
		if len(e.Items) == 0 {
			return fmt.Errorf("a %s needs items", e.Type)
		}
		if e.Type == "timeline" && len(e.Items) < 2 {
			return errors.New("a timeline has 2 to 8 milestones")
		}
	case "table":
		if len(e.Labels) < 2 || len(e.Labels) > 4 {
			return errors.New("a table has 2 to 4 column headers")
		}
		if len(e.Items) == 0 {
			return errors.New("a table needs rows")
		}
		for _, it := range e.Items {
			cells := strings.Split(it, "|")
			if len(cells) != len(e.Labels) {
				return fmt.Errorf("a table row has %d cells split by |, not %d (%q)", len(e.Labels), len(cells), it)
			}
			for _, c := range cells {
				if strings.TrimSpace(c) == "" {
					return fmt.Errorf("a table cell is empty (%q)", it)
				}
				if len([]rune(c)) > 60 {
					return fmt.Errorf("a table cell says at most 60 characters (%q)", it)
				}
			}
		}
	}
	return nil
}

// CaptionsOn reports whether the owner asked for captions: burned on
// screen and shipped as .srt. Unset or "off" means a clean picture.
func (s Spec) CaptionsOn() bool { return s.Captions == "on" }

// Duration is the motion's length in seconds.
func (s Spec) Duration() float64 {
	t := 0.0
	for _, sc := range s.Scenes {
		t += sc.Duration
	}
	return t
}

// Frame is the pixel size the motion renders at.
func (s Spec) Frame() (int, int) {
	switch s.Size {
	case "landscape":
		return 1280, 720
	case "square":
		return 1080, 1080
	}
	return 720, 1280
}
