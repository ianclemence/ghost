// Package jobs is what Ghost can take on for the owner, offered as jobs rather
// than skills: a morning brief, an inbox kept in hand, bills and
// subscriptions, trips, meals and groceries, life admin, the home, a week of
// health, learning something. Each job is a routine underneath (a time and an
// instruction the routine engine runs as a turn) written against the tools
// Ghost has, so turning a job on is one switch and a time, not a filing
// decision. What a job needs (a connected mailbox, Home Assistant, health
// shared from the phone) is said up front, honestly.
package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Job is one thing Ghost can take on.
type Job struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Promise string `json:"promise"`
	// When says how often, in words ("Every morning").
	When string `json:"when"`
	// Days is the cron day fields ("* * *" daily, "* * 0" Sundays, "1 * *" monthly).
	Days string `json:"-"`
	// Time is the default time of day (HH:MM); empty for a job with no routine.
	Time string `json:"time,omitempty"`
	// Needs names what must be connected or switched on: email, calendar, home, health.
	Needs []string `json:"needs,omitempty"`
	// Ask is what the job asks at setup (a topic for Learn), or "".
	Ask         string `json:"ask,omitempty"`
	instruction string
}

// Catalog is every job, in the order they are offered.
var Catalog = []Job{
	{ID: "morning_brief", Title: "Morning brief", When: "Every morning", Days: "* * *", Time: "07:30",
		Promise:     "Your day in one card: what's on, the weather, what needs you, and one thing worth knowing.",
		instruction: `Give the owner their morning brief, as one present_card and at most one sentence. Gather: today's calendar (calendar tool, if connected), the weather where they are (weather_now), anything that needs them today (bills due or renewing this week from money; documents expiring within 30 days from vault; birthdays this week from people; trips starting this week from trips), and at most one thing you noticed that is worth knowing. Use a timeline for the day's plan, facts for the weather, a list for what needs them. Leave out any part with nothing in it; never invent an event. If nothing at all is on, say so in one warm line instead of a card.`},
	{ID: "inbox", Title: "Inbox in hand", When: "Every evening", Days: "* * *", Time: "17:30", Needs: []string{"email"},
		Promise:     "What in today's email needs a reply, with the replies drafted for you to check and send.",
		instruction: `Look through the owner's email from today (email_search, newer_than:1d, skip newsletters, promotions and notifications). Show what needs a reply or an action as one present_card with a list (who, what about, what they need). For each one that needs a reply, write it with the draft tool (kind email, in the owner's voice, short) so they can check and send it. Do not send anything. If nothing needs them, say so in one line.`},
	{ID: "bills", Title: "Bills and subscriptions", When: "Every Sunday", Days: "* * 0", Time: "10:00",
		Promise:     "What's due, what renewed, and what you might not use any more.",
		instruction: `Do the owner's weekly money check. If a mailbox is connected, look through the last 7 days of email (email_search: receipt OR invoice OR subscription OR renewal OR "payment due") for bills and subscriptions not yet tracked (money list recurring=true); ask about them with ONE present_card that has a checklist of what you found (amount, how often, next date) and a submit "Track these", rather than tracking them yourself. Then show the month so far (money summary) as a present_card: a metric for what was spent, a chart of the weeks, the top categories as a list, and what is still due this month. If a subscription has renewed several times and the owner never mentions it, ask gently whether they still use it.`},
	{ID: "trips", Title: "Trip companion", When: "Every Sunday", Days: "* * 0", Time: "09:00", Needs: []string{"email"},
		Promise:     "Your bookings put together into trips, and the right reminder at the right time.",
		instruction: `Look through the owner's email from the last 7 days (email_search: booking OR reservation OR itinerary OR "e-ticket" OR "boarding pass" OR confirmation) for flights, trains, hotels and tickets. Save each trip with the trips tool (real times, references and places only; never guess). Then, for any trip in the next 14 days, show it with present_card as a timeline. If you found nothing new and nothing is coming up, stay silent: reply with nothing but "NOTHING".`},
	{ID: "meals", Title: "Meals and groceries", When: "Every Saturday", Days: "* * 6", Time: "09:00",
		Promise:     "A week of meals you'll actually like, and the shopping list to tick off in the shop.",
		instruction: `Plan the owner's meals for the coming week from what you know they like, avoid and can cook (memory and context_get), and what is in season. Show the week as one present_card (a list: day, meal, one line on it). Then send a second present_card titled "Groceries" with ONE checklist block (key items) of everything to buy, grouped sensibly in the labels (produce first, then dairy, pantry). The checklist needs no submit: the owner ticks it in the shop.`},
	{ID: "life_admin", Title: "Life admin", When: "On the 1st of each month", Days: "1 * *", Time: "09:00",
		Promise:     "Renewals, documents and deadlines for the month ahead, in one list.",
		instruction: `Do the owner's monthly life admin review. From vault (documents expiring or renewing in the next 90 days), money (bills and subscriptions due this month), people (birthdays this month) and trips (trips this month), make ONE present_card with a checklist block (key admin) of what needs doing this month, soonest first, each line saying what and when. No submit: the owner ticks them as they go. If a passport expires within six months of a trip, put it first. If there is nothing, say so in one line.`},
	{ID: "home", Title: "Home at night", When: "Every night", Days: "* * *", Time: "22:30", Needs: []string{"home"},
		Promise:     "A last look around the house before bed, and a word only if something's on.",
		instruction: `Check the owner's home through Home Assistant (device tool): lights left on, the heating or air-conditioning running, doors or windows open, locks unlocked. If everything is as it should be, reply with nothing but "NOTHING". Otherwise say what is on or open in one or two short lines and offer to switch it off; do not change anything without the owner's yes.`},
	{ID: "health", Title: "Health weekly", When: "Every Sunday evening", Days: "* * 0", Time: "19:00", Needs: []string{"health"},
		Promise:     "Your week of steps and sleep, gently, with one small idea for the next.",
		instruction: `Show the owner their week (phone health, days 14): one present_card with a bar chart of steps by day, a line chart of hours of sleep, and a note with one small, kind suggestion for the coming week. Compare with the week before in plain words. Never diagnose, never alarm; if something looks unusual, suggest they mention it to a doctor. If there is no health data, say that health sharing is off (Settings → Phone) in one line.`},
	{ID: "learn", Title: "Learn something", When: "Every evening", Days: "* * *", Time: "20:00", Ask: "What would you like to learn?",
		Promise:     "A few questions a day on what you want to learn, building on the last.",
		instruction: `Teach the owner a little about: %s. First look at what you asked them before (session_search for "Learn: %s") so today builds on it and revisits what they got wrong. Then send ONE present_card titled "Learn: %s" with three choice blocks (keys q1, q2, q3: one question each, 3 or 4 options, one right) and a submit "Check my answers". When their answers come back, say which were right, explain the ones they missed in a sentence each, and add one new idea for tomorrow.`},
	{ID: "watch", Title: "Watch this for me", When: "Whenever you ask",
		Promise: "Send a link and say what you're waiting for: a lower price, back in stock, a free slot. Ghost checks and tells you.",
	},
}

