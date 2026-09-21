package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/nontty"
)

// Guided onboarding: one resumable flow instead of two half-onboardings.
// setup.sh ends with system prep and points here; this finishes product
// setup: provider choice → credential-or-local verification → config +
// identity + workspace → readiness. Steps persist in a marker file, so
// re-running resumes instead of re-prompting; --force restarts.
//
// Non-TTY rule: every prompt has a flag. Without a terminal and without
// flags, guided fails loudly naming the flag instead of hanging.

// guidedOpts wires guided onboarding for production (os streams) and
// tests (buffers + stub Ollama endpoint).
type guidedOpts struct {
	ConfigPath  string
	Home        string
	Stdin       io.Reader
	Stdout      io.Writer
	OllamaBase  string // default http://localhost:11434
	Provider    string // flag: ollama|<provider> (empty = prompt, default ollama)
	Model       string // flag: model tag (empty = default for provider)
	APIKey      string // flag: credential for key providers
	SkipChannel bool
	Yes         bool
	Force       bool
	NoVerify    bool // tests only: skip network verification
}

type onboardState struct {
	Steps    map[string]bool `json:"steps"`
	Provider string          `json:"provider,omitempty"`
	Model    string          `json:"model,omitempty"`
}

func onboardMarkerPath(home string) string {
	return filepath.Join(home, ".ghost", ".onboard-state")
}

func loadOnboardState(home string) onboardState {
	st := onboardState{Steps: map[string]bool{}}
	raw, err := os.ReadFile(onboardMarkerPath(home))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(raw, &st)
	if st.Steps == nil {
		st.Steps = map[string]bool{}
	}
	return st
}

func saveOnboardState(home string, st onboardState) error {
	if err := os.MkdirAll(filepath.Dir(onboardMarkerPath(home)), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(onboardMarkerPath(home), raw, 0600)
}

// guidedPrompt resolves one value: flag wins, --yes takes the default,
// otherwise an interactive prompt. Non-TTY without flags is an error
// naming the flag, never a hang.
func guidedPrompt(o *guidedOpts, reader *bufio.Reader, flag, def, question, flagName string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if o.Yes {
		return def, nil
	}
	if err := nontty.RequireInteractive(question, flagName); err != nil {
		return "", err
	}
	fmt.Fprintf(o.Stdout, "%s [%s]: ", question, def)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("no input; re-run with %s", flagName)
	}
	if strings.TrimSpace(line) == "" {
		return def, nil
	}
	return strings.TrimSpace(line), nil
}

