package cards

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// A draft is something Ghost wrote for the owner to send: an email, a calendar
// invite, a text message. It is shown in full and can be edited on the card;
// nothing leaves until the owner taps Send, and the card says what really
// happened afterwards ("Sent 10:42", or why it did not go). The fields live in
// the card's Data as plain strings, checked here on every change.

// Draft kinds.
const (
	DraftEmail = "email"
	DraftEvent = "event"
	DraftSMS   = "sms"
	// DraftAlarm is an alarm for the owner's phone: the phone sets it when
	// the owner taps Set alarm.
	DraftAlarm = "alarm"
)

// Limits for a draft's fields.
const (
	MaxDraftTo       = 300
	MaxDraftSubject  = 200
	MaxDraftBody     = 8000
	MaxDraftSMS      = 1000
	MaxDraftLocation = 200
)

// draftFields names the fields each kind of draft has, and which are required.
var draftFields = map[string]map[string]bool{
	DraftEmail: {"to": true, "cc": false, "subject": true, "body": true},
	DraftEvent: {"subject": true, "start": true, "end": false, "location": false, "body": false, "all_day": false},
	DraftSMS:   {"to": true, "body": true},
	DraftAlarm: {"start": true, "subject": false},
}

// DraftKindOf is the kind of draft a card holds, or "".
func DraftKindOf(c Card) string {
	k, _ := c.Data["draft_kind"].(string)
	return k
}

// DraftField reads one of a draft's fields.
func DraftField(c Card, name string) string {
	v, _ := c.Data[name].(string)
	return v
}

// NewDraft builds a draft card from fields, checked.
func NewDraft(kind string, fields map[string]string) (Card, error) {
	allowed, ok := draftFields[kind]
	if !ok {
		return Card{}, fmt.Errorf("a draft is an email, an event or an sms, not %q", kind)
	}
	data := map[string]interface{}{"draft_kind": kind}
	for k, v := range fields {
		if _, ok := allowed[k]; !ok {
			return Card{}, fmt.Errorf("a %s draft has no field %q", kind, k)
		}
		data[k] = v
	}
	if err := normalizeDraft(data); err != nil {
		return Card{}, err
	}
	now := time.Now()
	c := Card{
		ID: newID(), Kind: KindDraft, Title: draftTitle(data), Data: data,
		Actions:   draftActions(kind),
		V:         CardVersion,
		CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if err := c.Validate(); err != nil {
		return Card{}, err
	}
	return c, nil
}

// EditDraft applies the owner's edits to a draft card. Only the fields its
// kind has may change; the result is checked as a whole.
func EditDraft(c *Card, edits map[string]string) error {
	kind := DraftKindOf(*c)
	allowed, ok := draftFields[kind]
	if !ok {
		return errors.New("that card is not a draft")
	}
	for k, v := range edits {
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("a %s draft has no field %q", kind, k)
		}
		c.Data[k] = v
	}
	if err := normalizeDraft(c.Data); err != nil {
		return err
	}
	c.Title = draftTitle(c.Data)
	return nil
}

func draftActions(kind string) []Action {
	send := Action{ID: "send", Label: "Send", Style: "primary", Kind: "act"}
	switch kind {
	case DraftEvent:
		send.Label = "Add to calendar"
	case DraftSMS:
		// The Pod cannot send a text: the phone opens it in Messages, ready
		// for the owner to send, and the card says exactly that.
		send.Label = "Open in Messages"
	case DraftAlarm:
		send.Label = "Set alarm"
	}
	return []Action{send, {ID: "discard", Label: "Discard", Kind: "dismiss"}}
}

func draftTitle(d map[string]interface{}) string {
	str := func(k string) string { v, _ := d[k].(string); return v }
	var t string
	switch str("draft_kind") {
	case DraftEmail:
		t = str("subject")
	case DraftEvent:
		t = str("subject")
	case DraftSMS:
		t = "Text to " + str("to")
	case DraftAlarm:
		t = "Alarm " + str("start")
		if s := str("subject"); s != "" {
			t += " · " + s
		}
	}
	if r := []rune(t); len(r) > 80 {
		t = string(r[:79]) + "…"
	}
	return t
}

