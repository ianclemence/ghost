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
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/chzyer/readline"
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
	"github.com/ianclemence/ghost/pkg/devices"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/heartbeat"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/maintenance"
	"github.com/ianclemence/ghost/pkg/mcp"
	"github.com/ianclemence/ghost/pkg/migrate"
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

	switch command {
	case "onboard":
		onboard()
	case "agent":
		agentCmd()
	case "dashboard":
		runDashboard()
	case "serve", "gateway":
		gatewayCmd()
	case "status":
		statusCmd()
	case "model":
		modelCmd()
	case "migrate":
		migrateCmd()
	case "reset":
		resetCmd()
	case "reset-password":
		resetPasswordCmd()
	case "auth":
		authCmd()
	case "mcp":
		mcpCmd()
	case "stt":
		sttCmd()
	case "tts":
		ttsCmd()
	case "skills":
		if len(os.Args) < 3 {
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
			skillsListCmd(skillsLoader)
		case "install":
			skillsInstallCmd(installer, workspace)
		case "remove", "uninstall":
			if len(os.Args) < 4 {
				fmt.Println("Usage: ghost skills remove <skill-name>")
				return
			}
			skillsRemoveCmd(installer, os.Args[3], workspace)
		case "install-builtin":
			skillsInstallBuiltinCmd(workspace)
		case "sync":
			syncEmbeddedSkills(workspace)
			fmt.Println("\n✓ Bundled skills synced (user-modified skills were preserved).")
		case "list-builtin":
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
	case "updater":
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
		replayCmd()
	case "version", "--version", "-v":
		printVersion()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf("%s Ghost - Personal AI Assistant v%s\n\n", logo, version)
	fmt.Println("Usage: ghost <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  onboard     Initialize Ghost configuration and workspace")
	fmt.Println("  agent       Interact with the agent directly")
	fmt.Println("  serve       Start the Ghost daemon (API + channels + cron + heartbeat)")
	fmt.Println("  gateway     Legacy alias for serve")
	fmt.Println("  dashboard   Launch the operator TUI")
	fmt.Println("  status      Show Ghost status")
	fmt.Println("  model       View or switch the active model (model [list|use <provider:model>])")
	fmt.Println("  update      Pull latest changes and rebuild")
	fmt.Println("  updater     Run auto-update daemon")
	fmt.Println("  auth        Manage authentication (login, logout, status)")
	fmt.Println("  reset       Factory reset (e.g. ghost reset all --exclude=devices,secrets)")
	fmt.Println("  reset-password  Reset the admin dashboard password (requires --force)")
	fmt.Println("  mcp         Manage MCP servers (list, add, edit, remove, test)")
	fmt.Println("  migrate     Migrate from OpenClaw to Ghost")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  stt         Manage local speech-to-text (setup, status)")
	fmt.Println("  tts         Manage local speech synthesis (setup, status)")
	fmt.Println("  state       Export, import, or inspect Ghost State archives")
	fmt.Println("  relay       Manage relay connection (run, pair, clients)")
	fmt.Println("  verify      Run appliance verification (real product checks)")
	fmt.Println("  benchmark   Run appliance benchmark + core score")
	fmt.Println("  golden      Run the Golden Conversation Suite (model NL evaluation)")
	fmt.Println("  version     Show version information")
}

func onboard() {
	configPath := getConfigPath()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists at %s\n", configPath)
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

func migrateCmd() {
	if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
		migrateHelp()
		return
	}

	opts := migrate.Options{}

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.DryRun = true
		case "--config-only":
			opts.ConfigOnly = true
		case "--workspace-only":
			opts.WorkspaceOnly = true
		case "--force":
			opts.Force = true
		case "--refresh":
			opts.Refresh = true
		case "--openclaw-home":
			if i+1 < len(args) {
				opts.OpenClawHome = args[i+1]
				i++
			}
		case "--ghost-home":
			if i+1 < len(args) {
				opts.GhostHome = args[i+1]
				i++
			}
		default:
			fmt.Printf("Unknown flag: %s\n", args[i])
			migrateHelp()
			os.Exit(1)
		}
	}

	result, err := migrate.Run(opts)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if !opts.DryRun {
		migrate.PrintSummary(result)
	}
}

