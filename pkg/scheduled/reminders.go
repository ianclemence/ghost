package scheduled

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A reminder is a promise Ghost made to the owner, not a scheduled message.
// Each time one goes off it leaves a delivery record (reminder_acks), and
// what the owner did with it lands there too: seen, done, snoozed or
// dismissed. That record is what lets Ghost tell "reminded" from
// "acknowledged" from "done", follow up on one that went unseen, and never
// nag about one already handled.

// Outcome is what the owner did with a delivered reminder.
type Outcome string

const (
	OutcomeNone      Outcome = ""
	OutcomeDone      Outcome = "done"
	OutcomeSnoozed   Outcome = "snoozed"
	OutcomeDismissed Outcome = "dismissed"
)

// Delivery is one firing of a reminder and what became of it.
type Delivery struct {
	ItemID      string
	Title       string
	Timezone    string
	DeliveredAt time.Time
	SeenAt      *time.Time
	Outcome     Outcome
	OutcomeAt   *time.Time
	Recurring   bool
}

var (
	// ErrNotReminder is returned for an action on something that is not one.
	ErrNotReminder = errors.New("that is not a reminder")
	// ErrClosed is returned for an action on a reminder already put away.
	ErrClosed = errors.New("that reminder is already closed")
)

func (s *Store) initReminderAcks() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS reminder_acks (
		item_id TEXT NOT NULL,
		delivered_at DATETIME NOT NULL,
		seen_at DATETIME,
		outcome TEXT DEFAULT '',
		outcome_at DATETIME,
		PRIMARY KEY (item_id, delivered_at)
	);
	CREATE INDEX IF NOT EXISTS idx_reminder_acks_delivered ON reminder_acks(delivered_at);`)
	return err
}

// RecordDelivery notes that a reminder just went off.
func (s *Store) RecordDelivery(itemID string, at time.Time) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO reminder_acks (item_id, delivered_at) VALUES (?, ?)`, itemID, at.UTC())
	return err
}

