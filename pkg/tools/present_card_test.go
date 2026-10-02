package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cards"
)

// args as the model delivers them: decoded JSON, so numbers are float64 and
// objects are map[string]interface{}.
func decode(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

const flightArgs = `{"title":"TP 1352","blocks":[
  {"type":"timeline","steps":[{"time":"09:40","title":"Depart","state":"done"},{"time":"11:05","title":"Land","state":"now"}]},
  {"type":"progress","label":"Flight","progress":0.6}],
  "actions":[{"id":"t","label":"Track it","kind":"reply","text":"track it","style":"primary"}]}`

func TestPresentCardPublishesAValidatedCard(t *testing.T) {
	var got cards.Card
	var ch, chat string
	tool := NewPresentCardTool()
	tool.SetContext("mobile", "default")
	tool.SetPublisher(func(channel, chatID, _ string, c cards.Card) { got, ch, chat = c, channel, chatID })

	res := tool.Execute(context.Background(), decode(t, flightArgs))
	if res.IsError {
		t.Fatalf("a valid card was refused: %s", res.ForLLM)
	}
	if ch != "mobile" || chat != "default" || got.Kind != cards.KindPresent || len(got.Blocks) != 2 || len(got.Actions) != 1 {
		t.Fatalf("published wrong: %s %s %+v", ch, chat, got)
	}
	if got.Blocks[1].Progress == nil || *got.Blocks[1].Progress != 0.6 {
		t.Fatalf("progress did not survive the round trip: %+v", got.Blocks[1])
	}
	if !strings.Contains(res.ForLLM, "at most one short sentence") {
		t.Fatalf("the model is not told to stay brief: %s", res.ForLLM)
	}
}

func TestPresentCardTellsTheModelWhyItWasRefused(t *testing.T) {
	published := false
	tool := NewPresentCardTool()
	tool.SetContext("mobile", "default")
	tool.SetPublisher(func(_, _, _ string, _ cards.Card) { published = true })

	res := tool.Execute(context.Background(), decode(t, `{"title":"x","blocks":[{"type":"iframe","text":"hi"}]}`))
	if !res.IsError || !strings.Contains(res.ForLLM, "not in the catalog") {
		t.Fatalf("the model should be told what was wrong: %+v", res)
	}
	if published {
		t.Fatal("a refused card was published")
	}
	// An action that tries to run something is refused the same way.
	res = tool.Execute(context.Background(), decode(t, `{"title":"x","blocks":[{"type":"text","text":"a"}],"actions":[{"id":"a","label":"Go","kind":"open","text":"https://evil.example"}]}`))
	if !res.IsError || published {
		t.Fatalf("an action outside reply/dismiss got through: %+v", res)
	}
}

func TestPresentCardOnASurfaceThatCannotDrawCards(t *testing.T) {
	tool := NewPresentCardTool() // no publisher, no surface: a terminal
	res := tool.Execute(context.Background(), decode(t, flightArgs))
	if res.IsError {
		t.Fatalf("refused: %s", res.ForLLM)
	}
	for _, want := range []string{"cannot show cards", "TP 1352", "09:40 - Depart", "Flight: 60%"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("the plain-text answer is missing %q:\n%s", want, res.ForLLM)
		}
	}
}
