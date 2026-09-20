package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/auth"
	"github.com/ianclemence/ghost/pkg/bench"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/clock"
	"github.com/ianclemence/ghost/pkg/commands"
	"github.com/ianclemence/ghost/pkg/config"
	ghostdb "github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/devices"
	"github.com/ianclemence/ghost/pkg/doctor"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/heartbeat"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/maintenance"
	"github.com/ianclemence/ghost/pkg/mcp"
	"github.com/ianclemence/ghost/pkg/nontty"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/relayclient"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/schema"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/state"
	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/turnlog"
	"github.com/ianclemence/ghost/pkg/verify"
	"github.com/ianclemence/ghost/pkg/voice"
	"github.com/joho/godotenv"
	_ "modernc.org/sqlite"
)

//go:generate cp -r ../../workspace .
//go:embed workspace
var embeddedFiles embed.FS

var (
	version   = "dev"
	gitCommit string
	buildTime string
	goVersion string
)

const logo = "👻"

// formatVersion returns the version string with optional git commit
func formatVersion() string {
	v := version
	if gitCommit != "" {
		v += fmt.Sprintf(" (git: %s)", gitCommit)
	}
	return v
}

// formatBuildInfo returns build time and go version info
func formatBuildInfo() (build string, goVer string) {
	if buildTime != "" {
		build = buildTime
	}
	goVer = goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	return
}

// deprecationWarning tells an operator that a legacy command name still
// works but has a canonical replacement. It never blocks.
func deprecationWarning(oldCmd, newCmd string) {
	fmt.Fprintf(os.Stderr, "note: 'ghost %s' is deprecated; use 'ghost %s'.\n", oldCmd, newCmd)
}

// speechCmd groups local speech-to-text and speech-synthesis provisioning
// under one command. It re-slices os.Args so the existing stt/tts handlers see
// their expected argument positions (no duplication).
func speechCmd() {
	if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
		speechHelp()
		return
	}
	switch os.Args[2] {
	case "stt":
		os.Args = append([]string{os.Args[0], "stt"}, os.Args[3:]...)
		sttCmd()
	case "tts":
		os.Args = append([]string{os.Args[0], "tts"}, os.Args[3:]...)
		ttsCmd()
	case "status":
		// Combined readiness view across both engines.
		os.Args = append([]string{os.Args[0], "stt", "status"}, os.Args[3:]...)
		sttStatusCmd()
		os.Args = append([]string{os.Args[0], "tts", "status"}, os.Args[3:]...)
		ttsStatusCmd(os.Args[3:])
	default:
		fmt.Printf("Unknown speech command: %s\n", os.Args[2])
		speechHelp()
	}
}

func speechHelp() {
	fmt.Println("Usage: ghost speech <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  stt setup|status    Local speech-to-text (whisper sidecar + model)")
	fmt.Println("  tts setup|status    Local speech synthesis (engine + voice)")
	fmt.Println("  status              Show both engines' readiness")
	fmt.Println()
	fmt.Println("Legacy: 'ghost stt' and 'ghost tts' still work.")
}

// evalCmd groups model/behavior evaluation tools that previously sat as
// sibling top-level commands with no shared home.
func evalCmd() {
	if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
		evalHelp()
		return
	}
	switch os.Args[2] {
	case "golden":
		os.Args = append([]string{os.Args[0], "golden"}, os.Args[3:]...)
		goldenCmd()
	case "benchmark":
		os.Args = append([]string{os.Args[0], "benchmark"}, os.Args[3:]...)
		benchmarkCmd()
	case "replay":
		os.Args = append([]string{os.Args[0], "replay"}, os.Args[3:]...)
		replayCmd()
	default:
		fmt.Printf("Unknown eval command: %s\n", os.Args[2])
		evalHelp()
	}
}

func evalHelp() {
	fmt.Println("Usage: ghost eval <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  golden [flags]      Run the Golden Conversation Suite")
	fmt.Println("  benchmark [flags]   Run the personal AI benchmark + core score")
	fmt.Println("  replay <id> [--json] Show a recorded trajectory (execution evidence)")
}

func printVersion() {
	fmt.Printf("%s Ghost %s\n", logo, formatVersion())
	build, goVer := formatBuildInfo()
	if build != "" {
		fmt.Printf("  Build: %s\n", build)
	}
	if goVer != "" {
		fmt.Printf("  Go: %s\n", goVer)
	}
}

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func main() {
	// Try loading .env files from various locations
	// Priority: current dir > parent dir > config dir > ~/.ghost > ~/ghost
	var envLoaded bool

	if err := godotenv.Load(".env"); err == nil {
		fmt.Fprintf(os.Stderr, "✓ Loaded .env (from current dir)\n")
		envLoaded = true
	} else if err := godotenv.Load("../.env"); err == nil {
		fmt.Fprintf(os.Stderr, "✓ Loaded .env (from parent dir)\n")
		envLoaded = true
	}

	if configDir := os.Getenv("GHOST_CONFIG_DIR"); configDir != "" {
		if err := godotenv.Load(filepath.Join(configDir, ".env")); err == nil {
			fmt.Fprintf(os.Stderr, "✓ Loaded .env (from GHOST_CONFIG_DIR)\n")
			envLoaded = true
		}
	}

	if !envLoaded {
		if home, err := os.UserHomeDir(); err == nil {
			if err := godotenv.Load(filepath.Join(home, ".ghost", ".env")); err == nil {
				fmt.Fprintf(os.Stderr, "✓ Loaded .env (from ~/.ghost)\n")
			} else if err := godotenv.Load(filepath.Join(home, "ghost", ".env")); err == nil {
				fmt.Fprintf(os.Stderr, "✓ Loaded .env (from ~/ghost)\n")
			}
		}
	}

	// Map simple env vars to internal config vars
	if val := os.Getenv("TELEGRAM_TOKEN"); val != "" {
		_ = os.Setenv("GHOST_CHANNELS_TELEGRAM_TOKEN", val)
		// Auto-enable Telegram if token is present
		if os.Getenv("GHOST_CHANNELS_TELEGRAM_ENABLED") == "" {
			_ = os.Setenv("GHOST_CHANNELS_TELEGRAM_ENABLED", "true")
		}
	}
	if val := os.Getenv("TELEGRAM_USER_ID"); val != "" {
		_ = os.Setenv("GHOST_CHANNELS_TELEGRAM_ALLOW_FROM", val)
	}

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	// Root ops commands default to the installed Ghost (config +
	// workspace) instead of the current checkout, matching how the
	// systemd units run. Explicit GHOST_CONFIG_DIR /
	// GHOST_WORKSPACE_DIR always win; non-root and non-device runs
	// are unaffected.
	if isApplianceOpsCommand(command) {
		applyApplianceTarget()
	} else if isInteractiveCommand(command) {
		// Interactive commands read the SAME config the Web Console and
		// the daemon use when an installed Ghost exists and this user can
		// read it — so "configured in the console" means "configured for
		// the CLI" too. A checkout config is not silently preferred over
		// an installed one; env always wins.
		applyInstalledConfig()
	}

	// Uniform --help: `ghost <command> --help` must print usage and NEVER run
	// the command. This guards the dangerous/long-running ones (agent, serve,
	// dev, update, reset, onboard) and any command without bespoke help.
	if len(os.Args) > 2 && wantsHelp(os.Args[2:]) {
		printCommandHelp(command)
		return
	}

	switch command {
	case "onboard":
		onboard()
	case "agent":
		agentCmd()
	case "serve", "gateway":
		gatewayCmd()
	case "dev":
		devCmd()
	case "status":
		statusCmd()
	case "model":
		modelCmd()
	case "reset":
		resetCmd()
	case "reset-password":
		deprecationWarning("reset-password", "auth reset-password")
		resetPasswordCmd()
	case "auth":
		authCmd()
	case "mcp":
		mcpCmd()
	case "connector":
		connectorCmd()
	case "stt":
		deprecationWarning("stt", "speech stt")
		sttCmd()
	case "tts":
		deprecationWarning("tts", "speech tts")
		ttsCmd()
	case "speech":
		speechCmd()
	case "skills":
		if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
			skillsHelp()
			return
		}

		subcommand := os.Args[2]

		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		workspace := cfg.WorkspacePath()
		installer := skills.NewSkillInstaller(workspace)
		// 获取全局配置目录和内置 skills 目录
		globalDir := filepath.Dir(getConfigPath())
		globalSkillsDir := filepath.Join(globalDir, "skills")
		builtinSkillsDir := filepath.Join(globalDir, "ghost", "skills")
		skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

		switch subcommand {
		case "list":
			// --builtin lists bundled skills instead of installed ones.
			if hasFlag(os.Args[3:], "--builtin") {
				skillsListBuiltinCmd()
				break
			}
			skillsListCmd(skillsLoader)
		case "add":
			skillsAddCmd(workspace, os.Args[3:])
		case "install":
			deprecationWarning("skills install", "skills add")
			skillsInstallCmd(installer, workspace)
		case "update":
			skillsUpdateCmd(workspace, os.Args[3:])
		case "remove", "uninstall":
			if subcommand == "uninstall" {
				deprecationWarning("skills uninstall", "skills remove")
			}
			if len(os.Args) < 4 {
				fmt.Println("Usage: ghost skills remove <skill-name>")
				return
			}
			skillsRemoveCmd(installer, os.Args[3], workspace)
		case "install-builtin":
			deprecationWarning("skills install-builtin", "skills add --builtin")
			skillsInstallBuiltinCmd(workspace)
		case "sync":
			syncEmbeddedSkills(workspace)
			fmt.Println("\n✓ Bundled skills synced (user-modified skills were preserved).")
		case "list-builtin":
			deprecationWarning("skills list-builtin", "skills list --builtin")
			skillsListBuiltinCmd()
		case "search":
			query := ""
			if len(os.Args) > 3 {
				query = os.Args[3]
			}
			skillsSearchCmd(cfg, query)
		case "show":
			if len(os.Args) < 4 {
				fmt.Println("Usage: ghost skills show <skill-name>")
				return
			}
			skillsShowCmd(skillsLoader, os.Args[3])
		default:
			fmt.Printf("Unknown skills command: %s\n", subcommand)
			skillsHelp()
		}
	case "state":
		stateCmd()
	case "update":
		updateCmd()
	case "auto-update", "updater":
		if command == "updater" {
			deprecationWarning("updater", "auto-update")
		}
		updaterCmd()
	case "relay":
		relayCmd()
	case "verify":
		verifyCmd()
	case "benchmark":
		benchmarkCmd()
	case "golden":
		goldenCmd()
	case "replay":
		deprecationWarning("replay", "eval replay")
		replayCmd()
	case "eval":
		evalCmd()
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

// printCommandHelp prints usage for one top-level command. Group commands
// delegate to their own help; leaf commands print a one-line usage plus a
// short explanation. It is the single place `ghost <cmd> --help` resolves.
func printCommandHelp(command string) {
	switch command {
	case "agent":
		fmt.Println("Usage: ghost agent [-m <message>] [-s <session>] [--debug] [--debug-log <file>]")
		fmt.Println("Chat with Ghost in the terminal. Without -m, starts interactive mode.")
		fmt.Println("Default session is main — the same conversation the app shows.")
		fmt.Println("When the gateway daemon runs, the CLI is its client (shared turns,")
		fmt.Println("approvals, memory); otherwise it runs an offline embedded loop.")
		fmt.Println("In interactive mode logs go to --debug-log (or are hidden); stderr stays clean for the TUI.")
	case "serve", "gateway":
		fmt.Println("Usage: ghost serve [--api-only] [--debug]")
		fmt.Println("Start the Ghost daemon (API + channels + scheduler + heartbeat).")
	case "dev":
		fmt.Println("Usage: ghost dev [--port=N] [--api-only] [--use-installed-key]")
		fmt.Println("Run an isolated development instance. Never touches the installed Ghost.")
	case "onboard":
		fmt.Println("Usage: ghost onboard")
		fmt.Println("Initialize Ghost configuration and workspace.")
	case "update":
		fmt.Println("Usage: ghost update [--dry-run] [--force]")
		fmt.Println("Deploy the tagged release to the installed Ghost. Refuses a dirty checkout")
		fmt.Println("unless --force. Exits quietly when already up to date (same --force")
		fmt.Println("overrides that too). Run with sudo.")
	case "auto-update", "updater":
		fmt.Println("Usage: ghost auto-update [--interval DURATION]")
		fmt.Println("Run the auto-update daemon: periodically pull the latest release and rebuild.")
		fmt.Println("  --interval/-i DURATION   how often to check (default 6h)")
		fmt.Println("For a one-shot deploy of the current release, use 'ghost update'.")
	case "reset":
		fmt.Println("Usage: ghost reset <all|scope...> [--exclude=scope,...] [--no-restart]")
		fmt.Println("Scopes: chats memory activity automations context devices secrets model")
	case "verify":
		fmt.Println("Usage: ghost verify")
		fmt.Println("Run personal AI verification (real product checks).")
	case "model":
		fmt.Println("Usage: ghost model [list | use <provider:model|preset>]")
		fmt.Println("With no arguments, shows the active model and presets.")
		fmt.Println("  list              show the active model and available presets")
		fmt.Println("  use <target>      switch to provider:model or a configured preset name")
	case "relay":
		relayHelp()
	case "auth":
		authHelp()
	case "mcp":
		mcpHelp()
	case "state":
		stateHelp()
	case "skills":
		skillsHelp()
	case "connector":
		connectorHelp()
	case "speech":
		speechHelp()
	case "eval":
		evalHelp()
	case "stt":
		sttHelp()
	case "tts":
		ttsHelp()
	case "version":
		printVersion()
	case "help":
		printHelp()
	default:
		// No bespoke help: fall back to the grouped top-level help.
		printHelp()
	}
}

func printHelp() {
	fmt.Printf("%s Ghost - Personal AI Assistant v%s\n\n", logo, version)
	fmt.Println("Usage: ghost <command> [args]")
	fmt.Println()
	fmt.Println("Talk")
	fmt.Println("  agent       Chat with Ghost directly")
	fmt.Println()
	fmt.Println("Run")
	fmt.Println("  serve       Start the Ghost daemon (API + channels + scheduler + heartbeat)")
	fmt.Println("  dev         Run an isolated development instance (own dir/port; never touches the installed Ghost)")
	fmt.Println()
	fmt.Println("Configure")
	fmt.Println("  auth        Credentials (login, logout, status, reset-password)")
	fmt.Println("  model       View or switch the active model (model [list|use <provider:model>])")
	fmt.Println("  mcp         Manage MCP servers (list, add, edit, remove, test)")
	fmt.Println("  connector   Portable connectors (validate, init, from-openapi, list, review, install, run, sign, verify)")
	fmt.Println("  skills      Skills (list, add, remove, show, search, sync, enable, disable)")
	fmt.Println("  speech      Local speech (stt setup|status, tts setup|status)")
	fmt.Println()
	fmt.Println("Observe")
	fmt.Println("  status      Show Ghost status")
	fmt.Println("  verify      Run personal AI verification (real product checks)")
	fmt.Println()
	fmt.Println("Evaluate")
	fmt.Println("  eval        Model/behavior evaluation (golden, benchmark, replay)")
	fmt.Println()
	fmt.Println("Data")
	fmt.Println("  state       Export, import, inspect, backup, or prune Ghost State")
	fmt.Println()
	fmt.Println("Deploy")
	fmt.Println("  update      Deploy the tagged release to the installed Ghost (sudo; refuses a dirty checkout; skips when already up to date; --force, --dry-run)")
	fmt.Println("  auto-update Run the auto-update daemon")
	fmt.Println()
	fmt.Println("Recover")
	fmt.Println("  reset       Factory reset (e.g. ghost reset all --exclude=devices,secrets)")
	fmt.Println("  relay       Manage the relay connection (run, pair, clients, revoke, setup)")
	fmt.Println()
	fmt.Println("Setup")
	fmt.Println("  onboard     Initialize Ghost configuration and workspace")
	fmt.Println()
	fmt.Println("Other")
	fmt.Println("  version     Show version information")
	fmt.Println("  help        Show this help (also: ghost <command> --help)")
}

func onboard() {
	configPath := getConfigPath()
	force := false
	guided := false
	var provider, model, apiKey string
	skipChannels := false
	yes := false
	for _, a := range os.Args[2:] {
		switch {
		case a == "--force":
			force = true
		case a == "--guided":
			guided = true
		case a == "--yes" || a == "-y":
			yes = true
		case a == "--skip-channels":
			skipChannels = true
		case strings.HasPrefix(a, "--provider="):
			provider = strings.TrimPrefix(a, "--provider=")
		case strings.HasPrefix(a, "--model="):
			model = strings.TrimPrefix(a, "--model=")
		case strings.HasPrefix(a, "--api-key="):
			apiKey = strings.TrimPrefix(a, "--api-key=")
		}
	}
	if guided {
		home, _ := os.UserHomeDir()
		if err := runGuided(guidedOpts{
			ConfigPath: configPath, Home: home,
			Provider: provider, Model: model, APIKey: apiKey,
			SkipChannel: skipChannels, Yes: yes, Force: force,
		}); err != nil {
			fmt.Printf("Onboarding failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if _, err := os.Stat(configPath); err == nil && !force {
		fmt.Printf("Config already exists at %s\n", configPath)
		if err := nontty.RequireInteractive("overwriting the config", "--force"); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Print("Overwrite? (y/n): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" {
			fmt.Println("Aborted.")
			return
		}
	}

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()
	createWorkspaceTemplates(workspace)
	if _, err := ghoststate.EnsureIdentity(workspace); err != nil {
		fmt.Printf("Error creating Ghost identity: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s ghost is ready!\n", logo)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Add your API key to", configPath)
	fmt.Println("     Get one at: https://openrouter.ai/keys")
	fmt.Println("  2. Chat: ghost agent -m \"Hello!\"")
}

func copyEmbeddedToTarget(targetDir string) error {
	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("Failed to create target directory: %w", err)
	}

	// Walk through all files in embed.FS
	err := fs.WalkDir(embeddedFiles, "workspace", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Bundled skills are managed by the manifest-aware sync, never by a
		// blind overwrite here (that would stomp user edits).
		if strings.HasPrefix(path, "workspace/skills/") {
			return nil
		}
		if d.Name() == skills.BundledManifestFile {
			return nil
		}

		// Read embedded file
		data, err := embeddedFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to read embedded file %s: %w", path, err)
		}

		new_path, err := filepath.Rel("workspace", path)
		if err != nil {
			return fmt.Errorf("Failed to get relative path for %s: %v\n", path, err)
		}

		// Build target file path
		targetPath := filepath.Join(targetDir, new_path)

		// Ensure target file's directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", filepath.Dir(targetPath), err)
		}

		// Write file
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("Failed to write file %s: %w", targetPath, err)
		}

		return nil
	})

	return err
}

