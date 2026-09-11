package commands

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// resetHandler implements /reset — factory reset for Ghost.
//
//	/reset all                        wipes EVERYTHING, no flags, no confirmation:
//	                                  chats, memory, activity, automations,
//	                                  personal context, paired devices, secrets,
//	                                  and the AI default (restored to local).
//	/reset all --exclude=devices,secrets
//	                                  wipes everything except the named scopes.
//	/reset <scope> [<scope>...]       wipes only the named scopes, e.g.
//	                                  /reset chats   or   /reset chats memory
//	/reset                            shows this help.
//
// Scopes: chats, memory, activity, automations, context, devices, secrets,
// model. A few common aliases are accepted (sessions, events, cron,
// personal, paired, ai) and canonicalized before anything runs.
func resetHandler(ctx context.Context, req Request, rt *Runtime) error {
	fields := strings.Fields(strings.TrimSpace(req.Text))

	if len(fields) < 2 {
		return req.Reply(resetHelp())
	}

	scopes, excludes, err := parseResetArgs(fields[1:])
	if err != nil {
		return req.Reply(fmt.Sprintf("%v\n\n%s", err, resetHelp()))
	}
	if len(scopes) == 0 {
		return req.Reply(resetHelp())
	}

	ws := workspaceForRuntime(rt)

	var results []string
	var errs []string
	run := func(name, okMsg string, fn func() error) {
		if err := fn(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		} else {
			results = append(results, okMsg)
		}
	}

	if scopes["chats"] {
		run("chats", "Chats: all sessions and messages cleared", func() error {
			return clearAllChats(ws, rt)
		})
	}
	if scopes["memory"] {
		run("memory", "Memory: MEMORY.md, daily notes, and memory_chunks cleared", func() error {
			return clearMemory(ws, rt)
		})
	}
	if scopes["activity"] {
		run("activity", "Activity: canonical events and event log cleared", func() error {
			return clearActivity(ws, rt)
		})
	}
	if scopes["model"] {
		msg, err := resetModelDefault(ws)
		if err != nil {
			errs = append(errs, fmt.Sprintf("model: %v", err))
		} else {
			results = append(results, msg)
		}
	}
	if scopes["automations"] {
		run("automations", "Automations: scheduled items, cron jobs, and execution history cleared", func() error {
			return clearAutomations(ws, rt)
		})
	}
	if scopes["context"] {
		run("context", "Personal Context: entries and knowledge profile cleared", func() error {
			return clearPersonalContext(ws, rt)
		})
	}
	if scopes["devices"] {
		run("devices", "Paired devices: cleared", func() error {
			return clearDevices(ws, rt)
		})
	}
	if scopes["secrets"] {
		run("secrets", "Secrets: API keys and device credentials cleared", func() error {
			return clearSecrets(ws)
		})
	}

	if len(excludes) > 0 {
		kept := make([]string, 0, len(excludes))
		for _, e := range excludes {
			kept = append(kept, e)
		}
		sort.Strings(kept)
		results = append(results, "Kept (--exclude): "+strings.Join(kept, ", "))
	}

	if scopes["chats"] && scopes["memory"] && scopes["activity"] &&
		scopes["automations"] && scopes["context"] && scopes["devices"] &&
		scopes["secrets"] && scopes["model"] {
		results = append(results, "Ghost is now fresh — like a new installation. Say hello to start.")
	}

	if len(errs) > 0 {
		return req.Reply(fmt.Sprintf("Reset completed with errors:\n- %s\n\n%s", strings.Join(errs, "\n- "), strings.Join(results, "\n- ")))
	}
	return req.Reply(fmt.Sprintf("Reset complete:\n- %s", strings.Join(results, "\n- ")))
}

// resetScopeAlias canonicalizes a scope name or alias. It reports whether
// the token is a known scope.
func resetScopeAlias(token string) (string, bool) {
	switch strings.ToLower(token) {
	case "all":
		return "all", true
	case "chats", "sessions", "messages":
		return "chats", true
	case "memory":
		return "memory", true
	case "activity", "events":
		return "activity", true
	case "automations", "automation", "cron", "scheduled":
		return "automations", true
	case "context", "personal-context", "personal":
		return "context", true
	case "model", "ai":
		return "model", true
	case "devices", "paired", "paired-devices":
		return "devices", true
	case "secrets", "secret", "keys":
		return "secrets", true
	}
	return "", false
}

var resetAllScopes = []string{
	"chats", "memory", "activity", "model",
	"automations", "context", "devices", "secrets",
}

// parseResetArgs resolves the scope set for a /reset invocation. It returns
// the active scopes plus the canonical exclude list (for reporting).
func parseResetArgs(args []string) (map[string]bool, []string, error) {
	scopes := map[string]bool{}
	var excludes []string
	sawScope := false
	for _, a := range args {
		if strings.HasPrefix(a, "--exclude=") {
			raw := strings.TrimPrefix(a, "--exclude=")
			if strings.TrimSpace(raw) == "" {
				return nil, nil, fmt.Errorf("--exclude needs a value, e.g. --exclude=devices,secrets")
			}
			for _, part := range strings.Split(raw, ",") {
				name, ok := resetScopeAlias(strings.TrimSpace(part))
				if !ok || name == "all" {
					return nil, nil, fmt.Errorf("cannot exclude %q: not a reset scope", strings.TrimSpace(part))
				}
				excludes = append(excludes, name)
			}
			continue
		}
		if strings.HasPrefix(a, "--") {
			return nil, nil, fmt.Errorf("unknown option %q", a)
		}
		name, ok := resetScopeAlias(a)
		if !ok {
			return nil, nil, fmt.Errorf("unknown reset scope %q", a)
		}
		if name == "all" {
			for _, s := range resetAllScopes {
				scopes[s] = true
			}
		} else {
			scopes[name] = true
		}
		sawScope = true
	}
	for _, e := range excludes {
		delete(scopes, e)
	}
	if !sawScope && len(excludes) > 0 {
		return nil, nil, fmt.Errorf("--exclude needs a scope to apply to, e.g. /reset all --exclude=devices")
	}
	return scopes, excludes, nil
}

func resetHelp() string {
	return "Usage:\n" +
		"  /reset all                                — wipe EVERYTHING (chats, memory, activity, automations, context, devices, secrets, AI default)\n" +
		"  /reset all --exclude=devices,secrets      — wipe everything except the named scopes\n" +
		"  /reset chats                              — clear all chat history\n" +
		"  /reset memory                             — clear MEMORY.md and daily notes\n" +
		"  /reset activity                           — clear Activity (canonical events, event log)\n" +
		"  /reset automations                        — clear automations and cron jobs\n" +
		"  /reset context                            — clear Personal Context and knowledge\n" +
		"  /reset devices                            — clear paired devices\n" +
		"  /reset secrets                            — clear API keys and device credentials\n" +
		"  /reset model                              — restore local default AI provider/model\n" +
		"\nScopes: chats, memory, activity, automations, context, devices, secrets, model.\n" +
		"Multiple scopes allowed: /reset chats memory"
}

func workspaceForRuntime(rt *Runtime) string {
	if rt != nil && rt.Workspace != "" {
		return rt.Workspace
	}
	if v := strings.TrimSpace(os.Getenv("GHOST_WORKSPACE_DIR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GHOST_WORKSPACE")); v != "" {
		return v
	}
	return "workspace"
}

