package cards

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Blocks are the vocabulary a presented card is built from. The model chooses
// and fills them; it never chooses a layout, a colour, a link or a style. The
// phone has one renderer per block type, so every card looks like it belongs,
// and a card needs no app update unless a genuinely new block type is added.
//
// Every limit here exists to keep a card a glance, not a document, and to keep
// anything the model writes inert: values are plain text, there are no URLs or
// markup, and nothing in a block can run or open anything.
type Block struct {
	Type string `json:"type"`

	// text, note
	Text string `json:"text,omitempty"`
	// facts
	Rows []Row `json:"rows,omitempty"`
	// list
	Items []Item `json:"items,omitempty"`
	// timeline
	Steps []Step `json:"steps,omitempty"`
	// metric, progress
	Label string `json:"label,omitempty"`
	Value string `json:"value,omitempty"`
	Unit  string `json:"unit,omitempty"`
	Delta string `json:"delta,omitempty"`
	// progress: how far along, 0 to 1
	Progress *float64 `json:"progress,omitempty"`
	Caption  string   `json:"caption,omitempty"`
	// code
	Language string `json:"language,omitempty"`
	Code     string `json:"code,omitempty"`

	// metric, note, facts rows, list items: how it should feel
	Tone string `json:"tone,omitempty"`
}

// Row is one line of a facts block: a label and its value.
type Row struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Tone  string `json:"tone,omitempty"`
}

// Item is one entry of a list block.
type Item struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Trailing string `json:"trailing,omitempty"`
	Tone     string `json:"tone,omitempty"`
}

