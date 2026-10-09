package life

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Trip is a journey the owner is taking, put together from what they said and
// the bookings they forwarded or Ghost read in their email: where and when,
// and each leg (a flight, a train, a hotel stay, a booking) with its own time
// and reference. Ghost uses it to say the right thing at the right moment: the
// week before, the day before a flight, when to leave for the airport, and
// how it went afterwards.
type Trip struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Destination string    `json:"destination,omitempty"`
	Start       string    `json:"start"` // 2006-01-02
	End         string    `json:"end"`   // 2006-01-02
	Legs        []Leg     `json:"legs,omitempty"`
	Notes       string    `json:"notes,omitempty"`
	Source      Source    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Leg is one part of a trip.
type Leg struct {
	Kind  string `json:"kind"` // flight | train | bus | ferry | car | hotel | event | other
	Title string `json:"title"`
	// Ref is a flight number or a booking reference.
	Ref  string `json:"ref,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Start and End are local times, 2006-01-02T15:04 (a hotel's check-in and
	// check-out; a flight's departure and arrival).
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
	Place string `json:"place,omitempty"`
	// International decides how early to be at the airport (3 hours, else 2).
	International bool `json:"international,omitempty"`
	// TravelMinutes is how long it takes the owner to get to where this leg
	// starts, when they said; otherwise Ghost allows 45 minutes.
	TravelMinutes int `json:"travel_minutes,omitempty"`
}

var legKinds = map[string]bool{"flight": true, "train": true, "bus": true, "ferry": true, "car": true, "hotel": true, "event": true, "other": true}

// Trips is the owner's journeys.
type Trips struct{ f *file[[]Trip] }

// OpenTrips opens the trips store in a workspace.
func OpenTrips(workspace string) *Trips {
	return &Trips{f: newFile(workspace, "trips.json", []Trip{})}
}

const (
	maxTrips = 300
	maxLegs  = 30
	// LegTime is the form a leg's times take.
	LegTime = "2006-01-02T15:04"
)

// TripInput is a trip to keep, or the whole of one to replace.
type TripInput struct {
	Title       string
	Destination string
	Start, End  string
	Legs        []Leg
	Notes       string
	Source      Source
}

func checkTrip(in *TripInput) error {
	var err error
	if in.Title, err = text("title", in.Title, 80, true); err != nil {
		return err
	}
	if in.Destination, err = text("destination", in.Destination, 80, false); err != nil {
		return err
	}
	if in.Notes, err = text("notes", in.Notes, 500, false); err != nil {
		return err
	}
	start, err := time.Parse("2006-01-02", strings.TrimSpace(in.Start))
	if err != nil {
		return fmt.Errorf("start %q is not a date (2026-10-12)", in.Start)
	}
	if strings.TrimSpace(in.End) == "" {
		in.End = in.Start
	}
	end, err := time.Parse("2006-01-02", strings.TrimSpace(in.End))
	if err != nil {
		return fmt.Errorf("end %q is not a date (2026-10-12)", in.End)
	}
	if end.Before(start) {
		return errors.New("the trip ends before it starts")
	}
	in.Start, in.End = start.Format("2006-01-02"), end.Format("2006-01-02")
	if len(in.Legs) > maxLegs {
		return fmt.Errorf("a trip has at most %d legs", maxLegs)
	}
	for i := range in.Legs {
		if err := checkLeg(&in.Legs[i]); err != nil {
			return fmt.Errorf("leg %d: %w", i+1, err)
		}
	}
	sort.SliceStable(in.Legs, func(i, j int) bool { return in.Legs[i].Start < in.Legs[j].Start })
	return in.Source.check()
}

func checkLeg(l *Leg) error {
	var err error
	l.Kind = strings.ToLower(strings.TrimSpace(l.Kind))
	if l.Kind == "" {
		l.Kind = "other"
	}
	if !legKinds[l.Kind] {
		return fmt.Errorf("a leg is a flight, train, bus, ferry, car, hotel, event or other, not %q", l.Kind)
	}
	if l.Title, err = text("title", l.Title, 80, true); err != nil {
		return err
	}
	for _, f := range []*string{&l.Ref, &l.From, &l.To, &l.Place} {
		if *f, err = text("detail", *f, 80, false); err != nil {
			return err
		}
	}
	if l.Kind == "flight" {
		l.Ref = strings.ToUpper(strings.ReplaceAll(l.Ref, " ", ""))
	}
	s, err := time.Parse(LegTime, strings.TrimSpace(l.Start))
	if err != nil {
		return fmt.Errorf("start %q is not a date and time (2026-10-12T09:40)", l.Start)
	}
	l.Start = s.Format(LegTime)
	if strings.TrimSpace(l.End) != "" {
		e, err := time.Parse(LegTime, strings.TrimSpace(l.End))
		if err != nil {
			return fmt.Errorf("end %q is not a date and time (2026-10-12T11:05)", l.End)
		}
		if e.Before(s) {
			return errors.New("a leg ends before it starts")
		}
		l.End = e.Format(LegTime)
	}
	if l.TravelMinutes < 0 || l.TravelMinutes > 600 {
		return errors.New("travel time is 0 to 600 minutes")
	}
	return nil
}

// Save keeps a trip: a new one, or the one with the same title starting the
// same day (Ghost read another booking for it), whose legs are merged (a leg
// with the same reference and start is not added twice).
func (t *Trips) Save(in TripInput, now time.Time) (Trip, bool, error) {
	if in.Source.At.IsZero() {
		in.Source.At = now
	}
	if err := checkTrip(&in); err != nil {
		return Trip{}, false, err
	}
	var out Trip
	var created bool
	err := t.f.with(true, func(d *[]Trip) error {
		for i, tr := range *d {
			if key(tr.Title) != key(in.Title) || tr.Start != in.Start {
				continue
			}
			tr.Destination = firstSet(in.Destination, tr.Destination)
			tr.End = in.End
			tr.Notes = firstSet(in.Notes, tr.Notes)
			for _, l := range in.Legs {
				dup := false
				for _, x := range tr.Legs {
					if x.Start == l.Start && (key(x.Ref) == key(l.Ref) || key(x.Title) == key(l.Title)) {
						dup = true
					}
				}
				if !dup && len(tr.Legs) < maxLegs {
					tr.Legs = append(tr.Legs, l)
				}
			}
			sort.SliceStable(tr.Legs, func(a, b int) bool { return tr.Legs[a].Start < tr.Legs[b].Start })
			tr.UpdatedAt = now
			(*d)[i] = tr
			out = tr
			return nil
		}
		if len(*d) >= maxTrips {
			*d = (*d)[1:]
		}
		out = Trip{ID: newID("trip"), Title: in.Title, Destination: in.Destination, Start: in.Start, End: in.End, Legs: in.Legs,
			Notes: in.Notes, Source: in.Source, CreatedAt: now, UpdatedAt: now}
		*d = append(*d, out)
		created = true
		return nil
	})
	return out, created, err
}

func firstSet(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// All lists trips: upcoming and current first (soonest first), then past
// ones (most recent first).
func (t *Trips) All(now time.Time) ([]Trip, error) {
	var out []Trip
	err := t.f.with(false, func(d *[]Trip) error {
		out = append(out, *d...)
		return nil
	})
	today := now.Format("2006-01-02")
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].End < today, out[j].End < today
		if pi != pj {
			return !pi
		}
		if pi {
			return out[i].End > out[j].End
		}
		return out[i].Start < out[j].Start
	})
	return out, err
}

// Get returns one trip.
func (t *Trips) Get(id string) (Trip, error) {
	var out Trip
	err := t.f.with(false, func(d *[]Trip) error {
		for _, tr := range *d {
			if tr.ID == id {
				out = tr
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// Forget removes a trip.
func (t *Trips) Forget(id string) error {
	return t.f.with(true, func(d *[]Trip) error {
		for i, tr := range *d {
			if tr.ID == id {
				*d = append((*d)[:i], (*d)[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

// LeaveBy is when to set off for a leg that starts at an airport or a station:
// the start, less the time to be there early (3 hours for an international
// flight, 2 for a domestic one, 30 minutes for a train or bus), less the time
// to get there. ok is false for a leg with nothing to leave for.
func LeaveBy(l Leg, loc *time.Location) (time.Time, bool) {
	start, err := time.ParseInLocation(LegTime, l.Start, loc)
	if err != nil {
		return time.Time{}, false
	}
	early := 0
	switch l.Kind {
	case "flight":
		early = 120
		if l.International {
			early = 180
		}
	case "train", "bus", "ferry":
		early = 30
	default:
		return time.Time{}, false
	}
	travel := l.TravelMinutes
	if travel == 0 {
		travel = 45
	}
	return start.Add(-time.Duration(early+travel) * time.Minute), true
}

var sharedTrips = map[string]*Trips{}

// TripsFor is the trips store of a workspace.
func TripsFor(workspace string) *Trips {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s, ok := sharedTrips[workspace]; ok {
		return s
	}
	s := OpenTrips(workspace)
	sharedTrips[workspace] = s
	return s
}
