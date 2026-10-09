package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/attention"
	"github.com/ianclemence/ghost/pkg/jobs"
	"github.com/ianclemence/ghost/pkg/life"
)

func TestLifeItemsComeAtTheRightTimes(t *testing.T) {
	ws := t.TempDir()
	loc := time.UTC
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, loc)
	src := life.Source{Kind: "conversation"}
	if _, _, err := life.PeopleFor(ws).Remember("", life.PersonUpdate{Name: "Grace Wanjiru", Aliases: []string{"mum"}, Birthday: "1961-10-16", Source: src}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := life.PeopleFor(ws).Remember("", life.PersonUpdate{Name: "Sam", Birthday: "10-10", Source: src}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := life.VaultFor(ws).Keep(life.PaperInput{Kind: "passport", Title: "Passport", Expires: "2026-11-08", Source: life.Source{Kind: "photo"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := life.FinancesFor(ws).Track(life.RecurringInput{Kind: "subscription", Name: "Netflix", Amount: "1100", Currency: "KES", Every: "month", Next: "2026-10-12", Source: src}, now); err != nil {
		t.Fatal(err)
	}
	items := lifeItems(ws, now, loc)
	lines := map[string]string{}
	for _, it := range items {
		lines[it.Source+":"+it.Key[strings.LastIndex(it.Key, ":")+1:]] = it.Line
	}
	if !strings.Contains(lines["birthday:week"], "Mum's birthday is on Friday 16 October (turning 65)") {
		t.Fatalf("mum's week-ahead note: %+v", lines)
	}
	if !strings.Contains(lines["birthday:eve"], "Sam's birthday is tomorrow") {
		t.Fatalf("sam's eve note: %+v", lines)
	}
	if !strings.Contains(lines["document_expiry:30"], "Your passport expires on 8 November (in 4 weeks)") {
		t.Fatalf("passport: %+v", lines)
	}
	if !strings.Contains(lines["finances_subscription:3"], "Netflix renews on Monday (KES 1,100)") {
		t.Fatalf("netflix: %+v", lines)
	}
	for _, it := range items {
		if it.Key == "" || it.Expires.IsZero() {
			t.Fatalf("every item has a key and an end: %+v", it)
		}
	}
	// A day with nothing due says nothing.
	if quiet := lifeItems(ws, time.Date(2026, 10, 20, 8, 0, 0, 0, loc), loc); len(quiet) != 0 {
		t.Fatalf("nothing is due on the 20th: %+v", quiet)
	}
}

func TestTripItems(t *testing.T) {
	ws := t.TempDir()
	loc := time.UTC
	src := life.Source{Kind: "conversation"}
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, loc)
	if _, _, err := life.TripsFor(ws).Save(life.TripInput{Title: "Lamu", Destination: "Lamu", Start: "2026-10-16", End: "2026-10-20",
		Legs: []life.Leg{{Kind: "flight", Title: "To Lamu", Ref: "JM101", To: "Lamu", Start: "2026-10-16T09:40"}}, Source: src}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := life.VaultFor(ws).Keep(life.PaperInput{Kind: "passport", Title: "Passport", Expires: "2027-02-01", Source: src}, now); err != nil {
		t.Fatal(err)
	}
	has := func(items []attention.Item, words string) bool {
		for _, it := range items {
			if strings.Contains(it.Line, words) {
				return true
			}
		}
		return false
	}
	week := tripItems(ws, now, loc, time.Date(2026, 10, 9, 0, 0, 0, 0, loc))
	if !has(week, "Your trip to Lamu starts on Friday 16 October") || !has(week, "less than six months after your trip to Lamu ends") {
		t.Fatalf("week before: %+v", week)
	}
	eve := time.Date(2026, 10, 15, 18, 0, 0, 0, loc)
	items := tripItems(ws, eve, loc, time.Date(2026, 10, 15, 0, 0, 0, 0, loc))
	if !has(items, "Tomorrow: JM101 to Lamu at 09:40") || items[0].Reply != "Watch my flight JM101 tomorrow." {
		t.Fatalf("eve: %+v", items)
	}
	early := time.Date(2026, 10, 16, 6, 30, 0, 0, loc)
	if has(tripItems(ws, early, loc, time.Date(2026, 10, 16, 0, 0, 0, 0, loc)), "Time to leave") {
		t.Fatal("too early to say leave")
	}
	leave := time.Date(2026, 10, 16, 6, 45, 0, 0, loc)
	got := tripItems(ws, leave, loc, time.Date(2026, 10, 16, 0, 0, 0, 0, loc))
	if !has(got, "be on your way by 06:55") {
		t.Fatalf("leave: %+v", got)
	}
	for _, it := range got {
		if strings.Contains(it.Line, "Time to leave") && !(it.Urgent && it.Reliable) {
			t.Fatal("leaving for a flight must be able to interrupt")
		}
	}
	back := tripItems(ws, time.Date(2026, 10, 21, 10, 0, 0, 0, loc), loc, time.Date(2026, 10, 21, 0, 0, 0, 0, loc))
	if !has(back, "Welcome back from Lamu") {
		t.Fatalf("after: %+v", back)
	}
}

func TestHealthWeekOnMondays(t *testing.T) {
	ws := t.TempDir()
	var days []life.HealthDay
	for i := 1; i <= 14; i++ {
		steps := 5000
		if i <= 7 {
			steps = 6200
		}
		days = append(days, life.HealthDay{Date: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -i).Format("2006-01-02"), Steps: steps, SleepMinutes: 410})
	}
	if _, err := life.DeviceFor(ws).AddHealth(days); err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC)
	var line string
	for _, it := range lifeItems(ws, monday, time.UTC) {
		if it.Source == "health_weekly" {
			line = it.Line
		}
	}
	if line != "Last week: about 6,200 steps a day (24% more than the week before), 6h50 of sleep a night." {
		t.Fatalf("line: %q", line)
	}
	for _, it := range lifeItems(ws, monday.AddDate(0, 0, 1), time.UTC) {
		if it.Source == "health_weekly" {
			t.Fatal("only on Mondays")
		}
	}
	// With the Health weekly job on, the job is the week: no second line.
	if err := jobs.Open(ws).Set("health", jobs.State{Enabled: true, RoutineID: "r1"}); err != nil {
		t.Fatal(err)
	}
	for _, it := range lifeItems(ws, monday, time.UTC) {
		if it.Source == "health_weekly" {
			t.Fatal("the health job and the Monday line both spoke")
		}
	}
}

func TestRoutineCardsBelongToTheConversation(t *testing.T) {
	if conversationOf("routine:r1") != "main" || conversationOf("main") != "main" || conversationOf("voice:1") != "voice:1" {
		t.Fatal("conversationOf")
	}
	for _, s := range []string{"NOTHING", " nothing. ", "**NOTHING**"} {
		if !SilentRoutineReply(s) {
			t.Fatalf("%q is silent", s)
		}
	}
	if SilentRoutineReply("Nothing is on today, enjoy it.") {
		t.Fatal("a sentence is not silence")
	}
}

func TestLearningItemsBeforeAnExamAndWhenQuiet(t *testing.T) {
	ws := t.TempDir()
	loc := time.UTC
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, loc)
	k := life.KnowledgeFor(ws)
	if _, _, err := k.Save(life.LearningInput{Title: "CPA Section 1", Kind: "subject", Goal: "the CPA exam", Due: "2026-10-16", Status: "active", Current: -1, Total: -1, Source: life.Source{Kind: "conversation"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Save(life.LearningInput{Title: "Sapiens", Current: 140, Total: 443, Source: life.Source{Kind: "conversation"}}, now.AddDate(0, 0, -20)); err != nil {
		t.Fatal(err)
	}
	items := learningItems(ws, now, time.Date(2026, 10, 9, 0, 0, 0, 0, loc))
	var week, quiet bool
	for _, it := range items {
		week = week || strings.Contains(it.Line, "A week to go: the CPA exam")
		quiet = quiet || strings.Contains(it.Line, "Still on Sapiens? You were on page 140 of 443.")
	}
	if !week || !quiet {
		t.Fatalf("items: %+v", items)
	}
}
