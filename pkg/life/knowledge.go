package life

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Learning is something the owner is reading, studying or wants to: a book, a
// course, a subject for an exam, an article or a podcast. It keeps where they
// are (page 140 of 320, lesson 6 of 12), what they made of it (notes, and the
// lines they wanted to keep), and when they started and finished, so Ghost can
// pick up where they left off, quiz them on it, and remind them gently when
// something they meant to finish has gone quiet.
type Learning struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // book | course | subject | article | podcast | video | paper | other
	Title  string `json:"title"`
	Author string `json:"author,omitempty"`
	// Status: want (to read or learn), active (reading or studying), paused, done.
	Status string `json:"status"`
	// Where they are: Current of Total, in Unit (page, chapter, lesson, module,
	// episode, percent). Total 0 when it isn't known.
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Unit    string `json:"unit,omitempty"`
	// Goal is what they are learning it for, in their words ("pass the CPA
	// exam in March", "read 20 books this year").
	Goal     string      `json:"goal,omitempty"`
	Due      string      `json:"due,omitempty"` // 2006-01-02: an exam, a deadline, a book club
	Notes    []StudyNote `json:"notes,omitempty"`
	Tags     []string    `json:"tags,omitempty"`
	Started  string      `json:"started,omitempty"`  // 2006-01-02
	Finished string      `json:"finished,omitempty"` // 2006-01-02
	Source   Source      `json:"source"`
	Created  time.Time   `json:"created_at"`
	Updated  time.Time   `json:"updated_at"`
	// LastTouched is when the owner last moved it on (progress or a note).
	LastTouched time.Time `json:"last_touched,omitempty"`
}

// StudyNote is one thing the owner made of it: a thought, a summary, or a quote
// they wanted to keep (with where it is, "p. 112").
type StudyNote struct {
	Text   string    `json:"text"`
	Quote  bool      `json:"quote,omitempty"`
	Where  string    `json:"where,omitempty"`
	At     time.Time `json:"at"`
	Source Source    `json:"source"`
}

var (
	learningKinds  = map[string]bool{"book": true, "course": true, "subject": true, "article": true, "podcast": true, "video": true, "paper": true, "other": true}
	learningStatus = map[string]bool{"want": true, "active": true, "paused": true, "done": true}
	learningUnits  = map[string]bool{"page": true, "chapter": true, "lesson": true, "module": true, "episode": true, "percent": true, "unit": true}
)

const (
	maxLearnings     = 500
	maxLearningNotes = 200
)

// Knowledge is what the owner reads and studies.
type Knowledge struct{ f *file[[]Learning] }

// OpenKnowledge opens the knowledge store in a workspace.
func OpenKnowledge(workspace string) *Knowledge {
	return &Knowledge{f: newFile(workspace, "knowledge.json", []Learning{})}
}

var (
	knowledgeMu     sync.Mutex
	sharedKnowledge = map[string]*Knowledge{}
)

// KnowledgeFor is the knowledge store of a workspace.
func KnowledgeFor(workspace string) *Knowledge {
	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()
	if s, ok := sharedKnowledge[workspace]; ok {
		return s
	}
	s := OpenKnowledge(workspace)
	sharedKnowledge[workspace] = s
	return s
}

// LearningInput is a change to keep: empty fields leave what is there.
type LearningInput struct {
	Kind, Title, Author, Status string
	// Current and Total of -1 leave them; Unit "" leaves it.
	Current, Total int
	Unit           string
	Goal, Due      string
	Tags           []string
	Note           string
	Quote          bool
	Where          string
	Source         Source
}