// ollamaModels lists model tags from an Ollama server.
func ollamaModels(base string, client *http.Client) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Get(strings.TrimSuffix(base, "/") + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range body.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

// runGuided executes the resumable onboarding flow.
func runGuided(o guidedOpts) error {
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.OllamaBase == "" {
		o.OllamaBase = config.DefaultLocalBaseURL
	}
	reader := bufio.NewReader(o.Stdin)
	st := loadOnboardState(o.Home)
	if o.Force {
		st = onboardState{Steps: map[string]bool{}}
	}
	mark := func(step string) error {
		st.Steps[step] = true
		return saveOnboardState(o.Home, st)
	}

	// Step 1: provider. Local leads (the differentiator); API keys are
	// the alternative. --provider ollama|<name> answers headless.
	if !st.Steps["provider"] {
		provider := o.Provider
		if provider == "" {
			fmt.Fprintln(o.Stdout, "Provider: [1] local Ollama (default, private, free)  [2] API key (openrouter/anthropic/...)")
			choice, err := guidedPrompt(&o, reader, "", "1", "Choose 1 or 2", "--provider")
			if err != nil {
				return err
			}
			if strings.TrimSpace(choice) == "2" {
				provider = "key"
			} else {
				provider = config.DefaultLocalProvider
			}
		}
		if provider == "key" {
			name, err := guidedPrompt(&o, reader, "", "openrouter", "API provider name", "--provider=<name>")
			if err != nil {
				return err
			}
			if !knownProviders[strings.ToLower(name)] {
				return fmt.Errorf("unknown provider %q (see `ghost model list` for options)", name)
			}
			provider = strings.ToLower(name)
		}
		st.Provider = provider
		if err := mark("provider"); err != nil {
			return err
		}
	}

	// Step 2: verification. Ollama: reachable server + present model.
	// Key providers: non-empty credential. No silent unconfigured state.
	if !st.Steps["verify"] {
		if st.Provider == config.DefaultLocalProvider {
			model := o.Model
			if model == "" {
				model = config.DefaultLocalTag
			}
			if !o.NoVerify {
				models, err := ollamaModels(o.OllamaBase, nil)
				if err != nil {
					return fmt.Errorf("ollama unreachable at %s: start it (`ollama serve`), or re-run with --provider=<name> --api-key=... (%v)", o.OllamaBase, err)
				}
				found := false
				for _, m := range models {
					if m == model || strings.HasPrefix(m, model+":") {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("ollama has no %s (has: %v): run `ollama pull %s`, then re-run", model, models, model)
				}
			}
			st.Model = model
		} else {
			// Key providers: the credential is collected (and stored
			// in secrets) at the config step; here we only record that
			// verification passed provider selection. Format is checked
			// when the key arrives; unknown providers were already
			// rejected at step 1.
			st.Model = st.Provider + ":default"
		}
		if err := mark("verify"); err != nil {
			return err
		}
	}

	// Step 3: config. Writes the verified selection; registers the local
	// connection so `ghost model` shows where Ollama lives. API keys go
	// to the secrets boundary, never config.json. An optional channel
	// step configures Telegram unless skipped.
	if !st.Steps["config"] {
		cfg := config.DefaultConfig()
		if existing, err := config.LoadConfig(o.ConfigPath); err == nil {
			cfg = existing
		}
		if st.Provider == config.DefaultLocalProvider {
			cfg.SetActiveModel(config.DefaultLocalProvider, st.Model)
			cfg.Agents.Connections = []config.ModelConnection{{
				Name: "local", Provider: config.DefaultLocalProvider,
				Model: st.Model, BaseURL: o.OllamaBase, AuthType: "none",
			}}
		} else {
			cfg.SetActiveModel(st.Provider, strings.TrimPrefix(st.Model, st.Provider+":"))
		}
		if !o.SkipChannel {
			tok, err := guidedPrompt(&o, reader, "", "", "Telegram bot token (empty skips channels)", "--skip-channels")
			if err != nil {
				// Non-TTY without --skip-channels fails here by design:
				// channels must not silently misconfigure headless runs.
				return err
			}
			if strings.TrimSpace(tok) != "" {
				cfg.Channels.Telegram.Token = strings.TrimSpace(tok)
				cfg.Channels.Telegram.Enabled = true
			}
		}
		if err := config.SaveConfig(o.ConfigPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		// Secrets last, through the credential authority: SaveConfig rewrites
		// the secrets file from the (credential-cleared) config, so storing
		// the API key must come after — and only via Vault.Store, never by
		// touching the secrets file directly (architecture boundary).
		if st.Provider != config.DefaultLocalProvider {
			key := o.APIKey
			if key == "" {
				var err error
				key, err = guidedPrompt(&o, reader, "", "", fmt.Sprintf("API key for %s", st.Provider), "--api-key=...")
				if err != nil {
					return err
				}
			}
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("empty API key; re-run with --api-key=...")
			}
			vault := credentials.New(filepath.Dir(o.ConfigPath))
			if err := vault.Store(st.Provider, strings.TrimSpace(key)); err != nil {
				return fmt.Errorf("store credential: %w", err)
			}
		}
		if err := mark("config"); err != nil {
			return err
		}
	}

	// Step 4: identity + workspace templates. Idempotent: re-running a
	// resumed flow never duplicates identity or clobbers user edits.
	if !st.Steps["identity"] {
		cfg, err := config.LoadConfig(o.ConfigPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		createWorkspaceTemplates(cfg.WorkspacePath())
		if _, err := ghoststate.EnsureIdentity(cfg.WorkspacePath()); err != nil {
			return fmt.Errorf("ensure identity: %w", err)
		}
		if err := mark("identity"); err != nil {
			return err
		}
	}

	fmt.Fprintf(o.Stdout, "\nGhost is ready (%s, model %s).\n", st.Provider, st.Model)
	fmt.Fprintln(o.Stdout, "Chat: ghost -m \"Hello!\"   Health: ghost status")
	return mark("done")
}
