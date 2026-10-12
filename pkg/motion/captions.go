package motion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cue is one captioned beat: spoken or shown between Start and End.
type Cue struct {
	Start float64
	End   float64
	Text  string
}

// Cues returns one cue per scene: the voice line when set, else the caption,
// else the first on-screen text. It is what is burned on screen and shipped
// as .srt, so a muted watch and an audio-only listen carry the same ideas.
func (s Spec) Cues() []Cue {
	starts := make([]float64, len(s.Scenes))
	t := 0.0
	for i, sc := range s.Scenes {
		starts[i] = t
		t += sc.Duration
	}
	var out []Cue
	for i, sc := range s.Scenes {
		text := strings.TrimSpace(sc.Voice)
		if text == "" {
			text = strings.TrimSpace(sc.Caption)
		}
		if text == "" {
			text = strings.TrimSpace(firstWords(sc))
		}
		if text == "" {
			continue
		}
		// Captions read as two short lines, never a paragraph.
		text = captionWrap(text)
		out = append(out, Cue{Start: starts[i] + 0.15, End: starts[i] + sc.Duration - 0.05, Text: text})
	}
	return out
}

func firstWords(sc Scene) string {
	for _, e := range sc.Elements {
		switch e.Type {
		case "title", "text", "quote":
			if e.Text != "" {
				if e.Sub != "" {
					return e.Text + " — " + e.Sub
				}
				return e.Text
			}
		case "number":
			if e.Label != "" {
				return e.Label
			}
		case "bars", "line", "donut", "compare", "gauge", "flow", "hub", "timeline", "table", "list", "steps":
			if e.Label != "" {
				return e.Label
			}
		}
	}
	return ""
}

// captionWrap keeps a cue to at most 2 lines of ~42 characters, the way
// burned captions stay readable at 390 px wide.
func captionWrap(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 42 {
		return s
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) > 42 && len(lines) == 0 {
			lines = append(lines, cur)
			cur = w
			continue
		}
		if len(lines) >= 1 && len(cur)+1+len(w) > 42 {
			cur += " " + w
			if len(cur) > 84 {
				break
			}
			continue
		}
		cur += " " + w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > 2 {
		lines = lines[:2]
	}
	return strings.Join(lines, "\n")
}

// FormatSRT renders cues as SubRip, shipped beside the MP4.
func FormatSRT(cues []Cue) string {
	var sb strings.Builder
	for i, c := range cues {
		fmt.Fprintf(&sb, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(c.Start), srtTime(c.End), c.Text)
	}
	return sb.String()
}

func srtTime(t float64) string {
	if t < 0 {
		t = 0
	}
	ms := int(t*1000 + 0.5)
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	mm := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, mm)
}

// CaptionsPath is where a motion's captions live (motion/x-v2.srt).
func CaptionsPath(motionRel string) string {
	return strings.TrimSuffix(motionRel, ".json") + ".srt"
}

// WriteCaptions writes the .srt for the motion at rel into the workspace.
func WriteCaptions(workspace, rel string) (string, error) {
	s, err := Load(workspace, rel)
	if err != nil {
		return "", err
	}
	crel := CaptionsPath(rel)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(workspace, filepath.FromSlash(crel))), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workspace, filepath.FromSlash(crel)), []byte(FormatSRT(s.Cues())), 0o644); err != nil {
		return "", err
	}
	return crel, nil
}