func checkLearning(in *LearningInput) error {
	var err error
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if in.Kind != "" && !learningKinds[in.Kind] {
		return fmt.Errorf("it is a book, course, subject, article, podcast, video, paper or other, not %q", in.Kind)
	}
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Status != "" && !learningStatus[in.Status] {
		return fmt.Errorf("status is want, active, paused or done, not %q", in.Status)
	}
	in.Unit = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in.Unit)), "s")
	if in.Unit == "%" {
		in.Unit = "percent"
	}
	if in.Unit != "" && !learningUnits[in.Unit] {
		return fmt.Errorf("progress is counted in pages, chapters, lessons, modules, episodes or percent, not %q", in.Unit)
	}
	if in.Title, err = text("title", in.Title, 160, true); err != nil {
		return err
	}
	if in.Author, err = text("author", in.Author, 120, false); err != nil {
		return err
	}
	if in.Goal, err = text("goal", in.Goal, 200, false); err != nil {
		return err
	}
	if in.Where, err = text("where", in.Where, 40, false); err != nil {
		return err
	}
	if in.Note = strings.TrimSpace(in.Note); len([]rune(in.Note)) > 2000 {
		return errors.New("a note is at most 2000 characters")
	}
	if in.Due = strings.TrimSpace(in.Due); in.Due != "" {
		if _, err := time.Parse("2006-01-02", in.Due); err != nil {
			return fmt.Errorf("due %q is not a date (2026-10-12)", in.Due)
		}
	}
	if in.Current < -1 || in.Total < -1 || in.Current > 100000 || in.Total > 100000 {
		return errors.New("progress is a count from 0")
	}
	if in.Unit == "percent" && in.Current > 100 {
		return errors.New("a percentage is 0 to 100")
	}
	if len(in.Tags) > 12 {
		return errors.New("at most 12 tags")
	}
	for i, t := range in.Tags {
		if in.Tags[i], err = text("tag", strings.ToLower(t), 30, true); err != nil {
			return err
		}
	}
	return in.Source.check()
}

// Save keeps it: a new one, or the one with the same title (and author, when
// both have one), changed by what is given. Moving progress on makes it
// active; reaching the end, or status done, finishes it.
func (k *Knowledge) Save(in LearningInput, now time.Time) (Learning, bool, error) {
	if in.Source.At.IsZero() {
		in.Source.At = now
	}
	if err := checkLearning(&in); err != nil {
		return Learning{}, false, err
	}
	today := now.Format("2006-01-02")
	var out Learning
	var created bool
	err := k.f.with(true, func(d *[]Learning) error {
		idx := -1
		for i, l := range *d {
			if key(l.Title) == key(in.Title) && (in.Author == "" || l.Author == "" || key(l.Author) == key(in.Author)) {
				idx = i
				break
			}
		}
		var l Learning
		if idx >= 0 {
			l = (*d)[idx]
		} else {
			if len(*d) >= maxLearnings {
				return fmt.Errorf("Ghost keeps up to %d things you read or study; forget some first", maxLearnings)
			}
			l = Learning{ID: newID("learn"), Kind: "book", Status: "want", Title: in.Title, Source: in.Source, Created: now}
			created = true
		}
		l.Kind = firstSet(in.Kind, l.Kind)
		l.Author = firstSet(in.Author, l.Author)
		l.Goal = firstSet(in.Goal, l.Goal)
		l.Due = firstSet(in.Due, l.Due)
		l.Unit = firstSet(in.Unit, l.Unit)
		if len(in.Tags) > 0 {
			l.Tags = in.Tags
		}
		moved := false
		if in.Total >= 0 && in.Total != 0 {
			l.Total = in.Total
		}
		if in.Current >= 0 && (in.Current != 0 || in.Status == "want") {
			moved = in.Current != l.Current
			l.Current = in.Current
		}
		if l.Unit == "" && (l.Current > 0 || l.Total > 0) {
			l.Unit = map[string]string{"book": "page", "course": "lesson", "podcast": "episode"}[l.Kind]
			if l.Unit == "" {
				l.Unit = "percent"
			}
		}
		if l.Total > 0 && l.Current > l.Total {
			l.Current = l.Total
		}
		status := in.Status
		if status == "" && moved {
			status = "active"
		}
		if (l.Total > 0 && l.Current >= l.Total) || (l.Unit == "percent" && l.Current >= 100) {
			status = "done"
		}
		if status != "" {
			if status == "active" && l.Started == "" {
				l.Started = today
			}
			if status == "done" {
				if l.Finished == "" {
					l.Finished = today
				}
				if l.Total > 0 {
					l.Current = l.Total
				}
			} else {
				l.Finished = ""
			}
			l.Status = status
		}
		if in.Note != "" {
			if len(l.Notes) >= maxLearningNotes {
				l.Notes = l.Notes[1:]
			}
			l.Notes = append(l.Notes, StudyNote{Text: in.Note, Quote: in.Quote, Where: in.Where, At: now, Source: in.Source})
			moved = true
		}
		if moved || created {
			l.LastTouched = now
		}
		l.Updated = now
		if idx >= 0 {
			(*d)[idx] = l
		} else {
			*d = append(*d, l)
		}
		out = l
		return nil
	})
	return out, created, err
}

