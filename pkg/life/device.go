package life

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// What the owner's phone tells Ghost, when the owner turned it on: the
// notifications of the apps they chose, their daily health totals (steps,
// sleep, resting heart rate), and the places they asked to be reminded at.
// All of it stays on the Pod; the phone sends only what each switch allows.

// PhoneNote is one notification the phone passed on.
type PhoneNote struct {
	App   string    `json:"app"`
	Title string    `json:"title,omitempty"`
	Text  string    `json:"text,omitempty"`
	At    time.Time `json:"at"`
}

// HealthDay is one day's totals.
type HealthDay struct {
	Date         string `json:"date"` // 2006-01-02
	Steps        int    `json:"steps,omitempty"`
	SleepMinutes int    `json:"sleep_minutes,omitempty"`
	RestingHR    int    `json:"resting_hr,omitempty"`
}

// Place is somewhere the owner asked to be reminded of something.
type Place struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Lat     float64   `json:"lat"`
	Lon     float64   `json:"lon"`
	Radius  int       `json:"radius"` // metres
	Message string    `json:"message"`
	On      string    `json:"on"` // enter | exit
	Once    bool      `json:"once"`
	Active  bool      `json:"active"`
	Fired   time.Time `json:"fired,omitempty"`
	Created time.Time `json:"created"`
}

// Sharing is what the owner lets the phone share, as the phone last said:
// whether notifications are on and from which apps (by name).
type Sharing struct {
	Notifications bool      `json:"notifications"`
	Apps          []string  `json:"apps,omitempty"`
	At            time.Time `json:"at"`
}

type deviceData struct {
	Sharing *Sharing    `json:"sharing,omitempty"`
	Notes   []PhoneNote `json:"notes"`
	Health  []HealthDay `json:"health"`
	Places  []Place     `json:"places"`
}

// Device is what the phone shares with the Pod.
type Device struct{ f *file[deviceData] }

const (
	maxNotes   = 600
	noteDays   = 7
	maxHealth  = 400
	maxPlaces  = 50
	maxPlacesR = 2000
)

var sharedDevice = map[string]*Device{}

// DeviceFor is the device store of a workspace.
func DeviceFor(workspace string) *Device {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s, ok := sharedDevice[workspace]; ok {
		return s
	}
	s := &Device{f: newFile(workspace, "device.json", deviceData{})}
	sharedDevice[workspace] = s
	return s
}

// SetSharing records what the phone says it shares.
func (d *Device) SetSharing(on bool, apps []string, now time.Time) error {
	clean := []string{}
	for _, a := range apps {
		if a, _ := text("app", a, 60, true); a != "" && len(clean) < 60 {
			clean = append(clean, a)
		}
	}
	return d.f.with(true, func(x *deviceData) error {
		x.Sharing = &Sharing{Notifications: on, Apps: clean, At: now.UTC()}
		return nil
	})
}

// SharingNow is what the phone last said it shares (nil before it has said).
func (d *Device) SharingNow() (*Sharing, error) {
	var out *Sharing
	err := d.f.with(false, func(x *deviceData) error {
		if x.Sharing != nil {
			c := *x.Sharing
			out = &c
		}
		return nil
	})
	return out, err
}

// AddNotes keeps notifications the phone sent (cut short, a week kept). The
// same notification sent twice is kept once.
func (d *Device) AddNotes(notes []PhoneNote, now time.Time) (int, error) {
	added := 0
	err := d.f.with(true, func(x *deviceData) error {
		seen := map[string]bool{}
		for _, n := range x.Notes {
			seen[n.App+"|"+n.Title+"|"+n.Text+"|"+n.At.Format(time.RFC3339)] = true
		}
		for _, n := range notes {
			app, _ := text("app", n.App, 60, true)
			if app == "" {
				continue
			}
			title, _ := text("title", trunc(n.Title, 120), 120, false)
			body, _ := text("text", trunc(n.Text, 400), 400, false)
			if n.At.IsZero() || n.At.After(now.Add(time.Hour)) {
				n.At = now
			}
			k := app + "|" + title + "|" + body + "|" + n.At.UTC().Format(time.RFC3339)
			if seen[k] {
				continue
			}
			seen[k] = true
			x.Notes = append(x.Notes, PhoneNote{App: app, Title: title, Text: body, At: n.At.UTC()})
			added++
		}
		cut := now.AddDate(0, 0, -noteDays)
		kept := x.Notes[:0]
		for _, n := range x.Notes {
			if n.At.After(cut) {
				kept = append(kept, n)
			}
		}
		x.Notes = kept
		if len(x.Notes) > maxNotes {
			x.Notes = x.Notes[len(x.Notes)-maxNotes:]
		}
		return nil
	})
	return added, err
}