func createWorkspaceTemplates(workspace string) {
	err := copyEmbeddedToTarget(workspace)
	if err != nil {
		fmt.Printf("Error copying workspace templates: %v\n", err)
	}
	// USER.md is per-installation state (not tracked in git), so a fresh
	// clone has no template on disk to copy — seed it when missing.
	if err := commands.EnsureUserDoc(workspace); err != nil {
		fmt.Printf("Error seeding USER.md template: %v\n", err)
	}
	syncEmbeddedSkills(workspace)
}

// syncEmbeddedSkills seeds or refreshes bundled skills from the embedded
// workspace into the runtime workspace, manifest-aware: new bundled skills are
// copied in, unchanged ones receive upstream updates, and skills the user has
// edited are never overwritten.
func syncEmbeddedSkills(workspace string) {
	sub, err := fs.Sub(embeddedFiles, "workspace/skills")
	if err != nil {
		fmt.Printf("Error loading bundled skills: %v\n", err)
		return
	}
	report, err := skills.SyncBundledFromFS(sub, filepath.Join(workspace, "skills"))
	if err != nil {
		fmt.Printf("Error syncing bundled skills: %v\n", err)
		return
	}
	if len(report.Seeded) > 0 {
		fmt.Printf("  • Seeded bundled skills: %s\n", strings.Join(report.Seeded, ", "))
	}
	if len(report.Updated) > 0 {
		fmt.Printf("  • Updated bundled skills: %s\n", strings.Join(report.Updated, ", "))
	}
	if len(report.UserModified) > 0 {
		fmt.Printf("  • Preserved user-modified skills: %s\n", strings.Join(report.UserModified, ", "))
	}
}

// knownProviders is the set of provider names Ghost can route to, including
// common aliases. Used to reject a misspelled provider instead of silently
// writing a broken config.
var knownProviders = map[string]bool{
	"moonshot": true, "kimi": true,
	"groq":   true,
	"openai": true, "gpt": true,
	"anthropic": true, "claude": true, "claude-cli": true, "claudecode": true, "claude-code": true,
	"openrouter": true,
	"zhipu":      true, "glm": true, "zai": true,
	"gemini": true, "google": true,
	"vllm":           true,
	"shengsuanyun":   true,
	"deepseek":       true,
	"qwen":           true,
	"github_copilot": true, "copilot": true,
	"ollama": true,
	"nvidia": true,
}