// Step is one point on a timeline block. State places it: done, now or next.
type Step struct {
	Time   string `json:"time,omitempty"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	State  string `json:"state,omitempty"`
}

// Block types.
const (
	BlockText     = "text"
	BlockFacts    = "facts"
	BlockList     = "list"
	BlockMetric   = "metric"
	BlockProgress = "progress"
	BlockNote     = "note"
	BlockTimeline = "timeline"
	BlockCode     = "code"
)

// Limits. The phone mirrors these; a shared fixture pins the two together.
const (
	MaxBlocks     = 8
	MaxText       = 400
	MaxLabel      = 60
	MaxValue      = 80
	MaxRows       = 10
	MaxItems      = 12
	MaxSteps      = 8
	MaxCodeChars  = 1200
	MaxCodeLines  = 40
	MaxLanguage   = 20
	MaxActionText = 200
)

var tones = map[string]bool{"": true, "neutral": true, "good": true, "warn": true, "bad": true, "info": true}
var stepStates = map[string]bool{"": true, "done": true, "now": true, "next": true}

// clean trims, collapses control characters (a model can emit anything) and
// reports whether the result fits max runes.
func clean(s string, max int) (string, bool) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= max
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// field cleans a single-line value and enforces its limit.
func field(name, s string, max int, required bool) (string, error) {
	c, ok := clean(oneLine(s), max)
	if !ok {
		return "", fmt.Errorf("%s is longer than %d characters", name, max)
	}
	if required && c == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return c, nil
}

func checkTone(name, t string) error {
	if !tones[t] {
		return fmt.Errorf("%s tone %q is not one of neutral, good, warn, bad, info", name, t)
	}
	return nil
}

// Validate cleans a block in place and rejects anything outside the catalog.
func (b *Block) Validate() error {
	var err error
	switch b.Type {
	case BlockText:
		t, ok := clean(b.Text, MaxText)
		if !ok || t == "" {
			return fmt.Errorf("text block needs text of 1 to %d characters", MaxText)
		}
		b.Text = t
	case BlockNote:
		t, ok := clean(b.Text, MaxText)
		if !ok || t == "" {
			return fmt.Errorf("note block needs text of 1 to %d characters", MaxText)
		}
		b.Text = t
		return checkTone("note", b.Tone)
	case BlockFacts:
		if len(b.Rows) == 0 || len(b.Rows) > MaxRows {
			return fmt.Errorf("facts block needs 1 to %d rows", MaxRows)
		}
		for i := range b.Rows {
			r := &b.Rows[i]
			if r.Label, err = field("facts label", r.Label, MaxLabel, true); err != nil {
				return err
			}
			if r.Value, err = field("facts value", r.Value, MaxValue, true); err != nil {
				return err
			}
			if err = checkTone("facts row", r.Tone); err != nil {
				return err
			}
		}
	case BlockList:
		if len(b.Items) == 0 || len(b.Items) > MaxItems {
			return fmt.Errorf("list block needs 1 to %d items", MaxItems)
		}
		for i := range b.Items {
			it := &b.Items[i]
			if it.Title, err = field("list title", it.Title, MaxValue, true); err != nil {
				return err
			}
			if it.Subtitle, err = field("list subtitle", it.Subtitle, MaxValue*2, false); err != nil {
				return err
			}
			if it.Trailing, err = field("list trailing", it.Trailing, MaxLabel, false); err != nil {
				return err
			}
			if err = checkTone("list item", it.Tone); err != nil {
				return err
			}
		}
	case BlockTimeline:
		if len(b.Steps) == 0 || len(b.Steps) > MaxSteps {
			return fmt.Errorf("timeline block needs 1 to %d steps", MaxSteps)
		}
		for i := range b.Steps {
			s := &b.Steps[i]
			if s.Time, err = field("timeline time", s.Time, MaxLabel, false); err != nil {
				return err
			}
			if s.Title, err = field("timeline title", s.Title, MaxValue, true); err != nil {
				return err
			}
			if s.Detail, err = field("timeline detail", s.Detail, MaxValue*2, false); err != nil {
				return err
			}
			if !stepStates[s.State] {
				return fmt.Errorf("timeline state %q is not one of done, now, next", s.State)
			}
		}
	case BlockMetric:
		if b.Label, err = field("metric label", b.Label, MaxLabel, true); err != nil {
			return err
		}
		if b.Value, err = field("metric value", b.Value, 24, true); err != nil {
			return err
		}
		if b.Unit, err = field("metric unit", b.Unit, 16, false); err != nil {
			return err
		}
		if b.Delta, err = field("metric delta", b.Delta, 24, false); err != nil {
			return err
		}
		return checkTone("metric", b.Tone)
	case BlockProgress:
		if b.Label, err = field("progress label", b.Label, MaxLabel, true); err != nil {
			return err
		}
		if b.Progress == nil || *b.Progress < 0 || *b.Progress > 1 {
			return fmt.Errorf("progress block needs a value from 0 to 1")
		}
		if b.Caption, err = field("progress caption", b.Caption, MaxValue, false); err != nil {
			return err
		}
	case BlockCode:
		code, ok := clean(strings.ReplaceAll(b.Code, "\r\n", "\n"), MaxCodeChars)
		if !ok || code == "" {
			return fmt.Errorf("code block needs code of 1 to %d characters", MaxCodeChars)
		}
		if strings.Count(code, "\n")+1 > MaxCodeLines {
			return fmt.Errorf("code block is longer than %d lines", MaxCodeLines)
		}
		b.Code = code
		lang := strings.ToLower(strings.TrimSpace(b.Language))
		if len(lang) > MaxLanguage {
			return fmt.Errorf("code language is longer than %d characters", MaxLanguage)
		}
		for _, r := range lang {
			if !(unicode.IsLower(r) || unicode.IsDigit(r) || r == '+' || r == '#' || r == '-' || r == '.') {
				return fmt.Errorf("code language %q has characters a language name never has", b.Language)
			}
		}
		b.Language = lang
	default:
		return fmt.Errorf("block type %q is not in the catalog", b.Type)
	}
	return nil
}

// blockText is the plain-text form of a block, for channels that cannot draw
// cards (a text message, the terminal) and for screen readers.
func blockText(b Block) string {
	var sb strings.Builder
	switch b.Type {
	case BlockText, BlockNote:
		sb.WriteString(b.Text)
	case BlockFacts:
		for i, r := range b.Rows {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(r.Label + ": " + r.Value)
		}
	case BlockList:
		for i, it := range b.Items {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("- " + it.Title)
			if it.Subtitle != "" {
				sb.WriteString(" (" + it.Subtitle + ")")
			}
			if it.Trailing != "" {
				sb.WriteString(": " + it.Trailing)
			}
		}
	case BlockTimeline:
		for i, s := range b.Steps {
			if i > 0 {
				sb.WriteString("\n")
			}
			if s.Time != "" {
				sb.WriteString(s.Time + " - ")
			}
			sb.WriteString(s.Title)
			if s.Detail != "" {
				sb.WriteString(" (" + s.Detail + ")")
			}
		}
	case BlockMetric:
		sb.WriteString(b.Label + ": " + b.Value)
		if b.Unit != "" {
			sb.WriteString(" " + b.Unit)
		}
		if b.Delta != "" {
			sb.WriteString(" (" + b.Delta + ")")
		}
	case BlockProgress:
		fmt.Fprintf(&sb, "%s: %d%%", b.Label, int((*b.Progress)*100+0.5))
		if b.Caption != "" {
			sb.WriteString(" (" + b.Caption + ")")
		}
	case BlockCode:
		sb.WriteString("```" + b.Language + "\n" + b.Code + "\n```")
	}
	return sb.String()
}