func migrateHelp() {
	fmt.Println("\nMigrate from OpenClaw to Ghost")
	fmt.Println()
	fmt.Println("Usage: ghost migrate [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --dry-run          Show what would be migrated without making changes")
	fmt.Println("  --refresh          Re-sync workspace files from OpenClaw (repeatable)")
	fmt.Println("  --config-only      Only migrate config, skip workspace files")
	fmt.Println("  --workspace-only   Only migrate workspace files, skip config")
	fmt.Println("  --force            Skip confirmation prompts")
	fmt.Println("  --openclaw-home    Override OpenClaw home directory (default: ~/.openclaw)")
	fmt.Println("  --ghost-home    Override Ghost home directory (default: ~/.ghost)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ghost migrate              Detect and migrate from OpenClaw")
	fmt.Println("  ghost migrate --dry-run    Show what would be migrated")
	fmt.Println("  ghost migrate --refresh    Re-sync workspace files")
	fmt.Println("  ghost migrate --force      Migrate without confirmation")
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
	if preset := cfg.FindModelPreset(target); preset != nil {
		provider = preset.Provider
		model = preset.Model
	} else if strings.Contains(target, ":") {
		parts := strings.SplitN(target, ":", 2)
		if parts[0] == "" || parts[1] == "" {
			fmt.Println("Invalid format — use provider:model (e.g. openai:gpt-4o).")
			os.Exit(1)
		}
		provider, model = parts[0], parts[1]
	}

	// Guard against typos: an unknown provider would silently produce a broken
	// config, so reject it up front.
	if provider != "" && !knownProviders[strings.ToLower(provider)] {
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
	sessionKey := "cli:default"
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
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
	// Seed bundled skills for a fresh workspace (idempotent, manifest-aware).
	syncEmbeddedSkills(cfg.WorkspacePath())

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
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
		fmt.Printf("%s Interactive mode (Ctrl+C to exit)\n\n", logo)
		interactiveMode(agentLoop, sessionKey)
	}
}

func interactiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	prompt := fmt.Sprintf("%s You: ", logo)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     filepath.Join(os.TempDir(), ".ghost_history"),
		HistoryLimit:    100,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		fmt.Println("Falling back to simple input mode...")
		simpleInteractiveMode(agentLoop, sessionKey)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
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
		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %s\n", friendlyAgentError(err))
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

func simpleInteractiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
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
		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %s\n", friendlyAgentError(err))
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
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
		fmt.Printf("Error creating provider: %v\n", err)
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
		fmt.Printf("❌ Database migration failed: %v\n", err)
		fmt.Println("   Ghost cannot start safely. Restore from a backup with `ghost state import` and try again.")
		os.Exit(1)
	} else {
		fmt.Printf("  • Database schema v%d\n", v)
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

	// Retention: keep the appliance responsible on small disks (SD card).
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

	configPath := getConfigPath()

	fmt.Printf("%s Ghost Status\n", logo)
	fmt.Printf("Version: %s\n", formatVersion())
	build, _ := formatBuildInfo()
	if build != "" {
		fmt.Printf("Build: %s\n", build)
	}
	fmt.Println()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)

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
	if len(os.Args) < 3 {
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
	if len(os.Args) < 3 {
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
	if len(os.Args) < 3 {
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
		return
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
		return
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
		return
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
	fmt.Println()
	fmt.Println("Import only runs on a fresh installation unless --force is given.")
	fmt.Println("Rebound (device-specific) state is never exported; secrets need --include-secrets.")
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
	fmt.Fprint(os.Stderr, prompt)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(strings.TrimSpace(response)) == "y"
}

// mcpCmd manages MCP servers from the CLI, mirroring the dashboard's MCP
// section for headless or scripted configuration.
func mcpCmd() {
	if len(os.Args) < 3 {
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

func skillsHelp() {
	fmt.Println("\nSkills commands:")
	fmt.Println("  list                    List installed skills")
	fmt.Println("  install <owner>/<repo>  Install skill from GitHub (root SKILL.md)")
	fmt.Println("  install-builtin          Install all builtin skills to workspace")
	fmt.Println("  list-builtin             List available builtin skills")
	fmt.Println("  remove <name>            Remove installed skill (alias: uninstall)")
	fmt.Println("  search [query]           Search available skills in the registry")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ghost skills list")
	fmt.Println("  ghost skills install sipeed/GHOST-skills")
	fmt.Println("  ghost skills search weather")
	fmt.Println("  ghost skills install-builtin")
	fmt.Println("  ghost skills list-builtin")
	fmt.Println("  ghost skills remove weather")
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

// verifyCmd runs the canonical appliance verification suite: real
// product checks (identity, memory, capabilities, governance,
// automation, events, activity, credentials, offline, providers,
// security) against a scratch appliance plus the live workspace.
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

// benchmarkCmd runs the appliance benchmark, prints the core score,
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