func knownProviderNames() []string {
	var out []string
	for k := range knownProviders {
		if k == "kimi" || k == "gpt" || k == "claude" || k == "glm" || k == "zai" || k == "google" || k == "claudecode" || k == "claude-code" || k == "copilot" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// modelCmd lets a user view or switch the active model from the CLI, mirroring
// how coding-agent CLIs expose model selection. It reads/writes the same
// config (honoring GHOST_CONFIG_DIR) that the gateway and agent use, so the
// default model stays constant throughout.
func modelCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	configPath := getConfigPath()

	// Mirror the gateway's /v1/model shape: active is the model name and the
	// provider is reported separately.
	cleanModel := func(p, m string) string {
		if p != "" {
			if strings.HasPrefix(m, p+"/") || strings.HasPrefix(m, p+":") {
				return m[len(p)+1:]
			}
		}
		return m
	}

	presets := func() []string {
		var out []string
		for _, p := range cfg.Agents.ModelList {
			if p.Name == "" {
				continue
			}
			line := fmt.Sprintf("  %-16s %s (%s)", p.Name, p.Provider, cleanModel(p.Provider, p.Model))
			if ok, reason := providers.PresetAvailable(cfg, p.Provider, p.Model); !ok {
				line += fmt.Sprintf("  [unavailable: %s]", reason)
			}
			out = append(out, line)
		}
		return out
	}
	connections := func() []string {
		var out []string
		for _, cn := range cfg.Agents.Connections {
			if cn.Name == "" {
				continue
			}
			line := fmt.Sprintf("  %-16s %s (%s)", cn.Name, cn.Provider, cn.Model)
			if _, err := cfg.ResolveConnection(cn.Name); err != nil {
				line += fmt.Sprintf("  [unavailable: %s]", err)
			}
			out = append(out, line)
		}
		return out
	}

	args := os.Args[2:]
	if len(args) == 0 || args[0] == "list" {
		fmt.Printf("Active: %s\n", cleanModel(cfg.Agents.Defaults.Provider, cfg.Agents.Defaults.Model))
		if cfg.Agents.Defaults.Provider != "" {
			fmt.Printf("Provider: %s\n", cfg.Agents.Defaults.Provider)
		}
		fmt.Println("Config:", configPath)
		if ps := presets(); len(ps) > 0 {
			fmt.Println("\nPresets:")
			for _, s := range ps {
				fmt.Println(s)
			}
		}
		if cs := connections(); len(cs) > 0 {
			fmt.Println("\nConnections:")
			for _, s := range cs {
				fmt.Println(s)
			}
		}
		// Configured providers without a preset (e.g. a deepseek key but
		// no deepseek preset): selectable via `ghost model use
		// <provider:model>`, and listed in the terminal picker.
		var prov []string
		for _, o := range providers.AvailableModelOptions(cfg) {
			if o.Kind != "provider" {
				continue
			}
			prov = append(prov, fmt.Sprintf("  %-16s %s (%s)", o.Name, o.Provider, o.Model))
		}
		if len(prov) > 0 {
			fmt.Println("\nProviders (configured keys, no preset needed):")
			for _, s := range prov {
				fmt.Println(s)
			}
		}
		return
	}

	if args[0] != "use" && args[0] != "set" {
		fmt.Println("Usage: ghost model [list|use <provider:model|preset>]")
		return
	}

	if len(args) < 2 {
		fmt.Println("Usage: ghost model use <provider:model|preset>")
		return
	}
	target := args[1]

	provider, model := "", target
	fromConnection := false
	if preset := cfg.FindModelPreset(target); preset != nil {
		provider = preset.Provider
		model = preset.Model
	} else if conn, cerr := cfg.ResolveConnection(target); cerr == nil {
		// Named connection: resolution (endpoint + credential) is the
		// validation. The credential stays in the environment — activation
		// writes provider/model only, never secrets.
		provider, model = conn.Provider, conn.Model
		fromConnection = true
		if conn.AuthEnv != "" {
			fmt.Printf("Connection %q authenticates via %s (must be set at runtime).\n", conn.Name, conn.AuthEnv)
		}
	} else if strings.Contains(target, ":") {
		parts := strings.SplitN(target, ":", 2)
		if parts[0] == "" || parts[1] == "" {
			fmt.Println("Invalid format — use provider:model (e.g. openai:gpt-4o).")
			os.Exit(1)
		}
		provider, model = parts[0], parts[1]
	}

	// Guard against typos: an unknown provider would silently produce a broken
	// config, so reject it up front. Connections skip this guard —
	// ResolveConnection already validated the endpoint and credential.
	if provider != "" && !fromConnection && !knownProviders[strings.ToLower(provider)] {
		fmt.Printf("Unknown provider %q — expected one of: %s\n", provider, strings.Join(knownProviderNames(), ", "))
		os.Exit(1)
	}

	canonical := model
	if provider != "" {
		canonical = provider + ":" + model
	}
	// Validate the model resolves to a usable provider before committing.
	if _, err := providers.CreateProviderForModel(cfg, canonical); err != nil {
		fmt.Printf("Could not switch model: %s\n", err)
		os.Exit(1)
	}

	cfg.SetActiveModel(provider, model)
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Could not save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Active model set to %s\n", canonical)
}

func agentCmd() {
	// Self-heal: a brand-new workspace has no skills yet, which would make
	// every capability report "not installed". Seed bundled skills
	// (manifest-aware; never overwrites user edits) so a first-run user who
	// simply starts chatting gets working capabilities.
	message := ""
	sessionKey := MainSessionID
	debugLog := ""
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		case "--debug-log":
			if i+1 < len(args) {
				debugLog = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-s", "--session":
			if i+1 < len(args) {
				sessionKey = args[i+1]
				i++
			}
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Option 2 ("one conversation"): when the gateway daemon is reachable,
	// the CLI becomes its thin client — same session store, same broker,
	// same turn lifecycle as the app, live over SSE. Otherwise it falls
	// back to the embedded loop (today's offline-capable behavior).
	if base := gatewayBaseURL(cfg); gatewayReachable(base) {
		fmt.Fprintf(os.Stderr, "✓ Connected to Ghost gateway (%s) — one shared conversation with the app\n", base)
		agentGatewayCmd(&gatewayRuntime{baseURL: base, http: &http.Client{}}, message, sessionKey)
		return
	}
	fmt.Fprintf(os.Stderr, "○ Gateway unreachable — local mode (this conversation stays on this machine until the daemon runs)\n")

	// Seed bundled skills for a fresh workspace (idempotent, manifest-aware).
	syncEmbeddedSkills(cfg.WorkspacePath())

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		printProviderSetupHelp(cfg, err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop, err := agent.NewAgentLoop(cfg, msgBus, provider)
	if err != nil {
		fmt.Printf("Error initializing Ghost: %v\n", err)
		os.Exit(1)
	}
	agentLoop.SetConfigPath(getConfigPath())

	// Print agent startup info (only for interactive mode)
	startupInfo := agentLoop.GetStartupInfo()
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      startupInfo["tools"].(map[string]interface{})["count"],
			"skills_total":     startupInfo["skills"].(map[string]interface{})["total"],
			"skills_available": startupInfo["skills"].(map[string]interface{})["available"],
		})

	if message != "" {
		ctx := context.Background()
		// A one-shot CLI reset must run while the daemon is stopped, or the
		// running gateway re-persists its in-memory state and undoes it.
		isReset := strings.HasPrefix(strings.TrimSpace(message), "/reset")
		wasRunning := false
		stopped := false
		if isReset {
			wasRunning = ghostServiceRunning()
			stopped = stopGhostDaemon()
		}
		response, err := agentLoop.ProcessDirect(ctx, message, sessionKey)
		if isReset && wasRunning {
			// Stopped above: start it. Could not stop (no privilege):
			// restart anyway so the daemon reloads from disk.
			if stopped {
				startGhostDaemon(true)
			} else if rerr := restartGhostDaemon(); rerr != nil {
				fmt.Printf("Reset applied, but restart failed: %v\nRun: sudo systemctl restart ghost\n", rerr)
			} else {
				fmt.Println("Ghost service restarted — reset applied.")
			}
		}
		if err != nil {
			fmt.Printf("Error: %s\n", friendlyAgentError(err))
			os.Exit(1)
		}
		fmt.Printf("\n%s %s\n", logo, response)
	} else {
		// robust-tty-detection: never hang on a swallowed prompt in pipes
		// or CI — fail early and name the non-interactive flag.
		if err := nontty.RequireInteractive("interactive chat", "`ghost agent -m \"...\"`"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		interactiveMode(embeddedRuntime{agentLoop}, sessionKey, debugLog, nil)
	}
}

// agentGatewayCmd runs `ghost agent` against the live daemon: one-shot
// turns print and exit; interactive mode backfills the shared transcript
// first so the terminal opens on the same conversation the app shows.
// The reset stop/start dance is embedded-mode only — remotely the daemon
// IS the executor, and stopping it would kill the server mid-turn.
func agentGatewayCmd(gw *gatewayRuntime, message, sessionKey string) {
	if message != "" {
		resp, err := gw.ProcessDirectWithChannel(context.Background(), message, sessionKey, "cli", "direct", nil, nil, nil)
		if err != nil {
			fmt.Printf("Error: %s\n", friendlyAgentError(err))
			os.Exit(1)
		}
		fmt.Printf("\n%s %s\n", logo, resp)
		return
	}
	if err := nontty.RequireInteractive("interactive chat", "`ghost agent -m \"...\"`"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var preload []entry
	if hist, err := gw.LoadConversationHistory(sessionKey, conversationBackfillCap); err == nil {
		for _, h := range hist {
			at := time.Unix(h.Timestamp, 0)
			if h.Timestamp <= 0 {
				at = time.Time{}
			}
			switch h.Role {
			case "user":
				preload = append(preload, entry{kind: entryUser, text: h.Content, at: at})
			case "assistant":
				preload = append(preload, entry{kind: entryAssistant, text: h.Content, at: at})
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "note: couldn't load shared history (%v); starting fresh\n", err)
	}
	interactiveMode(gw, sessionKey, "", preload)
}

// silenceStderr parks the process stderr on devnull and returns a
// restore function. The TUI calls it around program.Run so a stray write
// from any dependency (std log, subprocess chatter) can never corrupt the
// alt-screen frame. Stdout stays untouched — tea renders there.
func silenceStderr() func() {
	log.SetOutput(io.Discard)
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return func() { log.SetOutput(os.Stderr) }
	}
	orig := os.Stderr
	os.Stderr = devnull
	return func() {
		os.Stderr = orig
		log.SetOutput(os.Stderr)
		_ = devnull.Close()
	}
}

func interactiveMode(runtime agentRuntime, sessionKey, debugLog string, preload []entry) {
	// P0 correctness: the TUI owns the screen. Route logs to a file (or
	// drop the stderr line entirely) so INFO lines can never paint over
	// the alt-screen transcript and input box.
	if debugLog != "" {
		if err := logger.EnableFileLogging(debugLog); err != nil {
			fmt.Fprintf(os.Stderr, "note: could not open --debug-log %s: %v\n", debugLog, err)
		}
	}
	logger.SetSilent(true)
	m := newAgentTUI(runtime, sessionKey)
	for _, e := range preload {
		m.append(e)
	}
	// Own the whole frame (terminal-ui skill: render-single-write): the
	// tea renderer writes stdout, and stderr is parked on devnull so no
	// dependency's stray log line can ever paint over the alt-screen.
	// Restored before any post-TUI output below.
	restoreStderr := silenceStderr()
	agentProgram = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	defer func() { agentProgram = nil }()
	_, runErr := agentProgram.Run()
	restoreStderr()
	if runErr != nil {
		// A TUI failure must not strand the user: fall back to the simple
		// line mode with an honest note (stdin is a TTY here — checked
		// above — so this cannot hang).
		fmt.Printf("Interactive UI unavailable (%v); using simple mode.\n", runErr)
		simpleInteractiveMode(runtime, sessionKey)
		return
	}
	// ux-intro-outro: close the session the way the welcome card opened
	// it — one line on what the conversation produced.
	fmt.Printf("\n%s Ghost session ended · %d turn%s · %s\n", logo, m.turnCount, plural(m.turnCount), runtime.GetCurrentModel())
}

func simpleInteractiveMode(runtime agentRuntime, sessionKey string) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(fmt.Sprintf("%s You: ", logo))
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		ctx := context.Background()
		response, err := runtime.ProcessDirectWithChannel(ctx, input, sessionKey, "cli", "direct", nil, nil, nil)
		if err != nil {
			fmt.Printf("Error: %s\n", friendlyAgentError(err))
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

// devCmd launches an isolated development instance from the current checkout.
// It never touches the installed Ghost: its own GHOST_DIR, config, workspace,
// data, and port. Use it to prototype and test; promote to production only via
// a tagged release and `ghost update`.
//
// Flags:
//
//	--port=N     bind the gateway/internal API to this port (default 8877)
//	--api-only   same as `serve --api-only` in the dev instance
func devCmd() {
	port := 0
	apiOnly := false
	useInstalledKey := false
	for _, a := range os.Args[2:] {
		switch {
		case strings.HasPrefix(a, "--port="):
			fmt.Sscanf(strings.TrimPrefix(a, "--port="), "%d", &port)
		case a == "--api-only":
			apiOnly = true
		case a == "--use-installed-key":
			useInstalledKey = true
		}
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	target := appliance.ResolveDevTarget(os.Getenv, home, port)

	// Safety: refuse to run a dev instance against the production paths.
	if target.ConflictsWithProduction() {
		fmt.Fprintf(os.Stderr, "✗ Refusing to run: the dev target overlaps the installed Ghost.\n")
		fmt.Fprintf(os.Stderr, "  dev target: %s\n", target.Describe())
		fmt.Fprintf(os.Stderr, "  production: %s\n", appliance.DefaultGhostDir)
		fmt.Fprintf(os.Stderr, "  Set GHOST_DEV_DIR to a directory outside the install paths.\n")
		os.Exit(1)
	}

	// Materialize the isolated layout (config + workspace + data).
	for _, dir := range []string{target.ConfigDir, target.Workspace, target.DataDir, filepath.Join(target.Workspace, "memory")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "✗ Could not create %s: %v\n", dir, err)
			os.Exit(1)
		}
	}
	// First run in a fresh dev root: seed a setup-complete flag so the
	// instance starts configured (it is a scratch environment, not a
	// production onboarding), unless the developer already set one up.
	flag := filepath.Join(target.GhostDir, appliance.SetupCompleteFlag)
	if _, err := os.Stat(flag); err != nil {
		if werr := os.WriteFile(flag, []byte("dev\n"), 0o644); werr == nil {
			fmt.Printf("• Seeded dev setup flag at %s\n", flag)
		}
	}

	// Point this process at the isolated environment before serve resolves
	// anything.
	_ = os.Setenv("GHOST_DIR", target.GhostDir)
	_ = os.Setenv("GHOST_CONFIG_DIR", target.ConfigDir)
	_ = os.Setenv("GHOST_WORKSPACE_DIR", target.Workspace)
	_ = os.Setenv("GHOST_API_PORT", fmt.Sprintf("%d", target.Port))
	_ = os.Setenv("GHOST_GATEWAY_PORT", fmt.Sprintf("%d", target.Port))

	fmt.Printf("\n🧪 Ghost dev instance\n")
	fmt.Printf("   dir:       %s\n", target.GhostDir)
	fmt.Printf("   config:    %s\n", target.ConfigDir)
	fmt.Printf("   workspace: %s\n", target.Workspace)
	fmt.Printf("   port:      %d\n", target.Port)
	fmt.Printf("   production (%s) is NOT touched.\n\n", appliance.DefaultGhostDir)

	// Optional convenience: reuse the installed Ghost's provider config so a
	// dev instance can reach the same models without re-entering keys. Copied
	// ONLY when the dev config has no key yet, so it never clobbers dev work.
	if useInstalledKey {
		seedDevConfigFromInstalled(target)
	}

	// Reuse the normal serve path with --api-only when requested.
	if apiOnly {
		os.Args = append(os.Args[:2], "--api-only")
	}
	gatewayCmd()
}

// seedDevConfigFromInstalled copies the installed Ghost's provider config
// (including its secrets) into a dev instance's config dir, but ONLY when the
// dev config is missing provider configuration. This lets a developer reuse
// production keys in an isolated instance without re-entering them, and it
// never overwrites dev-local configuration. Failure is non-fatal.
func seedDevConfigFromInstalled(target appliance.DevTarget) {
	src := filepath.Join(appliance.DefaultConfigDir, "config.json")
	dst := filepath.Join(target.ConfigDir, "config.json")
	if _, err := os.Stat(src); err != nil {
		return // no installed config to seed from
	}
	// Only seed when the dev config does not already exist.
	if _, err := os.Stat(dst); err == nil {
		fmt.Println("• Dev config already exists; not overwriting it.")
		return
	}
	for _, name := range []string{"config.json", ".secrets.json", ".master-key", ".master-env"} {
		s := filepath.Join(appliance.DefaultConfigDir, name)
		d := filepath.Join(target.ConfigDir, name)
		data, rerr := os.ReadFile(s)
		if rerr != nil {
			continue
		}
		if werr := os.WriteFile(d, data, 0o600); werr != nil {
			fmt.Printf("• Could not copy %s into dev: %v\n", name, werr)
			return
		}
	}
	fmt.Println("• Seeded dev provider keys from the installed Ghost (reusable locally).")
}

func gatewayCmd() {
	// Check for recovery mode
	if os.Getenv("GHOST_RECOVERY_MODE") == "1" {
		// Check if recovery is disabled via flag file
		ghostDir := os.Getenv("GHOST_DIR")
		if ghostDir == "" {
			ghostDir = "/var/ghost"
		}
		disablePath := filepath.Join(ghostDir, "data", ".recovery-disabled")
		if _, err := os.Stat(disablePath); err == nil {
			fmt.Println("Recovery mode is disabled. Remove data/.recovery-disabled to re-enable.")
			os.Exit(0)
		}
		fmt.Println("🔧 Recovery mode active")
		recovery := appliance.NewRecoveryServer()
		if err := recovery.Start(); err != nil {
			fmt.Printf("Recovery server failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Check if setup is needed
	fb := appliance.NewSetupState()
	if fb.NeedsSetup() {
		fmt.Println("👋 Setup needed. Starting setup wizard...")
		// The web console should be running separately
		// If we get here without setup, show error and exit
		fmt.Println("Please run ghost-web to complete setup, or set GHOST_RECOVERY_MODE=1 for recovery.")
		os.Exit(1)
	}

	apiOnly := false

	// Check for flags
	args := os.Args[2:]
	for _, arg := range args {
		if arg == "--debug" || arg == "-d" {
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		}
		if arg == "--no-cron" {
			// Retained for CLI compatibility; the authoritative scheduler
			// is always on, so this flag is now a no-op.
			fmt.Println("🕒 --no-cron is deprecated (single scheduler always runs)")
		}
		if arg == "--api-only" {
			apiOnly = true
			fmt.Println("🔌 API Only mode enabled (Channels, Heartbeat disabled)")
		}
	}

	// Self-heal the workspace layout. An interrupted or skipped update can
	// leave the runtime workspace inside the install tree; move it to the
	// runtime location before anything (config, .env, provider, DB) is read so
	// this process uses the migrated paths throughout. Idempotent and safe.
	ghostDir := os.Getenv("GHOST_DIR")
	if ghostDir == "" {
		ghostDir = appliance.DefaultGhostDir
	}
	if newWorkspace, err := appliance.MigrateWorkspaceIfNeeded(ghostDir); err != nil {
		fmt.Printf("⚠️  Workspace migration failed: %v\n", err)
	} else if newWorkspace != "" {
		fmt.Printf("✅ Workspace migrated to %s\n", newWorkspace)
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Self-heal: if config.json leaked secrets, move them to .secrets.json
	// and rewrite config.json clean.
	configPath := getConfigPath()
	healSecretsBoundary(configPath, cfg)

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		printProviderSetupHelp(cfg, err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop, err := agent.NewAgentLoop(cfg, msgBus, provider)
	if err != nil {
		fmt.Printf("Error initializing Ghost: %v\n", err)
		os.Exit(1)
	}
	agentLoop.SetConfigPath(getConfigPath())

	// One authoritative Permission Broker for the whole runtime. Every
	// subsystem (HTTP API, scheduler, MCP, browser/computer, routines,
	// subagents) shares this instance rather than opening its own.
	apiWorkspaceDir = cfg.WorkspacePath()
	authBroker, err := InitAuthoritativeBroker(agentLoop.DB())
	if err != nil {
		fmt.Printf("❌ Could not open the permission broker: %v\n", err)
		os.Exit(1)
	}

	// Boot-time lifecycle recovery. After a restart no pre-restart task is
	// alive, so any surviving computer lease is stale. Expiring it here
	// prevents a crashed task from blocking new work until a TTL lapses.
	// (Expired permission requests are swept once the state store is ready,
	// inside startInternalAPI.)
	if n, err := agentLoop.RecoverStaleLeases(); err == nil && n > 0 {
		logger.InfoCF("lifecycle", "expired stale computer leases", map[string]interface{}{"count": n})
	}

	// Start the conservative, local background memory consolidation (learn/
	// reinforce/clean). Disposable and non-blocking. NOTE: this must stay
	// AFTER the scheduler wiring below — consolidation consults the live
	// scheduled items (echo detection, summary grounding), and a worker
	// started here would run its first pass with a nil scheduler.
	// Started after setupScheduledService; see below.

	// Sync bundled skills before starting so devices pick up new bundled
	// skills after updates without ever stomping user edits.
	syncEmbeddedSkills(cfg.WorkspacePath())

	// Mint or load the persistent, hardware-independent Ghost identity. This
	// is idempotent: the ghost_id is created once and then preserved for the
	// life of the Ghost, surviving upgrades and migrations.
	if _, err := ghoststate.EnsureIdentity(cfg.WorkspacePath()); err != nil {
		fmt.Printf("⚠️  Could not ensure Ghost identity: %v\n", err)
	}

	// Bring the database to the current schema before any subsystem touches
	// it. A failed migration stops startup loudly: running services against
	// a partially migrated schema would corrupt user state silently.
	if v, err := schema.MigrateToCurrent(agentLoop.DB()); err != nil {
		err = ghostdb.TranslateError("database migration", err)
		fmt.Printf("❌ Database migration failed: %v\n", err)
		fmt.Println("   Ghost cannot start safely. Restore from a backup with `ghost state import` and try again.")
		os.Exit(1)
	} else {
		fmt.Printf("  • Database schema v%d\n", v)
	}

	// Integrity gate: a migrated-but-corrupt database must not serve.
	// Refusing boot with the restore runbook beats serving wrong memories.
	if err := ghostdb.CheckIntegrity(agentLoop.DB()); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	} else {
		fmt.Printf("  • Database integrity ok\n")
	}

	// Print agent startup info
	fmt.Println("\n📦 Agent Status:")
	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n",
		skillsInfo["available"],
		skillsInfo["total"])

	// Log to file as well
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      toolsInfo["count"],
			"skills_total":     skillsInfo["total"],
			"skills_available": skillsInfo["available"],
		})

	// Wall-clock gate shared by both schedulers: an obviously wrong clock
	// (Pi without RTC after power loss) holds automation until time is
	// sane. Conversational and local functions are unaffected.
	clockGate := clock.NewGate(30 * time.Second)

	// Setup scheduled service (the single authoritative scheduler)
	scheduledService := setupScheduledService(agentLoop, msgBus, cfg.WorkspacePath(), authBroker)
	if scheduledService != nil {
		scheduledService.ClockGate = clockGate.Safe
		// One-time, idempotent migration of the legacy JSON cron scheduler
		// into the authoritative SQLite scheduler. Safe to run every boot.
		migrateLegacyCron(cfg.WorkspacePath(), scheduledService)
		// /loop and /remind schedule through the authoritative scheduler.
		agentLoop.SetScheduler(scheduledService)
	}

	// Retention: keep the personal AI responsible on small disks (SD card).
	// Runs once now, then daily. Conservative oldest-first cleanup only.
	go runMaintenanceLoop(agentLoop.DB(), cfg.WorkspacePath())

	// Setup schedule tool (natural-language scheduling)
	// Derive timezone from personal context location or system TZ so the
	// persisted schedule timezone and the LLM's "your local time" claim agree.
	if scheduledService != nil {
		tz := deriveScheduleTimezone(cfg.WorkspacePath())
		scheduleTool := tools.NewScheduleTool(scheduledService, tz)
		agentLoop.RegisterTool(scheduleTool)
	}

	// Memory consolidation starts here, after ALL runtime wiring above:
	// its first pass needs the scheduler (echo detection), the routine
	// signals, and the registered tools. Starting it earlier runs
	// consolidation against a half-wired loop.
	agentLoop.StartLearningWorker(context.Background())

	heartbeatService := heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	heartbeatService.SetScheduler(scheduledService)
	heartbeatService.SetBus(msgBus)
	heartbeatService.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		// Quiet hours (PROACTIVE_PREFERENCES.md, user timezone): skip the
		// model turn, but still poll signals so urgent items break through
		// via the quiet-aware deliverNotice. Evening reflection is a
		// silent write and runs even when quiet.
		now := time.Now()
		agentLoop.WriteEveningReflection(now)
		if agentLoop.ProactiveQuiet(now) {
			agentLoop.PollProactive()
			return tools.SilentResult("Heartbeat quiet hours")
		}
		// Proactive signals first: gated, reason-carrying notices go out
		// directly (no LLM round-trip); the heartbeat chatter below is
		// independent. PollProactive is idempotent — repeats are no-ops.
		agentLoop.PollProactive()
		// Use cli:direct as fallback if no valid channel
		if channel == "" || chatID == "" {
			channel, chatID = "cli", "direct"
		}
		// Use ProcessHeartbeat - no session history, each heartbeat is independent
		response, err := agentLoop.ProcessHeartbeat(context.Background(), prompt, channel, chatID)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("Heartbeat error: %v", err))
		}
		if response == "HEARTBEAT_OK" {
			return tools.SilentResult("Heartbeat OK")
		}
		// For heartbeat, always return silent - the subagent result will be
		// sent to user via processSystemMessage when the async task completes
		return tools.SilentResult(response)
	})

	// Pass agentLoop as ActiveSessionProvider to channelManager
	channelManager, err := channels.NewManager(cfg, msgBus, agentLoop)
	if err != nil {
		fmt.Printf("Error creating channel manager: %v\n", err)
		os.Exit(1)
	}
	channelManager.SetCommandDefinitions(agentLoop.CommandDefinitions())

	var transcriber voice.Transcriber
	if !apiOnly {
		transcriber = voice.SelectTranscriber(voice.SelectConfig{
			Engine:      cfg.STT.Engine,
			LocalURL:    voice.LocalBaseURL(cfg.STT.Port),
			MoonshotKey: cfg.Providers.Moonshot.APIKey,
			GroqKey:     cfg.Providers.Groq.APIKey,
		})
		if transcriber != nil {
			logger.InfoC("voice", "Voice transcription enabled ("+voice.DescribeTranscriber(transcriber)+")")
		}

		if transcriber != nil {
			if telegramChannel, ok := channelManager.GetChannel("telegram"); ok {
				if tc, ok := telegramChannel.(*channels.TelegramChannel); ok {
					tc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to Telegram channel")
				}
			}
			if discordChannel, ok := channelManager.GetChannel("discord"); ok {
				if dc, ok := discordChannel.(*channels.DiscordChannel); ok {
					dc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to Discord channel")
				}
			}
			if slackChannel, ok := channelManager.GetChannel("slack"); ok {
				if sc, ok := slackChannel.(*channels.SlackChannel); ok {
					sc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to Slack channel")
				}
			}
			if whatsappChannel, ok := channelManager.GetChannel("whatsapp"); ok {
				if wc, ok := whatsappChannel.(*channels.WhatsAppChannel); ok {
					wc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to WhatsApp channel")
				}
			}
			if lineChannel, ok := channelManager.GetChannel("line"); ok {
				if lc, ok := lineChannel.(*channels.LINEChannel); ok {
					lc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to LINE channel")
				}
			}
			if smsChannel, ok := channelManager.GetChannel("sms"); ok {
				if sc, ok := smsChannel.(*channels.SMSChannel); ok {
					sc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to SMS channel")
				}
			}
			if wechatChannel, ok := channelManager.GetChannel("wechat"); ok {
				if wc, ok := wechatChannel.(*channels.WeChatChannel); ok {
					wc.SetTranscriber(transcriber)
					logger.InfoC("voice", "Voice transcription attached to WeChat channel")
				}
			}
		}
	}

	enabledChannels := channelManager.GetEnabledChannels()
	if !apiOnly && len(enabledChannels) > 0 {
		fmt.Printf("✓ Channels enabled: %s\n", enabledChannels)
	} else if !apiOnly {
		fmt.Println("⚠ Warning: No channels enabled")
	}

	// Determine actual API port (respecting env as startInternalAPI does)
	apiPort := cfg.Gateway.Port
	if p := os.Getenv("GHOST_API_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &apiPort)
	}
	fmt.Printf("✓ Gateway started on %s:%d\n", cfg.Gateway.Host, apiPort)
	fmt.Println("Press Ctrl+C to stop")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if scheduledService != nil {
		if err := scheduledService.Start(); err != nil {
			fmt.Printf("Error starting scheduled service: %v\n", err)
		} else {
			fmt.Println("✓ Scheduled service started")
		}
	}

	if !apiOnly {
		if err := heartbeatService.Start(); err != nil {
			fmt.Printf("Error starting heartbeat service: %v\n", err)
		}
		fmt.Println("✓ Heartbeat service started")
	}

	stateManager := state.NewManager(cfg.WorkspacePath())
	deviceService := devices.NewService(devices.Config{
		Enabled:    cfg.Devices.Enabled,
		MonitorUSB: cfg.Devices.MonitorUSB,
	}, stateManager)
	deviceService.SetBus(msgBus)
	if err := deviceService.Start(ctx); err != nil {
		fmt.Printf("Error starting device service: %v\n", err)
	} else if cfg.Devices.Enabled {
		fmt.Println("✓ Device event service started")
	}

	if !apiOnly {
		if err := channelManager.StartAll(ctx); err != nil {
			fmt.Printf("Error starting channels: %v\n", err)
		}
	}

	go agentLoop.Run(ctx)
	go startInternalAPI(agentLoop, scheduledService, channelManager)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	fmt.Println("\nShutting down...")
	cancel()
	deviceService.Stop()
	if !apiOnly {
		heartbeatService.Stop()
	}
	if scheduledService != nil {
		scheduledService.Stop()
	}
	agentLoop.Stop()
	if !apiOnly {
		channelManager.StopAll(ctx)
	}
	fmt.Println("✓ Gateway stopped")
}

func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	asJSON := false
	for _, a := range os.Args[2:] {
		if a == "--json" {
			asJSON = true
		}
	}

	configPath := getConfigPath()

	// One health surface: the doctor rollup is the status. The static
	// config dump below stays for detail; the verdict up top answers
	// "is Ghost healthy" in one screen.
	results := runStatusDoctor(cfg)
	if asJSON {
		raw, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("%s Ghost Status\n", logo)
	fmt.Printf("Version: %s\n", formatVersion())
	build, _ := formatBuildInfo()
	if build != "" {
		fmt.Printf("Build: %s\n", build)
	}
	fmt.Println()
	printDoctorRollup(results)
	fmt.Println()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}
	// Honesty note: if this config is a checkout config shadowing an installed
	// Ghost, say so here too (not only on provider failure).
	if shadow, ok := appliance.DetectConfigShadow(configPath, appliance.ApplianceInstalled(), func(p string) string {
		c, cerr := config.LoadConfig(p)
		if cerr != nil || c == nil {
			return ""
		}
		return providerKeyFor(c, cfg.Agents.Defaults.Provider)
	}); ok {
		if shadow.InstalledHasSecret {
			fmt.Printf("  note: the installed Ghost at %s has a key for %s. Use it with:\n    GHOST_CONFIG_DIR=%s ghost <command>\n", shadow.InstalledPath, cfg.Agents.Defaults.Provider, filepath.Dir(shadow.InstalledPath))
		} else {
			fmt.Printf("  note: an installed Ghost config exists at %s\n", shadow.InstalledPath)
		}
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)

		printStatusSpend(cfg)

		hasOpenRouter := cfg.Providers.OpenRouter.APIKey != ""
		hasAnthropic := cfg.Providers.Anthropic.APIKey != ""
		hasOpenAI := cfg.Providers.OpenAI.APIKey != ""
		hasGemini := cfg.Providers.Gemini.APIKey != ""
		hasZhipu := cfg.Providers.Zhipu.APIKey != ""
		hasGroq := cfg.Providers.Groq.APIKey != ""

		status := func(enabled bool) string {
			if enabled {
				return "✓"
			}
			return "not set"
		}
		fmt.Println("OpenRouter API:", status(hasOpenRouter))
		fmt.Println("Anthropic API:", status(hasAnthropic))
		fmt.Println("OpenAI API:", status(hasOpenAI))
		fmt.Println("Gemini API:", status(hasGemini))
		fmt.Println("Zhipu API:", status(hasZhipu))
		fmt.Println("Moonshot/Kimi API:", status(cfg.Providers.Moonshot.APIKey != ""))
		fmt.Println("Groq API:", status(hasGroq))

		fmt.Println("\nRemote Bridge:")
		fmt.Printf("  API Port: %d\n", cfg.Gateway.Port)

		fmt.Println("\nChannels:")
		fmt.Printf("  Telegram: %s\n", status(cfg.Channels.Telegram.Enabled))
		if cfg.Channels.Telegram.Token != "" {
			masked := cfg.Channels.Telegram.Token
			if len(masked) > 10 {
				masked = masked[:5] + "..." + masked[len(masked)-5:]
			}
			fmt.Printf("    Token: %s\n", masked)
		}
		if len(cfg.Channels.Telegram.AllowFrom) > 0 {
			fmt.Printf("    AllowFrom: %v\n", cfg.Channels.Telegram.AllowFrom)
		}

		store, _ := auth.LoadStore()
		if store != nil && len(store.Credentials) > 0 {
			fmt.Println("\nOAuth/Token Auth:")
			for provider, cred := range store.Credentials {
				status := "authenticated"
				if cred.IsExpired() {
					status = "expired"
				} else if cred.NeedsRefresh() {
					status = "needs refresh"
				}
				fmt.Printf("  %s (%s): %s\n", provider, cred.AuthMethod, status)
			}
		}
	}
}

// runStatusDoctor executes the health checks for `ghost status` against
// the workspace database (read-only). A missing DB degrades checks to
// info rather than failing the whole status.
func runStatusDoctor(cfg *config.Config) []doctor.CheckResult {
	workspace := cfg.WorkspacePath()
	var db *sql.DB
	if workspace != "" {
		if odb, err := sql.Open("sqlite", "file:"+filepath.Join(workspace, "ghost.db")+"?mode=ro"); err == nil {
			db = odb
			defer db.Close()
		}
	}
	doc := doctor.New(db, nil, nil, workspace)
	doc.SetConfigPath(getConfigPath())
	return doc.RunAll(context.Background())
}

// printDoctorRollup prints the overall verdict plus every non-ok row.
// Green rows stay silent: healthy output is short output.
func printDoctorRollup(results []doctor.CheckResult) {
	overall := "ok"
	for _, r := range results {
		if r.Status == "error" {
			overall = "error"
			break
		}
		if r.Status == "warning" {
			overall = "warning"
		}
	}
	fmt.Printf("Health: %s (%d checks)\n", overall, len(results))
	for _, r := range results {
		if r.Status == "ok" || r.Status == "info" {
			continue
		}
		label := r.Label
		if label == "" {
			label = r.Name
		}
		fmt.Printf("  [%s] %s: %s\n", r.Status, label, r.Message)
	}
}

// printStatusSpend prints metered turn spend from canonical events.
// Absent instrumentation prints nothing: no rows means no claim.
func printStatusSpend(cfg *config.Config) {
	workspace := cfg.WorkspacePath()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(workspace, "ghost.db")+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()
	var total float64
	var turns int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(CAST(json_extract(payload,'$.cost_usd') AS REAL)),0), COUNT(*) FROM canonical_events WHERE type='usage.recorded'`).Scan(&total, &turns); err != nil || turns == 0 {
		return
	}
	fmt.Printf("Spend: $%.4f across %d metered turns\n", total, turns)
}

// resetCmd is the CLI factory reset. Hybrid strategy:
//  1. If the daemon is up, POST /v1/reset so it clears its own live state
//     (sessions, RAG index, personal context) synchronously — no restart
//     needed for the wipe itself.
//  2. Otherwise (daemon down/unreachable), stop it if needed and wipe the
//     DB/files directly, then restart it so it reloads the cleared state.
//
// A file-backed model default still needs a restart to take effect, so a
// restart is issued after an API reset whenever the model scope ran.
func resetCmd() {
	rawArgs := os.Args[2:]
	args := make([]string, 0, len(rawArgs))
	noRestart := false
	for _, a := range rawArgs {
		if a == "--no-restart" {
			noRestart = true
			continue
		}
		args = append(args, a)
	}
	if len(args) < 1 {
		fmt.Println("Usage: ghost reset <all|scope...> [--exclude=scope,...] [--no-restart]")
		fmt.Println("  ghost reset all --exclude=devices,secrets")
		fmt.Println("  ghost reset chats memory")
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	text := "/reset " + strings.Join(args, " ")

	// 1. Live path: ask the running daemon to reset itself.
	if out, handled := tryResetViaAPI(cfg, args, text); handled {
		if strings.TrimSpace(out) != "" {
			fmt.Println(out)
		}
		if !noRestart && resetTouchesModel(args, text) && ghostServiceRunning() {
			fmt.Println("Model default changed on disk — restarting Ghost to apply…")
			if err := restartGhostDaemon(); err != nil {
				fmt.Printf("Reset applied, but restart failed: %v\nRun: sudo systemctl restart ghost\n", err)
			} else {
				fmt.Println("Ghost service restarted — reset applied.")
			}
		}
		return
	}

	// 2. Offline fallback: daemon unreachable, wipe directly.
	wasRunning := ghostServiceRunning()
	stopped := false
	if wasRunning {
		stopped = stopGhostDaemon()
	}
	rt := &commands.Runtime{Workspace: cfg.WorkspacePath()}
	out, err := commands.RunReset(context.Background(), rt, text)
	if strings.TrimSpace(out) != "" {
		fmt.Println(out)
	}
	if wasRunning && !noRestart && err == nil && !resetIsValidationError(out) {
		// Stopped above: start it. Never managed to stop (no privilege):
		// restart anyway so it reloads from disk instead of keeping
		// stale in-memory state.
		if stopped {
			startGhostDaemon(true)
		} else if err := restartGhostDaemon(); err != nil {
			fmt.Printf("Reset applied, but restart failed: %v\nRun: sudo systemctl restart ghost\n", err)
		} else {
			fmt.Println("Ghost service restarted — reset applied.")
		}
	}
	if err != nil {
		fmt.Printf("Reset error: %v\n", err)
		os.Exit(1)
	}
}

// resetAPIPort resolves the internal API port the same way startInternalAPI
// does: config default, GHOST_API_PORT override, fallback 8766.
func resetAPIPort(cfg *config.Config) int {
	port := 8766
	if cfg != nil && cfg.Gateway.Port != 0 {
		port = cfg.Gateway.Port
	}
	if p := strings.TrimSpace(os.Getenv("GHOST_API_PORT")); p != "" {
		var v int
		if _, err := fmt.Sscanf(p, "%d", &v); err == nil && v > 0 {
			port = v
		}
	}
	return port
}

// tryResetViaAPI POSTs the reset to the live daemon. It reports handled=true
// when the daemon answered (success or a definitive validation error), in
// which case the caller must not fall back to the offline wipe.
// handled=false means the daemon is unreachable — use the offline path.
func tryResetViaAPI(cfg *config.Config, args []string, text string) (string, bool) {
	_ = args
	port := resetAPIPort(cfg)
	body, _ := json.Marshal(map[string]string{"text": text})
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("http://127.0.0.1:%d/v1/reset", port),
		"application/json", strings.NewReader(string(body)),
	)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var payload struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	_ = json.Unmarshal(raw, &payload)
	switch {
	case resp.StatusCode == http.StatusOK:
		return payload.Message, true
	case resp.StatusCode == http.StatusBadRequest:
		msg := payload.Message
		if msg == "" && payload.Error != nil {
			msg = fmt.Sprintf("%v", payload.Error)
		}
		if strings.TrimSpace(msg) == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return msg, true
	default:
		return "", false
	}
}

// resetIsValidationError reports whether reset output is a usage/validation
// message (nothing was wiped), in which case a restart is pointless.
func resetIsValidationError(out string) bool {
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "usage:") {
		return false
	}
	for _, s := range []string{"unknown reset scope", "unknown option", "--exclude needs", "needs a scope"} {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// resetTouchesModel reports whether the reset includes the model scope, whose
// file-backed default only takes effect after a restart.
func resetTouchesModel(args []string, text string) bool {
	lower := strings.ToLower(" " + strings.Join(args, " ") + " ")
	if strings.Contains(lower, " all ") || strings.Contains(lower, " all\t") {
		return true
	}
	for _, a := range args {
		base := strings.ToLower(strings.SplitN(a, "=", 2)[0])
		base = strings.TrimPrefix(base, "-")
		if base == "model" || base == "ai" {
			return true
		}
	}
	return strings.Contains(strings.ToLower(text), "model")
}

// runSystemctl runs systemctl, escalating via sudo (interactive prompt) when
// not root and the direct call fails on permissions.
func runSystemctl(sysArgs ...string) error {
	if err := exec.Command("systemctl", sysArgs...).Run(); err == nil {
		return nil
	} else if os.Geteuid() == 0 {
		return err
	}
	if _, lookErr := exec.LookPath("sudo"); lookErr != nil {
		return lookErr
	}
	cmd := exec.Command("sudo", append([]string{"systemctl"}, sysArgs...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// ghostServiceRunning reports whether the ghost daemon is active.
func ghostServiceRunning() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", "ghost").Run() == nil
}

// stopGhostDaemon quiesces the daemon before an offline CLI reset. A running
// gateway holds memory and sessions in RAM and can re-persist them after an
// out-of-process clear, so the reset must happen while it is stopped.
// Returns true when it actually stopped the service.
func stopGhostDaemon() bool {
	if !ghostServiceRunning() {
		return false
	}
	if err := runSystemctl("stop", "ghost"); err != nil {
		fmt.Printf("Could not stop Ghost (%v). Proceeding — a restart will follow.\n", err)
		return false
	}
	return true
}

// restartGhostDaemon restarts the daemon unconditionally (used after a live
// API reset that touched the on-disk model default, or when an offline reset
// could not stop the daemon first).
func restartGhostDaemon() error {
	return runSystemctl("restart", "ghost")
}

// startGhostDaemon restarts the daemon after a CLI reset so it reloads the
// cleared state.
func startGhostDaemon(stopped bool) {
	if !stopped {
		return
	}
	if err := runSystemctl("start", "ghost"); err != nil {
		fmt.Printf("Reset applied, but starting Ghost failed: %v\nRun: sudo systemctl start ghost\n", err)
		return
	}
	fmt.Println("Ghost service restarted — reset applied.")
}

func resetPasswordCmd() {
	force := false
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--force":
			force = true
		case "--help", "-h":
			fmt.Println("Usage: ghost reset-password --force")
			fmt.Println("  Reset the admin dashboard password. Requires --force.")
			return
		default:
			fmt.Printf("Unknown flag: %s\n", arg)
			return
		}
	}

	if !force {
		fmt.Println("This resets the admin dashboard password. Use --force to confirm.")
		return
	}

	ghostDir := os.Getenv("GHOST_DIR")
	if ghostDir == "" {
		ghostDir = appliance.DefaultGhostDir
	}

	if !appliance.AdminConfigured(ghostDir) {
		fmt.Println("No admin password is configured yet. Run the setup wizard first.")
		return
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("New admin password: ")
	pw1, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Failed to read password: %v\n", err)
		os.Exit(1)
	}
	pw1 = strings.TrimSpace(pw1)

	fmt.Print("Confirm new admin password: ")
	pw2, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Failed to read password: %v\n", err)
		os.Exit(1)
	}
	pw2 = strings.TrimSpace(pw2)

	if pw1 != pw2 {
		fmt.Println("Passwords do not match.")
		os.Exit(1)
	}

	if err := appliance.ValidatePassword(pw1); err != nil {
		fmt.Printf("Password rejected: %v\n", err)
		os.Exit(1)
	}

	if err := appliance.SetAdminPassword(ghostDir, pw1); err != nil {
		fmt.Printf("Failed to set password: %v\n", err)
		os.Exit(1)
	}

	logger.InfoCF("auth", "Admin password reset via CLI", nil)
	fmt.Println("✓ Admin password updated.")

	// Restart the wizard service to invalidate all in-memory sessions.
	if runtime.GOOS == "linux" {
		if err := exec.Command("systemctl", "restart", "ghost-web").Run(); err != nil {
			fmt.Printf("Warning: could not restart ghost-web to invalidate sessions: %v\n", err)
		} else {
			fmt.Println("✓ Sessions invalidated (ghost-web restarted).")
		}
	}
}

func authCmd() {
	if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
		authHelp()
		return
	}

	switch os.Args[2] {
	case "login":
		authLoginCmd()
	case "logout":
		authLogoutCmd()
	case "status":
		authStatusCmd()
	case "reset-password":
		resetPasswordCmd()
	default:
		fmt.Printf("Unknown auth command: %s\n", os.Args[2])
		authHelp()
	}
}

func authHelp() {
	fmt.Println("\nAuth commands:")
	fmt.Println("  login       Login via OAuth or paste token")
	fmt.Println("  logout      Remove stored credentials")
	fmt.Println("  status      Show current auth status")
	fmt.Println("  reset-password  Reset the admin dashboard password (requires --force)")
	fmt.Println()
	fmt.Println("Login options:")
	fmt.Println("  --provider <name>    Provider to login with (openai, anthropic)")
	fmt.Println("  --device-code        Use device code flow (for headless environments)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ghost auth login --provider openai")
	fmt.Println("  ghost auth login --provider openai --device-code")
	fmt.Println("  ghost auth login --provider anthropic")
	fmt.Println("  ghost auth logout --provider openai")
	fmt.Println("  ghost auth status")
}

func authLoginCmd() {
	provider := ""
	useDeviceCode := false

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		case "--device-code":
			useDeviceCode = true
		}
	}

	if provider == "" {
		fmt.Println("Error: --provider is required")
		fmt.Println("Supported providers: openai, anthropic")
		return
	}

	switch provider {
	case "openai":
		authLoginOpenAI(useDeviceCode)
	case "anthropic":
		authLoginPasteToken(provider)
	default:
		fmt.Printf("Unsupported provider: %s\n", provider)
		fmt.Println("Supported providers: openai, anthropic")
	}
}

func authLoginOpenAI(useDeviceCode bool) {
	cfg := auth.OpenAIOAuthConfig()

	var cred *auth.AuthCredential
	var err error

	if useDeviceCode {
		cred, err = auth.LoginDeviceCode(cfg)
	} else {
		cred, err = auth.LoginBrowser(cfg)
	}

	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	if err := auth.SetCredential("openai", cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		appCfg.Providers.OpenAI.AuthMethod = "oauth"
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Println("Login successful!")
	if cred.AccountID != "" {
		fmt.Printf("Account: %s\n", cred.AccountID)
	}
}

func authLoginPasteToken(provider string) {
	cred, err := auth.LoginPasteToken(provider, os.Stdin)
	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	if err := auth.SetCredential(provider, cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		switch provider {
		case "anthropic":
			appCfg.Providers.Anthropic.AuthMethod = "token"
		case "openai":
			appCfg.Providers.OpenAI.AuthMethod = "token"
		}
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Printf("Token saved for %s!\n", provider)
}

func authLogoutCmd() {
	provider := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		}
	}

	if provider != "" {
		if err := auth.DeleteCredential(provider); err != nil {
			fmt.Printf("Failed to remove credentials: %v\n", err)
			os.Exit(1)
		}

		appCfg, err := loadConfig()
		if err == nil {
			switch provider {
			case "openai":
				appCfg.Providers.OpenAI.AuthMethod = ""
			case "anthropic":
				appCfg.Providers.Anthropic.AuthMethod = ""
			}
			config.SaveConfig(getConfigPath(), appCfg)
		}

		fmt.Printf("Logged out from %s\n", provider)
	} else {
		if err := auth.DeleteAllCredentials(); err != nil {
			fmt.Printf("Failed to remove credentials: %v\n", err)
			os.Exit(1)
		}

		appCfg, err := loadConfig()
		if err == nil {
			appCfg.Providers.OpenAI.AuthMethod = ""
			appCfg.Providers.Anthropic.AuthMethod = ""
			config.SaveConfig(getConfigPath(), appCfg)
		}

		fmt.Println("Logged out from all providers")
	}
}

func authStatusCmd() {
	store, err := auth.LoadStore()
	if err != nil {
		fmt.Printf("Error loading auth store: %v\n", err)
		return
	}

	if len(store.Credentials) == 0 {
		fmt.Println("No authenticated providers.")
		fmt.Println("Run: ghost auth login --provider <name>")
		return
	}

	fmt.Println("\nAuthenticated Providers:")
	fmt.Println("------------------------")
	for provider, cred := range store.Credentials {
		status := "active"
		if cred.IsExpired() {
			status = "expired"
		} else if cred.NeedsRefresh() {
			status = "needs refresh"
		}

		fmt.Printf("  %s:\n", provider)
		fmt.Printf("    Method: %s\n", cred.AuthMethod)
		fmt.Printf("    Status: %s\n", status)
		if cred.AccountID != "" {
			fmt.Printf("    Account: %s\n", cred.AccountID)
		}
		if !cred.ExpiresAt.IsZero() {
			fmt.Printf("    Expires: %s\n", cred.ExpiresAt.Format("2006-01-02 15:04"))
		}
	}
}

func getConfigPath() string {
	if configDir := os.Getenv("GHOST_CONFIG_DIR"); configDir != "" {
		return filepath.Join(configDir, "config.json")
	}
	// Priority: current dir/config/config.json > current dir/config.json > ~/ghost/config/config.json > ~/.ghost/config.json
	if _, err := os.Stat("config/config.json"); err == nil {
		return "config/config.json"
	}
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	home, _ := os.UserHomeDir()

	fallback := filepath.Join(home, "ghost", "config", "config.json")
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}

	return filepath.Join(home, ".ghost", "config.json")
}

// isApplianceOpsCommand reports whether a CLI command operates on the
// installed Ghost rather than the current checkout. Only these
// commands receive personal AI path defaults (see applyApplianceTarget);
// agent/serve/skills and friends keep checkout-relative resolution.
func isApplianceOpsCommand(command string) bool {
	switch command {
	case "reset", "reset-password", "verify", "status":
		return true
	}
	return false
}

// isInteractiveCommand reports whether a command loads the config to run the
// model/agent (as opposed to ops or pure-local commands). These are the
// commands whose config MUST match the console and the daemon.
func isInteractiveCommand(command string) bool {
	switch command {
	case "agent", "serve", "gateway", "model", "golden", "benchmark":
		return true
	}
	return false
}

// applyInstalledConfig points interactive commands at the installed Ghost's
// config directory (and workspace) when they exist and this user can read
// them. This keeps the CLI consistent with the Web Console and the daemon —
// the fix for "configured it in the console but the CLI says no key". It never
// overrides an explicit GHOST_CONFIG_DIR or GHOST_WORKSPACE_DIR.
func applyInstalledConfig() {
	configDir, ok := appliance.ResolveInstalledConfig(os.Getenv, appliance.ApplianceInstalled(), func(dir string) bool {
		_, err := os.ReadFile(filepath.Join(dir, "config.json"))
		return err == nil
	})
	if !ok {
		return
	}
	_ = os.Setenv("GHOST_CONFIG_DIR", configDir)

	// Adopt the installed workspace too, or the CLI would read the installed
	// config but still try to use a checkout workspace. Only when readable and
	// not already set by the operator.
	if os.Getenv("GHOST_WORKSPACE_DIR") == "" {
		ws := appliance.DefaultWorkspaceDir
		if _, err := os.Stat(ws); err == nil {
			_ = os.Setenv("GHOST_WORKSPACE_DIR", ws)
		}
	}
	fmt.Fprintf(os.Stderr, "✓ Using installed Ghost config (%s)\n", configDir)
}

// applyApplianceTarget points root ops commands at the installed
// personal AI. Without it, `sudo ghost reset` from a source checkout
// silently operates on the checkout's config/workspace instead of the
// live system. Explicit env always wins (see appliance.ResolveOpsTarget);
// the .env is loaded too, without overriding existing vars,
// so behavior matches the systemd units.
func applyApplianceTarget() {
	configDir, workspace, ok := appliance.ResolveOpsTarget(os.Getenv, os.Geteuid(), appliance.ApplianceInstalled())
	if !ok {
		return
	}
	if configDir != "" {
		_ = os.Setenv("GHOST_CONFIG_DIR", configDir)
		if err := godotenv.Load(filepath.Join(filepath.Dir(configDir), ".env")); err == nil {
			fmt.Fprintf(os.Stderr, "✓ Loaded .env (from Ghost dir)\n")
		}
	}
	if workspace != "" {
		_ = os.Setenv("GHOST_WORKSPACE_DIR", workspace)
	}
}

// executeScheduledCommand runs a scheduled shell command: static deny-list
// first, then the Permission Broker, then the registry's default-deny exec.
// The scheduler never authorizes.
func executeScheduledCommand(ctx context.Context, agentLoop *agent.AgentLoop, item *scheduled.ScheduledItem) error {
	if err := tools.CheckCronCommand(item.Action.Command); err != nil {
		return fmt.Errorf("scheduled command blocked: %w", err)
	}
	authorizedCtx, err := agentLoop.AuthorizeScheduledCommand(ctx, item.Action.Command)
	if err != nil {
		return fmt.Errorf("scheduled command not authorized: %w", err)
	}
	res := agentLoop.Tools().ExecuteWithContext(authorizedCtx, "exec",
		map[string]interface{}{"command": item.Action.Command}, item.Channel, item.ChatID, "", nil)
	if res != nil && res.IsError {
		return fmt.Errorf("scheduled command failed: %s", res.ForLLM)
	}
	return nil
}

// migrateLegacyCron imports legacy cron/jobs.json state into the
// authoritative scheduler exactly once. Idempotent and safe to run every
// boot; the legacy file is left in place as a migrated artifact.
func migrateLegacyCron(workspace string, svc *scheduled.Service) {
	if svc == nil {
		return
	}
	legacyPath := filepath.Join(workspace, "cron", "jobs.json")
	res, err := svc.MigrateLegacyCron(legacyPath)
	if err != nil {
		logger.ErrorCF("scheduler", "legacy cron migration failed", map[string]interface{}{"error": err.Error()})
		return
	}
	if res.Migrated > 0 || len(res.Errors) > 0 {
		logger.InfoCF("scheduler", "legacy cron migrated into authoritative scheduler", map[string]interface{}{
			"migrated": res.Migrated, "skipped": res.Skipped, "errors": len(res.Errors),
		})
	}
}

func deriveScheduleTimezone(workspace string) string {
	// The owner's stated location wins if known.
	if loc := locationToTimezone(workspace); loc != "" {
		return loc
	}
	// Otherwise use the device's configured timezone (the same default a
	// smart speaker uses), not a hardcoded UTC.
	return tools.DeviceTimezone()
}

func locationToTimezone(workspace string) string {
	// Minimal mapping for known user locations; fallback is UTC.
	mapping := map[string]string{
		"london":    "Europe/London",
		"bangkok":   "Asia/Bangkok",
		"tokyo":     "Asia/Tokyo",
		"oslo":      "Europe/Oslo",
		"new york":  "America/New_York",
		"paris":     "Europe/Paris",
		"berlin":    "Europe/Berlin",
		"singapore": "Asia/Singapore",
	}
	// Read location from personal context entries or user-profile
	profilePath := filepath.Join(workspace, "knowledge", "self", "user-profile.md")
	if data, err := os.ReadFile(profilePath); err == nil {
		lower := strings.ToLower(string(data))
		for k, tz := range mapping {
			if strings.Contains(lower, "location: "+k) {
				return tz
			}
		}
	}
	return ""
}

func setupScheduledService(agentLoop *agent.AgentLoop, msgBus *bus.MessageBus, workspace string, broker *permissions.Broker) *scheduled.Service {
	// Get the SQLite database connection
	d := agentLoop.DB()

	store := scheduled.NewStore(d)
	if err := store.InitSchema(); err != nil {
		fmt.Printf("Error initializing scheduled schema: %v\n", err)
		return nil
	}
	// One-time repair: rows written with non-UTC offsets never compare
	// correctly in ListDue and would fire hours late or never.
	if n, err := store.NormalizeStoredTimesToUTC(); err != nil {
		fmt.Printf("Warning: schedule time normalization failed: %v\n", err)
	} else if n > 0 {
		fmt.Printf("Normalized %d stored schedule time(s) to UTC.\n", n)
	}

	// Create executor that sends messages through the agent.
	// Routine-aware: items carrying routine metadata run through the
	// routines product layer (same pipeline as interactive requests:
	// capability → permission → execution → event → activity) instead
	// of raw bus injection. Non-routine items keep the legacy path.
	routineSvc, _ := routines.New(d, store)
	// The single runtime-owned broker is passed in; the scheduler does not
	// open its own authority over the same database.
	cstream, _ := cevents.Open(d, routineEventDir(workspace))
	executor := func(ctx context.Context, item *scheduled.ScheduledItem) error {
		if routineSvc != nil {
			if r, err := routineSvc.Get(item.ID); err == nil {
				return executeRoutine(ctx, agentLoop, msgBus, routineSvc, broker, cstream, r, item)
			}
		}
		// Scheduled shell commands are consequential and resolve through
		// the Permission Broker before execution — the scheduler never
		// authorizes.
		if item.Action.Kind == scheduled.ActionCommand && item.Action.Command != "" {
			return executeScheduledCommand(ctx, agentLoop, item)
		}
		if item.Action.Content == "" {
			return fmt.Errorf("no message content")
		}

		// Send message through the inbound bus for agent processing.
		// Scheduler-originated turns run under their own session
		// ("automation:<item-id>") with an explicit scheduler sender, so
		// they group per-automation for history/debugging — and so the
		// memory extractor and affect scorer can tell machine-scheduled
		// turns apart from user turns (scheduler echo must never become a
		// "user preference", and poem prose must not inflate affinity).
		if msgBus != nil {
			msgBus.PublishInbound(bus.InboundMessage{
				Channel:    item.Channel,
				ChatID:     item.ChatID,
				Content:    item.Action.Content,
				SessionKey: "automation:" + item.ID,
				SenderID:   "scheduler",
			})
		}

		return nil
	}

	// Create simple event bus adapter
	events := &scheduled.SimpleEventBus{}

	service := scheduled.NewService(store, events, executor)
	// Feed the proactive signal scan: routine waits/failures propose
	// gated, reason-carrying notices on heartbeat ticks.
	agentLoop.SetRoutineSignals(routineSvc, service)
	return service
}

func loadConfig() (*config.Config, error) {
	return config.LoadConfig(getConfigPath())
}

// printProviderSetupHelp turns a provider-creation failure into an
// actionable diagnostic. The most common cause is not "no key was ever
// saved" but "the CLI resolved a different config than the one that holds
// the key" — e.g. running from a source checkout while the installed Ghost
// (and the console that saved the key) uses a different config directory.
//
// It names the config file that was read, and when an installed Ghost config
// exists with a key for this provider, says exactly how to use it. It never
// silently changes resolution.
func printProviderSetupHelp(cfg *config.Config, err error) {
	configPath := getConfigPath()
	provider, model := "", ""
	if cfg != nil {
		provider = cfg.Agents.Defaults.Provider
		model = cfg.Agents.Defaults.Model
	}

	fmt.Fprintf(os.Stderr, "Error creating provider: %v\n", err)
	fmt.Fprintf(os.Stderr, "  config read: %s\n", configPath)
	if provider != "" {
		fmt.Fprintf(os.Stderr, "  provider: %s (model %s)\n", provider, model)
	}

	// Is a configured, installed Ghost being shadowed by this config?
	if shadow, ok := appliance.DetectConfigShadow(configPath, appliance.ApplianceInstalled(), func(p string) string {
		c, cerr := config.LoadConfig(p)
		if cerr != nil || c == nil {
			return ""
		}
		return providerKeyFor(c, provider)
	}); ok {
		fmt.Fprintln(os.Stderr, "  note: this is not the installed Ghost's config.")
		if shadow.InstalledHasSecret {
			fmt.Fprintf(os.Stderr, "  the installed Ghost at %s has a key for %s.\n", shadow.InstalledPath, provider)
			fmt.Fprintf(os.Stderr, "  To use it here:\n    GHOST_CONFIG_DIR=%s ghost agent\n", filepath.Dir(shadow.InstalledPath))
		} else {
			fmt.Fprintf(os.Stderr, "  installed Ghost config: %s\n", shadow.InstalledPath)
		}
	}

	fmt.Fprintln(os.Stderr, "  Fix it with either:")
	envVar := providerEnvVar(provider)
	if envVar != "" {
		fmt.Fprintf(os.Stderr, "    - export %s=... then retry, or\n", envVar)
	}
	fmt.Fprintf(os.Stderr, "    - the Web Console → AI → configure %s.\n", provider)
}

// providerEnvVar returns the conventional API-key environment variable for a
// provider name, or "" when none is defined. It mirrors the mapping in
// pkg/config so the CLI hint matches what the loader actually reads.
func providerEnvVar(provider string) string {
	switch provider {
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "groq":
		return "GROQ_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	case "zhipu":
		return "ZHIPU_API_KEY"
	case "moonshot", "kimi":
		return "KIMI_API_KEY"
	}
	return ""
}

// providerKeyFor returns the stored API key for a provider name, or "".
func providerKeyFor(c *config.Config, provider string) string {
	if c == nil {
		return ""
	}
	switch provider {
	case "deepseek":
		return c.Providers.DeepSeek.APIKey
	case "anthropic":
		return c.Providers.Anthropic.APIKey
	case "openai":
		return c.Providers.OpenAI.APIKey
	case "openrouter":
		return c.Providers.OpenRouter.APIKey
	case "groq":
		return c.Providers.Groq.APIKey
	case "gemini":
		return c.Providers.Gemini.APIKey
	case "zhipu":
		return c.Providers.Zhipu.APIKey
	case "moonshot", "kimi":
		return c.Providers.Moonshot.APIKey
	case "qwen":
		return c.Providers.Qwen.APIKey
	case "nvidia":
		return c.Providers.Nvidia.APIKey
	case "ollama":
		return c.Providers.Ollama.APIKey
	}
	return ""
}

// healSecretsBoundary checks if config.json contains secrets that should only
// be in .secrets.json. If found, it saves the config (which strips secrets via
// SaveConfig) to restore the clean boundary.
func healSecretsBoundary(configPath string, cfg *config.Config) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	gw, ok := raw["gateway"]
	if !ok {
		return
	}
	var gwCfg map[string]json.RawMessage
	if err := json.Unmarshal(gw, &gwCfg); err != nil {
		return
	}
	if _, has := gwCfg["bridge_secret"]; !has {
		return
	}
	// config.json has a bridge_secret — SaveConfig will strip it and write
	// the secret to .secrets.json.
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("⚠️  Failed to clean secrets boundary: %v\n", err)
	} else {
		fmt.Println("✅ Cleaned leaked secrets from config.json")
	}
}

func relayCmd() {
	if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
		relayHelp()
		return
	}

	subcommand := os.Args[2]
	switch subcommand {
	case "run":
		relayRunCmd()
	case "pair":
		relayPairCmd()
	case "clients":
		relayClientsCmd()
	case "revoke":
		relayRevokeCmd()
	case "setup":
		relaySetupCmd()
	default:
		fmt.Printf("Unknown relay command: %s\n", subcommand)
		relayHelp()
	}
}

func relayHelp() {
	fmt.Println("\nRelay commands:")
	fmt.Println("  run              Connect to relay server (runs in foreground)")
	fmt.Println("  pair             Generate a pairing token for a new client")
	fmt.Println("  clients          List paired clients")
	fmt.Println("  revoke <token-hash-prefix>   Revoke a client (use the ID shown by clients)")
	fmt.Println("  setup            Generate device secret and configure relay")
}

func relayRunCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	if !cfg.Relay.Enabled {
		fmt.Println("Relay is not enabled. Set relay.enabled=true and relay.server in config.")
		os.Exit(1)
	}
	if cfg.Relay.Server == "" {
		fmt.Println("Relay server not configured. Set relay.server in config.")
		os.Exit(1)
	}
	if cfg.Relay.DeviceSecret == "" {
		fmt.Println("Device secret not configured. Run 'ghost relay setup' first.")
		os.Exit(1)
	}

	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error loading identity: %v\n", err)
		os.Exit(1)
	}

	gatewayURL := cfg.Relay.GatewayURL
	if gatewayURL == "" {
		gatewayURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)
	}

	client := relayclient.NewClient(relayclient.ClientConfig{
		DeviceID:     ghostID.GhostID,
		DeviceSecret: cfg.Relay.DeviceSecret,
		RelayServer:  cfg.Relay.Server,
		GatewayURL:   gatewayURL,
		ReconnectMin: cfg.Relay.ReconnectMin,
		ReconnectMax: cfg.Relay.ReconnectMax,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nRelay disconnecting...")
		cancel()
	}()

	fmt.Printf("Relay client connecting to %s...\n", cfg.Relay.Server)
	if err := client.Run(ctx); err != nil && err != context.Canceled {
		fmt.Printf("Relay error: %v\n", err)
		os.Exit(1)
	}
}

func relayPairCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error loading identity: %v\n", err)
		os.Exit(1)
	}

	name := "Phone"
	if len(os.Args) > 3 {
		name = os.Args[3]
	}

	token, err := relayclient.AddClient(ghostID.GhostID, name)
	if err != nil {
		fmt.Printf("Error generating token: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nPairing token generated for: %s\n", name)
	fmt.Printf("Token: %s\n\n", token)
	fmt.Println("Add this URL to your Ghost app:")
	fmt.Printf("  ghost://connect?transport=relay&relay=%s&ghost=%s&token=%s\n",
		cfg.Relay.Server, ghostID.GhostID, token)
	fmt.Println("\nNote: This token is shown once. Store it securely.")
}

func relayClientsCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error loading identity: %v\n", err)
		os.Exit(1)
	}

	clients, err := relayclient.ListClients(ghostID.GhostID)
	if err != nil {
		fmt.Printf("Error listing clients: %v\n", err)
		os.Exit(1)
	}

	if len(clients) == 0 {
		fmt.Println("No paired clients.")
		return
	}

	fmt.Println("Paired clients:")
	for _, c := range clients {
		name := c.Name
		if name == "" {
			name = "(unnamed)"
		}
		prefix := c.TokenHash
		if len(prefix) > 16 {
			prefix = prefix[:16]
		}
		fmt.Printf("  %s  %s  created %s\n", prefix, name, c.CreatedAt)
	}
}

func relayRevokeCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost relay revoke <token-hash-prefix>")
		os.Exit(1)
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error loading identity: %v\n", err)
		os.Exit(1)
	}

	prefix := os.Args[3]
	if err := relayclient.RemoveClient(ghostID.GhostID, prefix); err != nil {
		fmt.Printf("Error revoking client: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Client revoked.")
}

func relaySetupCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error loading identity: %v\n", err)
		os.Exit(1)
	}

	// Generate device secret if not set
	if cfg.Relay.DeviceSecret == "" {
		secret, err := relayclient.GenerateToken()
		if err != nil {
			fmt.Printf("Error generating device secret: %v\n", err)
			os.Exit(1)
		}
		cfg.Relay.DeviceSecret = secret
	}

	// Prompt for relay server
	if cfg.Relay.Server == "" {
		fmt.Print("Relay server URL (e.g., ws://127.0.0.1:8080): ")
		fmt.Scanln(&cfg.Relay.Server)
	}

	// Enable relay
	cfg.Relay.Enabled = true

	// Save config
	configPath := getConfigPath()
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nRelay configured:\n")
	fmt.Printf("  Device ID:     %s\n", ghostID.GhostID)
	fmt.Printf("  Device Secret: %s\n", cfg.Relay.DeviceSecret)
	fmt.Printf("  Relay Server:  %s\n", cfg.Relay.Server)
	fmt.Printf("  Enabled:       %v\n\n", cfg.Relay.Enabled)
	fmt.Println("Next steps:")
	fmt.Println("  1. Add this device to the relay server:")
	fmt.Printf("     ghost-relay-server add-device %s --name \"My Ghost\"\n", ghostID.GhostID)
	fmt.Println("  2. Start the relay connection:")
	fmt.Println("     ghost relay run")
	fmt.Println("  3. Generate pairing tokens for your phone:")
	fmt.Println("     ghost relay pair")
}

func stateCmd() {
	if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
		stateHelp()
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	switch os.Args[2] {
	case "export":
		stateExportCmd(cfg)
	case "import":
		stateImportCmd(cfg)
	case "inspect":
		stateInspectCmd(cfg)
	case "backup":
		stateBackupCmd(cfg)
	case "prune":
		statePruneCmd(cfg)
	default:
		stateHelp()
	}
}

func stateExportCmd(cfg *config.Config) {
	includeSecrets := false
	dest := ""
	for _, arg := range os.Args[3:] {
		switch arg {
		case "--include-secrets":
			includeSecrets = true
		case "--help", "-h":
			stateHelp()
			return
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Printf("Unknown flag: %s\n", arg)
				stateHelp()
				return
			}
			dest = arg
		}
	}
	if dest == "" {
		fmt.Println("Usage: ghost state export <archive> [--include-secrets]")
		os.Exit(2)
	}

	passphrase, err := readPassphrase("Passphrase (used to encrypt the archive): ", true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if includeSecrets {
		fmt.Println("⚠️  Including secrets: API keys and channel tokens will be embedded in this encrypted archive.")
		fmt.Println("    Store the archive securely. Secrets are NOT included by default.")
		if !confirm("Continue? (y/n): ") {
			fmt.Println("Aborted.")
			return
		}
	}

	manifest, err := ghoststate.Export(ghoststate.ExportOptions{
		Workspace:      cfg.WorkspacePath(),
		ConfigPath:     getConfigPath(),
		Destination:    dest,
		Passphrase:     passphrase,
		IncludeSecrets: includeSecrets,
	})
	if err != nil {
		fmt.Printf("Error exporting Ghost State: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Exported Ghost State (%s) to %s\n", manifest.GhostID, dest)
	if len(manifest.Rebound) > 0 {
		fmt.Println("  Device-specific (not exported):")
		for _, r := range manifest.Rebound {
			fmt.Printf("    - %s\n", r)
		}
	}
	if !includeSecrets {
		fmt.Println("  Secrets excluded. Re-add them on the new machine or export with --include-secrets.")
	}
}

func stateImportCmd(cfg *config.Config) {
	force := false
	src := ""
	importContext := ""
	for _, arg := range os.Args[3:] {
		switch arg {
		case "--force":
			force = true
		case "--help", "-h":
			stateHelp()
			return
		default:
			if strings.HasPrefix(arg, "--context=") {
				importContext = strings.TrimPrefix(arg, "--context=")
				continue
			}
			if strings.HasPrefix(arg, "-") {
				fmt.Printf("Unknown flag: %s\n", arg)
				stateHelp()
				return
			}
			src = arg
		}
	}
	if src == "" {
		fmt.Println("Usage: ghost state import <archive> [--force] [--context <id>]")
		os.Exit(2)
	}

	if force {
		fmt.Println("⚠️  --force set: the target workspace will be overwritten.")
		if !confirm("Continue? (y/n): ") {
			fmt.Println("Aborted.")
			return
		}
	}

	passphrase, err := readPassphrase("Passphrase (to decrypt the archive): ", false)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	manifest, err := ghoststate.Import(ghoststate.ImportOptions{
		Workspace:  cfg.WorkspacePath(),
		ConfigPath: getConfigPath(),
		Source:     src,
		Passphrase: passphrase,
		Force:      force,
		Context:    importContext,
	})
	if err != nil {
		fmt.Printf("Error importing Ghost State: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Imported Ghost State (%s) into %s\n", manifest.GhostID, cfg.WorkspacePath())
	if importContext != "" {
		fmt.Printf("  Untagged memories assigned to context %q.\n", importContext)
	}
	if err != nil {
		fmt.Printf("Error importing Ghost State: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Imported Ghost State (%s) into %s\n", manifest.GhostID, cfg.WorkspacePath())
	if manifest.SecretsIncluded {
		fmt.Println("  Secrets restored from the archive.")
	} else {
		fmt.Println("  Secrets were not in this archive. Re-add them if needed.")
	}
}

func stateInspectCmd(cfg *config.Config) {

	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost state inspect <archive>")
		os.Exit(2)
	}
	src := os.Args[3]
	passphrase, err := readPassphrase("Passphrase (to decrypt the archive): ", false)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	manifest, err := ghoststate.Inspect(src, passphrase)
	if err != nil {
		fmt.Printf("Error inspecting archive: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Ghost State archive: %s\n", src)
	fmt.Printf("  Format:        %s (schema v%d)\n", manifest.Format, manifest.SchemaVersion)
	fmt.Printf("  Ghost ID:      %s\n", manifest.GhostID)
	fmt.Printf("  Exported:      %s\n", manifest.ExportedAt)
	fmt.Printf("  From:          %s\n", manifest.Origin.Hostname)
	fmt.Printf("  Secrets:       %s\n", map[bool]string{true: "included", false: "excluded"}[manifest.SecretsIncluded])
	fmt.Printf("  Files:         %d\n", len(manifest.Files))
	var portable, derived int
	for _, f := range manifest.Files {
		if f.Category == ghoststate.CategoryPortable {
			portable++
		}
		if f.Category == ghoststate.CategoryDerived {
			derived++
		}
	}
	if portable > 0 {
		fmt.Printf("    portable: %d, derived: %d\n", portable, derived)
	}
	if len(manifest.Rebound) > 0 {
		fmt.Println("  Rebound (device-specific, not restored):")
		for _, r := range manifest.Rebound {
			fmt.Printf("    - %s\n", r)
		}
	}
	if len(manifest.SecretsExcluded) > 0 {
		fmt.Println("  Secrets excluded:")
		for _, s := range manifest.SecretsExcluded {
			fmt.Printf("    - %s\n", s)
		}
	}
}

func stateHelp() {
	fmt.Println("\nGhost State commands:")
	fmt.Println("  export <archive> [--include-secrets]   Export portable Ghost State to an encrypted archive")
	fmt.Println("  inspect <archive>                      Show what an archive contains without importing")
	fmt.Println("  import <archive> [--force]             Restore an archive into a fresh Ghost installation")
	fmt.Println("  backup [--cron]                        Take a recovery snapshot (same archive, retention kept)")
	fmt.Println("  prune [--keep=N]                       Enforce snapshot retention (default keeps 5)")
	fmt.Println()
	fmt.Println("Import only runs on a fresh installation unless --force is given.")
	fmt.Println("Rebound (device-specific) state is never exported; secrets need --include-secrets.")
	fmt.Println("Backup snapshots live beside the workspace backups dir and always include secrets;")
	fmt.Println("recovery needs only the archive and the vault master key.")
}

// stateBackupCmd takes a recovery snapshot and enforces retention. It is
// the scheduled-backup entry point (systemd timer) and the pre-update
// snapshot path: same archive as export, keyed by the vault master key,
// never interactive (passphrase comes from key resolution, not a prompt).
func stateBackupCmd(cfg *config.Config) {
	dest, removed, err := ghoststate.SnapshotAndPrune(cfg.WorkspacePath(), getConfigPath(), ghoststate.SnapshotKeep)
	if err != nil {
		fmt.Printf("Error taking snapshot: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Snapshot: %s\n", dest)
	for _, r := range removed {
		fmt.Printf("Pruned: %s\n", r)
	}
}

// statePruneCmd enforces snapshot retention without taking a new one.
// Named by disk-full recovery paths ("prune oldest backups").
func statePruneCmd(cfg *config.Config) {
	keep := ghoststate.SnapshotKeep
	for _, a := range os.Args[3:] {
		var n int
		if _, err := fmt.Sscanf(a, "--keep=%d", &n); err == nil && n >= 1 {
			keep = n
		}
	}
	removed, err := ghoststate.PruneSnapshots(ghoststate.BackupDir(cfg.WorkspacePath()), keep)
	if err != nil {
		fmt.Printf("Error pruning snapshots: %v\n", err)
		os.Exit(1)
	}
	if len(removed) == 0 {
		fmt.Println("Nothing to prune.")
		return
	}
	for _, r := range removed {
		fmt.Printf("Pruned: %s\n", r)
	}
}

// readPassphrase reads a passphrase from the terminal without echoing, or
// falls back to reading a line from stdin when not attached to a terminal.
func readPassphrase(prompt string, confirm bool) (string, error) {
	// A single reader is shared across reads: bufio read-ahead would swallow
	// extra piped lines into a discarded buffer otherwise.
	var stdin *bufio.Reader
	read := func() (string, error) {
		fd := uintptr(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			fmt.Fprint(os.Stderr, prompt)
			b, err := term.ReadPassword(fd)
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
		if stdin == nil {
			stdin = bufio.NewReader(os.Stdin)
		}
		line, err := stdin.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), err
	}
	p1, err := read()
	if err != nil {
		return "", err
	}
	if p1 == "" {
		return "", fmt.Errorf("passphrase must not be empty")
	}
	if confirm {
		p2, err := read()
		if err != nil {
			return "", err
		}
		if p1 != p2 {
			return "", fmt.Errorf("passphrases do not match")
		}
	}
	return p1, nil
}

func confirm(prompt string) bool {
	if err := nontty.RequireInteractive("confirmation", "--yes"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false
	}
	fmt.Fprint(os.Stderr, prompt)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(strings.TrimSpace(response)) == "y"
}

// mcpCmd manages MCP servers from the CLI, mirroring the web console's MCP
// section for headless or scripted configuration.
func mcpCmd() {
	if len(os.Args) < 3 || wantsHelp(os.Args[2:]) {
		mcpHelp()
		return
	}

	subcommand := os.Args[2]

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	switch subcommand {
	case "list":
		mcpListCmd(cfg)
	case "add":
		mcpAddCmd(cfg)
	case "edit":
		mcpEditCmd(cfg)
	case "remove":
		mcpRemoveCmd(cfg)
	case "test":
		mcpTestCmd(cfg)
	default:
		fmt.Printf("Unknown mcp command: %s\n", subcommand)
		mcpHelp()
	}
}

func mcpHelp() {
	fmt.Println("\nMCP server commands:")
	fmt.Println("  list                    List configured MCP servers")
	fmt.Println("  add <name> -- <command> [args...]   Add a stdio MCP server")
	fmt.Println("  add <name> --http <url>  Add an HTTP/SSE MCP server")
	fmt.Println("  edit <name> -- <command> [args...]  Update an MCP server")
	fmt.Println("  remove <name>            Remove an MCP server")
	fmt.Println("  test <name>              Connect and list tools from a server")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ghost mcp add filesystem -- npx -y @modelcontextprotocol/server-filesystem /tmp")
	fmt.Println("  ghost mcp add remote --http https://example.com/sse")
}

func mcpListCmd(cfg *config.Config) {
	servers := cfg.Tools.MCP.Servers
	if len(servers) == 0 {
		fmt.Println("No MCP servers configured.")
		return
	}
	fmt.Println("Configured MCP servers:")
	for name, s := range servers {
		state := "disabled"
		if s.Enabled {
			state = "enabled"
		}
		kind := "stdio"
		if s.HTTP {
			kind = "http"
		}
		target := s.Command
		if s.HTTP {
			target = s.HTTPURL
		}
		fmt.Printf("  %-20s %-8s %-7s %s\n", name, state, kind, target)
	}
}

func mcpAddCmd(cfg *config.Config) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost mcp add <name> -- <command> [args...]")
		fmt.Println("       ghost mcp add <name> --http <url>")
		return
	}
	name := os.Args[3]

	server, ok := buildMCPServerFromArgs(os.Args[4:])
	if !ok {
		return
	}
	server.Enabled = true
	if cfg.Tools.MCP.Servers == nil {
		cfg.Tools.MCP.Servers = map[string]config.MCPServerConfig{}
	}
	cfg.Tools.MCP.Servers[name] = server

	if err := saveConfigFromCLI(cfg); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ MCP server '%s' added\n", name)
}

func mcpEditCmd(cfg *config.Config) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost mcp edit <name> -- <command> [args...]")
		fmt.Println("       ghost mcp edit <name> --http <url>")
		return
	}
	name := os.Args[3]
	if _, exists := cfg.Tools.MCP.Servers[name]; !exists {
		fmt.Printf("✗ MCP server '%s' not found\n", name)
		return
	}
	server, ok := buildMCPServerFromArgs(os.Args[4:])
	if !ok {
		return
	}
	server.Enabled = cfg.Tools.MCP.Servers[name].Enabled
	cfg.Tools.MCP.Servers[name] = server

	if err := saveConfigFromCLI(cfg); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ MCP server '%s' updated\n", name)
}

func mcpRemoveCmd(cfg *config.Config) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost mcp remove <name>")
		return
	}
	name := os.Args[3]
	if _, exists := cfg.Tools.MCP.Servers[name]; !exists {
		fmt.Printf("✗ MCP server '%s' not found\n", name)
		return
	}
	delete(cfg.Tools.MCP.Servers, name)
	if err := saveConfigFromCLI(cfg); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ MCP server '%s' removed\n", name)
}

func mcpTestCmd(cfg *config.Config) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost mcp test <name>")
		return
	}
	name := os.Args[3]
	server, exists := cfg.Tools.MCP.Servers[name]
	if !exists {
		fmt.Printf("✗ MCP server '%s' not found\n", name)
		return
	}

	manager := mcp.NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := manager.ConnectServer(ctx, name, server); err != nil {
		fmt.Printf("✗ Failed to connect to '%s': %v\n", name, err)
		return
	}
	defer manager.Close()

	tools := manager.ListToolInfos()
	fmt.Printf("✓ Connected to '%s': %d tool(s)\n", name, len(tools))
	for _, ti := range tools {
		fmt.Printf("  - %s\n", ti.Tool.Name)
	}
}

// buildMCPServerFromArgs parses CLI args into an MCPServerConfig.
// Supports: <name> -- <command> [args...]  OR  <name> --http <url>
func buildMCPServerFromArgs(args []string) (config.MCPServerConfig, bool) {
	if len(args) == 0 {
		fmt.Println("Missing server configuration.")
		return config.MCPServerConfig{}, false
	}

	// HTTP mode: --http <url>
	if args[0] == "--http" {
		if len(args) < 2 {
			fmt.Println("Usage: ghost mcp add <name> --http <url>")
			return config.MCPServerConfig{}, false
		}
		return config.MCPServerConfig{
			HTTP:    true,
			HTTPURL: args[1],
			Enabled: true,
		}, true
	}

	// stdio mode: -- <command> [args...]
	if args[0] != "--" {
		fmt.Println("Expected '--' before the command. Usage: ghost mcp add <name> -- <command> [args...]")
		return config.MCPServerConfig{}, false
	}
	if len(args) < 2 {
		fmt.Println("Missing command after '--'.")
		return config.MCPServerConfig{}, false
	}
	return config.MCPServerConfig{
		Command: args[1],
		Args:    args[2:],
		Enabled: true,
	}, true
}

// saveConfigFromCLI persists the config (and split secrets) to disk.
func saveConfigFromCLI(cfg *config.Config) error {
	return config.SaveConfig(getConfigPath(), cfg)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// wantsHelp reports whether the first argument is a help request. Command
// groups call it before dispatching so `ghost <group> --help` prints help
// cleanly instead of "Unknown <group> command: --help".
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "--help", "-h", "help":
			return true
		}
		return false // only the first token decides
	}
	return false
}

func skillsHelp() {
	fmt.Println("\nUsage: ghost skills <command> [args]")
	fmt.Println()
	fmt.Println("  list [--builtin]        List installed skills (or bundled ones with --builtin)")
	fmt.Println("  add <source> [--copy]   Install a skill (owner/repo[@skill][#ref], git URL, local path)")
	fmt.Println("  remove <name>           Remove an installed skill")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println("  search [query]          Search available skills in the registry")
	fmt.Println("  update [name] [--apply] Check (or apply) upstream updates for locked skills")
	fmt.Println("  sync                    Re-seed bundled skills (preserves user edits)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ghost skills list")
	fmt.Println("  ghost skills add sipeed/Ghost-skills")
	fmt.Println("  ghost skills search weather")
	fmt.Println("  ghost skills add --builtin")
	fmt.Println("  ghost skills remove weather")
	fmt.Println()
	fmt.Println("Legacy aliases: install (-> add), uninstall (-> remove),")
	fmt.Println("install-builtin (-> add --builtin), list-builtin (-> list --builtin).")
}

func skillsListCmd(loader *skills.SkillsLoader) {
	allSkills := loader.ListSkills()

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	fmt.Println("\nInstalled Skills:")
	fmt.Println("------------------")
	for _, skill := range allSkills {
		fmt.Printf("  ✓ %s (%s)\n", skill.Name, skill.Source)
		if skill.Description != "" {
			fmt.Printf("    %s\n", skill.Description)
		}
	}
}

func skillsInstallCmd(installer *skills.SkillInstaller, workspace string) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost skills install <github-repo>")
		fmt.Println("Example: ghost skills install sipeed/ghost-skills/weather")
		return
	}

	repo := os.Args[3]
	fmt.Printf("Installing skill from %s...\n", repo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installer.InstallFromGitHub(ctx, repo); err != nil {
		fmt.Printf("✗ Failed to install skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' installed successfully!\n", filepath.Base(repo))
	cliEmitSkillEvent(workspace, cevents.SkillInstalled, filepath.Base(repo))
}

// skillsAddCmd installs through the package-manager distributor:
// canonical store + symlink, dual locks, bounded fetch. Explicit
// command invocation is consent; the command never prompts (it fails
// loudly instead), so it is safe in pipelines and agents.
func skillsAddCmd(workspace string, args []string) {
	copyMode := false
	var sources []string
	for _, a := range args {
		if a == "--copy" {
			copyMode = true
			continue
		}
		if a == "-y" || a == "--yes" {
			continue
		}
		sources = append(sources, a)
	}
	if len(sources) != 1 {
		fmt.Println("Usage: ghost skills add <source> [--copy]")
		fmt.Println("  source: owner/repo[@skill][/path][#ref], git URL, local path, host@skill")
		fmt.Println("Example: ghost skills add vercel-labs/agent-skills@web-design-guidelines")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dataDir := skills.StoreBaseDir(workspace)
	slug, err := skills.InstallSource(ctx, dataDir, workspace, sources[0], copyMode)
	if err != nil {
		fmt.Printf("✗ Failed to add skill: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Skill '%s' added (locked).\n", slug)
	cliEmitSkillEvent(workspace, cevents.SkillInstalled, slug)
}

// skillsUpdateCmd checks locked skills against upstream; --apply
// reinstalls the ones that moved.
func skillsUpdateCmd(workspace string, args []string) {
	apply := false
	var names []string
	for _, a := range args {
		if a == "--apply" {
			apply = true
			continue
		}
		names = append(names, a)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dataDir := skills.StoreBaseDir(workspace)
	targets := names
	if len(targets) == 0 {
		var err error
		targets, err = skills.LockedSlugs(dataDir)
		if err != nil {
			fmt.Printf("✗ Cannot read skill lock: %v\n", err)
			os.Exit(1)
		}
	}
	if len(targets) == 0 {
		fmt.Println("No locked skills. Use: ghost skills add <source>")
		return
	}
	changed := 0
	for _, slug := range targets {
		current, latest, moved, err := skills.UpdateCheck(ctx, dataDir, slug)
		if err != nil {
			fmt.Printf("  ? %s: check failed: %v\n", slug, err)
			continue
		}
		if !moved {
			fmt.Printf("  = %s: up to date\n", slug)
			continue
		}
		changed++
		fmt.Printf("  ≠ %s: %s -> %s\n", slug, shortID(current), shortID(latest))
		if !apply {
			continue
		}
		source, serr := skills.LockedSource(dataDir, slug)
		if serr != nil {
			fmt.Printf("    update failed: %v\n", serr)
			continue
		}
		fmt.Printf("    updating %s...\n", slug)
		if _, uerr := skills.InstallSource(ctx, dataDir, workspace, source, skills.InstalledCopyMode(workspace, slug)); uerr != nil {
			fmt.Printf("    update failed: %v\n", uerr)
			continue
		}
		fmt.Printf("    ✓ %s updated\n", slug)
	}
	if changed > 0 && !apply {
		fmt.Println("Re-run with --apply to update.")
	}
}

func shortID(id string) string {
	if i := strings.Index(id, ":"); i >= 0 && i+9 < len(id) {
		return id[:i+1] + id[i+1:i+9]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func skillsRemoveCmd(installer *skills.SkillInstaller, skillName, workspace string) {
	fmt.Printf("Removing skill '%s'...\n", skillName)

	if err := installer.Uninstall(skillName); err != nil {
		fmt.Printf("✗ Failed to remove skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' removed successfully!\n", skillName)
	cliEmitSkillEvent(workspace, cevents.SkillRemoved, skillName)
}

// cliEmitSkillEvent records a skill lifecycle event from the CLI into the
// workspace's canonical event stream so owner-driven CLI changes carry the
// same audit trail as gateway/UI changes. Best-effort: if there is no Ghost
// database yet, there is nothing to record and the CLI result still stands.
func cliEmitSkillEvent(ws string, typ cevents.Type, name string) {
	path := filepath.Join(ws, "ghost.db")
	if _, err := os.Stat(path); err != nil {
		return
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='canonical_events'`).Scan(&n); err != nil || n == 0 {
		return
	}
	st, err := cevents.Open(db, filepath.Join(ws, "events"))
	if err != nil {
		return
	}
	st.Publish(&cevents.Event{Type: typ, AgentID: "cli",
		Payload: map[string]interface{}{"name": name}})
}

