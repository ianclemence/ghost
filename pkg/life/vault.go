package life

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Paper is one of the owner's important documents, as Ghost keeps it: what it
// is, whose it is, the facts on it worth having to hand (a passport number,
// a policy's insurer), when it expires or renews, and the photo or file it was
// read from. Ghost reads the facts off the document; it never makes them up.
type Paper struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Holder string `json:"holder,omitempty"` // whose it is, when not the owner's
	Facts  []Fact `json:"facts,omitempty"`
	// Expires and Renews are 2006-01-02.
	Expires string `json:"expires,omitempty"`
	Renews  string `json:"renews,omitempty"`
	// File is the workspace path of the photo or scan it was read from.
	File      string    `json:"file,omitempty"`
	Source    Source    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Fact is one labelled value read off a document.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PaperKinds is the closed list of what a document can be.
var PaperKinds = []string{"passport", "id", "visa", "license", "insurance", "warranty", "lease", "contract", "vehicle", "medical", "certificate", "ticket", "receipt", "other"}

func validKind(k string) bool {
	for _, x := range PaperKinds {
		if x == k {
			return true
		}
	}
	return false
}

// Vault is the owner's important documents.
type Vault struct{ f *file[[]Paper] }

// OpenVault opens the vault in a workspace.
func OpenVault(workspace string) *Vault {
	return &Vault{f: newFile(workspace, "vault.json", []Paper{})}
}

const (
	maxPapers = 1000
	maxFacts  = 16
)

// PaperInput is a document to keep, or changes to one.
type PaperInput struct {
	Kind    string
	Title   string
	Holder  string
	Facts   []Fact
	Expires string
	Renews  string
	File    string
	Source  Source
}

func checkPaper(in *PaperInput) error {
	var err error
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if in.Kind == "" {
		in.Kind = "other"
	}
	if !validKind(in.Kind) {
		return fmt.Errorf("a document is one of %s, not %q", strings.Join(PaperKinds, ", "), in.Kind)
	}
	if in.Title, err = text("title", in.Title, 100, true); err != nil {
		return err
	}
	if in.Holder, err = text("holder", in.Holder, 80, false); err != nil {
		return err
	}
	if len(in.Facts) > maxFacts {
		return fmt.Errorf("keep at most %d facts from one document", maxFacts)
	}
	for i := range in.Facts {
		if in.Facts[i].Label, err = text("fact label", in.Facts[i].Label, 40, true); err != nil {
			return err
		}
		if in.Facts[i].Value, err = text("fact value", in.Facts[i].Value, 120, true); err != nil {
			return err
		}
	}
	for _, d := range []*string{&in.Expires, &in.Renews} {
		*d = strings.TrimSpace(*d)
		if *d == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", *d); err != nil {
			return fmt.Errorf("%q is not a date (2026-10-12)", *d)
		}
	}
	if in.File != "" {
		f := strings.TrimSpace(in.File)
		if strings.HasPrefix(f, "/") || strings.Contains(f, "..") || len(f) > 300 {
			return errors.New("the file must be a path inside the workspace")
		}
		in.File = f
	}
	return in.Source.check()
}

// Keep adds a document, or updates the one with the same kind, holder and
// title (a renewed passport replaces the old record).
func (v *Vault) Keep(in PaperInput, now time.Time) (Paper, bool, error) {
	if in.Source.At.IsZero() {
		in.Source.At = now
	}
	if err := checkPaper(&in); err != nil {
		return Paper{}, false, err
	}
	var out Paper
	var created bool
	err := v.f.with(true, func(d *[]Paper) error {
		for i, p := range *d {
			if p.Kind == in.Kind && key(p.Holder) == key(in.Holder) && key(p.Title) == key(in.Title) {
				p.Facts, p.Expires, p.Renews, p.UpdatedAt, p.Source = in.Facts, in.Expires, in.Renews, now, in.Source
				if in.File != "" {
					p.File = in.File
				}
				(*d)[i] = p
				out = p
				return nil
			}
		}
		if len(*d) >= maxPapers {
			return errors.New("the vault is full")
		}
		out = Paper{ID: newID("paper"), Kind: in.Kind, Title: in.Title, Holder: in.Holder, Facts: in.Facts,
			Expires: in.Expires, Renews: in.Renews, File: in.File, Source: in.Source, CreatedAt: now, UpdatedAt: now}
		*d = append(*d, out)
		created = true
		return nil
	})
	return out, created, err
}

// Update replaces a document's fields (the owner edited it).
func (v *Vault) Update(id string, in PaperInput, now time.Time) (Paper, error) {
	if in.Source.Kind == "" {
		in.Source = Source{Kind: "owner", At: now}
	}
	if err := checkPaper(&in); err != nil {
		return Paper{}, err
	}
	var out Paper
	err := v.f.with(true, func(d *[]Paper) error {
		for i, p := range *d {
			if p.ID != id {
				continue
			}
			p.Kind, p.Title, p.Holder, p.Facts, p.Expires, p.Renews, p.UpdatedAt = in.Kind, in.Title, in.Holder, in.Facts, in.Expires, in.Renews, now
			if in.File != "" {
				p.File = in.File
			}
			(*d)[i] = p
			out = p
			return nil
		}
		return ErrNotFound
	})
	return out, err
}

// All lists the documents: soonest to expire first, then by title.
func (v *Vault) All() ([]Paper, error) {
	var out []Paper
	err := v.f.with(false, func(d *[]Paper) error {
		out = append(out, *d...)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		a, b := dueDate(out[i]), dueDate(out[j])
		if a != b {
			if a == "" {
				return false
			}
			if b == "" {
				return true
			}
			return a < b
		}
		return key(out[i].Title) < key(out[j].Title)
	})
	return out, err
}

// Find returns documents whose kind, title, holder or facts mention q.
func (v *Vault) Find(q string) ([]Paper, error) {
	all, err := v.All()
	if err != nil {
		return nil, err
	}
	qk := key(q)
	var out []Paper
	for _, p := range all {
		blob := key(p.Kind + " " + p.Title + " " + p.Holder)
		for _, f := range p.Facts {
			blob += " " + key(f.Label+" "+f.Value)
		}
		if qk == "" || strings.Contains(blob, qk) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Get returns one document.
func (v *Vault) Get(id string) (Paper, error) {
	var out Paper
	err := v.f.with(false, func(d *[]Paper) error {
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

// Forget removes a document from the vault (the photo or file stays where it is).
func (v *Vault) Forget(id string) error {
	return v.f.with(true, func(d *[]Paper) error {
		for i, p := range *d {
			if p.ID == id {
				*d = append((*d)[:i], (*d)[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

// dueDate is the date that matters next on a document: its expiry, else its renewal.
func dueDate(p Paper) string {
	if p.Expires != "" {
		return p.Expires
	}
	return p.Renews
}

// DaysLeft is how many whole days until a document's next date, and which
// (expires/renews); ok is false when it has none.
func DaysLeft(p Paper, now time.Time, loc *time.Location) (days int, what string, ok bool) {
	d := p.Expires
	what = "expires"
	if d == "" {
		d, what = p.Renews, "renews"
	}
	if d == "" {
		return 0, "", false
	}
	t, err := time.ParseInLocation("2006-01-02", d, loc)
	if err != nil {
		return 0, "", false
	}
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	return int(t.Sub(today).Hours() / 24), what, true
}
