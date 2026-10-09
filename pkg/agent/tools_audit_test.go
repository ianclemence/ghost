package agent

import (
	"encoding/json"
	"fmt"
	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/tools"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestToolInventoryIsSound audits every tool the runtime actually registers:
// a real schema, a description a model can act on, required fields that exist,
// and no two tools with the same name. GHOST_TOOL_AUDIT=1 also writes the
// inventory for a human to read.
func TestToolInventoryIsSound(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	defs := al.Tools().GetDefinitions()
	if len(defs) < 20 {
		t.Fatalf("expected a full registry, got %d tools", len(defs))
	}
	seen := map[string]bool{}
	var report []string
	for _, d := range defs {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]interface{})
		if name == "" {
			t.Errorf("tool with no name: %v", d)
			continue
		}
		if seen[name] {
			t.Errorf("duplicate tool name %q", name)
		}
		seen[name] = true
		if len(strings.TrimSpace(desc)) < 20 {
			t.Errorf("%s: description too thin for a model to choose it (%q)", name, desc)
		}
		if params == nil || params["type"] != "object" {
			t.Errorf("%s: parameters must be an object schema, got %v", name, params["type"])
			continue
		}
		props, _ := params["properties"].(map[string]interface{})
		if req, ok := params["required"].([]interface{}); ok {
			for _, r := range req {
				if _, has := props[fmt.Sprint(r)]; !has {
					t.Errorf("%s: required field %q is not a declared property", name, r)
				}
			}
		}
		for pn, pv := range props {
			pm, _ := pv.(map[string]interface{})
			if _, ok := pm["type"]; !ok && pm["enum"] == nil && pm["oneOf"] == nil && pm["anyOf"] == nil {
				t.Errorf("%s.%s: property has no type", name, pn)
			}
		}
		var keys []string
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		report = append(report, fmt.Sprintf("%-22s %3d chars desc, %d params (%s)", name, len(desc), len(props), strings.Join(keys, ",")))
	}
	if os.Getenv("GHOST_TOOL_AUDIT") != "" {
		sort.Strings(report)
		b, _ := json.MarshalIndent(report, "", " ")
		os.WriteFile(os.Getenv("GHOST_TOOL_AUDIT"), b, 0644)
	}
}

// reviewedUngoverned are the registered tools that sit outside every
// governance table on purpose. Each was read and judged to have no external
// side effect beyond Ghost's own state: they read local state, answer a
// question, or render to the owner's own screen. A NEW tool that is not
// governed and not on this list fails the test, so adding a tool forces the
// question "does this need approval?" to be answered in review.
var reviewedUngoverned = map[string]string{
	"canvas":         "renders HTML to the owner's own screen",
	"clarify":        "asks the owner a question",
	"connections":    "list/status/begin only; setup steps, no changes",
	"doc_parser":     "reads a workspace-confined file",
	"memory_explain": "reads memory provenance",
	"present_card":   "renders a validated block card to the owner's own screen; every field is checked against a fixed catalog and nothing in it can run, open or style anything",
	"memory_correct": "changes one of Ghost's own beliefs only with confirmed=true; old values stay in history; no external effect",
	"networking":     "status/tailscale/bonjour info, read-only",
	"oracle":         "workspace-confined files to the configured model, same egress as a turn",
	"spawn":          "delegate; the subagent's own tools stay governed",
	"subagent":       "delegate; the subagent's own tools stay governed",
	"switch_lane":    "changes Ghost's own routing lane",
	"system_status":  "read-only health",
	"todo":           "Ghost's own task list",
	"tts":            "speech synthesis of given text",
	"video_frames":   "workspace-confined ffmpeg frame extraction",
	"vision":         "workspace-confined image or a safety-checked URL",
	"voice_wake":     "toggles Ghost's own wake listener",
	"move_file":      "renames inside the workspace only, never overwrites, refuses Ghost's default files and folders",
	"draft":          "shows a draft (email, event, text) on the owner's own screen; it never sends: sending is the owner's tap on the card, carried out by the Pod's card handler, not by the model",
	"document":       "writes a document into the workspace and lays it out as a PDF locally (no network while printing); shown on the owner's own screen",
	"people":         "the owner's own records of the people in their life, on the Pod; forgetting needs confirmed=true",
	"vault":          "the owner's own records of their documents, on the Pod; reads only workspace files; forgetting needs confirmed=true",
	"money":          "the owner's own spending records on the Pod; imports a workspace file; no bank, no payment; forgetting needs confirmed=true",
	"trips":          "the owner's own trip records on the Pod; no booking, no payment; forgetting needs confirmed=true",
	"phone":          "reads what the owner's phone shared with the Pod (notifications, health totals) and keeps place reminders the phone watches; nothing leaves the Pod",
}

func TestEveryToolIsGovernedOrReviewed(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	for _, n := range al.Tools().List() {
		_, capability := capability.ForTool(n)
		if capability || tools.IsFreeConsequentialTool(n) || tools.GrantRequired(n) {
			continue
		}
		if _, ok := reviewedUngoverned[n]; !ok {
			t.Errorf("tool %q is registered but neither governed nor reviewed: decide whether it needs an approval and classify it", n)
		}
	}
}
