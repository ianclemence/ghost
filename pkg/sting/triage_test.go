package sting

import (
	"testing"
)

func triageTools() []ToolSchema {
	return []ToolSchema{
		{Name: "web_search", Description: "Live web search for current external facts.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
					"count": map[string]interface{}{"type": "integer", "minimum": 1.0, "maximum": 10.0},
				},
				"required": []string{"query"},
			}},
		{Name: "weather_now", Description: "Current weather for a place.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{"type": "string"},
				},
			}},
		{Name: "currency_convert", Description: "Convert amounts between currencies.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from":   map[string]interface{}{"type": "string"},
					"to":     map[string]interface{}{"type": "string"},
					"amount": map[string]interface{}{"type": "number"},
				},
				"required": []string{"from", "to"},
			}},
	}
}

func TestTriageActWhenComplete(t *testing.T) {
	for _, q := range []string{
		"search the web for raspberry pi guides",
		"check the weather in berlin",
		"how much is 250 dollars in euros",
	} {
		o := Triage(q, triageTools())
		if o.Verdict != TriageAct {
			t.Fatalf("%q: expected act, got %+v", q, o)
		}
	}
}

func TestTriageAskWhenRequiredMissing(t *testing.T) {
	o := Triage("search the web", triageTools())
	if o.Verdict != TriageAsk || o.Tool != "web_search" {
		t.Fatalf("expected ask/web_search, got %+v", o)
	}
	if len(o.Missing) == 0 || o.Missing[0] != "query" {
		t.Fatalf("expected missing query, got %+v", o)
	}
}

func TestTriageRefuseNegatedOnly(t *testing.T) {
	o := Triage("don't search anything", triageTools())
	if o.Verdict != TriageRefuse {
		t.Fatalf("expected refuse, got %+v", o)
	}
}

func TestTriageOfftopicStillActs(t *testing.T) {
	// Off-topic refusal belongs to the engine (empty call []): triage
	// must not outrank it with keyword matching. False-act costs one
	// cheap gated call; false-refuse would push to a bigger model.
	for _, q := range []string{"hello ghost", "what is the capital of france"} {
		o := Triage(q, triageTools())
		if o.Verdict != TriageAct {
			t.Fatalf("%q: expected act, got %+v", q, o)
		}
	}
}

func TestTriageWeatherBareActs(t *testing.T) {
	// location is optional: omission is a valid call, so generate.
	o := Triage("is it raining", triageTools())
	if o.Verdict != TriageAct {
		t.Fatalf("expected act (optional omission), got %+v", o)
	}
}

func TestTriageCurrencyNoAmountActs(t *testing.T) {
	// amount is optional: omit and act.
	o := Triage("convert dollars to euros", triageTools())
	if o.Verdict != TriageAct {
		t.Fatalf("expected act, got %+v", o)
	}
}
