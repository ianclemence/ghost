package cards

import (
	"testing"
)

func TestRenderSpecValid(t *testing.T) {
	raw := []byte(`{"kind":"suggestion","title":"Stretch","topic":"health","actions":[{"id":"ok","label":"Done","style":"primary"}]}`)
	c, err := RenderSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != KindSuggestion || c.Topic != "health" || len(c.Actions) != 1 {
		t.Fatalf("%+v", c)
	}
	if fb := c.TextFallback(); fb == "" {
		t.Fatal("fallback required")
	}
}

func TestRenderSpecRejects(t *testing.T) {
	cases := map[string]string{
		"unknown kind":      `{"kind":"popup","title":"x"}`,
		"missing title":     `{"kind":"suggestion","topic":"health"}`,
		"missing topic":     `{"kind":"suggestion","title":"x"}`,
		"missing goal":      `{"kind":"goal_update","title":"x"}`,
		"missing url":       `{"kind":"browser_view","title":"x"}`,
		"checkout reserved": `{"kind":"checkout_sheet","title":"x"}`,
		"too many actions":  `{"kind":"suggestion","title":"x","topic":"h","actions":[{"id":"a","label":"a"},{"id":"b","label":"b"},{"id":"c","label":"c"}]}`,
		"bad style":         `{"kind":"suggestion","title":"x","topic":"h","actions":[{"id":"a","label":"a","style":"explosive"}]}`,
		"dup action":        `{"kind":"suggestion","title":"x","topic":"h","actions":[{"id":"a","label":"a"},{"id":"a","label":"b"}]}`,
		"nested data":       `{"kind":"suggestion","title":"x","topic":"h","data":{"obj":{"a":1}}}`,
		"unknown field":     `{"kind":"suggestion","title":"x","topic":"h","zzz":1}`,
		"not json":          `nope`,
	}
	for name, raw := range cases {
		if _, err := RenderSpec([]byte(raw)); err == nil {
			t.Errorf("%s: must fail closed", name)
		}
	}
}

func TestActionLogBoundAndOrder(t *testing.T) {
	l := &ActionLog{}
	for i := 0; i < actionLogCap+10; i++ {
		l.Record(ActionRecord{CardID: "c", ActionID: "a", Actor: "user"})
	}
	got := l.Recent(5)
	if len(got) != 5 {
		t.Fatalf("got %d", len(got))
	}
	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()
	if n != actionLogCap {
		t.Fatalf("log unbounded: %d", n)
	}
	if len(l.Recent(0)) != actionLogCap {
		t.Fatal("Recent(0) must return all")
	}
}