func skillsInstallBuiltinCmd(workspace string) {
	// Builtin skills live in the embedded workspace FS; install through the
	// same manifest-aware sync as `skills sync` so user edits are preserved
	// and the result is reported honestly (no blind overwrites, no fake
	// per-skill success lines).
	fmt.Println("Installing builtin skills to workspace...")
	syncEmbeddedSkills(workspace)
	fmt.Println("\n✓ Builtin skills installed (user-modified skills were preserved).")
}

func skillsListBuiltinCmd() {
	// Builtin skills live in the embedded workspace FS (the same source
	// `skills sync` seeds from). Never resolve them from a hand-built
	// filesystem path: those pointed at layouts that no longer exist.
	sub, err := fs.Sub(embeddedFiles, "workspace/skills")
	if err != nil {
		fmt.Printf("Error loading bundled skills: %v\n", err)
		return
	}

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")

	type builtinSkill struct{ name, description string }
	var found []builtinSkill
	walkErr := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "SKILL.md" {
			return nil
		}
		// Top-level skill directories only (nested containers are not skills).
		if dir := filepath.Dir(path); dir == "." || strings.Contains(dir, "/") {
			return nil
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return nil
		}
		name := filepath.Dir(path)
		description := "No description"
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if rest, ok := strings.CutPrefix(line, "description:"); ok {
				if desc := strings.Trim(strings.TrimSpace(rest), `"'`); desc != "" {
					description = desc
				}
				break
			}
		}
		found = append(found, builtinSkill{name: name, description: description})
		return nil
	})
	if walkErr != nil {
		fmt.Printf("Error reading bundled skills: %v\n", walkErr)
		return
	}
	if len(found) == 0 {
		fmt.Println("No builtin skills available.")
		return
	}
	sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })
	for _, s := range found {
		fmt.Printf("  ✓  %s\n", s.name)
		fmt.Printf("     %s\n", s.description)
	}
}

