package life

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Person is someone in the owner's life, as the owner has told Ghost about
// them: who they are to the owner, the dates that matter, how to reach them,
// what they like, and when they last spoke. Every note keeps its source.
type Person struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"` // "Mum", "Mom", "Ma"
	Relation string   `json:"relation,omitempty"`
	// Birthday is 2006-01-02, or 01-02 when the year is not known.
	Birthday string   `json:"birthday,omitempty"`
	Phone    string   `json:"phone,omitempty"`
	Email    string   `json:"email,omitempty"`
	Likes    []string `json:"likes,omitempty"`
	Notes    []Note   `json:"notes,omitempty"`
	// KeepInTouchDays: when set, the owner wants a nudge after this long without contact.
	KeepInTouchDays int       `json:"keep_in_touch_days,omitempty"`
	LastContact     time.Time `json:"last_contact,omitempty"`
	Source          Source    `json:"source"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Note is one thing known about a person, with where it came from.
type Note struct {
	Text   string `json:"text"`
	Source Source `json:"source"`
}

// PersonUpdate is what is learned about someone. Empty fields change nothing;
// Likes and Note add to what is there.
type PersonUpdate struct {
	Name            string
	Aliases         []string
	Relation        string
	Birthday        string
	Phone           string
	Email           string
	Likes           []string
	Note            string
	KeepInTouchDays *int
	Source          Source
}

// People is the owner's people.
type People struct{ f *file[[]Person] }

// OpenPeople opens the people store in a workspace.
func OpenPeople(workspace string) *People {
	return &People{f: newFile(workspace, "people.json", []Person{})}
}

const (
	maxPeople      = 2000
	maxNotesPerson = 60
	maxLikes       = 30
)

var phoneRE = regexp.MustCompile(`^\+?[0-9 ()-]{5,24}$`)

// match reports whether q names the person (their name, first name, or an alias).
func (p Person) match(q string) bool {
	q = key(q)
	if q == "" {
		return false
	}
	if key(p.Name) == q {
		return true
	}
	for _, a := range p.Aliases {
		if key(a) == q {
			return true
		}
	}
	return false
}

// Find returns the people a name or words point at, best first: an exact
// name or alias, then a first name, then a relation ("mum"), then anyone
// whose name, notes or likes contain the words.
func (s *People) Find(q string) ([]Person, error) {
	var out []Person
	err := s.f.with(false, func(d *[]Person) error {
		qk := key(q)
		type scored struct {
			p     Person
			score int
		}
		var hits []scored
		for _, p := range *d {
			first := strings.Fields(key(p.Name))
			sc := 0
			switch {
			case p.match(qk):
				sc = 100
			case len(first) > 0 && first[0] == qk:
				sc = 80
			case qk != "" && key(p.Relation) == qk:
				sc = 70
			case qk != "" && strings.Contains(key(p.Name+" "+strings.Join(p.Aliases, " ")), qk):
				sc = 50
			default:
				blob := key(p.Relation + " " + strings.Join(p.Likes, " "))
				for _, n := range p.Notes {
					blob += " " + key(n.Text)
				}
				if qk != "" && strings.Contains(blob, qk) {
					sc = 20
				}
			}
			if sc > 0 {
				hits = append(hits, scored{p, sc})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		for _, h := range hits {
			out = append(out, h.p)
		}
		return nil
	})
	return out, err
}

// All lists everyone, by name.
func (s *People) All() ([]Person, error) {
	var out []Person
	err := s.f.with(false, func(d *[]Person) error {
		out = append(out, *d...)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return key(out[i].Name) < key(out[j].Name) })
	return out, err
}

// Get returns one person by id.
func (s *People) Get(id string) (Person, error) {
	var out Person
	err := s.f.with(false, func(d *[]Person) error {
		for _, p := range *d {
			if p.ID == id {
				out = p
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// Remember adds what was learned about someone: to the person it names (by id,
// or by an exact name or alias), or as someone new. It returns the person as
// they now stand and whether they are new.
func (s *People) Remember(id string, u PersonUpdate, now time.Time) (Person, bool, error) {
	if err := u.Source.check(); err != nil {
		return Person{}, false, err
	}
	if u.Source.At.IsZero() {
		u.Source.At = now
	}
	var out Person
	var created bool
	err := s.f.with(true, func(d *[]Person) error {
		idx := -1
		for i, p := range *d {
			if (id != "" && p.ID == id) || (id == "" && u.Name != "" && p.match(u.Name)) {
				idx = i
				break
			}
		}
		if idx < 0 && id != "" {
			return ErrNotFound
		}
		var p Person
		if idx >= 0 {
			p = (*d)[idx]
		} else {
			if len(*d) >= maxPeople {
				return errors.New("Ghost already keeps the most people it can")
			}
			name, err := text("name", u.Name, 80, true)
			if err != nil {
				return err
			}
			p = Person{ID: newID("person"), Name: name, Source: u.Source, CreatedAt: now}
			created = true
		}
		if err := applyPerson(&p, u, idx >= 0); err != nil {
			return err
		}
		p.UpdatedAt = now
		if idx >= 0 {
			(*d)[idx] = p
		} else {
			*d = append(*d, p)
		}
		out = p
		return nil
	})
	return out, created, err
}

func applyPerson(p *Person, u PersonUpdate, existing bool) error {
	var err error
	if existing && u.Name != "" && !p.match(u.Name) {
		// A new name for someone known (a full name for "Sam"): the old one
		// stays as an alias so it still finds them.
		name, err := text("name", u.Name, 80, true)
		if err != nil {
			return err
		}
		p.Aliases = appendUnique(p.Aliases, p.Name)
		p.Name = name
	}
	for _, a := range u.Aliases {
		a, err := text("alias", a, 60, false)
		if err != nil {
			return err
		}
		if a != "" && key(a) != key(p.Name) {
			p.Aliases = appendUnique(p.Aliases, a)
		}
	}
	if len(p.Aliases) > 12 {
		p.Aliases = p.Aliases[len(p.Aliases)-12:]
	}
	if u.Relation != "" {
		if p.Relation, err = text("relation", u.Relation, 60, false); err != nil {
			return err
		}
	}
	if u.Birthday != "" {
		if _, _, err := day(u.Birthday); err != nil {
			return fmt.Errorf("birthday: %w", err)
		}
		p.Birthday = strings.TrimSpace(u.Birthday)
	}
	if u.Phone != "" {
		ph := strings.TrimSpace(u.Phone)
		if !phoneRE.MatchString(ph) {
			return fmt.Errorf("%q is not a phone number", ph)
		}
		p.Phone = ph
	}
	if u.Email != "" {
		a, err := mail.ParseAddress(strings.TrimSpace(u.Email))
		if err != nil {
			return fmt.Errorf("%q is not an email address", u.Email)
		}
		p.Email = a.Address
	}
	for _, l := range u.Likes {
		l, err := text("like", l, 80, false)
		if err != nil {
			return err
		}
		if l != "" {
			p.Likes = appendUnique(p.Likes, l)
		}
	}
	if len(p.Likes) > maxLikes {
		p.Likes = p.Likes[len(p.Likes)-maxLikes:]
	}
	if u.Note != "" {
		n, err := text("note", u.Note, 300, false)
		if err != nil {
			return err
		}
		dup := false
		for _, x := range p.Notes {
			if key(x.Text) == key(n) {
				dup = true
			}
		}
		if !dup {
			p.Notes = append(p.Notes, Note{Text: n, Source: u.Source})
		}
		if len(p.Notes) > maxNotesPerson {
			p.Notes = p.Notes[len(p.Notes)-maxNotesPerson:]
		}
	}
	if u.KeepInTouchDays != nil {
		if *u.KeepInTouchDays < 0 || *u.KeepInTouchDays > 365 {
			return errors.New("keep in touch is 0 (off) to 365 days")
		}
		p.KeepInTouchDays = *u.KeepInTouchDays
	}
	return nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if key(x) == key(v) {
			return list
		}
	}
	return append(list, v)
}

// Contacted records that the owner was in touch with someone.
func (s *People) Contacted(id string, at time.Time) (Person, error) {
	var out Person
	err := s.f.with(true, func(d *[]Person) error {
		for i := range *d {
			if (*d)[i].ID == id {
				if at.After((*d)[i].LastContact) {
					(*d)[i].LastContact = at
				}
				out = (*d)[i]
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// Edit replaces the fields the owner changed on the People screen. Fields left
// empty are cleared (the owner removed them); notes are only removed by index.
func (s *People) Edit(id string, name, relation, birthday, phone, email string, likes []string, keepInTouch int, dropNotes []int, now time.Time) (Person, error) {
	var out Person
	err := s.f.with(true, func(d *[]Person) error {
		for i := range *d {
			p := (*d)[i]
			if p.ID != id {
				continue
			}
			n, err := text("name", name, 80, true)
			if err != nil {
				return err
			}
			p.Name = n
			p.Relation, p.Birthday, p.Phone, p.Email, p.Likes = "", "", "", "", nil
			ki := keepInTouch
			if err := applyPerson(&p, PersonUpdate{Relation: relation, Birthday: birthday, Phone: phone, Email: email, Likes: likes, KeepInTouchDays: &ki}, false); err != nil {
				return err
			}
			drop := map[int]bool{}
			for _, x := range dropNotes {
				drop[x] = true
			}
			var kept []Note
			for j, nt := range p.Notes {
				if !drop[j] {
					kept = append(kept, nt)
				}
			}
			p.Notes = kept
			p.UpdatedAt = now
			(*d)[i] = p
			out = p
			return nil
		}
		return ErrNotFound
	})
	return out, err
}

// Forget removes someone and everything known about them.
func (s *People) Forget(id string) error {
	return s.f.with(true, func(d *[]Person) error {
		for i, p := range *d {
			if p.ID == id {
				*d = append((*d)[:i], (*d)[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

// NextBirthday is when someone's next birthday is on or after now (in loc),
// and how old they turn if the year is known (0 if not).
func NextBirthday(p Person, now time.Time, loc *time.Location) (time.Time, int, bool) {
	if p.Birthday == "" {
		return time.Time{}, 0, false
	}
	b, hasYear, err := day(p.Birthday)
	if err != nil {
		return time.Time{}, 0, false
	}
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	year := today.Year()
	next := birthdayIn(year, b, loc)
	if next.Before(today) {
		year++
		next = birthdayIn(year, b, loc)
	}
	age := 0
	if hasYear && b.Year() > 1900 {
		age = year - b.Year()
	}
	return next, age, true
}

// birthdayIn is the day a birthday falls on in a year; the 29th of February
// is kept on the 28th in other years.
func birthdayIn(year int, b time.Time, loc *time.Location) time.Time {
	m, d := b.Month(), b.Day()
	if m == time.February && d == 29 && !isLeap(year) {
		d = 28
	}
	return time.Date(year, m, d, 0, 0, 0, 0, loc)
}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }
