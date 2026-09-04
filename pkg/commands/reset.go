package commands

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resetHandler implements /reset — factory reset for Ghost.
// It supports selective clearing with confirmation and never touches secrets
// or paired devices unless explicitly flagged.
//
// Usage:
//
//	/reset all --yes                              (all chats, memory, automations, context)
//	/reset chats --yes
//	/reset memory --yes
//	/reset automations --yes
//	/reset context --yes
//	/reset devices --yes  (paired devices)
//	/reset all --yes --include-secrets --include-devices
//
// Without --yes, it shows what would be deleted and asks for confirmation.
// Secrets (config/.secrets.json, .env) are kept by default.
// Paired devices are kept by default (use --include-devices to wipe them).
func resetHandler(ctx context.Context, req Request, rt *Runtime) error {
	text := strings.TrimSpace(req.Text)
	fields := strings.Fields(text)

	if len(fields) < 2 {
		return req.Reply(resetHelp())
	}

	target := strings.ToLower(fields[1])
	// allow /reset --all --yes form
	if target == "--all" {
		target = "all"
	}
	validTargets := map[string]bool{
		"all": true, "chats": true, "sessions": true, "messages": true,
		"memory": true, "automations": true, "cron": true, "scheduled": true,
		"context": true, "personal-context": true, "personal": true,
		"devices": true, "paired": true, "paired-devices": true,
	}
	if !validTargets[target] && !strings.HasPrefix(target, "--") {
		return req.Reply(fmt.Sprintf("Unknown target %q.\n\n%s", target, resetHelp()))
	}
	// if target is a flag like --yes alone, treat as all
	if strings.HasPrefix(target, "--") {
		target = "all"
	}

	hasYes := hasFlag(fields, "--yes") || hasFlag(fields, "-y") || hasFlag(fields, "--confirm")
	includeSecrets := hasFlag(fields, "--include-secrets")
	includeDevices := hasFlag(fields, "--include-devices")

	if !hasYes {
		return req.Reply(resetPreview(target, includeSecrets, includeDevices))
	}

	ws := workspaceForRuntime(rt)

	var results []string
	var errs []string

	doChats := target == "all" || target == "chats" || target == "sessions" || target == "messages"
	doMemory := target == "all" || target == "memory"
	doAutomations := target == "all" || target == "automations" || target == "cron" || target == "scheduled"
	doContext := target == "all" || target == "context" || target == "personal-context" || target == "personal"
	doDevices := target == "devices" || target == "paired" || target == "paired-devices" || (target == "all" && includeDevices)

	if doChats {
		if err := clearAllChats(ws, rt); err != nil {
			errs = append(errs, fmt.Sprintf("chats: %v", err))
		} else {
			results = append(results, "Chats: all sessions and messages cleared")
		}
	}
	if doMemory {
		if err := clearMemory(ws, rt); err != nil {
			errs = append(errs, fmt.Sprintf("memory: %v", err))
		} else {
			results = append(results, "Memory: MEMORY.md, daily notes, and memory_chunks cleared")
		}
	}
	if doAutomations {
		if err := clearAutomations(ws, rt); err != nil {
			errs = append(errs, fmt.Sprintf("automations: %v", err))
		} else {
			results = append(results, "Automations: scheduled items, cron jobs, and execution history cleared")
		}
	}
	if doContext {
		if err := clearPersonalContext(ws, rt); err != nil {
			errs = append(errs, fmt.Sprintf("context: %v", err))
		} else {
			results = append(results, "Personal Context: entries and knowledge profile cleared")
		}
	}
	if doDevices {
		if err := clearDevices(ws, rt); err != nil {
			errs = append(errs, fmt.Sprintf("devices: %v", err))
		} else {
			results = append(results, "Paired devices: cleared")
		}
	} else if target == "all" && !includeDevices {
		results = append(results, "Paired devices: kept (use --include-devices to clear)")
	}

	if includeSecrets {
		if err := clearSecrets(ws); err != nil {
			errs = append(errs, fmt.Sprintf("secrets: %v", err))
		} else {
			results = append(results, "Secrets: cleared (config/.secrets.json)")
		}
	} else {
		if doAll(target) {
			results = append(results, "Secrets: kept (use --include-secrets to clear)")
		}
	}

	// On full reset, set a clean slate for new user
	if target == "all" {
		results = append(results, "Ghost is now fresh — like a new installation. Say hello to start.")
	}

	if len(errs) > 0 {
		return req.Reply(fmt.Sprintf("Reset completed with errors:\n- %s\n\n%s", strings.Join(errs, "\n- "), strings.Join(results, "\n- ")))
	}
	return req.Reply(fmt.Sprintf("Reset complete:\n- %s", strings.Join(results, "\n- ")))
}

func doAll(t string) bool { return t == "all" }

func resetHelp() string {
	return "Usage:\n" +
		"  /reset all --yes                              — factory reset (keeps secrets & paired devices)\n" +
		"  /reset chats --yes                            — clear all chat history\n" +
		"  /reset memory --yes                           — clear MEMORY.md and daily notes\n" +
		"  /reset automations --yes                      — clear automations and cron jobs\n" +
		"  /reset context --yes                          — clear Personal Context and knowledge\n" +
		"  /reset devices --yes                          — clear paired devices\n" +
		"  /reset all --yes --include-secrets --include-devices  — full wipe including secrets\n" +
		"\nAdd --yes to confirm. Without it, Ghost shows a preview."
}

func resetPreview(target string, includeSecrets, includeDevices bool) string {
	var what []string
	switch target {
	case "all":
		what = []string{"all chats (all sessions)", "memory (MEMORY.md, daily notes)", "automations (schedules, cron)", "personal context (beliefs, knowledge profile)"}
		if includeDevices {
			what = append(what, "paired devices")
		} else {
			what = append(what, "paired devices (kept unless --include-devices)")
		}
		if includeSecrets {
			what = append(what, "secrets (config/.secrets.json)")
		} else {
			what = append(what, "secrets (kept unless --include-secrets)")
		}
	case "chats", "sessions", "messages":
		what = []string{"all chats (all sessions and messages)"}
	case "memory":
		what = []string{"memory (MEMORY.md, daily notes, memory_chunks)"}
	case "automations", "cron", "scheduled":
		what = []string{"automations (scheduled_items, cron jobs, execution history)"}
	case "context", "personal-context", "personal":
		what = []string{"personal context (entries.jsonl, knowledge profile)"}
	case "devices", "paired", "paired-devices":
		what = []string{"paired devices"}
	}
	return fmt.Sprintf("This will delete:\n- %s\n\nAdd `--yes` to confirm: `/reset %s --yes`", strings.Join(what, "\n- "), target)
}

func hasFlag(fields []string, flag string) bool {
	for _, f := range fields {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
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
	return nil
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
	// Config secrets live outside workspace: config dir
	cfgDir := os.Getenv("GHOST_CONFIG_DIR")
	if cfgDir == "" {
		// Try workspace-adjacent config
		home, _ := os.UserHomeDir()
		cfgDir = filepath.Join(home, ".config", "ghost")
		if _, err := os.Stat(cfgDir); os.IsNotExist(err) {
			cfgDir = "/var/ghost/config"
		}
	}
	for _, name := range []string{".secrets.json", ".env"} {
		p := filepath.Join(cfgDir, name)
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
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