// All lists what is being read or studied first (most recently moved on),
// then what they want to, then paused, then finished (most recent first).
func (k *Knowledge) All() ([]Learning, error) {
	var out []Learning
	err := k.f.with(false, func(d *[]Learning) error {
		out = append(out, *d...)
		return nil
	})
	rank := map[string]int{"active": 0, "want": 1, "paused": 2, "done": 3}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		if a.Status == "done" {
			return a.Finished > b.Finished
		}
		return a.LastTouched.After(b.LastTouched) || (a.LastTouched.Equal(b.LastTouched) && a.Updated.After(b.Updated))
	})
	return out, err
}

// Get returns one.
func (k *Knowledge) Get(id string) (Learning, error) {
	var out Learning
	err := k.f.with(false, func(d *[]Learning) error {
		for _, l := range *d {
			if l.ID == id {
				out = l
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// Find returns the one whose title matches (whole, then the start of it).
func (k *Knowledge) Find(title string) (Learning, error) {
	all, err := k.All()
	if err != nil {
		return Learning{}, err
	}
	q := key(title)
	for _, l := range all {
		if key(l.Title) == q {
			return l, nil
		}
	}
	for _, l := range all {
		if q != "" && strings.HasPrefix(key(l.Title), q) {
			return l, nil
		}
	}
	return Learning{}, ErrNotFound
}

// Edit replaces what the owner changed on the phone: the fields as given
// (progress, status, title and so on), and notes kept except those dropped.
func (k *Knowledge) Edit(id string, in LearningInput, dropNotes []int, now time.Time) (Learning, error) {
	in.Source = Source{Kind: "owner", At: now}
	if err := checkLearning(&in); err != nil {
		return Learning{}, err
	}
	var out Learning
	err := k.f.with(true, func(d *[]Learning) error {
		for i, l := range *d {
			if l.ID != id {
				continue
			}
			l.Title, l.Author, l.Goal, l.Due = in.Title, in.Author, in.Goal, in.Due
			l.Kind = firstSet(in.Kind, l.Kind)
			l.Unit = in.Unit
			if in.Current >= 0 {
				l.Current = in.Current
			}
			if in.Total >= 0 {
				l.Total = in.Total
			}
			if in.Status != "" && in.Status != l.Status {
				l.Status = in.Status
				if in.Status == "done" && l.Finished == "" {
					l.Finished = now.Format("2006-01-02")
				}
				if in.Status == "active" && l.Started == "" {
					l.Started = now.Format("2006-01-02")
				}
				if in.Status != "done" {
					l.Finished = ""
				}
			}
			if in.Tags != nil {
				l.Tags = in.Tags
			}
			if len(dropNotes) > 0 {
				drop := map[int]bool{}
				for _, n := range dropNotes {
					drop[n] = true
				}
				kept := l.Notes[:0:0]
				for n, note := range l.Notes {
					if !drop[n] {
						kept = append(kept, note)
					}
				}
				l.Notes = kept
			}
			l.LastTouched, l.Updated = now, now
			(*d)[i] = l
			out = l
			return nil
		}
		return ErrNotFound
	})
	return out, err
}

// Forget removes one.
func (k *Knowledge) Forget(id string) error {
	return k.f.with(true, func(d *[]Learning) error {
		for i, l := range *d {
			if l.ID == id {
				*d = append((*d)[:i], (*d)[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

// Quiet is what is being read or studied but has not moved on in `after`, for
// a gentle nudge ("Still reading Sapiens? You were on page 140").
func (k *Knowledge) Quiet(now time.Time, after time.Duration) []Learning {
	all, _ := k.All()
	var out []Learning
	for _, l := range all {
		if l.Status == "active" && !l.LastTouched.IsZero() && now.Sub(l.LastTouched) > after {
			out = append(out, l)
		}
	}
	return out
}

// Progress is "page 140 of 320", "lesson 6", "40%", or "".
func (l Learning) Progress() string {
	if l.Current == 0 && l.Total == 0 {
		return ""
	}
	if l.Unit == "percent" {
		return fmt.Sprintf("%d%%", l.Current)
	}
	u := l.Unit
	if u == "" {
		u = "page"
	}
	if l.Total > 0 {
		return fmt.Sprintf("%s %d of %d", u, l.Current, l.Total)
	}
	return fmt.Sprintf("%s %d", u, l.Current)
}
