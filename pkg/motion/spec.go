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
	Accent string  `json:"accent,omitempty"`
	Scenes []Scene `json:"scenes"`
}

// Scene is one beat, shown for Duration seconds.
type Scene struct {
	Duration float64   `json:"duration"`
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
//
// At is when it enters, in seconds from the start of its scene; Stay, when set,
// is how long it stays (otherwise to the end of the scene).
type Element struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	Sub      string    `json:"sub,omitempty"`
	Label    string    `json:"label,omitempty"`
	From     float64   `json:"from,omitempty"`
	To       float64   `json:"to,omitempty"`
	Prefix   string    `json:"prefix,omitempty"`
	Suffix   string    `json:"suffix,omitempty"`
	Decimals int       `json:"decimals,omitempty"`
	Unit     string    `json:"unit,omitempty"`
	Labels   []string  `json:"labels,omitempty"`
	Values   []float64 `json:"values,omitempty"`
	Items    []string  `json:"items,omitempty"`
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
	elementTypes = map[string]bool{"title": true, "text": true, "number": true, "bars": true, "line": true, "donut": true, "list": true, "steps": true, "compare": true, "quote": true}
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
	switch s.Size {
	case "":
		s.Size = "portrait"
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
	total := 0.0
	for i := range s.Scenes {
		sc := &s.Scenes[i]
		if sc.Duration < 1 || sc.Duration > 20 || math.IsNaN(sc.Duration) {
			return fmt.Errorf("scene %d: a scene lasts 1 to 20 seconds", i+1)
		}
		total += sc.Duration
		if len([]rune(sc.Caption)) > 120 {
			return fmt.Errorf("scene %d: the caption is longer than 120 characters", i+1)
		}
		if len(sc.Elements) == 0 || len(sc.Elements) > MaxElements {
			return fmt.Errorf("scene %d: a scene has 1 to %d elements", i+1, MaxElements)
		}
		for j := range sc.Elements {
			if err := sc.Elements[j].check(sc.Duration); err != nil {
				return fmt.Errorf("scene %d, element %d: %w", i+1, j+1, err)
			}
		}
	}
	if total > MaxDuration {
		return fmt.Errorf("a motion is at most %.0f seconds; this one is %.0f", MaxDuration, total)
	}
	return nil
}

func (e *Element) check(sceneDur float64) error {
	if !elementTypes[e.Type] {
		return fmt.Errorf("type is title, text, number, bars, line, donut, list, steps, compare or quote, not %q", e.Type)
	}
	// A prefix or suffix keeps its spacing ("KES " before, " sold" after).
	for _, f := range []*string{&e.Text, &e.Sub, &e.Label, &e.Unit} {
		*f = strings.TrimSpace(*f)
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
	for _, v := range append([]float64{e.From, e.To}, e.Values...) {
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
	case "compare":
		if len(e.Values) != 2 || len(e.Labels) != 2 {
			return errors.New("a compare has two labels and two values")
		}
	case "list", "steps":
		if len(e.Items) == 0 {
			return fmt.Errorf("a %s needs items", e.Type)
		}
	}
	return nil
}

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