func trunc(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return string(r)
}

// Notes returns notifications from the last hours (newest first), from one
// app or all, whose words contain q.
func (d *Device) Notes(app, q string, hours int, now time.Time) ([]PhoneNote, error) {
	if hours <= 0 || hours > 24*noteDays {
		hours = 24
	}
	var out []PhoneNote
	err := d.f.with(false, func(x *deviceData) error {
		cut := now.Add(-time.Duration(hours) * time.Hour)
		for i := len(x.Notes) - 1; i >= 0; i-- {
			n := x.Notes[i]
			if n.At.Before(cut) {
				continue
			}
			if app != "" && !strings.Contains(key(n.App), key(app)) {
				continue
			}
			if q != "" && !strings.Contains(key(n.Title+" "+n.Text+" "+n.App), key(q)) {
				continue
			}
			out = append(out, n)
		}
		return nil
	})
	return out, err
}

// ForgetNotes clears every notification kept (the owner turned sharing off).
func (d *Device) ForgetNotes() error {
	return d.f.with(true, func(x *deviceData) error { x.Notes = nil; return nil })
}

// AddHealth keeps daily totals; a day sent again replaces the old numbers.
func (d *Device) AddHealth(days []HealthDay) (int, error) {
	n := 0
	err := d.f.with(true, func(x *deviceData) error {
		for _, h := range days {
			if _, err := time.Parse("2006-01-02", h.Date); err != nil {
				continue
			}
			if h.Steps < 0 || h.Steps > 200000 || h.SleepMinutes < 0 || h.SleepMinutes > 24*60 || h.RestingHR < 0 || h.RestingHR > 250 {
				continue
			}
			replaced := false
			for i := range x.Health {
				if x.Health[i].Date == h.Date {
					x.Health[i] = h
					replaced = true
				}
			}
			if !replaced {
				x.Health = append(x.Health, h)
			}
			n++
		}
		sort.Slice(x.Health, func(i, j int) bool { return x.Health[i].Date < x.Health[j].Date })
		if len(x.Health) > maxHealth {
			x.Health = x.Health[len(x.Health)-maxHealth:]
		}
		return nil
	})
	return n, err
}

// Health returns the days from..to (inclusive), oldest first.
func (d *Device) Health(from, to string) ([]HealthDay, error) {
	var out []HealthDay
	err := d.f.with(false, func(x *deviceData) error {
		for _, h := range x.Health {
			if h.Date >= from && h.Date <= to {
				out = append(out, h)
			}
		}
		return nil
	})
	return out, err
}

// HealthWeek is a week of health in a few numbers, against the week before.
type HealthWeek struct {
	Days        int     `json:"days"`
	Steps       int     `json:"steps"`
	PrevSteps   int     `json:"prev_steps"`
	SleepMin    int     `json:"sleep_minutes"`
	PrevSleep   int     `json:"prev_sleep_minutes"`
	RestingHR   int     `json:"resting_hr"`
	PrevRestHR  int     `json:"prev_resting_hr"`
	StepsChange float64 `json:"steps_change"`
}

