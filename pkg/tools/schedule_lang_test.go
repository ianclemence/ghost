package tools

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

func TestLooksEnglish(t *testing.T) {
	for _, s := range []string{"remind me tomorrow at 9 to call Sam", "every day at 8 please", "call the bank in the morning"} {
		if !looksEnglish(s) {
			t.Errorf("%q is English", s)
		}
	}
	for _, s := range []string{"recuérdame mañana a las 9 de la mañana llamar a Amara", "nikumbushe kesho saa tatu asubuhi", "我明天九点需要打电话", "rappelle-moi demain à 9h", "พรุ่งนี้ตอนเช้า"} {
		if looksEnglish(s) {
			t.Errorf("%q is not English", s)
		}
	}
}

// A translated time is accepted only if it agrees with the clock number the
// owner wrote.
func TestTranslatedTimeMustAgreeWithTheOwnersNumbers(t *testing.T) {
	ref := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	nine, _ := scheduled.ParseNaturalLanguage("tomorrow at 9am", ref, "UTC")
	if !timeAgreesWithOwner("recuérdame mañana a las 9 de la mañana", nine, "UTC") {
		t.Error("nine and nine agree")
	}
	if timeAgreesWithOwner("recuérdame mañana a las 7", nine, "UTC") {
		t.Error("the owner said 7 and the translation says 9: they disagree")
	}
	if !timeAgreesWithOwner("nikumbushe kesho asubuhi", nine, "UTC") {
		t.Error("no number to contradict is not a disagreement")
	}
	weekdays, _ := scheduled.ParseNaturalLanguage("every weekday at 8am", ref, "UTC")
	if !timeAgreesWithOwner("todos los días laborables a las 8", weekdays, "UTC") || timeAgreesWithOwner("todos los días laborables a las 6", weekdays, "UTC") {
		t.Error("recurring hours are checked too")
	}
}

func item(id, title string) *scheduled.ScheduledItem {
	return &scheduled.ScheduledItem{ID: id, Title: title, State: scheduled.StateScheduled}
}

// Cancelling by description picks one item, and never guesses between two.
func TestCancelMatchesOneItemAndNeverGuesses(t *testing.T) {
	items := []*scheduled.ScheduledItem{
		item("a", "Take my vitamins"), item("b", "Give me a short brief of my week"), item("c", "Look at the Shenzhen flights"), item("d", "Call Amara"),
	}
	if m := scheduleMatches(items, "the vitamins reminder"); len(m) != 1 || m[0].ID != "a" {
		t.Fatalf("vitamins: %+v", m)
	}
	if m := scheduleMatches(items, "my Monday brief"); len(m) != 1 || m[0].ID != "b" {
		t.Fatalf("brief: %+v", m)
	}
	if m := scheduleMatches(items, "c"); len(m) != 1 || m[0].ID != "c" {
		t.Fatalf("an exact id wins: %+v", m)
	}
	if m := scheduleMatches(items, "the dentist"); len(m) != 0 {
		t.Fatalf("nothing matches: %+v", m)
	}
	twin := append(items, item("e", "Call Amara about the flights"))
	if m := scheduleMatches(twin, "call Amara"); len(m) < 2 {
		t.Fatalf("two close matches must both come back so the owner can choose: %+v", m)
	}
	// A cancelled or finished item is never offered.
	done := item("f", "Take my vitamins")
	done.State = scheduled.StateCancelled
	if m := scheduleMatches([]*scheduled.ScheduledItem{done}, "vitamins"); len(m) != 0 {
		t.Fatal("only live items can be cancelled")
	}
}
