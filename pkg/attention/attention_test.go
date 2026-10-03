package attention

import (
	"testing"
	"time"
)

var bkk, _ = time.LoadLocation("Asia/Bangkok")

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, bkk) }

func TestFindSpan(t *testing.T) {
	heard := day(2026, 10, 2)
	cases := []struct {
		text       string
		start, end time.Time
	}{
		{"Is planning a trip to Shenzhen from around 26 October to 6 November", day(2026, 10, 26), day(2026, 11, 6)},
		{"Chelsea v Bournemouth — Sat 10 Oct 2026, 21:00 UK kickoff", day(2026, 10, 10), day(2026, 10, 10)},
		{"Flies to Paris on Oct 12, 2026", day(2026, 10, 12), day(2026, 10, 12)},
		{"Jas's birthday is on 3 March", day(2027, 3, 3), day(2027, 3, 3)},
		{"Dentist appointment 2026-10-20", day(2026, 10, 20), day(2026, 10, 20)},
	}
	for _, c := range cases {
		sp, ok := FindSpan(c.text, heard, bkk)
		if !ok || !sp.Start.Equal(c.start) || !sp.End.Equal(c.end) {
			t.Errorf("%q → %v–%v (%v), want %v–%v", c.text, sp.Start, sp.End, ok, c.start, c.end)
		}
	}
	for _, s := range []string{"Is planning a trip next month, departing on the 2nd", "Prefers responses without dashes", "Works 9 to 5"} {
		if _, ok := FindSpan(s, heard, bkk); ok {
			t.Errorf("%q read as a date", s)
		}
	}
}

// Small things wait for the morning and arrive together; urgent, reliable
// things go now; nothing is said twice.
func TestQueueDecides(t *testing.T) {
	q := Open(t.TempDir())
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC) // 08:00 Bangkok
	if d := q.Offer(Item{Key: "a", Source: "upcoming", Line: "Shenzhen in a week", Priority: 6}, now); d != Digest {
		t.Fatalf("a = %s", d)
	}
	if d := q.Offer(Item{Key: "b", Source: "pod", Line: "Disk almost full", Urgent: true, Reliable: true}, now); d != Now {
		t.Fatalf("b = %s", d)
	}
	if d := q.Offer(Item{Key: "b", Source: "pod", Line: "Disk almost full", Urgent: true, Reliable: true}, now); d != Drop {
		t.Fatalf("b twice = %s", d)
	}
	if d := q.Offer(Item{Key: "c", Source: "upcoming", Line: "Unconfirmed", Urgent: true}, now); d != Digest {
		t.Fatalf("urgent but unconfirmed must not interrupt: %s", d)
	}
	if !q.DigestDue(now, bkk, "08:00") {
		t.Fatal("digest due at 08:00")
	}
	got := q.TakeDigest(now, bkk, 5)
	if len(got) != 2 || got[0].Key != "a" {
		t.Fatalf("digest = %+v", got)
	}
	if q.DigestDue(now.Add(time.Hour), bkk, "08:00") {
		t.Fatal("one digest a day")
	}
	if d := q.Offer(Item{Key: "a", Source: "upcoming", Line: "Shenzhen in a week"}, now); d != Drop {
		t.Fatal("said once, never again")
	}
}

// A source ignored three mornings running goes quiet; answering resets it.
func TestQueueLearnsFromIgnores(t *testing.T) {
	q := Open(t.TempDir())
	start := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		now := start.AddDate(0, 0, i)
		q.Offer(Item{Key: "u" + string(rune('0'+i)), Source: "reminder_unseen", Line: "x"}, now)
		q.TakeDigest(now, bkk, 5)
	}
	now := start.AddDate(0, 0, 4)
	if d := q.Offer(Item{Key: "late", Source: "reminder_unseen", Line: "x"}, now); d != Drop {
		t.Fatalf("an ignored source should be quiet, got %s", d)
	}
	q2 := Open(t.TempDir())
	q2.Offer(Item{Key: "k", Source: "upcoming", Line: "x"}, start)
	q2.TakeDigest(start, bkk, 5)
	q2.Answered("upcoming")
	q2.Offer(Item{Key: "k2", Source: "upcoming", Line: "x"}, start.AddDate(0, 0, 1))
	q2.TakeDigest(start.AddDate(0, 0, 1), bkk, 5)
	if q2.st.Ignored["upcoming"] != 0 {
		t.Fatalf("an answered morning is not an ignore; ignored = %d", q2.st.Ignored["upcoming"])
	}
	q2.TakeDigest(start.AddDate(0, 0, 2), bkk, 5)
	if q2.st.Ignored["upcoming"] != 1 {
		t.Fatalf("an unanswered morning counts once; ignored = %d", q2.st.Ignored["upcoming"])
	}
}