// Week averages the seven days ending on day (and the seven before them).
func (d *Device) Week(day time.Time) (HealthWeek, error) {
	end := day.Format("2006-01-02")
	start := day.AddDate(0, 0, -6).Format("2006-01-02")
	prevEnd := day.AddDate(0, 0, -7).Format("2006-01-02")
	prevStart := day.AddDate(0, 0, -13).Format("2006-01-02")
	cur, err := d.Health(start, end)
	if err != nil {
		return HealthWeek{}, err
	}
	prev, _ := d.Health(prevStart, prevEnd)
	avg := func(list []HealthDay, f func(HealthDay) int) int {
		sum, n := 0, 0
		for _, h := range list {
			if v := f(h); v > 0 {
				sum += v
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return int(math.Round(float64(sum) / float64(n)))
	}
	steps := func(h HealthDay) int { return h.Steps }
	sleep := func(h HealthDay) int { return h.SleepMinutes }
	hr := func(h HealthDay) int { return h.RestingHR }
	w := HealthWeek{Days: len(cur), Steps: avg(cur, steps), PrevSteps: avg(prev, steps), SleepMin: avg(cur, sleep), PrevSleep: avg(prev, sleep), RestingHR: avg(cur, hr), PrevRestHR: avg(prev, hr)}
	if w.PrevSteps > 0 {
		w.StepsChange = float64(w.Steps-w.PrevSteps) / float64(w.PrevSteps) * 100
	}
	return w, nil
}

// AddPlace keeps a place reminder.
func (d *Device) AddPlace(name string, lat, lon float64, radius int, message, on string, once bool, now time.Time) (Place, error) {
	var err error
	p := Place{Lat: lat, Lon: lon, Radius: radius, On: strings.ToLower(strings.TrimSpace(on)), Once: once, Active: true, Created: now.UTC()}
	if p.Name, err = text("place", name, 60, true); err != nil {
		return Place{}, err
	}
	if p.Message, err = text("message", message, 200, true); err != nil {
		return Place{}, err
	}
	if p.On == "" {
		p.On = "enter"
	}
	if p.On != "enter" && p.On != "exit" {
		return Place{}, errors.New("a place reminder goes off on arriving (enter) or leaving (exit)")
	}
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return Place{}, errors.New("the place needs real coordinates")
	}
	if p.Radius == 0 {
		p.Radius = 150
	}
	if p.Radius < 75 || p.Radius > maxPlacesR {
		return Place{}, fmt.Errorf("the radius is 75 to %d metres", maxPlacesR)
	}
	err = d.f.with(true, func(x *deviceData) error {
		active := 0
		for _, q := range x.Places {
			if q.Active {
				active++
			}
		}
		if active >= maxPlaces {
			return fmt.Errorf("Ghost already watches %d places (the most a phone allows)", maxPlaces)
		}
		p.ID = newID("place")
		x.Places = append(x.Places, p)
		return nil
	})
	return p, err
}

// Places lists place reminders (active ones when active is true).
func (d *Device) Places(active bool) ([]Place, error) {
	var out []Place
	err := d.f.with(false, func(x *deviceData) error {
		for _, p := range x.Places {
			if !active || p.Active {
				out = append(out, p)
			}
		}
		return nil
	})
	return out, err
}

// CancelPlace stops a place reminder.
func (d *Device) CancelPlace(id string) (Place, error) {
	var out Place
	err := d.f.with(true, func(x *deviceData) error {
		for i := range x.Places {
			if x.Places[i].ID == id {
				x.Places[i].Active = false
				out = x.Places[i]
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// Arrived records that the phone crossed a place's edge, and returns the
// reminder to say if this is the crossing it waits for. A reminder that fires
// once stops; any reminder is said at most once an hour.
func (d *Device) Arrived(id, event string, now time.Time) (Place, bool, error) {
	var out Place
	fire := false
	err := d.f.with(true, func(x *deviceData) error {
		for i := range x.Places {
			p := &x.Places[i]
			if p.ID != id {
				continue
			}
			out = *p
			if !p.Active || p.On != event || (!p.Fired.IsZero() && now.Sub(p.Fired) < time.Hour) {
				return nil
			}
			p.Fired = now.UTC()
			if p.Once {
				p.Active = false
			}
			out = *p
			fire = true
			return nil
		}
		return ErrNotFound
	})
	return out, fire, err
}
