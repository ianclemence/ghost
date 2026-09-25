package agent

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

// The footer grounds summaries in scheduler rows, so it must carry full
// dates: "Sat 21:00" names a different day on every later read, and the
// summary outlives the day it was written.
func TestOpenSchedulesFooterCarriesFullDates(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatal(err)
	}
	ssvc := scheduled.NewService(store, &scheduled.SimpleEventBus{}, nil)

	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	at := time.Date(2026, 9, 27, 7, 0, 0, 0, loc)
	if err := ssvc.CreateItem(&scheduled.ScheduledItem{
		Type:        scheduled.TypeReminder,
		Title:       "Call Jas",
		Description: "say hi",
		State:       scheduled.StateScheduled,
		Schedule:    scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &at},
		Timezone:    "Asia/Bangkok",
		Action:      scheduled.Action{Kind: scheduled.ActionMessage, Content: "say hi", Deliver: true},
		NextRunAt:   &at,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}

	al := pinTestLoop(t, nil)
	al.schedSvc = ssvc
	footer := al.openSchedulesFooter()

	if !regexp.MustCompile(`\[Open schedules @\d{4}-\d{2}-\d{2} \d{2}:\d{2} `).MatchString(footer) {
		t.Errorf("footer header must carry a full date, got %q", footer)
	}
	wantRow := "- Call Jas → " + at.Format("Mon 2006-01-02 15:04") + " — say hi"
	if !strings.Contains(footer, wantRow) {
		t.Errorf("footer row %q missing, got:\n%s", wantRow, footer)
	}
}
