package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/attention"
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
	if _, _, err := life.MoneyFor(ws).Track(life.RecurringInput{Kind: "subscription", Name: "Netflix", Amount: "1100", Currency: "KES", Every: "month", Next: "2026-10-12", Source: src}, now); err != nil {
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
	if !strings.Contains(lines["money_subscription:3"], "Netflix renews on Monday (KES 1,100)") {
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
