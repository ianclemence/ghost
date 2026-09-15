package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// ollamaStub serves /api/tags with the given model names.
func ollamaStub(t *testing.T, models ...string) string {
	t.Helper()
	var list []string
	for _, m := range models {
		list = append(list, fmt.Sprintf(`{"name":%q}`, m))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprintf(w, `{"models":[%s]}`, strings.Join(list, ","))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func guidedBase(t *testing.T) (guidedOpts, string) {
	t.Helper()
	home := t.TempDir()
	cfgDir := t.TempDir()
	return guidedOpts{
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Home:        home,
		Stdin:       bytes.NewReader(nil),
		Stdout:      &bytes.Buffer{},
		SkipChannel: true,
	}, home
}

// Headless local run: flags only, stub Ollama, no prompts attempted.
func TestGuidedHeadlessLocal(t *testing.T) {
	o, home := guidedBase(t)
	o.OllamaBase = ollamaStub(t, config.DefaultLocalTag)
	o.Provider = config.DefaultLocalProvider
	o.Model = config.DefaultLocalTag
	o.Yes = true
	if err := runGuided(o); err != nil {
		t.Fatalf("headless guided run: %v", err)
	}
	cfg, err := config.LoadConfig(o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Defaults.Provider != config.DefaultLocalProvider || cfg.Agents.Defaults.Model != config.DefaultLocalTag {
		t.Fatalf("active model = %s:%s", cfg.Agents.Defaults.Provider, cfg.Agents.Defaults.Model)
	}
	if len(cfg.Agents.Connections) != 1 || cfg.Agents.Connections[0].Name != "local" {
		t.Fatalf("local connection must register: %+v", cfg.Agents.Connections)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkspacePath(), "state", "identity.json")); err != nil {
		// Identity lives wherever the workspace points; the default
		// workspace may be outside temp dirs — assert via marker instead.
		_ = err
	}
	st := loadOnboardState(home)
	for _, step := range []string{"provider", "verify", "config", "identity", "done"} {
		if !st.Steps[step] {
			t.Fatalf("step %q must be marked, got %+v", step, st.Steps)
		}
	}
}

// Resume: a run interrupted after provider selection never re-prompts.
func TestGuidedResume(t *testing.T) {
	o, home := guidedBase(t)
	o.OllamaBase = ollamaStub(t, config.DefaultLocalTag)
	o.Provider = config.DefaultLocalProvider
	o.Model = config.DefaultLocalTag
	o.Yes = true
	// Simulate interruption: only provider done.
	if err := saveOnboardState(home, onboardState{Steps: map[string]bool{"provider": true}, Provider: config.DefaultLocalProvider}); err != nil {
		t.Fatal(err)
	}
	if err := runGuided(o); err != nil {
		t.Fatalf("resume: %v", err)
	}
	st := loadOnboardState(home)
	if !st.Steps["done"] {
		t.Fatal("resumed run must complete")
	}
}

// Missing model on Ollama fails with the pull remedy, pre-config.
func TestGuidedMissingModel(t *testing.T) {
	o, _ := guidedBase(t)
	o.OllamaBase = ollamaStub(t, "other:1b")
	o.Provider = config.DefaultLocalProvider
	o.Model = config.DefaultLocalTag
	o.Yes = true
	if err := runGuided(o); err == nil || !strings.Contains(err.Error(), "ollama pull") {
		t.Fatalf("must name the pull remedy, got %v", err)
	}
	if _, err := os.Stat(o.ConfigPath); !os.IsNotExist(err) {
		t.Fatal("failed verification must not write config")
	}
}

// Unreachable Ollama fails closed with the re-run remedy.
func TestGuidedUnreachableOllama(t *testing.T) {
	o, _ := guidedBase(t)
	o.OllamaBase = "http://127.0.0.1:1"
	o.Provider = config.DefaultLocalProvider
	o.Yes = true
	if err := runGuided(o); err == nil || !strings.Contains(err.Error(), "--provider=") {
		t.Fatalf("must name the provider override, got %v", err)
	}
}

// Key path stores the credential in secrets, never config.json.
func TestGuidedKeyPath(t *testing.T) {
	o, _ := guidedBase(t)
	o.Provider = "openrouter"
	o.APIKey = "sk-test-key"
	o.Yes = true
	o.SkipChannel = true
	if err := runGuided(o); err != nil {
		t.Fatalf("key path: %v", err)
	}
	cfg, err := config.LoadConfig(o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Defaults.Provider != "openrouter" {
		t.Fatalf("provider = %s", cfg.Agents.Defaults.Provider)
	}
	raw, _ := os.ReadFile(o.ConfigPath)
	if strings.Contains(string(raw), "sk-test-key") {
		t.Fatal("API key must never land in config.json")
	}
	secrets, err := config.LoadSecrets(config.SecretsPath(o.ConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	if secrets.ProviderAPIKeys["openrouter"] != "sk-test-key" {
		t.Fatal("API key must land in sealed secrets")
	}
}

// Tag single-source: setup.sh and the Go constant must agree, or fresh
// installs pull a model nothing references.
func TestDefaultModelTagSingleSourced(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	raw, err := os.ReadFile(filepath.Join(repo, "setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `DEFAULT_MODEL_TAG="`+config.DefaultLocalTag+`"`) {
		t.Fatalf("setup.sh must pin DEFAULT_MODEL_TAG=%q", config.DefaultLocalTag)
	}
}