// normalizeDraft cleans every field in place and checks the whole draft.
func normalizeDraft(d map[string]interface{}) error {
	kind, _ := d["draft_kind"].(string)
	allowed, ok := draftFields[kind]
	if !ok {
		return fmt.Errorf("unknown draft kind %q", kind)
	}
	get := func(k string) string {
		switch v := d[k].(type) {
		case string:
			return v
		case bool:
			if v {
				return "true"
			}
			return "false"
		}
		return ""
	}
	for k := range d {
		if k == "draft_kind" || k == "sent_id" || k == "sent_link" {
			continue
		}
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("a %s draft has no field %q", kind, k)
		}
	}
	limit := map[string]int{"to": MaxDraftTo, "cc": MaxDraftTo, "subject": MaxDraftSubject, "location": MaxDraftLocation, "start": 32, "end": 32, "all_day": 5, "body": MaxDraftBody}
	if kind == DraftSMS {
		limit["body"] = MaxDraftSMS
		limit["to"] = 120
	}
	if kind == DraftAlarm {
		limit["subject"] = 60
		limit["start"] = 5
	}
	for k, required := range allowed {
		v := get(k)
		multi := k == "body"
		var c string
		var fits bool
		if multi {
			c, fits = clean(strings.ReplaceAll(v, "\r\n", "\n"), limit[k])
		} else {
			c, fits = clean(oneLine(v), limit[k])
		}
		if !fits {
			return fmt.Errorf("%s is longer than %d characters", k, limit[k])
		}
		if required && c == "" {
			return fmt.Errorf("the draft needs a %s", draftWord(k))
		}
		if c == "" {
			delete(d, k)
			continue
		}
		d[k] = c
	}
	switch kind {
	case DraftEmail:
		for _, k := range []string{"to", "cc"} {
			if v := get(k); v != "" {
				list, err := mail.ParseAddressList(v)
				if err != nil || len(list) == 0 {
					return fmt.Errorf("%q is not an email address", v)
				}
				if len(list) > 10 {
					return errors.New("a draft goes to at most 10 people")
				}
			}
		}
	case DraftAlarm:
		if _, err := time.Parse("15:04", get("start")); err != nil {
			return fmt.Errorf("%q is not a time (07:30)", get("start"))
		}
	case DraftEvent:
		allDay := get("all_day") == "true"
		if v := get("all_day"); v != "" && v != "true" && v != "false" {
			return errors.New("all_day is true or false")
		}
		start, err := parseDraftTime(get("start"), allDay)
		if err != nil {
			return err
		}
		if v := get("end"); v != "" {
			end, err := parseDraftTime(v, allDay)
			if err != nil {
				return err
			}
			if end.Before(start) {
				return errors.New("the event ends before it starts")
			}
		}
	}
	return nil
}

func draftWord(k string) string {
	switch k {
	case "to":
		return "recipient"
	case "subject":
		return "title"
	case "start":
		return "start time"
	case "body":
		return "message"
	}
	return k
}

// DraftTimeLayouts are the forms a draft's times may take: a local date and
// time ("2026-10-12T14:30"), or a date for an all-day event.
const (
	DraftDateTime = "2006-01-02T15:04"
	DraftDate     = "2006-01-02"
)

func parseDraftTime(v string, allDay bool) (time.Time, error) {
	if allDay {
		t, err := time.ParseInLocation(DraftDate, v, time.Local)
		if err != nil {
			return time.Time{}, fmt.Errorf("%q is not a date (2006-01-02)", v)
		}
		return t, nil
	}
	t, err := time.ParseInLocation(DraftDateTime, v, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date and time (2006-01-02T15:04)", v)
	}
	return t, nil
}

// ParseDraftTime reads a draft's start or end.
func ParseDraftTime(v string, allDay bool) (time.Time, error) { return parseDraftTime(v, allDay) }

func validateDraftData(d map[string]interface{}) error {
	copyOf := map[string]interface{}{}
	for k, v := range d {
		copyOf[k] = v
	}
	return normalizeDraft(copyOf)
}
