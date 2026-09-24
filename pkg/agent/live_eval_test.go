package agent

// Live conversational-intelligence evaluation: real model on the real
// production turn path (ProcessDirectWithChannel), scratch workspace copy,
// scratch sessions. Gated: GHOST_LIVE_EVAL=1, otherwise skipped so CI and
// normal runs never touch the network or the model.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

func liveEvalGate(t *testing.T) {
	t.Helper()
	if os.Getenv("GHOST_LIVE_EVAL") != "1" {
		t.Skip("live eval needs GHOST_LIVE_EVAL=1 (real model, real cost)")
	}
}

func liveEvalConfigDir() string {
	if d := os.Getenv("GHOST_CONFIG_DIR"); d != "" {
		return d
	}
	return "/var/ghost/config"
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	// tar preserves permissions; the root-owned affect.json (written by the
	// root-running daemon) is unreadable here, so it is excluded and the
	// harness starts with fresh affect state (disclosed deviation).
	cmd := exec.Command("tar", "-cf", "-",
		"--exclude=personal-context/affect.json", "-C", src, ".")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("workspace copy failed: %v %s", err, out.String())
	}
	untar := exec.Command("tar", "-xf", "-", "-C", dst)
	untar.Stdin = &out
	if cmdOut, err := untar.CombinedOutput(); err != nil {
		t.Fatalf("workspace unpack failed: %v %s", err, cmdOut)
	}
}

func TestLiveEvalProbe(t *testing.T) {
	liveEvalGate(t)
	cfg, err := config.LoadConfig(filepath.Join(liveEvalConfigDir(), "config.json"))
	if err != nil {
		t.Fatalf("load prod config: %v", err)
	}
	p, err := providers.CreateProvider(cfg)
	if err != nil {
		t.Fatalf("BLOCKED: no live provider from prod config: %v", err)
	}
	_ = p
	loop, err := NewAgentLoop(cfg, bus.NewMessageBus(), p)
	if err != nil {
		t.Fatalf("loop init: %v", err)
	}
	_ = loop
	t.Logf("live provider ready: model=%s", cfg.Agents.Defaults.Model)
}

type liveTurn struct {
	User  string   `json:"user"`
	Reply string   `json:"reply"`
	Tools []string `json:"tools"`
	Ms    int64    `json:"ms"`
	Error string   `json:"error,omitempty"`
}

type liveConversation struct {
	ID       string     `json:"id"`
	Category string     `json:"category"`
	Turns    []liveTurn `json:"turns"`
}

// TestLiveEvalRun drives the corpus through the real production turn path
// (ProcessDirectWithChannel) on a scratch workspace copy in scratch
// sessions. Nothing touches production state; the copy is discarded.
func TestLiveEvalRun(t *testing.T) {
	liveEvalGate(t)
	// Persistent scratch workspace (NOT t.TempDir): post-run forensics
	// needs the DB after the test ends. Rebuilt fresh every run.
	wsCopy := "/tmp/opencode/liveeval/ws"
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
		t.Fatalf("BLOCKED: %v", err)
	}
	loop, err := NewAgentLoop(cfg, bus.NewMessageBus(), p)
	if err != nil {
		t.Fatalf("loop init: %v", err)
	}

	raw, err := os.ReadFile("testdata/live_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Conversations []struct {
			ID       string   `json:"id"`
			Category string   `json:"category"`
			Turns    []string `json:"turns"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}

	var out []liveConversation
	for _, c := range corpus.Conversations {
		session := "eval-live-" + c.ID
		lc := liveConversation{ID: c.ID, Category: c.Category}
		for _, text := range c.Turns {
			var tools []string
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			reply, err := loop.ProcessDirectWithChannel(ctx, text, session, "cli", "live-eval", nil, nil,
				func(tool, args string) { tools = append(tools, tool) })
			cancel()
			lt := liveTurn{User: text, Reply: reply, Tools: tools, Ms: time.Since(start).Milliseconds()}
			if err != nil {
				lt.Error = err.Error()
			}
			lc.Turns = append(lc.Turns, lt)
			time.Sleep(2 * time.Second)
		}
		out = append(out, lc)
		t.Logf("done %s (%d turns)", c.ID, len(lc.Turns))
	}

	dir := "/tmp/opencode/liveeval"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "transcripts.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
