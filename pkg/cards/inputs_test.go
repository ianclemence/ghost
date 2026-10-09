package cards

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func askCard(t *testing.T, spec string) Card {
	t.Helper()
	c, err := RenderSpec([]byte(spec))
	if err != nil {
		t.Fatalf("spec refused: %v", err)
	}
	return c
}

const dinnerSpec = `{"kind":"present","title":"Dinner",
 "blocks":[
  {"type":"choice","key":"place","label":"Where","options":[{"id":"brera","label":"Cafe Brera"},{"id":"diner","label":"Diner 54"}]},
  {"type":"choice","key":"extras","label":"Extras","multiple":true,"options":[{"id":"wine","label":"Wine"},{"id":"cake","label":"Cake"},{"id":"flowers","label":"Flowers"}]},
  {"type":"datetime","key":"when","label":"When","mode":"datetime","earliest":"2026-10-10T00:00"},
  {"type":"slider","key":"people","label":"People","min":1,"max":8,"step":1,"number":2},
  {"type":"field","key":"note","label":"Note","optional":true}],
 "actions":[{"id":"go","label":"Book it","kind":"submit"}]}`

func TestAnswersAreCheckedAgainstTheCard(t *testing.T) {
	c := askCard(t, dinnerSpec)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	ans, err := c.CheckAnswer(map[string]interface{}{
		"place": "brera", "extras": []interface{}{"cake", "wine", "cake"},
		"when": "2026-10-16T19:30", "people": 4.0,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	want := "Where: Cafe Brera\nExtras: Wine, Cake\nWhen: Fri 16 Oct, 19:30\nPeople: 4"
	if ans.Text != want {
		t.Fatalf("text = %q, want %q", ans.Text, want)
	}
	if _, ok := ans.Values["note"]; ok {
		t.Fatal("an optional field left empty is not an answer")
	}
	if got := ans.Values["extras"].([]string); strings.Join(got, ",") != "wine,cake" {
		t.Fatalf("extras kept in option order without repeats, got %v", got)
	}

	bad := []map[string]interface{}{
		{"place": "nowhere", "when": "2026-10-16T19:30", "people": 2.0, "extras": []interface{}{"wine"}},
		{"place": "brera", "when": "2026-10-01T19:30", "people": 2.0, "extras": []interface{}{"wine"}}, // before earliest
		{"place": "brera", "when": "friday", "people": 2.0, "extras": []interface{}{"wine"}},
		{"place": "brera", "when": "2026-10-16T19:30", "people": 9.0, "extras": []interface{}{"wine"}},
		{"place": "brera", "when": "2026-10-16T19:30", "people": 2.5, "extras": []interface{}{"wine"}},
		{"place": "brera", "when": "2026-10-16T19:30", "people": 2.0},                                          // extras missing
		{"place": "brera", "when": "2026-10-16T19:30", "people": 2.0, "extras": []interface{}{"wine"}, "x": 1}, // unknown key
		{"place": []interface{}{"brera", "diner"}, "when": "2026-10-16T19:30", "people": 2.0, "extras": []interface{}{"wine"}},
	}
	for i, raw := range bad {
		if _, err := c.CheckAnswer(raw, now); err == nil {
			t.Fatalf("bad answer %d was accepted", i)
		}
	}
}

func TestRespondResolvesOnceAndKeepsTheAnswers(t *testing.T) {
	s := &Store{items: map[string][]Card{}}
	c := askCard(t, dinnerSpec)
	s.Add("mobile", c)
	now := time.Now()
	raw := map[string]interface{}{"place": "diner", "extras": []interface{}{}, "when": now.AddDate(0, 0, 3).Format(dateTimeLayout), "people": 3.0}
	if _, _, err := s.Respond("mobile", c.ID, map[string]interface{}{"place": "diner"}, now); err == nil {
		t.Fatal("an incomplete answer resolved the card")
	}
	if got, _ := s.Find("mobile", c.ID); got.Resolved != nil {
		t.Fatal("a refused answer must leave the card open")
	}
	got, ans, err := s.Respond("mobile", c.ID, raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resolved == nil || got.Resolved.ActionID != "go" || got.Resolved.Answers["place"] != "diner" {
		t.Fatalf("resolution = %+v", got.Resolved)
	}
	if !strings.HasPrefix(ans.Label, "Diner 54") {
		t.Fatalf("label = %q", ans.Label)
	}
	if _, _, err := s.Respond("mobile", c.ID, raw, now); !errors.Is(err, ErrResolved) {
		t.Fatalf("second answer err = %v, want ErrResolved", err)
	}
	if _, _, err := s.Respond("mobile", "card_nope", raw, now); !errors.Is(err, ErrNoCard) {
		t.Fatalf("missing card err = %v", err)
	}
}

func TestChecklistTicksPersistWithoutResolving(t *testing.T) {
	s := &Store{items: map[string][]Card{}}
	c := askCard(t, `{"kind":"present","title":"Groceries","blocks":[{"type":"checklist","key":"items","checks":[{"id":"milk","label":"Milk"},{"id":"eggs","label":"Eggs"}]}]}`)
	s.Add("mobile", c)
	got, err := s.Check("mobile", c.ID, "items", "eggs", true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocks[0].Checks[1].Done || got.Blocks[0].Checks[0].Done || got.Resolved != nil {
		t.Fatalf("ticks = %+v", got.Blocks[0].Checks)
	}
	if _, err := s.Check("mobile", c.ID, "items", "bread", true); err == nil {
		t.Fatal("ticked an item that is not on the list")
	}
	if _, err := s.Check("mobile", c.ID, "other", "eggs", true); err == nil {
		t.Fatal("ticked a checklist the card does not have")
	}
}

func TestDraftsAreCheckedOnEveryChange(t *testing.T) {
	c, err := NewDraft(DraftEmail, map[string]string{"to": "sam@example.com", "subject": "Dinner", "body": "Friday at 7?"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != KindDraft || c.Title != "Dinner" || len(c.Actions) != 2 || c.Actions[0].Kind != "act" {
		t.Fatalf("draft = %+v", c)
	}
	s := &Store{items: map[string][]Card{}}
	s.Add("mobile", c)
	got, err := s.Update("mobile", c.ID, func(x *Card) error {
		return EditDraft(x, map[string]string{"subject": "Dinner on Friday", "cc": "kim@example.com"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Dinner on Friday" || DraftField(got, "cc") != "kim@example.com" {
		t.Fatalf("edit not applied: %+v", got.Data)
	}
	for _, edit := range []map[string]string{{"to": "not an address"}, {"body": ""}, {"start": "2026-10-10T10:00"}} {
		if _, err := s.Update("mobile", c.ID, func(x *Card) error { return EditDraft(x, edit) }); err == nil {
			t.Fatalf("bad edit %v was taken", edit)
		}
	}
	if kept, _ := s.Find("mobile", c.ID); DraftField(kept, "to") != "sam@example.com" {
		t.Fatal("a refused edit changed the draft")
	}

	if _, err := NewDraft(DraftEvent, map[string]string{"subject": "Dentist", "start": "2026-10-12T09:00", "end": "2026-10-12T08:00"}); err == nil {
		t.Fatal("an event that ends before it starts was taken")
	}
	ev, err := NewDraft(DraftEvent, map[string]string{"subject": "Trip", "start": "2026-10-12", "all_day": "true"})
	if err != nil || ev.Actions[0].Label != "Add to calendar" {
		t.Fatalf("all-day event: %v %+v", err, ev.Actions)
	}
	sms, err := NewDraft(DraftSMS, map[string]string{"to": "Mum", "body": "Landed safely"})
	if err != nil || sms.Title != "Text to Mum" || sms.Actions[0].Label != "Open in Messages" {
		t.Fatalf("sms: %v %+v", err, sms)
	}
	// The card survives a round trip through the store's JSON.
	b, _ := json.Marshal(sms)
	var back Card
	if err := json.Unmarshal(b, &back); err != nil || back.Validate() != nil {
		t.Fatalf("round trip: %v %v", err, back.Validate())
	}
}
