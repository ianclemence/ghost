package cards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type contract struct {
	Valid []struct {
		Name   string          `json:"name"`
		Spec   json.RawMessage `json:"spec"`
		Blocks int             `json:"blocks"`
	} `json:"valid"`
	Invalid []struct {
		Name string          `json:"name"`
		Spec json.RawMessage `json:"spec"`
	} `json:"invalid"`
}

func loadContract(t *testing.T) contract {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "blocks_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c contract
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// The phone reads the same file and must show what the Pod accepts and nothing
// it rejects, so these two lists are the whole agreement between them.
func TestContractValidCardsAreAccepted(t *testing.T) {
	for _, tc := range loadContract(t).Valid {
		t.Run(tc.Name, func(t *testing.T) {
			c, err := RenderSpec(tc.Spec)
			if err != nil {
				t.Fatalf("a card the phone must show was refused: %v", err)
			}
			if len(c.Blocks) != tc.Blocks {
				t.Fatalf("blocks = %d, contract says %d", len(c.Blocks), tc.Blocks)
			}
			if c.V != CardVersion || c.Kind != KindPresent {
				t.Fatalf("version/kind not set: v=%d kind=%s", c.V, c.Kind)
			}
			if c.TextFallback() == "" {
				t.Fatal("a card must always have a text form")
			}
		})
	}
}

func TestContractInvalidCardsAreRefused(t *testing.T) {
	for _, tc := range loadContract(t).Invalid {
		t.Run(tc.Name, func(t *testing.T) {
			if c, err := RenderSpec(tc.Spec); err == nil {
				t.Fatalf("accepted a card that must be refused: %+v", c)
			}
		})
	}
}

func TestBlocksAreCleanedNotTrusted(t *testing.T) {
	spec := `{"kind":"present","title":"  Hello\u0007  world  ","blocks":[
		{"type":"facts","rows":[{"label":"  Rain\n","value":"10%\u0000"}]},
		{"type":"text","text":"  line one\nline two  "}]}`
	c, err := RenderSpec([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Hello world" {
		t.Fatalf("title = %q", c.Title)
	}
	if c.Blocks[0].Rows[0].Label != "Rain" || c.Blocks[0].Rows[0].Value != "10%" {
		t.Fatalf("row not cleaned: %+v", c.Blocks[0].Rows[0])
	}
	if c.Blocks[1].Text != "line one\nline two" {
		t.Fatalf("text = %q", c.Blocks[1].Text)
	}
}

func TestLimitsHold(t *testing.T) {
	long := strings.Repeat("x", MaxText+1)
	for name, spec := range map[string]string{
		"long text":   `{"kind":"present","title":"t","blocks":[{"type":"text","text":"` + long + `"}]}`,
		"long title":  `{"kind":"present","title":"` + strings.Repeat("t", 81) + `","blocks":[{"type":"text","text":"a"}]}`,
		"long reply":  `{"kind":"present","title":"t","blocks":[{"type":"text","text":"a"}],"actions":[{"id":"a","label":"Go","kind":"reply","text":"` + strings.Repeat("r", MaxActionText+1) + `"}]}`,
		"long code":   `{"kind":"present","title":"t","blocks":[{"type":"code","code":"` + strings.Repeat("c", MaxCodeChars+1) + `"}]}`,
		"many lines":  `{"kind":"present","title":"t","blocks":[{"type":"code","code":"` + strings.Repeat("a\\n", MaxCodeLines+1) + `"}]}`,
		"many rows":   `{"kind":"present","title":"t","blocks":[{"type":"facts","rows":[` + strings.TrimSuffix(strings.Repeat(`{"label":"a","value":"b"},`, MaxRows+1), ",") + `]}]}`,
		"4 actions":   `{"kind":"present","title":"t","blocks":[{"type":"text","text":"a"}],"actions":[{"id":"1","label":"a","kind":"dismiss"},{"id":"2","label":"b","kind":"dismiss"},{"id":"3","label":"c","kind":"dismiss"},{"id":"4","label":"d","kind":"dismiss"}]}`,
		"dup actions": `{"kind":"present","title":"t","blocks":[{"type":"text","text":"a"}],"actions":[{"id":"1","label":"a","kind":"dismiss"},{"id":"1","label":"b","kind":"dismiss"}]}`,
	} {
		if _, err := RenderSpec([]byte(spec)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTextFormReadsWithoutTheCard(t *testing.T) {
	c, err := RenderSpec([]byte(`{"kind":"present","title":"TP 1352","blocks":[
		{"type":"metric","label":"Delay","value":"12","unit":"min","tone":"warn"},
		{"type":"timeline","steps":[{"time":"09:40","title":"Depart","state":"done"},{"time":"11:05","title":"Land","detail":"T1A"}]},
		{"type":"progress","label":"Flight","progress":0.5}],
		"actions":[{"id":"a","label":"Track it","kind":"reply","text":"track it"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := c.TextFallback()
	for _, want := range []string{"TP 1352", "Delay: 12 min", "09:40 - Depart", "11:05 - Land (T1A)", "Flight: 50%", "[Track it]"} {
		if !strings.Contains(got, want) {
			t.Errorf("text form missing %q:\n%s", want, got)
		}
	}
}

func TestStorePersistsAndResolves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cards.json")
	s := &Store{}
	if err := s.Persist(path); err != nil {
		t.Fatal(err)
	}
	c, err := RenderSpec([]byte(`{"kind":"present","title":"Pick","blocks":[{"type":"text","text":"a"}],
		"actions":[{"id":"a","label":"Yes","kind":"reply","text":"yes"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	s.Add("mobile", c)

	// A fresh process reads it back.
	s2 := &Store{}
	if err := s2.Persist(path); err != nil {
		t.Fatal(err)
	}
	got := s2.List("mobile")
	if len(got) != 1 || got[0].ID != c.ID || len(got[0].Blocks) != 1 {
		t.Fatalf("not restored: %+v", got)
	}

	// An action the card never offered is refused; one it offered resolves once.
	if _, ok := s2.Resolve("mobile", c.ID, "nope"); ok {
		t.Fatal("resolved with an action the card did not offer")
	}
	r, ok := s2.Resolve("mobile", c.ID, "a")
	if !ok || r.Resolved == nil || r.Resolved.Label != "Yes" {
		t.Fatalf("resolve failed: %+v ok=%v", r, ok)
	}
	if _, ok := s2.Resolve("mobile", c.ID, "a"); ok {
		t.Fatal("resolved twice")
	}

	// And the resolution survives a restart.
	s3 := &Store{}
	_ = s3.Persist(path)
	if l := s3.List("mobile"); len(l) != 1 || l[0].Resolved == nil {
		t.Fatalf("resolution lost: %+v", l)
	}
}

func TestDismissIsAlwaysAllowed(t *testing.T) {
	s := &Store{}
	c, _ := RenderSpec([]byte(`{"kind":"present","title":"t","blocks":[{"type":"text","text":"a"}]}`))
	s.Add("mobile", c)
	r, ok := s.Resolve("mobile", c.ID, "dismiss")
	if !ok || r.Resolved.Label != "Dismissed" {
		t.Fatalf("dismiss failed: %+v ok=%v", r, ok)
	}
}

func TestOlderKindsKeepWorking(t *testing.T) {
	// A phone built before blocks must not break: the older kinds are unchanged.
	c, err := New(KindSuggestion, "Nudge", "Body")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks) != 0 || c.V != 0 {
		t.Fatalf("an older card grew blocks: %+v", c)
	}
	if err := (Card{Kind: KindPresent, Title: "x"}).Validate(); err == nil {
		t.Fatal("a presented card with no blocks validated")
	}
}
