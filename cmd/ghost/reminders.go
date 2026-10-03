package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Reminders as promises, not messages. When one goes off the owner gets the
// reminder in their conversation and a card to act on it (Done, Snooze,
// Tomorrow); what they choose is carried out here and recorded, so a reminder
// is never just a line of text that scrolls away.

// reminderCardChannel is where cards for the phone live (GET /v1/cards).
const reminderCardChannel = "mobile"

// deliverReminder says a due reminder and puts its card beside it.
func deliverReminder(al *agent.AgentLoop, svc *scheduled.Service, stream *cevents.Stream, item *scheduled.ScheduledItem) {
	now := time.Now()
	text := agent.ReminderText(item.Title)
	if item.NextRunAt != nil {
		if note := agent.LateNote(*item.NextRunAt, now, item.Timezone); note != "" {
			text += " " + note
		}
	}
	al.DeliverToOwner(item.Channel, item.ChatID, text, map[string]interface{}{"reminder": true, "item_id": item.ID})
	if svc != nil {
		_ = svc.RecordDelivery(item.ID, now)
	}
	publishReminderEvent(stream, cevents.ReminderDelivered, item, "")
	if card, err := reminderCard(item, now); err == nil {
		cards.Publish(al.Bus(), nil, reminderCardChannel, "default", "main", card)
	}
}

// reminderCard is the card a reminder arrives with.
func reminderCard(item *scheduled.ScheduledItem, now time.Time) (cards.Card, error) {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "Reminder"
	}
	when := scheduled.InZone(now, item.Timezone).Format("3:04 PM")
	c, err := cards.New(cards.KindReminder, title, "Reminder · "+when)
	if err != nil {
		return c, err
	}
	c.Data = map[string]interface{}{"item_id": item.ID, "recurring": !item.IsOneTime()}
	c.Actions = []cards.Action{
		{ID: "done", Label: "Done", Kind: "act", Style: "primary"},
		{ID: "snooze_10m", Label: "10 min", Kind: "act"},
		{ID: "snooze_1h", Label: "1 hour", Kind: "act"},
		{ID: "snooze_tomorrow", Label: "Tomorrow", Kind: "act"},
	}
	c.ExpiresAt = now.Add(7 * 24 * time.Hour)
	return c, nil
}

// actOnReminder carries out an owner's choice on a reminder and returns the
// words to show for it. action is done, dismiss, seen or snooze_<10m|1h|tomorrow>.
func actOnReminder(svc *scheduled.Service, stream *cevents.Stream, itemID, action string) (string, error) {
	if svc == nil {
		return "", errors.New("reminders are unavailable right now")
	}
	if stream == nil {
		stream = substrateEvents // the gateway's stream, ready by the time anyone taps
	}
	now := time.Now()
	switch {
	case action == "seen":
		return "", svc.MarkSeen(itemID, now)
	case action == "done":
		item, err := svc.MarkDone(itemID, now)
		if err != nil {
			return "", err
		}
		publishReminderEvent(stream, cevents.ReminderDone, item, "")
		return "Done", nil
	case action == "dismiss":
		item, err := svc.Dismiss(itemID, now)
		if err != nil {
			return "", err
		}
		publishReminderEvent(stream, cevents.ReminderDismissed, item, "")
		return "Put away", nil
	case strings.HasPrefix(action, "snooze_"):
		item, err := svc.GetItem(itemID)
		if err != nil || item == nil {
			return "", errors.New("no such reminder")
		}
		until, err := scheduled.SnoozeUntil(strings.TrimPrefix(action, "snooze_"), now, item.Timezone)
		if err != nil {
			return "", err
		}
		item, err = svc.Snooze(itemID, until, now)
		if err != nil {
			return "", err
		}
		local := scheduled.InZone(until, item.Timezone)
		when := local.Format("3:04 PM")
		if local.YearDay() != scheduled.InZone(now, item.Timezone).YearDay() {
			when = local.Format("Mon 3:04 PM")
		}
		publishReminderEvent(stream, cevents.ReminderSnoozed, item, "Until "+when)
		return "Snoozed until " + when, nil
	}
	return "", fmt.Errorf("unknown reminder action %q", action)
}

// registerReminderActions wires the reminder card's buttons and the
// reminders endpoint (used by notification buttons and the app).
func registerReminderActions(mux *http.ServeMux, svc *scheduled.Service, stream *cevents.Stream, al *agent.AgentLoop) {
	// A reminder the owner answered is no longer "still open": any morning
	// note waiting about it goes.
	settle := func(id, action string) {
		if al != nil && action != "seen" {
			al.ForgetAttention("unseen:" + id + ":")
		}
	}
	cards.OnResolve(cards.KindReminder, func(c cards.Card, actionID string) (string, error) {
		id, _ := c.Data["item_id"].(string)
		if id == "" {
			return "", errors.New("this card has no reminder")
		}
		label, err := actOnReminder(svc, stream, id, actionID)
		if err == nil {
			settle(id, actionID)
		}
		return label, err
	})

	//	POST /v1/reminders/{id}/{done|dismiss|seen|snooze_10m|snooze_1h|snooze_tomorrow}
	// A choice made outside the card (a notification button) also puts the
	// reminder's card away, so the conversation shows what happened.
	mux.HandleFunc("/v1/reminders/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1024))
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/reminders/"), "/"), "/")
		if len(parts) != 2 || parts[0] == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "use /v1/reminders/{id}/{action}")
			return
		}
		id, action := parts[0], parts[1]
		label, err := actOnReminder(svc, stream, id, action)
		if err == nil {
			settle(id, action)
		}
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, scheduled.ErrClosed) {
				status = http.StatusConflict
			}
			jsonError(w, status, "action_failed", err.Error())
			return
		}
		if action != "seen" {
			if c, ok := cards.DefaultStore.FindByData(reminderCardChannel, cards.KindReminder, "item_id", id); ok {
				if _, done := cards.DefaultStore.Resolve(reminderCardChannel, c.ID, action); done && label != "" {
					cards.DefaultStore.Relabel(reminderCardChannel, c.ID, label)
				}
			}
		}
		resp := map[string]interface{}{"ok": true, "label": label}
		raw, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
}