// latestDelivery returns the newest delivery of an item, or nil.
func (s *Store) latestDelivery(itemID string) (*Delivery, error) {
	row := s.db.QueryRow(`SELECT delivered_at, seen_at, outcome, outcome_at FROM reminder_acks WHERE item_id=? ORDER BY delivered_at DESC LIMIT 1`, itemID)
	d := &Delivery{ItemID: itemID}
	var seen, outAt sql.NullTime
	var outcome string
	if err := row.Scan(&d.DeliveredAt, &seen, &outcome, &outAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if seen.Valid {
		t := seen.Time
		d.SeenAt = &t
	}
	if outAt.Valid {
		t := outAt.Time
		d.OutcomeAt = &t
	}
	d.Outcome = Outcome(outcome)
	return d, nil
}

func (s *Store) setOutcome(itemID string, deliveredAt time.Time, o Outcome, at time.Time) error {
	_, err := s.db.Exec(`UPDATE reminder_acks SET outcome=?, outcome_at=?, seen_at=COALESCE(seen_at, ?) WHERE item_id=? AND delivered_at=?`,
		string(o), at.UTC(), at.UTC(), itemID, deliveredAt.UTC())
	return err
}

// MarkSeen records that the owner saw the latest delivery. Idempotent.
func (s *Store) MarkSeen(itemID string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE reminder_acks SET seen_at=? WHERE item_id=? AND seen_at IS NULL AND delivered_at=(SELECT MAX(delivered_at) FROM reminder_acks WHERE item_id=?)`,
		at.UTC(), itemID, itemID)
	return err
}

// OpenDeliveries returns reminders delivered between from and to that the
// owner has neither seen nor answered: the ones Ghost may gently bring up
// again, once.
func (s *Store) OpenDeliveries(from, to time.Time) ([]Delivery, error) {
	rows, err := s.db.Query(`
		SELECT a.item_id, a.delivered_at, i.title, i.timezone, i.schedule_kind
		FROM reminder_acks a JOIN scheduled_items i ON i.id = a.item_id
		WHERE a.delivered_at >= ? AND a.delivered_at < ? AND a.seen_at IS NULL AND COALESCE(a.outcome,'') = ''
		  AND i.state NOT IN ('done','dismissed','cancelled')
		ORDER BY a.delivered_at ASC`, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var kind string
		if err := rows.Scan(&d.ItemID, &d.DeliveredAt, &d.Title, &d.Timezone, &kind); err != nil {
			return out, err
		}
		d.Recurring = ScheduleKind(kind) != ScheduleAt
		out = append(out, d)
	}
	return out, rows.Err()
}

// reminderFor loads an item and checks it is a reminder still open to act on.
func (s *Service) reminderFor(id string) (*ScheduledItem, error) {
	item, err := s.store.Get(id)
	if err != nil || item == nil {
		return nil, fmt.Errorf("no such reminder")
	}
	if item.Type != TypeReminder {
		return nil, ErrNotReminder
	}
	switch item.State {
	case StateDone, StateDismissed, StateCancelled:
		return nil, ErrClosed
	}
	return item, nil
}

// MarkDone closes a reminder because the owner did the thing. A one-time
// reminder that has not fired yet is closed and never fires ("I already
// watered the plants"); one that fired is recorded as done. A recurring one
// records this occurrence as done and keeps its schedule.
func (s *Service) MarkDone(id string, now time.Time) (*ScheduledItem, error) {
	return s.close(id, OutcomeDone, StateDone, now)
}

// Dismiss puts a reminder away without it being done.
func (s *Service) Dismiss(id string, now time.Time) (*ScheduledItem, error) {
	return s.close(id, OutcomeDismissed, StateDismissed, now)
}

func (s *Service) close(id string, o Outcome, state ItemState, now time.Time) (*ScheduledItem, error) {
	item, err := s.reminderFor(id)
	if err != nil {
		return nil, err
	}
	if d, _ := s.store.latestDelivery(id); d != nil && d.Outcome == OutcomeNone {
		_ = s.store.setOutcome(id, d.DeliveredAt, o, now)
	}
	if item.IsOneTime() {
		item.State = state
		item.UpdatedAt = now
		if err := s.store.Update(item); err != nil {
			return nil, err
		}
	}
	return item, nil
}

// Snooze brings a reminder back at until, keeping its identity: the same
// reminder, later, not a new one. A recurring reminder fires once more at
// until and then resumes its cadence.
func (s *Service) Snooze(id string, until, now time.Time) (*ScheduledItem, error) {
	if !until.After(now) {
		return nil, errors.New("snooze needs a time in the future")
	}
	item, err := s.reminderFor(id)
	if err != nil {
		return nil, err
	}
	if d, _ := s.store.latestDelivery(id); d != nil && d.Outcome == OutcomeNone {
		_ = s.store.setOutcome(id, d.DeliveredAt, OutcomeSnoozed, now)
	}
	u := until.UTC()
	if item.IsOneTime() {
		item.Schedule.At = &u
	}
	item.NextRunAt = &u
	item.State = StateScheduled
	item.UpdatedAt = now
	if err := s.store.Update(item); err != nil {
		return nil, err
	}
	return item, nil
}

// MarkSeen records that the owner saw the reminder's latest delivery.
func (s *Service) MarkSeen(id string, now time.Time) error {
	if _, err := s.reminderFor(id); err != nil && !errors.Is(err, ErrClosed) {
		return err
	}
	return s.store.MarkSeen(id, now)
}

// RecordDelivery notes a reminder going off (the executor calls it).
func (s *Service) RecordDelivery(id string, at time.Time) error {
	return s.store.RecordDelivery(id, at)
}

// OpenDeliveries lists reminders delivered in [from, to) that were neither
// seen nor answered.
func (s *Service) OpenDeliveries(from, to time.Time) ([]Delivery, error) {
	return s.store.OpenDeliveries(from, to)
}

// SnoozeUntil turns a snooze choice into a time: "10m", "1h", or "tomorrow"
// (the same clock time tomorrow in the reminder's zone).
func SnoozeUntil(choice string, now time.Time, tz string) (time.Time, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "10m", "10min":
		return now.Add(10 * time.Minute), nil
	case "1h", "hour":
		return now.Add(time.Hour), nil
	case "tomorrow":
		loc := time.UTC
		if l, err := time.LoadLocation(tz); err == nil && tz != "" {
			loc = l
		}
		return now.In(loc).AddDate(0, 0, 1), nil
	}
	if d, err := time.ParseDuration(choice); err == nil && d > 0 && d <= 7*24*time.Hour {
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("unknown snooze %q", choice)
}