// clearAllChats deletes all chat history from DB and filesystem.
func clearAllChats(ws string, rt *Runtime) error {
	// Prefer runtime session manager if available
	if rt != nil && rt.Sessions != nil {
		// Enumerate via DB if possible
		if db := dbFromRuntime(rt, ws); db != nil {
			_, _ = db.Exec(`DELETE FROM messages`)
			_, _ = db.Exec(`DELETE FROM sessions`)
			_, _ = db.Exec(`DELETE FROM kv_store WHERE key LIKE 'session:%'`)
			// FTS triggers handle cleanup; rebuild to be safe
			_, _ = db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`)
		}
	}
	// Filesystem: sessions/*.jsonl or similar
	_ = os.RemoveAll(filepath.Join(ws, "sessions"))
	_ = os.MkdirAll(filepath.Join(ws, "sessions"), 0755)
	_ = os.RemoveAll(filepath.Join(ws, "conversations"))
	return nil
}

func clearMemory(ws string, rt *Runtime) error {
	db := dbFromRuntime(rt, ws)
	if db != nil {
		_, _ = db.Exec(`DELETE FROM memory_chunks`)
	}
	// Drop the in-memory vector index too — otherwise Retrieve keeps
	// serving deleted memories until the process restarts.
	if rt != nil && rt.RAG != nil {
		rt.RAG.Reset()
	}
	// Files: memory/MEMORY.md, memory/YYYYMM/*.md, knowledge, data
	memDir := filepath.Join(ws, "memory")
	if _, err := os.Stat(memDir); err == nil {
		_ = os.RemoveAll(memDir)
		_ = os.MkdirAll(memDir, 0755)
		_ = os.MkdirAll(filepath.Join(memDir, "202609"), 0755)
		_ = os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Memory\n\n"), 0644)
	}
	// Keep knowledge dir but clear daily notes already handled
	// Clear data captures/reminders (derived, not canonical user data? Keep but truncate)
	for _, p := range []string{
		filepath.Join(ws, "data", "captures.md"),
		filepath.Join(ws, "data", "reminders.md"),
	} {
		_ = os.WriteFile(p, []byte(""), 0644)
	}
	_ = os.RemoveAll(filepath.Join(ws, "journal"))
	_ = os.MkdirAll(filepath.Join(ws, "journal"), 0755)
	return nil
}

func clearActivity(ws string, rt *Runtime) error {
	// Canonical events are the audit stream behind /v1/activity. A factory
	// reset must clear them too, or old activity (skill lifecycle, agent
	// outcomes) survives into what should look like a new installation.
	if db := dbFromRuntime(rt, ws); db != nil {
		_, _ = db.Exec(`DELETE FROM canonical_events`)
	}
	_ = os.RemoveAll(filepath.Join(ws, "events"))
	_ = os.MkdirAll(filepath.Join(ws, "events"), 0755)
	return nil
}

// resetModelDefault restores the default AI provider/model to a LOCAL runtime
// after a factory reset, so the appliance does not boot pointed at a cloud
// provider whose credentials were (correctly) kept out of the reset. A local
// default never produces a spurious "missing credentials for <cloud model>"
// state. Best-effort: if no local preset exists, the current default is left
// untouched rather than inventing one.
func resetModelDefault(ws string) (string, error) {
	path := resolveConfigFilePath(ws)
	if path == "" {
		return "", fmt.Errorf("config file not found")
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return "", err
	}
	var pick *config.ModelPreset
	for i := range cfg.Agents.ModelList {
		p := &cfg.Agents.ModelList[i]
		if strings.EqualFold(strings.TrimSpace(p.Provider), "ollama") {
			pick = p
			break
		}
	}
	if pick == nil {
		return "AI: no local ollama preset found; default left unchanged", nil
	}
	// Canonical default model: "ollama/qwen3:0.6b". If the preset already
	// carries the provider prefix (slash form), never double it.
	canonical := strings.TrimSpace(pick.Model)
	if !strings.Contains(canonical, "/") {
		canonical = pick.Provider + "/" + canonical
	}
	alreadyLocal := strings.EqualFold(strings.TrimSpace(cfg.Agents.Defaults.Provider), pick.Provider) &&
		strings.TrimSpace(cfg.Agents.Defaults.Model) == canonical
	if alreadyLocal {
		return "AI: default provider already local (" + canonical + ")", nil
	}
	cfg.Agents.Defaults.Provider = pick.Provider
	cfg.Agents.Defaults.Model = canonical
	cfg.Agents.Defaults.FallbackModels = []string{}
	if err := config.SaveConfig(path, cfg); err != nil {
		return "", err
	}
	return "AI: default provider reset to local (" + canonical + ") — restart Ghost to apply", nil
}

// resolveConfigFilePath finds the runtime config.json for a workspace. The
// gateway process runs with its config directory nearby, so several standard
// locations are tried and the first existing file wins.
func resolveConfigFilePath(ws string) string {
	var candidates []string
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		candidates = append(candidates, filepath.Join(d, "config.json"))
	}
	if ws != "" {
		candidates = append(candidates,
			filepath.Join(ws, "config", "config.json"),
			filepath.Join(ws, "..", "config", "config.json"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "config", "config.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "ghost", "config", "config.json"),
			filepath.Join(home, ".ghost", "config.json"),
		)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func clearAutomations(ws string, rt *Runtime) error {
	db := dbFromRuntime(rt, ws)
	if db != nil {
		_, _ = db.Exec(`DELETE FROM scheduled_items`)
		_, _ = db.Exec(`DELETE FROM execution_history`)
		_, _ = db.Exec(`DELETE FROM jobs`)
	}
	// Cron legacy
	cronPath := filepath.Join(ws, "cron", "jobs.json")
	_ = os.MkdirAll(filepath.Join(ws, "cron"), 0755)
	_ = os.WriteFile(cronPath, []byte(`{"version":1,"jobs":[]}`), 0644)
	// State evolution/learning derived
	_ = os.RemoveAll(filepath.Join(ws, "state"))
	_ = os.MkdirAll(filepath.Join(ws, "state"), 0755)
	return nil
}

func clearPersonalContext(ws string, rt *Runtime) error {
	// Reset the live store first: it clears in-memory state AND truncates
	// the log. File-only wipes leave stale beliefs visible until restart.
	if rt != nil && rt.PersonalContext != nil {
		if err := rt.PersonalContext.Reset(); err != nil {
			return err
		}
	} else {
		pcPath := filepath.Join(ws, "personal-context", "entries.jsonl")
		_ = os.MkdirAll(filepath.Join(ws, "personal-context"), 0755)
		_ = os.WriteFile(pcPath, []byte(""), 0644)
	}
	// Knowledge profile
	prof := filepath.Join(ws, "knowledge", "self", "user-profile.md")
	_ = os.MkdirAll(filepath.Dir(prof), 0755)
	_ = os.WriteFile(prof, []byte(""), 0644)
	// Workspace identity doc: USER.md is injected into every prompt, so a
	// name left here survives all other wipes and the model greets a
	// stranger by the old name. Restore the placeholder template.
	userDoc := filepath.Join(ws, "USER.md")
	_ = os.WriteFile(userDoc, []byte(userDocTemplate), 0644)
	return nil
}

// UserDocTemplate is the fresh-install USER.md: no names, no locations,
// no timezone assumptions. Personal facts live in personal-context and the
// knowledge profile, both cleared above.
const UserDocTemplate = `# User Profile

This file stores durable, high-signal user facts and preferences.
Only update this file when information is stable over time.

## Identity

- **Name**: (set by user)
- **Role**: (set by user)
- **Location**: (set by user)
- **Timezone**: (device-derived; confirm if the user travels)
`

// userDocTemplate is kept as an alias for existing internal callers.
const userDocTemplate = UserDocTemplate

// EnsureUserDoc writes the fresh-install USER.md template when the workspace
// has none (e.g. a fresh clone: USER.md is per-installation state and is not
// tracked in git). Never touches an existing file.
func EnsureUserDoc(ws string) error {
	userDoc := filepath.Join(ws, "USER.md")
	if _, err := os.Stat(userDoc); err == nil {
		return nil
	}
	_ = os.MkdirAll(ws, 0755)
	return os.WriteFile(userDoc, []byte(UserDocTemplate), 0644)
}

func clearDevices(ws string, rt *Runtime) error {
	db := dbFromRuntime(rt, ws)
	if db != nil {
		_, _ = db.Exec(`DELETE FROM paired_devices`)
		_, _ = db.Exec(`DELETE FROM pending_pairings`)
	}
	return nil
}

func clearSecrets(ws string) error {
	// Secrets live in the config dir, whose location depends on the layout
	// (appliance, repo checkout, or GHOST_CONFIG_DIR override). Sweep every
	// candidate so /reset all cannot leave keys behind in one layout while
	// wiping them in another.
	var candidates []string
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		candidates = append(candidates, d)
	}
	if ws != "" {
		candidates = append(candidates,
			filepath.Join(ws, "config"),
			filepath.Join(ws, "..", "config"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "config"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "ghost"))
	}
	candidates = append(candidates, "/var/ghost/config")
	seen := map[string]bool{}
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		for _, name := range []string{".secrets.json", ".env"} {
			p := filepath.Join(abs, name)
			if _, err := os.Stat(p); err == nil {
				_ = os.Remove(p)
			}
		}
	}
	return nil
}

func dbFromRuntime(rt *Runtime, ws string) *sql.DB {
	if rt != nil && rt.Sessions != nil {
		if s, ok := rt.Sessions.Store().(interface{ DB() *sql.DB }); ok {
			if db := s.DB(); db != nil {
				return db
			}
		}
	}
	// Fallback: open DB directly
	dbPath := filepath.Join(ws, "ghost.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil
	}
	return db
}
