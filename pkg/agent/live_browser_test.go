package agent

// Live browser end-to-end: real model (configured deepseek-flash),
// real agent-browser CLI, real gate/broker/evidence — on a scratch
// workspace copy, in scratch sessions. Gated: GHOST_LIVE_BROWSER=1,
// otherwise skipped so CI and normal runs never touch the network,
// the model, or a browser.
//
// This is the proof run for the interaction surface: navigate's merged
// accessibility tree, find/wait/scroll/screenshot/console/a11y
// observation, batch fill_form under the broker, and the explicit
// screenshot attaching to model context.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/providers"
)

type liveBrowserStep struct {
	Tool string `json:"tool"`
	Args string `json:"args,omitempty"`
}

type liveBrowserTurn struct {
	User  string            `json:"user"`
	Reply string            `json:"reply"`
	Tools []liveBrowserStep `json:"tools"`
	Ms    int64             `json:"ms"`
	Error string            `json:"error,omitempty"`
}

func liveBrowserGate(t *testing.T) {
	t.Helper()
	if os.Getenv("GHOST_LIVE_BROWSER") != "1" {
		t.Skip("live browser needs GHOST_LIVE_BROWSER=1 (real model, real browser, real cost)")
	}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Skip("agent-browser not installed")
	}
}

// wireLiveBrowserGovernance attaches broker/events/contexts exactly as
// golden + gateway startup do, with one disclosed deviation: ModeFull —
// the owner has pre-authorized browser use for this test run so the
// model-driven flow completes without interactive approval cards.
// High impact (submit, upload) still evaluates to ASK and would block;
// the prompts below deliberately exclude them.
func wireLiveBrowserGovernance(loop *AgentLoop, ws string) error {
	db := loop.DB()
	broker, err := permissions.Open(db, permissions.ModeAsk, 0)
	if err != nil {
		return fmt.Errorf("open broker: %w", err)
	}
	broker.SetMode(permissions.ModeFull)
	events, err := cevents.Open(db, filepath.Join(ws, "events"))
	if err != nil {
		return fmt.Errorf("open events: %w", err)
	}
	gid := "ghost-live"
	if id, err := ghoststate.LoadIdentity(ws); err == nil && id != nil {
		gid = id.GhostID
	}
	cs, err := contexts.Open(ws, gid)
	if err != nil {
		return fmt.Errorf("open contexts: %w", err)
	}
	gov := NewGovernance(events, broker, gid, "agent-main")
	gov.Contexts = cs
	loop.SetGovernance(gov)
	return nil
}

func TestLiveBrowser(t *testing.T) {
	liveBrowserGate(t)

	wsCopy := "/tmp/opencode/livebrowser/ws"
	os.RemoveAll(wsCopy)
	if err := os.MkdirAll(wsCopy, 0o755); err != nil {
		t.Fatal(err)
	}
	copyDir(t, "/var/lib/ghost/workspace", wsCopy)

	cfg, err := config.LoadConfig(filepath.Join(liveEvalConfigDir(), "config.json"))
	if err != nil {
		t.Fatalf("load prod config: %v", err)
	}
	cfg.Agents.Defaults.Workspace = wsCopy
	p, err := providers.CreateProvider(cfg)
	if err != nil {
		t.Fatalf("BLOCKED: no live provider from prod config: %v", err)
	}
	loop, err := NewAgentLoop(cfg, bus.NewMessageBus(), p)
	if err != nil {
		t.Fatalf("loop init: %v", err)
	}
	if err := wireLiveBrowserGovernance(loop, wsCopy); err != nil {
		t.Fatalf("governance: %v", err)
	}
	t.Logf("live browser: model=%s workspace=%s", cfg.Agents.Defaults.Model, wsCopy)

	turns := []string{
		"Open https://nairobiunwind.com in the browser and tell me what this site is about: its title, what kind of place or business it is, and the main navigation links you can see.",
		"Take a browser_screenshot of the page, then browser_scroll down one screen, then browser_wait until the page has settled, and report what is now visible. Also run browser_console and browser_a11y and summarize any errors or accessibility violations you find.",
		"On the current page, use browser_find to locate a form field. Preferred: a contact or newsletter signup with name and email — fill both with one browser_fill_form call (name 'Ian Test', email 'ian.test@example.com'). If there is no such form, click the 'Register' link with browser_click to open the registration form and fill its name and email fields with browser_fill_form. If that has no name/email fields either, fill any text input you find with browser_fill_form (value 'go-karting'). Never submit and never press enter — fill only, then confirm the fields show the values.",
		"Pick one of the links you saw earlier, re-snapshot to get its fresh ref, click it with browser_click, then tell me the title and URL of the page you landed on and take a browser_screenshot of it.",
	}

	dir := "/tmp/opencode/livebrowser"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var out []liveBrowserTurn
	dump := func() {
		data, _ := json.MarshalIndent(out, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "transcript.json"), data, 0o644)
	}

	for i, text := range turns {
		var steps []liveBrowserStep
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
		reply, err := loop.ProcessDirectWithChannel(ctx, text, "live-browser-nrw", "cli", "live-browser", nil, nil,
			func(tool, args string) {
				steps = append(steps, liveBrowserStep{Tool: tool, Args: args})
				t.Logf("turn %d tool: %s %.120s", i+1, tool, strings.ReplaceAll(args, "\n", " "))
			})
		cancel()
		lt := liveBrowserTurn{User: text, Reply: reply, Tools: steps, Ms: time.Since(start).Milliseconds()}
		if err != nil {
			lt.Error = err.Error()
		}
		out = append(out, lt)
		dump()
		t.Logf("turn %d done in %ds, tools=%d, err=%q", i+1, lt.Ms/1000, len(steps), lt.Error)
		t.Logf("turn %d reply: %.600s", i+1, reply)
		time.Sleep(2 * time.Second)
	}

	// Forensic summary: which of the new actions actually ran.
	ran := map[string]int{}
	for _, lt := range out {
		for _, s := range lt.Tools {
			ran[s.Tool]++
		}
	}
	t.Logf("tool usage across turns: %v", ran)
	for _, required := range []string{"browser_navigate", "browser_click", "browser_screenshot", "browser_fill_form"} {
		if ran[required] == 0 {
			t.Errorf("expected %s to run at least once", required)
		}
	}
	for _, lt := range out {
		if lt.Error != "" {
			t.Errorf("turn failed: %.100s → %s", lt.User, lt.Error)
		}
	}
}
