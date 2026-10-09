package life

import (
	"strings"
	"testing"
	"time"
)

var said = Source{Kind: "conversation", Quote: "my mum's birthday is 12 March"}

func TestPeopleAreFoundByNameAliasAndRelation(t *testing.T) {
	ws := t.TempDir()
	p := OpenPeople(ws)
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	mum, created, err := p.Remember("", PersonUpdate{Name: "Grace Wanjiru", Aliases: []string{"Mum"}, Relation: "mother", Birthday: "1961-03-12", Likes: []string{"orchids"}, Source: said}, now)
	if err != nil || !created {
		t.Fatalf("remember: %v %v", created, err)
	}
	// The same person again, by an alias: one person, more known.
	again, created, err := p.Remember("", PersonUpdate{Name: "mum", Note: "Lives in Nyeri", Likes: []string{"Orchids", "tea"}, Source: said}, now)
	if err != nil || created || again.ID != mum.ID || len(again.Likes) != 2 || len(again.Notes) != 1 {
		t.Fatalf("second remember: created=%v %+v %v", created, again, err)
	}
	for _, q := range []string{"Mum", "grace", "mother", "nyeri"} {
		found, _ := p.Find(q)
		if len(found) == 0 || found[0].ID != mum.ID {
			t.Fatalf("find %q: %+v", q, found)
		}
	}
	if _, _, err := p.Remember("", PersonUpdate{Name: "Sam", Email: "not-an-email", Source: said}, now); err == nil {
		t.Fatal("a bad email was kept")
	}
	if _, _, err := p.Remember("", PersonUpdate{Name: "Sam", Birthday: "the 3rd", Source: said}, now); err == nil {
		t.Fatal("a bad birthday was kept")
	}
	if _, _, err := p.Remember("", PersonUpdate{Name: "Sam", Source: Source{Kind: "rumour"}}, now); err == nil {
		t.Fatal("an unknown source was kept")
	}
	next, age, ok := NextBirthday(again, now, time.UTC)
	if !ok || next.Format("2006-01-02") != "2027-03-12" || age != 66 {
		t.Fatalf("next birthday %v age %d", next, age)
	}
	if err := p.Forget(mum.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := OpenPeople(ws).All(); len(all) != 0 {
		t.Fatalf("forgotten person is still kept on disk: %+v", all)
	}
}

func TestLeapDayBirthdays(t *testing.T) {
	p := Person{Birthday: "02-29"}
	next, age, ok := NextBirthday(p, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if !ok || next.Format("01-02") != "02-28" || age != 0 {
		t.Fatalf("leap day in a common year: %v %d", next, age)
	}
}

func TestVaultKeepsOneRecordPerDocumentAndOrdersByWhatIsDue(t *testing.T) {
	v := OpenVault(t.TempDir())
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	photo := Source{Kind: "photo", Ref: "uploads/passport.jpg"}
	pass, created, err := v.Keep(PaperInput{Kind: "passport", Title: "Kenyan passport", Facts: []Fact{{"Number", "AK123456"}}, Expires: "2027-02-01", File: "uploads/passport.jpg", Source: photo}, now)
	if err != nil || !created {
		t.Fatal(err)
	}
	renewed, created, err := v.Keep(PaperInput{Kind: "passport", Title: "kenyan passport", Facts: []Fact{{"Number", "AK999999"}}, Expires: "2037-02-01", Source: photo}, now)
	if err != nil || created || renewed.ID != pass.ID || renewed.File != "uploads/passport.jpg" {
		t.Fatalf("renewed: %v %+v", created, renewed)
	}
	if _, _, err := v.Keep(PaperInput{Kind: "insurance", Title: "Car insurance", Renews: "2026-11-01", Source: photo}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Keep(PaperInput{Kind: "spaceship", Title: "x", Source: photo}, now); err == nil {
		t.Fatal("an unknown kind was kept")
	}
	if _, _, err := v.Keep(PaperInput{Kind: "other", Title: "x", File: "../../etc/passwd", Source: photo}, now); err == nil {
		t.Fatal("a file outside the workspace was kept")
	}
	all, _ := v.All()
	if len(all) != 2 || all[0].Kind != "insurance" {
		t.Fatalf("soonest due first: %+v", all)
	}
	days, what, ok := DaysLeft(all[0], now, time.UTC)
	if !ok || days != 23 || what != "renews" {
		t.Fatalf("days left %d %s", days, what)
	}
	if found, _ := v.Find("ak999"); len(found) != 1 {
		t.Fatalf("find by a fact: %+v", found)
	}
}

func TestMoneyAmountsSummariesAndRecurring(t *testing.T) {
	m := OpenMoney(t.TempDir())
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	src := Source{Kind: "conversation"}
	if got, _ := ParseAmount("1,250.50", "KES"); got != 125050 {
		t.Fatalf("amount = %d", got)
	}
	if got, _ := ParseAmount("1200", "UGX"); got != 1200 {
		t.Fatalf("no minor units: %d", got)
	}
	if FormatAmount(125050, "KES") != "KES 1,250.50" || FormatAmount(100000, "KES") != "KES 1,000" {
		t.Fatalf("format: %s %s", FormatAmount(125050, "KES"), FormatAmount(100000, "KES"))
	}
	for _, bad := range []EntryInput{
		{Amount: "-5", Currency: "KES", Source: src},
		{Amount: "5", Currency: "shillings", Source: src},
		{Amount: "5", Currency: "KES", Category: "yachts", Source: src},
		{Amount: "5", Currency: "KES", Date: "2026-12-25", Source: src},
	} {
		if _, _, err := m.Record(bad, now); err == nil {
			t.Fatalf("bad entry kept: %+v", bad)
		}
	}
	rec := func(amount, cat, date string) {
		if _, _, err := m.Record(EntryInput{Amount: amount, Currency: "KES", Category: cat, Merchant: "x" + cat, Date: date, Source: src}, now); err != nil {
			t.Fatal(err)
		}
	}
	rec("1000", "groceries", "2026-10-02")
	rec("500", "transport", "2026-10-15")
	rec("2500", "groceries", "2026-10-29")
	rec("800", "groceries", "2026-09-10")
	if _, added, _ := m.Record(EntryInput{Amount: "1000", Currency: "KES", Category: "groceries", Merchant: "xgroceries", Date: "2026-10-02", Source: src}, now); added {
		t.Fatal("the same receipt was kept twice")
	}
	if _, _, err := m.Record(EntryInput{Kind: "income", Amount: "60000", Currency: "KES", Date: "2026-10-01", Source: src}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Record(EntryInput{Amount: "20", Currency: "USD", Category: "subscriptions", Date: "2026-10-05", Source: src}, now); err != nil {
		t.Fatal(err)
	}
	netflix, _, err := m.Track(RecurringInput{Kind: "subscription", Name: "Netflix", Amount: "1100", Currency: "KES", Every: "month", Next: "2026-10-31", Source: src}, now)
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.Summary("2026-10", now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Currency != "KES" || s.Spent != 400000 || s.Earned != 6000000 || s.PrevSpent != 80000 {
		t.Fatalf("summary: %+v", s)
	}
	if s.ByCategory[0].Category != "groceries" || s.ByCategory[0].Amount != 350000 {
		t.Fatalf("by category: %+v", s.ByCategory)
	}
	if s.ByWeek[0] != 100000 || s.ByWeek[2] != 50000 || s.ByWeek[4] != 250000 {
		t.Fatalf("by week: %+v", s.ByWeek)
	}
	if s.Other["USD"] != 2000 || len(s.Upcoming) != 1 || s.Upcoming[0].ID != netflix.ID {
		t.Fatalf("other currencies and upcoming: %+v %+v", s.Other, s.Upcoming)
	}
	moved, _ := m.Advance(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC), time.UTC)
	if len(moved) != 1 || moved[0].Next != "2026-11-30" {
		t.Fatalf("the 31st falls on the last day of November: %+v", moved)
	}
	moved, _ = m.Advance(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC), time.UTC)
	if len(moved) != 1 || moved[0].Next != "2026-12-31" {
		t.Fatalf("and back on the 31st in December: %+v", moved)
	}
}

func TestImportStatements(t *testing.T) {
	m := OpenMoney(t.TempDir())
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	mpesa := "Receipt No.,Completion Time,Details,Transaction Status,Paid In,Withdrawn,Balance\n" +
		"QJK1,2026-10-01 08:12:01,Pay Bill to KPLC PREPAID,Completed,,1500.00,9000\n" +
		"QJK2,2026-10-02 13:00:00,Merchant Payment to NAIVAS,Completed,,2340.50,6659.50\n" +
		"QJK3,2026-10-03 09:00:00,Funds received from SAM,Completed,5000.00,,11659.50\n" +
		"QJK4,not a date,Broken,Completed,,1,1\n"
	added, skipped, problem := m.ImportCSV(strings.NewReader(mpesa), "KES", "uploads/mpesa.csv", now)
	if added != 3 || skipped != 0 || problem == nil {
		t.Fatalf("mpesa: added=%d skipped=%d problem=%v", added, skipped, problem)
	}
	entries, _ := m.Entries("", "")
	cats := map[string]string{}
	for _, e := range entries {
		cats[e.Merchant] = e.Category
	}
	if cats["Pay Bill to KPLC PREPAID"] != "utilities" || cats["Merchant Payment to NAIVAS"] != "groceries" || cats["Funds received from SAM"] != "income" {
		t.Fatalf("categories: %+v", cats)
	}
	added, skipped, _ = m.ImportCSV(strings.NewReader(mpesa), "KES", "uploads/mpesa.csv", now)
	if added != 0 || skipped != 3 {
		t.Fatalf("importing the same statement twice: added=%d skipped=%d", added, skipped)
	}
	bank := "Date,Description,Amount\n05/10/2026,NETFLIX.COM,-11.99\n06/10/2026,Salary,2500\n"
	if added, _, _ := m.ImportCSV(strings.NewReader(bank), "USD", "bank.csv", now); added != 2 {
		t.Fatalf("bank: %d", added)
	}
	if _, _, err := m.ImportCSV(strings.NewReader("Foo,Bar\n1,2\n"), "USD", "x.csv", now); err == nil {
		t.Fatal("a file with no date or amount was read")
	}
}

func TestTripsMergeBookingsAndKnowWhenToLeave(t *testing.T) {
	tr := OpenTrips(t.TempDir())
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	src := Source{Kind: "email", Ref: "msg-1"}
	trip, created, err := tr.Save(TripInput{Title: "Lamu", Destination: "Lamu", Start: "2026-10-16", End: "2026-10-20",
		Legs: []Leg{{Kind: "flight", Title: "Nairobi to Lamu", Ref: "jm 101", Start: "2026-10-16T09:40", End: "2026-10-16T11:05", To: "Lamu"}}, Source: src}, now)
	if err != nil || !created || trip.Legs[0].Ref != "JM101" {
		t.Fatalf("save: %v %+v", err, trip)
	}
	again, created, err := tr.Save(TripInput{Title: "lamu", Start: "2026-10-16", End: "2026-10-20",
		Legs: []Leg{{Kind: "hotel", Title: "Peponi Hotel", Start: "2026-10-16T14:00", End: "2026-10-20T10:00"},
			{Kind: "flight", Title: "Nairobi to Lamu", Ref: "JM101", Start: "2026-10-16T09:40"}}, Source: src}, now)
	if err != nil || created || again.ID != trip.ID || len(again.Legs) != 2 || again.Legs[1].Kind != "hotel" {
		t.Fatalf("merge: %v %v %+v", err, created, again.Legs)
	}
	for _, bad := range []TripInput{
		{Title: "x", Start: "2026-10-20", End: "2026-10-16", Source: src},
		{Title: "x", Start: "2026-10-16", Legs: []Leg{{Kind: "rocket", Title: "y", Start: "2026-10-16T09:00"}}, Source: src},
		{Title: "x", Start: "2026-10-16", Legs: []Leg{{Kind: "flight", Title: "y", Start: "morning"}}, Source: src},
	} {
		if _, _, err := tr.Save(bad, now); err == nil {
			t.Fatalf("bad trip kept: %+v", bad)
		}
	}
	by, ok := LeaveBy(Leg{Kind: "flight", Start: "2026-10-16T09:40"}, time.UTC)
	if !ok || by.Format("15:04") != "06:55" {
		t.Fatalf("domestic: leave by %v", by)
	}
	by, _ = LeaveBy(Leg{Kind: "flight", Start: "2026-10-16T09:40", International: true, TravelMinutes: 70}, time.UTC)
	if by.Format("15:04") != "05:30" {
		t.Fatalf("international, 70 minutes away: %v", by)
	}
	if _, ok := LeaveBy(Leg{Kind: "hotel", Start: "2026-10-16T14:00"}, time.UTC); ok {
		t.Fatal("nobody leaves for a hotel check-in by the clock")
	}
}