func skillsSearchCmd(cfg *config.Config, query string) {
	fmt.Println("Searching for available skills...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Search the live ClawHub registry (the source of truth). A previous
	// implementation queried a static skills.json snapshot that no longer
	// exists upstream, so search always failed.
	registry := skills.NewClawHubRegistry(cfg.Skills.ClawHub)
	results, err := registry.Search(ctx, query, 20)
	if err != nil {
		fmt.Printf("✗ Skill search failed: %v\n", err)
		return
	}
	if len(results) == 0 {
		fmt.Println("No skills found.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(results))
	fmt.Println("--------------------")
	for _, skill := range results {
		fmt.Printf("  📦 %s\n", skill.DisplayName)
		if skill.Summary != "" {
			fmt.Printf("     %s\n", skill.Summary)
		}
		if skill.Version != "" {
			fmt.Printf("     Slug: %s (version %s)\n", skill.Slug, skill.Version)
		} else {
			fmt.Printf("     Slug: %s\n", skill.Slug)
		}
		fmt.Println()
	}
}

func skillsShowCmd(loader *skills.SkillsLoader, skillName string) {
	content, ok := loader.LoadSkill(skillName)
	if !ok {
		fmt.Printf("✗ Skill '%s' not found\n", skillName)
		return
	}

	fmt.Printf("\n📦 Skill: %s\n", skillName)
	fmt.Println("----------------------")
	fmt.Println(content)
}

// runMaintenanceLoop runs retention at startup and every 24h, and sweeps
// expired permission requests every 15 minutes. Conservative oldest-first
// cleanup only; canonical memory and active task state are never touched.
func runMaintenanceLoop(db *sql.DB, workspace string) {
	rep := maintenance.Run(workspace, db)
	logger.InfoCF("maintenance", "retention run", map[string]interface{}{"actions": len(rep.Actions)})
	sweep := time.NewTicker(15 * time.Minute)
	defer sweep.Stop()
	daily := time.NewTicker(24 * time.Hour)
	defer daily.Stop()
	for {
		select {
		case <-sweep.C:
			// Expired approvals linger as rows until read; sweep them so the
			// request table reflects reality and the UI stops showing them.
			if b, err := permBroker(); err == nil {
				if n := b.SweepExpires(); n > 0 {
					logger.InfoCF("maintenance", "expired permission requests", map[string]interface{}{"count": n})
				}
			}
		case <-daily.C:
			rep := maintenance.Run(workspace, db)
			logger.InfoCF("maintenance", "retention run", map[string]interface{}{"actions": len(rep.Actions)})
		}
	}
}

// routineEventDir keeps canonical routine event logs beside other state.
func routineEventDir(workspace string) string {
	if workspace != "" {
		return workspace + "/events"
	}
	return "./events"
}

// executeRoutine runs one scheduled routine occurrence through the same
// pipeline as an interactive request and delivers the result back to the
// routine's origin channel. Idempotent per occurrence; permission blocks
// park the run as waiting (never hang, never double-execute).
func executeRoutine(ctx context.Context, agentLoop *agent.AgentLoop, msgBus *bus.MessageBus, routineSvc *routines.Service, broker *permissions.Broker, events *cevents.Stream, r *routines.Routine, item *scheduled.ScheduledItem) error {
	fireAt := time.Now()
	if item.NextRunAt != nil {
		fireAt = *item.NextRunAt
	}
	execKey := scheduled.ExecutionKey(item.ID, fireAt)
	sessionKey := "routine:" + r.ID
	channel := item.Channel
	if channel == "" {
		channel = "routine"
	}
	// One trajectory for the whole routine run: the routine events and the
	// agent turn it drives share it, so a scheduled failure is replayable.
	trajectoryID := turnlog.NewTrajectoryID()
	ctx = turnlog.WithTrajectoryID(ctx, trajectoryID)
	emit := func(typ cevents.Type, status string, summary string) {
		if events == nil {
			return
		}
		payload := map[string]interface{}{}
		if summary != "" {
			payload["summary"] = summary
		}
		events.Publish(&cevents.Event{Type: typ, GhostID: r.GhostID, AgentID: "agent-main",
			RoutineID: r.ID, TrajectoryID: trajectoryID, Status: status, Payload: payload})
	}
	emit(cevents.RoutineStarted, "running", r.Name+" started")
	outcome, err := routineSvc.Run(ctx, r.ID, execKey, func(ctx context.Context, r *routines.Routine) routines.RunOutcome {
		agentLoop.SetRoutineContext(sessionKey, r.ID, r.AllowedCapabilities)
		defer agentLoop.ClearRoutineContext(sessionKey)
		resp, err := agentLoop.ProcessDirectWithChannel(ctx, r.Instruction, sessionKey, channel, item.ChatID, nil, nil, nil)
		if err != nil {
			return routines.RunOutcome{Completion: product.CompletionFailed, Message: "routine execution failed"}
		}
		// Runtime evidence of a permission block (not text parsing): a
		// fresh pending request for this run's session.
		if broker != nil {
			if pending, ok := broker.PendingForSession(sessionKey); ok && pending.Status == permissions.StatusPending {
				return routines.RunOutcome{Completion: product.CompletionWaitingForPermission, WaitingOn: pending.ID}
			}
		}
		// Deliver the result where the routine was created.
		if strings.TrimSpace(resp) != "" && msgBus != nil && item.Channel != "" {
			msgBus.PublishOutbound(bus.OutboundMessage{Channel: item.Channel, ChatID: item.ChatID, Content: resp})
		}
		return routines.RunOutcome{Completion: product.CompletionSuccess, Message: resp}
	})
	if err != nil {
		emit(cevents.RoutineFailed, "failed", r.Name+" failed")
		return err
	}
	switch outcome.Completion {
	case product.CompletionSuccess:
		emit(cevents.RoutineCompleted, "success", r.Name+" done")
	case product.CompletionWaitingForPermission, product.CompletionWaitingForConfig,
		product.CompletionWaitingForAuth, product.CompletionWaitingForUser:
		emit(cevents.RoutineWaiting, "waiting", r.Name+" waiting")
	default:
		emit(cevents.RoutineFailed, "failed", r.Name+" failed")
	}
	return nil
}

// verifyCmd runs the canonical personal AI verification suite: real
// product checks (identity, memory, capabilities, governance,
// automation, events, activity, credentials, offline, providers,
// security) against a scratch device plus the live workspace.
func verifyCmd() {
	asJSON := false
	live := false
	for _, a := range os.Args[2:] {
		if a == "--json" {
			asJSON = true
		}
		if a == "--live" {
			live = true
		}
	}
	workspace := ""
	if cfg, err := loadConfig(); err == nil {
		workspace = cfg.WorkspacePath()
	}
	rep := verify.Run(verify.Options{Workspace: workspace, Live: live, Timeout: 5 * time.Minute})
	if asJSON {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Print(verify.Render(rep))
	}
	if rep.Overall != "PASS" {
		os.Exit(1)
	}
}

// benchmarkCmd runs the personal AI benchmark, prints the core score,
// and appends to local history for change→compare loops.
func benchmarkCmd() {
	asJSON := false
	save := true
	for _, a := range os.Args[2:] {
		if a == "--json" {
			asJSON = true
		}
		if a == "--no-save" {
			save = false
		}
	}
	workspace := ""
	if cfg, err := loadConfig(); err == nil {
		workspace = cfg.WorkspacePath()
	}
	if workspace == "" {
		workspace = "./workspace"
	}
	rep := bench.Run(workspace, 5*time.Minute)
	if asJSON {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Print(bench.Render(rep))
	}
	if save {
		hist, err := bench.SaveHistory(workspace, rep, 20)
		if err != nil {
			fmt.Printf("warning: history not saved: %v\n", err)
		} else if len(hist) >= 2 {
			fmt.Print("\nCHANGE VS PREVIOUS\n")
			fmt.Print(bench.Compare(hist[len(hist)-2], hist[len(hist)-1]))
		}
	}
	if rep.Overall != "PASS" {
		os.Exit(1)
	}
}

// friendlyAgentError maps a turn's transport/provider error to product
// language the CLI user can act on, without hiding the underlying cause
// when debugging. Provider errors may quote endpoints or timeouts, never
// secrets — still, we surface only the category to a normal user.
func friendlyAgentError(err error) string {
	if err == nil {
		return "something went wrong."
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "network is unreachable"), strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "client.timeout"):
		return "Ghost couldn't reach its thinking engine right now. Check that your local AI is running, then try again."
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"),
		strings.Contains(msg, "api key"), strings.Contains(msg, "invalid key"):
		return "Ghost's thinking engine rejected its credentials. Reconnect it in Ghost settings, then try again."
	default:
		// Fall back to the raw text only when it cannot be a transport
		// secret; keep CLI honest but terse.
		return err.Error()
	}
}