// Find returns a job by id.
func Find(id string) (Job, bool) {
	for _, j := range Catalog {
		if j.ID == id {
			return j, true
		}
	}
	return Job{}, false
}

var timeRE = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// Settings are what the owner chose for a job.
type Settings struct {
	Time  string `json:"time,omitempty"`
	Topic string `json:"topic,omitempty"`
}

// Check validates settings for a job, filling the default time.
func (j Job) Check(s Settings) (Settings, error) {
	if j.Time == "" {
		return s, errors.New("this job has nothing to schedule: it works whenever you ask")
	}
	if s.Time == "" {
		s.Time = j.Time
	}
	if !timeRE.MatchString(s.Time) {
		return s, fmt.Errorf("%q is not a time (07:30)", s.Time)
	}
	s.Topic = strings.Join(strings.Fields(s.Topic), " ")
	if j.Ask != "" && s.Topic == "" {
		return s, errors.New(j.Ask)
	}
	if len([]rune(s.Topic)) > 80 {
		return s, errors.New("keep the topic under 80 characters")
	}
	return s, nil
}

// Cron is the job's schedule at the owner's time.
func (j Job) Cron(s Settings) string {
	m := timeRE.FindStringSubmatch(s.Time)
	return fmt.Sprintf("%s %s %s", strings.TrimLeft(m[2], "0")+zeroIfEmpty(m[2]), strings.TrimLeft(m[1], "0")+zeroIfEmpty(m[1]), j.Days)
}

func zeroIfEmpty(s string) string {
	if strings.TrimLeft(s, "0") == "" {
		return "0"
	}
	return ""
}

// Instruction is what the routine asks Ghost to do each time.
func (j Job) Instruction(s Settings) string {
	if strings.Contains(j.instruction, "%s") {
		t := s.Topic
		return fmt.Sprintf(j.instruction, t, t, t)
	}
	return j.instruction
}

// State is whether a job is on, and the routine that carries it.
type State struct {
	Enabled   bool      `json:"enabled"`
	RoutineID string    `json:"routine_id,omitempty"`
	Settings  Settings  `json:"settings"`
	Since     time.Time `json:"since,omitempty"`
}

// Store keeps which jobs are on (data/jobs.json).
type Store struct {
	mu   sync.Mutex
	path string
}

func Open(workspace string) *Store {
	return &Store{path: filepath.Join(workspace, "data", "jobs.json")}
}

func (s *Store) load() (map[string]State, error) {
	out := map[string]State{}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// All returns every job's state.
func (s *Store) All() (map[string]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Set records a job's state.
func (s *Store) Set(id string, st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	if st.Enabled {
		all[id] = st
	} else {
		delete(all, id)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(all, "", " ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
