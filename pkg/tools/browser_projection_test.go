package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Console output stays bounded and recent-first; page text survives
// (redaction/labeling happens later on the enforced path) but can
// never flood context.
func TestProjectConsoleBoundsAndFields(t *testing.T) {
	var msgs []map[string]interface{}
	for i := 0; i < 120; i++ {
		msgs = append(msgs, map[string]interface{}{
			"type":      "error",
			"text":      fmt.Sprintf("boom %d", i),
			"location":  map[string]interface{}{"url": "https://x.test/app.js", "lineNumber": float64(i)},
			"timestamp": float64(1758000000000 + i),
		})
	}
	payload := map[string]interface{}{
		"success": true,
		"data":    map[string]interface{}{"messages": msgs},
	}
	raw, _ := json.Marshal(payload)
	res := &ToolResult{ForLLM: string(raw), ForUser: string(raw)}
	projectConsoleOutput(res)

	var env map[string]interface{}
	if err := json.Unmarshal([]byte(res.ForLLM), &env); err != nil {
		t.Fatalf("projection must stay JSON: %v", err)
	}
	data := env["data"].(map[string]interface{})
	out := data["messages"].([]interface{})
	if len(out) != 40 {
		t.Fatalf("messages = %d, want 40 (bounded recent-first)", len(out))
	}
	last := out[39].(map[string]interface{})
	if last["text"] != "boom 119" {
		t.Fatalf("recent messages must survive, got %v", last["text"])
	}
	if last["level"] != "error" {
		t.Fatalf("level = %v", last["level"])
	}
	if at, _ := last["at"].(string); !strings.Contains(at, "app.js:119") {
		t.Fatalf("at = %v", last["at"])
	}
	note, _ := data["note"].(string)
	if !strings.Contains(note, "120") {
		t.Fatalf("truncation must be disclosed, note = %q", note)
	}

	// Per-message text cap.
	long := map[string]interface{}{"type": "log", "text": strings.Repeat("x", 5000)}
	raw2, _ := json.Marshal(map[string]interface{}{"data": map[string]interface{}{"messages": []interface{}{long}}})
	res2 := &ToolResult{ForLLM: string(raw2)}
	projectConsoleOutput(res2)
	var env2 map[string]interface{}
	_ = json.Unmarshal([]byte(res2.ForLLM), &env2)
	txt := env2["data"].(map[string]interface{})["messages"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if len([]rune(txt)) > 410 {
		t.Fatalf("message text not capped: %d runes", len([]rune(txt)))
	}
}

func TestProjectConsoleUntouchedOnOddPayloads(t *testing.T) {
	// Not JSON: unchanged.
	res := &ToolResult{ForLLM: "plain failure text"}
	projectConsoleOutput(res)
	if res.ForLLM != "plain failure text" {
		t.Fatalf("non-JSON rewritten: %q", res.ForLLM)
	}
	// Error results: unchanged.
	errRes := &ToolResult{ForLLM: `{"data":{"messages":[]}}`, IsError: true}
	projectConsoleOutput(errRes)
	if errRes.ForLLM != `{"data":{"messages":[]}}` {
		t.Fatal("error result must not be rewritten")
	}
	// JSON without messages: unchanged.
	noMsgs := &ToolResult{ForLLM: `{"success":true,"data":{"other":1}}`}
	projectConsoleOutput(noMsgs)
	if noMsgs.ForLLM != `{"success":true,"data":{"other":1}}` {
		t.Fatalf("payload without messages rewritten: %s", noMsgs.ForLLM)
	}
}

// A11y keeps signal (counts + violations with example targets) and
// drops pass/incomplete node dumps.
func TestProjectA11yKeepsSignalDropsNoise(t *testing.T) {
	payload := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"axeVersion": "4.8",
			"counts":     map[string]interface{}{"violations": float64(2), "passes": float64(30)},
			"violations": []interface{}{
				map[string]interface{}{
					"id": "button-name", "impact": "critical", "help": "Buttons must have discernible text",
					"nodes": []interface{}{
						map[string]interface{}{"html": "<button class='x'></button>", "target": []interface{}{"button.x"}},
						map[string]interface{}{"html": "<button class='y'></button>", "target": []interface{}{"button.y"}},
						map[string]interface{}{"html": "<button class='z'></button>", "target": []interface{}{"button.z"}},
						map[string]interface{}{"html": "<button class='w'></button>", "target": []interface{}{"button.w"}},
					},
				},
				map[string]interface{}{
					"id": "color-contrast", "impact": "serious", "help": "Contrast",
					"nodes": []interface{}{map[string]interface{}{"html": "<p>a</p>", "target": []interface{}{"p"}}},
				},
			},
			"passes":     []interface{}{map[string]interface{}{"id": "x", "nodes": []interface{}{"massive dump"}}},
			"incomplete": []interface{}{map[string]interface{}{"id": "y", "nodes": []interface{}{"more dump"}}},
		},
	}
	raw, _ := json.Marshal(payload)
	res := &ToolResult{ForLLM: string(raw), ForUser: string(raw)}
	projectA11yOutput(res)

	if strings.Contains(res.ForLLM, "massive dump") || strings.Contains(res.ForLLM, "more dump") {
		t.Fatalf("pass/incomplete dumps must be dropped: %s", res.ForLLM)
	}
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(res.ForLLM), &env); err != nil {
		t.Fatalf("must stay JSON: %v", err)
	}
	data := env["data"].(map[string]interface{})
	if _, ok := data["passes"]; ok {
		t.Fatal("passes key must be removed")
	}
	viols := data["violations"].([]interface{})
	if len(viols) != 2 {
		t.Fatalf("violations = %d", len(viols))
	}
	first := viols[0].(map[string]interface{})
	if first["id"] != "button-name" || first["impact"] != "critical" {
		t.Fatalf("violation fields lost: %+v", first)
	}
	if first["nodes"] != float64(4) {
		t.Fatalf("node count = %v, want 4", first["nodes"])
	}
	examples := first["examples"].([]interface{})
	if len(examples) != 3 {
		t.Fatalf("examples = %d, want 3 (bounded per violation)", len(examples))
	}
	// Counts survive (the headline numbers).
	if _, ok := data["counts"]; !ok {
		t.Fatal("counts must survive projection")
	}
}
