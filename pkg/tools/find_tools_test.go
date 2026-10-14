package tools

import (
	"context"
	"strings"
	"testing"
)

type described struct{ name, desc string }

func (d described) Name() string        { return d.name }
func (d described) Description() string { return d.desc }
func (described) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (described) Execute(context.Context, map[string]interface{}) *ToolResult {
	return NewToolResult("ok")
}

func catalogue() *ToolRegistry {
	r := NewToolRegistry()
	for _, t := range []described{
		{"weather_now", "Current weather and forecast for a place."},
		{"device", "Read or control smart-home devices: lights, plugs, thermostat. Use for turn on, turn off, set the temperature."},
		{"media_play", "Play, pause or skip music and podcasts on the owner's phone or speaker."},
		{"flight_status", "Look up the live status of a flight by number."},
	} {
		r.Register(t)
	}
	return r
}

func TestFindToolsOffersTheBestMatchAndNothingElse(t *testing.T) {
	all := catalogue()
	offered := NewToolRegistry()
	w, _ := all.Get("weather_now")
	offered.Register(w)
	ctx := WithTurnTools(context.Background(), &TurnTools{Offered: offered})

	res := NewFindToolsTool(all).Execute(ctx, map[string]interface{}{"need": "turn off the living room lights"})
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "- device:") {
		t.Fatalf("the lights tool was not found:\n%s", res.ForLLM)
	}
	if _, ok := offered.Get("device"); !ok {
		t.Fatal("a found tool must become callable straight away")
	}
	if _, ok := offered.Get("media_play"); ok {
		t.Fatal("an unrelated tool was offered")
	}
}

func TestFindToolsSkipsWhatIsAlreadyOfferedAndHidden(t *testing.T) {
	all := catalogue()
	all.RegisterHidden(described{"exec", "Run a shell command to turn things off"}, 0)
	offered := NewToolRegistry()
	d, _ := all.Get("device")
	offered.Register(d)
	ctx := WithTurnTools(context.Background(), &TurnTools{Offered: offered})
	res := NewFindToolsTool(all).Execute(ctx, map[string]interface{}{"need": "turn off the lights"})
	if strings.Contains(res.ForLLM, "device") || strings.Contains(res.ForLLM, "exec") {
		t.Fatalf("offered or hidden tools must not be listed:\n%s", res.ForLLM)
	}
}

func TestFindToolsSaysPlainlyWhenNothingFits(t *testing.T) {
	ctx := WithTurnTools(context.Background(), &TurnTools{Offered: NewToolRegistry()})
	res := NewFindToolsTool(catalogue()).Execute(ctx, map[string]interface{}{"need": "teleport my sofa"})
	if res.IsError || !strings.Contains(res.ForLLM, "No other tool matches") {
		t.Fatalf("an empty search must say so: %+v", res)
	}
}

func TestFindToolsNeedsATurn(t *testing.T) {
	res := NewFindToolsTool(catalogue()).Execute(context.Background(), map[string]interface{}{"need": "lights"})
	if !res.IsError {
		t.Fatal("outside a turn there is no tool set to widen")
	}
}

func TestRankToolsPrefersNameOverDescriptionMentions(t *testing.T) {
	cands := map[string]Tool{
		"flight_status": described{"flight_status", "Look up a flight. Mentions music rarely."},
		"media_play":    described{"media_play", "Play music."},
	}
	got := rankTools("play some music", cands)
	if len(got) == 0 || got[0].name != "media_play" {
		t.Fatalf("ranking wrong: %+v", got)
	}
}
