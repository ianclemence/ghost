package jobs

import (
	"strings"
	"testing"
)

func TestJobsScheduleAtTheOwnersTime(t *testing.T) {
	brief, _ := Find("morning_brief")
	s, err := brief.Check(Settings{})
	if err != nil || s.Time != "07:30" || brief.Cron(s) != "30 7 * * *" {
		t.Fatalf("default: %v %+v %q", err, s, brief.Cron(s))
	}
	s, _ = brief.Check(Settings{Time: "06:05"})
	if brief.Cron(s) != "5 6 * * *" {
		t.Fatalf("06:05: %q", brief.Cron(s))
	}
	bills, _ := Find("bills")
	s, _ = bills.Check(Settings{Time: "00:00"})
	if bills.Cron(s) != "0 0 * * 0" {
		t.Fatalf("midnight sunday: %q", bills.Cron(s))
	}
	admin, _ := Find("life_admin")
	s, _ = admin.Check(Settings{})
	if admin.Cron(s) != "0 9 1 * *" {
		t.Fatalf("monthly: %q", admin.Cron(s))
	}
	if _, err := brief.Check(Settings{Time: "7.30"}); err == nil {
		t.Fatal("not a time")
	}
	learn, _ := Find("learn")
	if _, err := learn.Check(Settings{}); err == nil {
		t.Fatal("learn asks what to learn")
	}
	s, _ = learn.Check(Settings{Topic: "  Kiswahili   verbs "})
	if in := learn.Instruction(s); !strings.Contains(in, "Learn: Kiswahili verbs") || !strings.Contains(in, "Learn cards: Kiswahili verbs") || strings.Contains(in, "{topic}") {
		t.Fatalf("instruction: %s", in)
	}
	watch, _ := Find("watch")
	if _, err := watch.Check(Settings{}); err == nil {
		t.Fatal("watch has nothing to schedule")
	}
	for _, j := range Catalog {
		if j.Time != "" && strings.TrimSpace(j.instruction) == "" {
			t.Fatalf("%s has a time and no instruction", j.ID)
		}
	}
}

func TestStoreKeepsWhatIsOn(t *testing.T) {
	st := Open(t.TempDir())
	if err := st.Set("bills", State{Enabled: true, RoutineID: "r1", Settings: Settings{Time: "10:00"}}); err != nil {
		t.Fatal(err)
	}
	all, _ := st.All()
	if all["bills"].RoutineID != "r1" {
		t.Fatalf("%+v", all)
	}
	_ = st.Set("bills", State{Enabled: false})
	if all, _ := st.All(); len(all) != 0 {
		t.Fatalf("off is gone: %+v", all)
	}
}
