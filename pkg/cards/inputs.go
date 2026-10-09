package cards

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A card can ask as well as show. The input blocks (choice, datetime, slider,
// field, checklist) are questions the owner answers in place, and the view
// blocks (compare, chart, map) show things a list or a fact cannot. Everything
// here keeps the card contract's promise: the model fills fields, it never
// chooses a layout, and nothing it writes can run, open or style anything. An
// answer comes back keyed by the block's Key and is checked against the block
// it answers before anything else sees it.

// Option is one choice in a choice block, or one thing compared in a compare block.
type Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// Check is one line of a checklist.
type Check struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Done  bool   `json:"done,omitempty"`
}

// Point is one value on a chart.
type Point struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Place is one pin on a map block.
type Place struct {
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

// Limits for the input and view blocks. The phone mirrors these.
const (
	MaxOptions      = 8
	MaxCompare      = 4
	MaxCompareRows  = 8
	MaxChecks       = 20
	MaxPoints       = 24
	MaxPlaces       = 10
	MaxAnswerText   = 400
	MaxPlaceholder  = 80
	maxIDLen        = 24
	dateLayout      = "2006-01-02"
	timeLayout      = "15:04"
	dateTimeLayout  = "2006-01-02T15:04"
	sliderMaxSteps  = 1000
	compareCellChar = 40
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,23}$`)

// IsInput reports whether a block asks the owner something.
func (b Block) IsInput() bool {
	switch b.Type {
	case BlockChoice, BlockDatetime, BlockSlider, BlockField, BlockChecklist:
		return true
	}
	return false
}

func checkID(what, id string) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("%s %q must be 1 to %d lowercase letters, digits, _ or -", what, id, maxIDLen)
	}
	return id, nil
}

func layoutFor(mode string) string {
	switch mode {
	case "date":
		return dateLayout
	case "time":
		return timeLayout
	}
	return dateTimeLayout
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func (b *Block) validateInput() error {
	var err error
	if b.Key, err = checkID("key", b.Key); err != nil {
		return fmt.Errorf("%s block: %w", b.Type, err)
	}
	if b.Label, err = field(b.Type+" label", b.Label, MaxLabel, b.Type != BlockChecklist); err != nil {
		return err
	}
	switch b.Type {
	case BlockChoice:
		if len(b.Options) < 2 || len(b.Options) > MaxOptions {
			return fmt.Errorf("choice block needs 2 to %d options", MaxOptions)
		}
		seen := map[string]bool{}
		for i := range b.Options {
			o := &b.Options[i]
			if o.ID == "" {
				o.ID = strconv.Itoa(i + 1)
			}
			if o.ID, err = checkID("option id", o.ID); err != nil {
				return err
			}
			if seen[o.ID] {
				return fmt.Errorf("choice block has two options with id %q", o.ID)
			}
			seen[o.ID] = true
			if o.Label, err = field("option label", o.Label, MaxLabel, true); err != nil {
				return err
			}
			if o.Detail, err = field("option detail", o.Detail, MaxValue, false); err != nil {
				return err
			}
		}
	case BlockDatetime:
		if b.Mode == "" {
			b.Mode = "datetime"
		}
		if b.Mode != "date" && b.Mode != "time" && b.Mode != "datetime" {
			return fmt.Errorf("datetime mode %q is not date, time or datetime", b.Mode)
		}
		for _, v := range []*string{&b.Value, &b.Earliest} {
			*v = strings.TrimSpace(*v)
			if *v == "" {
				continue
			}
			if _, perr := time.Parse(layoutFor(b.Mode), *v); perr != nil {
				return fmt.Errorf("datetime value %q is not in the form %s", *v, layoutFor(b.Mode))
			}
		}
	case BlockSlider:
		if b.Min == nil || b.Max == nil || !finite(*b.Min) || !finite(*b.Max) || *b.Min >= *b.Max {
			return errors.New("slider block needs a min below its max")
		}
		if b.Step == nil {
			one := 1.0
			b.Step = &one
		}
		if !finite(*b.Step) || *b.Step <= 0 || *b.Step > *b.Max-*b.Min || (*b.Max-*b.Min)/(*b.Step) > sliderMaxSteps {
			return fmt.Errorf("slider step must be above 0 and give at most %d steps", sliderMaxSteps)
		}
		if b.Number == nil {
			n := *b.Min
			b.Number = &n
		}
		if !finite(*b.Number) || *b.Number < *b.Min || *b.Number > *b.Max {
			return errors.New("slider starting value must be within its range")
		}
		if b.Unit, err = field("slider unit", b.Unit, 16, false); err != nil {
			return err
		}
	case BlockField:
		if b.Placeholder, err = field("field placeholder", b.Placeholder, MaxPlaceholder, false); err != nil {
			return err
		}
		v, ok := clean(b.Value, MaxAnswerText)
		if !ok {
			return fmt.Errorf("field value is longer than %d characters", MaxAnswerText)
		}
		b.Value = v
	case BlockChecklist:
		if len(b.Checks) == 0 || len(b.Checks) > MaxChecks {
			return fmt.Errorf("checklist block needs 1 to %d items", MaxChecks)
		}
		seen := map[string]bool{}
		for i := range b.Checks {
			c := &b.Checks[i]
			if c.ID == "" {
				c.ID = strconv.Itoa(i + 1)
			}
			if c.ID, err = checkID("checklist id", c.ID); err != nil {
				return err
			}
			if seen[c.ID] {
				return fmt.Errorf("checklist has two items with id %q", c.ID)
			}
			seen[c.ID] = true
			if c.Label, err = field("checklist item", c.Label, MaxValue, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Block) validateView() error {
	var err error
	switch b.Type {
	case BlockCompare:
		if len(b.Options) < 2 || len(b.Options) > MaxCompare {
			return fmt.Errorf("compare block needs 2 to %d options", MaxCompare)
		}
		for i := range b.Options {
			o := &b.Options[i]
			if o.ID == "" {
				o.ID = strconv.Itoa(i + 1)
			}
			if o.ID, err = checkID("option id", o.ID); err != nil {
				return err
			}
			if o.Label, err = field("compare option", o.Label, compareCellChar, true); err != nil {
				return err
			}
			if o.Detail, err = field("compare option detail", o.Detail, MaxLabel, false); err != nil {
				return err
			}
		}
		if len(b.Rows) == 0 || len(b.Rows) > MaxCompareRows {
			return fmt.Errorf("compare block needs 1 to %d rows", MaxCompareRows)
		}
		for i := range b.Rows {
			r := &b.Rows[i]
			if r.Label, err = field("compare row", r.Label, compareCellChar, true); err != nil {
				return err
			}
			if r.Value != "" {
				return errors.New("a compare row has values (one per option), not a value")
			}
			if len(r.Values) != len(b.Options) {
				return fmt.Errorf("compare row %q needs %d values, one per option", r.Label, len(b.Options))
			}
			for j := range r.Values {
				if r.Values[j], err = field("compare value", r.Values[j], compareCellChar, false); err != nil {
					return err
				}
			}
			if err = checkTone("compare row", r.Tone); err != nil {
				return err
			}
		}
		if b.Pick != nil && (*b.Pick < 0 || *b.Pick >= len(b.Options)) {
			return errors.New("compare pick must name one of its options (0-based)")
		}
	case BlockChart:
		if b.Chart == "" {
			b.Chart = "bar"
		}
		if b.Chart != "bar" && b.Chart != "line" {
			return fmt.Errorf("chart %q is not bar or line", b.Chart)
		}
		if b.Label, err = field("chart label", b.Label, MaxLabel, true); err != nil {
			return err
		}
		if len(b.Points) < 2 || len(b.Points) > MaxPoints {
			return fmt.Errorf("chart block needs 2 to %d points", MaxPoints)
		}
		for i := range b.Points {
			p := &b.Points[i]
			if p.Label, err = field("chart point label", p.Label, 16, true); err != nil {
				return err
			}
			if !finite(p.Value) {
				return errors.New("chart values must be numbers")
			}
		}
		if b.Unit, err = field("chart unit", b.Unit, 12, false); err != nil {
			return err
		}
		if b.Caption, err = field("chart caption", b.Caption, MaxValue, false); err != nil {
			return err
		}
	case BlockMap:
		if len(b.Places) == 0 || len(b.Places) > MaxPlaces {
			return fmt.Errorf("map block needs 1 to %d places", MaxPlaces)
		}
		for i := range b.Places {
			p := &b.Places[i]
			if p.Name, err = field("place name", p.Name, MaxLabel, true); err != nil {
				return err
			}
			if p.Detail, err = field("place detail", p.Detail, MaxValue, false); err != nil {
				return err
			}
			if !finite(p.Lat) || !finite(p.Lon) || p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
				return fmt.Errorf("place %q has no real coordinates", p.Name)
			}
			if p.Lat == 0 && p.Lon == 0 {
				return fmt.Errorf("place %q has no real coordinates", p.Name)
			}
		}
	}
	return nil
}

// extraBlockText is the plain-text form of the newer blocks.
func extraBlockText(b Block) string {
	var sb strings.Builder
	switch b.Type {
	case BlockChoice:
		sb.WriteString(b.Label)
		for _, o := range b.Options {
			sb.WriteString("\n- " + o.Label)
			if o.Detail != "" {
				sb.WriteString(" (" + o.Detail + ")")
			}
		}
	case BlockDatetime, BlockSlider, BlockField:
		sb.WriteString(b.Label)
	case BlockChecklist:
		if b.Label != "" {
			sb.WriteString(b.Label + "\n")
		}
		for i, c := range b.Checks {
			if i > 0 {
				sb.WriteString("\n")
			}
			mark := "[ ] "
			if c.Done {
				mark = "[x] "
			}
			sb.WriteString(mark + c.Label)
		}
	case BlockCompare:
		names := make([]string, len(b.Options))
		for i, o := range b.Options {
			names[i] = o.Label
		}
		sb.WriteString(strings.Join(names, " | "))
		for _, r := range b.Rows {
			sb.WriteString("\n" + r.Label + ": " + strings.Join(r.Values, " | "))
		}
		if b.Pick != nil {
			sb.WriteString("\nPick: " + b.Options[*b.Pick].Label)
		}
	case BlockChart:
		sb.WriteString(b.Label + ":")
		for _, p := range b.Points {
			sb.WriteString(" " + p.Label + " " + formatNumber(p.Value) + b.Unit)
		}
	case BlockMap:
		for i, p := range b.Places {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("- " + p.Name)
			if p.Detail != "" {
				sb.WriteString(" (" + p.Detail + ")")
			}
		}
	}
	return sb.String()
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e12 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// checkInputs enforces the card-level rules for input blocks: keys are unique,
// a card that asks something has exactly one submit, and a card that asks
// nothing has none. A card whose only inputs are checklists may go without a
// submit: it is a list the owner ticks as they go (groceries, packing).
func checkInputs(blocks []Block, actions []Action) error {
	keys := map[string]bool{}
	asks, onlyChecklists := false, true
	for _, b := range blocks {
		if !b.IsInput() {
			continue
		}
		asks = true
		if b.Type != BlockChecklist {
			onlyChecklists = false
		}
		if keys[b.Key] {
			return fmt.Errorf("two blocks answer to the key %q", b.Key)
		}
		keys[b.Key] = true
	}
	submits := 0
	for _, a := range actions {
		if a.Kind == "submit" {
			submits++
		}
	}
	switch {
	case !asks && submits > 0:
		return errors.New("a submit action needs a block that asks something")
	case asks && submits > 1:
		return errors.New("a card has at most one submit action")
	case asks && submits == 0 && !onlyChecklists:
		return errors.New("a card that asks something needs one submit action (kind submit)")
	}
	return nil
}

// Asks reports whether a card has input blocks.
func (c Card) Asks() bool {
	for _, b := range c.Blocks {
		if b.IsInput() {
			return true
		}
	}
	return false
}

// Answer is the owner's answer to a card, checked and in two forms: the values
// by key (what is stored) and the sentence that is sent on (what Ghost reads).
type Answer struct {
	Values map[string]interface{}
	Text   string
	// Label is the few words the answered card shows ("Fri 12 Oct · TP 1352").
	Label string
}

// CheckAnswer validates raw answers against the card's input blocks. Every
// required input must be answered, a choice only with an option it offered, a
// date in its block's form and not before its earliest, a number within its
// range and on its step, text within its limit. Unknown keys are refused.
func (c Card) CheckAnswer(raw map[string]interface{}, now time.Time) (Answer, error) {
	byKey := map[string]Block{}
	for _, b := range c.Blocks {
		if b.IsInput() {
			byKey[b.Key] = b
		}
	}
	if len(byKey) == 0 {
		return Answer{}, errors.New("this card does not ask anything")
	}
	for k := range raw {
		if _, ok := byKey[k]; !ok {
			return Answer{}, fmt.Errorf("the card does not ask %q", k)
		}
	}
	values := map[string]interface{}{}
	var lines, short []string
	for _, b := range c.Blocks {
		if !b.IsInput() {
			continue
		}
		v, has := raw[b.Key]
		if c.Kind == KindQuestion && b.Type == BlockChoice && (!has || v == nil || v == "") {
			// A question with choices is answered by a pick or by words in
			// the box beside it; the box is checked below.
			if other, _ := raw[OtherKey].(string); strings.TrimSpace(other) != "" {
				continue
			}
		}
		shown, val, err := answerFor(b, v, has, now)
		if err != nil {
			return Answer{}, err
		}
		if val == nil {
			continue
		}
		values[b.Key] = val
		label := b.Label
		if label == "" {
			label = "Ticked"
		}
		lines = append(lines, label+": "+shown)
		short = append(short, shown)
	}
	if len(lines) == 0 {
		return Answer{}, errors.New("nothing was answered")
	}
	text := strings.Join(lines, "\n")
	if len(lines) == 1 {
		text = lines[0]
	}
	label := strings.Join(short, " · ")
	if r := []rune(label); len(r) > 60 {
		label = string(r[:59]) + "…"
	}
	return Answer{Values: values, Text: text, Label: label}, nil
}

func answerFor(b Block, v interface{}, has bool, now time.Time) (string, interface{}, error) {
	missing := !has || v == nil
	switch b.Type {
	case BlockChoice:
		var picked []string
		switch x := v.(type) {
		case string:
			if x != "" {
				picked = []string{x}
			}
		case []interface{}:
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					return "", nil, fmt.Errorf("%s: choices are option ids", b.Label)
				}
				picked = append(picked, s)
			}
		case nil:
		default:
			return "", nil, fmt.Errorf("%s: choices are option ids", b.Label)
		}
		if len(picked) == 0 {
			if b.Multiple && has && v != nil {
				// Picking none of several is an answer too.
				return "none", []string{}, nil
			}
			return "", nil, fmt.Errorf("%s: pick one", b.Label)
		}
		if !b.Multiple && len(picked) > 1 {
			return "", nil, fmt.Errorf("%s: pick only one", b.Label)
		}
		labels := map[string]string{}
		for _, o := range b.Options {
			labels[o.ID] = o.Label
		}
		seen := map[string]bool{}
		for _, id := range picked {
			if _, ok := labels[id]; !ok {
				return "", nil, fmt.Errorf("%s: %q is not one of the options", b.Label, id)
			}
			seen[id] = true
		}
		// In the order the card offered them, each once.
		var names []string
		var ids []string
		for _, o := range b.Options {
			if seen[o.ID] {
				names = append(names, o.Label)
				ids = append(ids, o.ID)
			}
		}
		if b.Multiple {
			return strings.Join(names, ", "), ids, nil
		}
		return names[0], ids[0], nil
	case BlockDatetime:
		s, ok := v.(string)
		if (missing || (ok && strings.TrimSpace(s) == "")) && b.Optional {
			// Not known, and allowed to be left (a birthday the owner doesn't know).
			return "", nil, nil
		}
		if missing || !ok || strings.TrimSpace(s) == "" {
			return "", nil, fmt.Errorf("%s: choose a %s", b.Label, map[string]string{"date": "date", "time": "time", "datetime": "date and time"}[b.Mode])
		}
		s = strings.TrimSpace(s)
		t, err := time.ParseInLocation(layoutFor(b.Mode), s, now.Location())
		if err != nil {
			return "", nil, fmt.Errorf("%s: %q is not a %s", b.Label, s, b.Mode)
		}
		if b.Earliest != "" {
			if e, err := time.ParseInLocation(layoutFor(b.Mode), b.Earliest, now.Location()); err == nil && t.Before(e) {
				return "", nil, fmt.Errorf("%s: that is before %s", b.Label, humanTime(b.Mode, e, now))
			}
		}
		return humanTime(b.Mode, t, now), s, nil
	case BlockSlider:
		f, ok := v.(float64)
		if missing || !ok || !finite(f) {
			return "", nil, fmt.Errorf("%s: choose a number", b.Label)
		}
		if f < *b.Min || f > *b.Max {
			return "", nil, fmt.Errorf("%s: %s is outside %s to %s", b.Label, formatNumber(f), formatNumber(*b.Min), formatNumber(*b.Max))
		}
		steps := (f - *b.Min) / *b.Step
		if math.Abs(steps-math.Round(steps)) > 1e-6 {
			return "", nil, fmt.Errorf("%s: %s is not on a step of %s", b.Label, formatNumber(f), formatNumber(*b.Step))
		}
		shown := formatNumber(f)
		if b.Unit != "" {
			shown += " " + b.Unit
		}
		return shown, f, nil
	case BlockField:
		s, ok := v.(string)
		if !missing && !ok {
			return "", nil, fmt.Errorf("%s: answer in words", b.Label)
		}
		t, fits := clean(s, MaxAnswerText)
		if !fits {
			return "", nil, fmt.Errorf("%s: keep it under %d characters", b.Label, MaxAnswerText)
		}
		if !b.Multiline {
			t = oneLine(t)
		}
		if t == "" {
			if b.Optional {
				return "", nil, nil
			}
			return "", nil, fmt.Errorf("%s: write something", b.Label)
		}
		return t, t, nil
	case BlockChecklist:
		var ids []string
		switch x := v.(type) {
		case []interface{}:
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					return "", nil, errors.New("checklist answers are item ids")
				}
				ids = append(ids, s)
			}
		case nil:
		default:
			return "", nil, errors.New("checklist answers are item ids")
		}
		labels := map[string]string{}
		order := map[string]int{}
		for i, c := range b.Checks {
			labels[c.ID] = c.Label
			order[c.ID] = i
		}
		seen := map[string]bool{}
		var kept []string
		for _, id := range ids {
			if _, ok := labels[id]; !ok {
				return "", nil, fmt.Errorf("%q is not on the checklist", id)
			}
			if !seen[id] {
				seen[id] = true
				kept = append(kept, id)
			}
		}
		sort.Slice(kept, func(i, j int) bool { return order[kept[i]] < order[kept[j]] })
		names := make([]string, len(kept))
		for i, id := range kept {
			names[i] = labels[id]
		}
		shown := "none"
		if len(names) > 0 {
			shown = strings.Join(names, ", ")
		}
		if kept == nil {
			kept = []string{}
		}
		return shown, kept, nil
	}
	return "", nil, fmt.Errorf("block %q does not take an answer", b.Type)
}

// humanTime is a date or time the way a person says it: "Fri 12 Oct", "14:30",
// "Fri 12 Oct, 14:30"; the year only when it is not this one.
func humanTime(mode string, t, now time.Time) string {
	day := t.Format("Mon 2 Jan")
	if t.Year() != now.Year() {
		day += t.Format(" 2006")
	}
	switch mode {
	case "date":
		return day
	case "time":
		return t.Format("15:04")
	}
	return day + ", " + t.Format("15:04")
}

// QuestionKey is the key a question card's answer comes back under, and
// OtherKey the optional "something else" box offered beside the choices.
const (
	QuestionKey = "answer"
	OtherKey    = "other"
)

// NewQuestion builds the card for something Ghost needs to ask to go on: the
// question as its title, the choices (when there are some) as one tap each,
// with a box for something else, or a box alone for an open question.
func NewQuestion(question string, choices []string, questionID string) (Card, error) {
	q, ok := clean(oneLine(question), 200)
	if !ok || q == "" {
		return Card{}, errors.New("a question needs 1 to 200 characters")
	}
	var blocks []Block
	if len(choices) >= 2 {
		if len(choices) > MaxOptions {
			choices = choices[:MaxOptions]
		}
		opts := make([]Option, 0, len(choices))
		for i, c := range choices {
			opts = append(opts, Option{ID: strconv.Itoa(i + 1), Label: c})
		}
		blocks = append(blocks,
			Block{Type: BlockChoice, Key: QuestionKey, Label: "Choose one", Options: opts},
			Block{Type: BlockField, Key: OtherKey, Label: "Or say something else", Optional: true},
		)
	} else {
		blocks = append(blocks, Block{Type: BlockField, Key: QuestionKey, Label: "Your answer", Multiline: true})
	}
	for i := range blocks {
		if err := blocks[i].Validate(); err != nil {
			return Card{}, err
		}
	}
	now := time.Now()
	c := Card{
		ID: newID(), Kind: KindQuestion, Title: q, Blocks: blocks, V: CardVersion,
		Data:      map[string]interface{}{"question_id": questionID},
		Actions:   []Action{{ID: "send", Label: "Send", Style: "primary", Kind: "submit"}},
		CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour),
	}
	return c, c.Validate()
}

// QuestionReply is what the owner said to a question card, as plain words: what
// they wrote in the box if they wrote something, otherwise the choice they made.
// A choice made with a note beside it keeps both.
func QuestionReply(c Card, ans Answer) string {
	other, _ := ans.Values[OtherKey].(string)
	var choice string
	if id, ok := ans.Values[QuestionKey].(string); ok {
		choice = id
		for _, b := range c.Blocks {
			if b.Type == BlockChoice {
				for _, o := range b.Options {
					if o.ID == id {
						choice = o.Label
					}
				}
			} else if b.Type == BlockField && b.Key == QuestionKey {
				choice = id
			}
		}
	}
	switch {
	case choice != "" && other != "":
		return choice + ". " + other
	case other != "":
		return other
	}
	return choice
}
