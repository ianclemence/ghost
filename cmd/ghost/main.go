package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/chzyer/readline"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/auth"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/cron"
	"github.com/ianclemence/ghost/pkg/devices"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/heartbeat"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/mcp"
	"github.com/ianclemence/ghost/pkg/migrate"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/relayclient"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/state"
	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/voice"
	"github.com/joho/godotenv"
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
		fmt.Printf("✓ Loaded .env (from current dir)\n")
		envLoaded = true
	} else if err := godotenv.Load("../.env"); err == nil {
		fmt.Printf("✓ Loaded .env (from parent dir)\n")
		envLoaded = true
	}

	if configDir := os.Getenv("GHOST_CONFIG_DIR"); configDir != "" {
		if err := godotenv.Load(filepath.Join(configDir, ".env")); err == nil {
			fmt.Printf("✓ Loaded .env (from GHOST_CONFIG_DIR)\n")
			envLoaded = true
		}
	}

	if !envLoaded {
		if home, err := os.UserHomeDir(); err == nil {
			if err := godotenv.Load(filepath.Join(home, ".ghost", ".env")); err == nil {
				fmt.Printf("✓ Loaded .env (from ~/.ghost)\n")
			} else if err := godotenv.Load(filepath.Join(home, "ghost", ".env")); err == nil {
				fmt.Printf("✓ Loaded .env (from ~/ghost)\n")
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
	case "gateway":
		gatewayCmd()
	case "status":
		statusCmd()
	case "migrate":
		migrateCmd()
	case "reset-password":
		resetPasswordCmd()
	case "auth":
		authCmd()
	case "cron":
		cronCmd()
	case "mcp":
		mcpCmd()
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
			skillsInstallCmd(installer)
		case "remove", "uninstall":
			if len(os.Args) < 4 {
				fmt.Println("Usage: ghost skills remove <skill-name>")
				return
			}
			skillsRemoveCmd(installer, os.Args[3])
		case "install-builtin":
			skillsInstallBuiltinCmd(workspace)
		case "sync":
			syncEmbeddedSkills(workspace)
			fmt.Println("\n✓ Bundled skills synced (user-modified skills were preserved).")
		case "list-builtin":
			skillsListBuiltinCmd()
		case "search":
			skillsSearchCmd(installer)
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
	fmt.Println("  dashboard   Launch the operator TUI")
	fmt.Println("  gateway     Start Ghost gateway")
	fmt.Println("  status      Show Ghost status")
	fmt.Println("  update      Pull latest changes and rebuild")
	fmt.Println("  updater     Run auto-update daemon")
	fmt.Println("  auth        Manage authentication (login, logout, status)")
	fmt.Println("  reset-password  Reset the admin dashboard password (requires --force)")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  migrate     Migrate from OpenClaw to Ghost")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  state       Export, import, or inspect Ghost State archives")
	fmt.Println("  relay       Manage relay connection (run, pair, clients)")
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

func agentCmd() {
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

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)
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
		response, err := agentLoop.ProcessDirect(ctx, message, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
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
			fmt.Printf("Error: %v\n", err)
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
			fmt.Printf("Error: %v\n", err)
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

	noCron := false
	apiOnly := false

	// Check for flags
	args := os.Args[2:]
	for _, arg := range args {
		if arg == "--debug" || arg == "-d" {
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		}
		if arg == "--no-cron" {
			noCron = true
			fmt.Println("🕒 Cron service disabled")
		}
		if arg == "--api-only" {
			apiOnly = true
			noCron = true
			fmt.Println("🔌 API Only mode enabled (Cron, Channels, Heartbeat disabled)")
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
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)
	agentLoop.SetConfigPath(getConfigPath())

	// Sync bundled skills before starting so devices pick up new bundled
	// skills after updates without ever stomping user edits.
	syncEmbeddedSkills(cfg.WorkspacePath())

	// Mint or load the persistent, hardware-independent Ghost identity. This
	// is idempotent: the ghost_id is created once and then preserved for the
	// life of the Ghost, surviving upgrades and migrations.
	if _, err := ghoststate.EnsureIdentity(cfg.WorkspacePath()); err != nil {
		fmt.Printf("⚠️  Could not ensure Ghost identity: %v\n", err)
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

	// Setup cron tool and service
	cronService := setupCronTool(agentLoop, msgBus, cfg.WorkspacePath())

	heartbeatService := heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	heartbeatService.SetCronService(cronService)
	heartbeatService.SetBus(msgBus)
	heartbeatService.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
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
		if cfg.Providers.Moonshot.APIKey != "" {
			transcriber = voice.NewKimiTranscriber(cfg.Providers.Moonshot.APIKey)
			logger.InfoC("voice", "Kimi voice transcription enabled")
		} else if cfg.Providers.Groq.APIKey != "" {
			transcriber = voice.NewGroqTranscriber(cfg.Providers.Groq.APIKey)
			logger.InfoC("voice", "Groq voice transcription enabled")
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

	if !noCron {
		if err := cronService.Start(); err != nil {
			fmt.Printf("Error starting cron service: %v\n", err)
		}
		fmt.Println("✓ Cron service started")
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
	go startInternalAPI(agentLoop, cronService, channelManager)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	fmt.Println("\nShutting down...")
	cancel()
	deviceService.Stop()
	if !apiOnly {
		heartbeatService.Stop()
	}
	if !noCron {
		cronService.Stop()
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

func setupCronTool(agentLoop *agent.AgentLoop, msgBus *bus.MessageBus, workspace string) *cron.CronService {
	cronStorePath := filepath.Join(workspace, "cron", "jobs.json")

	// Create cron service
	cronService := cron.NewCronService(cronStorePath, nil, msgBus)

	// Create and register CronTool
	cronTool := tools.NewCronTool(cronService, agentLoop, msgBus, workspace)
	agentLoop.RegisterTool(cronTool)

	// Set the onJob handler
	cronService.SetOnJob(func(job *cron.CronJob) (string, error) {
		return cronTool.ExecuteJob(context.Background(), job)
	})

	// Auto-discover scheduled workflows
	sl := skills.NewSkillsLoader(workspace, "", "")
	existingJobs := cronService.ListJobs(true)
	existingNames := make(map[string]bool)
	for _, j := range existingJobs {
		existingNames[j.Name] = true
	}

	for _, skill := range sl.ListSkills() {
		if skill.Schedule != "" {
			workflowName := "Workflow: " + skill.Name
			if !existingNames[workflowName] {
				parsedCron := cron.ParseSchedule(skill.Schedule)
				if parsedCron != "" {
					cronService.AddJob(workflowName, cron.CronSchedule{Kind: "cron", Expr: parsedCron}, fmt.Sprintf("Execute the %s skill", skill.Name), true, "", "", nil)
					logger.InfoCF("cron", "Auto-discovered scheduled workflow", map[string]interface{}{
						"name": skill.Name,
						"cron": parsedCron,
					})
				} else {
					logger.InfoCF("cron", "Failed to parse schedule for workflow", map[string]interface{}{
						"name":     skill.Name,
						"schedule": skill.Schedule,
					})
				}
			}
		}
	}

	return cronService
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

func cronCmd() {
	if len(os.Args) < 3 {
		cronHelp()
		return
	}

	subcommand := os.Args[2]

	// Load config to get workspace path
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	cronStorePath := filepath.Join(cfg.WorkspacePath(), "cron", "jobs.json")

	switch subcommand {
	case "list":
		cronListCmd(cronStorePath)
	case "add":
		cronAddCmd(cronStorePath)
	case "remove":
		if len(os.Args) < 4 {
			fmt.Println("Usage: ghost cron remove <job_id>")
			return
		}
		cronRemoveCmd(cronStorePath, os.Args[3])
	case "enable":
		cronEnableCmd(cronStorePath, false)
	case "disable":
		cronEnableCmd(cronStorePath, true)
	default:
		fmt.Printf("Unknown cron command: %s\n", subcommand)
		cronHelp()
	}
}

func cronHelp() {
	fmt.Println("\nCron commands:")
	fmt.Println("  list              List all scheduled jobs")
	fmt.Println("  add              Add a new scheduled job")
	fmt.Println("  remove <id>       Remove a job by ID")
	fmt.Println("  enable <id>      Enable a job")
	fmt.Println("  disable <id>     Disable a job")
	fmt.Println()
	fmt.Println("Add options:")
	fmt.Println("  -n, --name       Job name")
	fmt.Println("  -m, --message    Message for agent")
	fmt.Println("  -e, --every      Run every N seconds")
	fmt.Println("  -c, --cron       Cron expression (e.g. '0 9 * * *')")
	fmt.Println("  -d, --deliver     Deliver response to channel")
	fmt.Println("  --to             Recipient for delivery")
	fmt.Println("  --channel        Channel for delivery")
	fmt.Println("  --skills         Comma-separated skills to load (e.g. 'planning,code-review')")
	fmt.Println("  --no-agent       Run script directly without agent (script IS the job)")
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
	fmt.Println("  revoke <token>   Revoke a client's access")
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
	fmt.Printf("     ghost-relay add-device %s --name \"My Ghost\"\n", ghostID.GhostID)
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
	for _, arg := range os.Args[3:] {
		switch arg {
		case "--force":
			force = true
		case "--help", "-h":
			stateHelp()
			return
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Printf("Unknown flag: %s\n", arg)
				stateHelp()
				return
			}
			src = arg
		}
	}
	if src == "" {
		fmt.Println("Usage: ghost state import <archive> [--force]")
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
	})
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

func cronListCmd(storePath string) {
	cs := cron.NewCronService(storePath, nil, nil)
	jobs := cs.ListJobs(true) // Show all jobs, including disabled

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}

	fmt.Println("\nScheduled Jobs:")
	fmt.Println("----------------")
	for _, job := range jobs {
		var schedule string
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "cron" {
			schedule = job.Schedule.Expr
		} else {
			schedule = "one-time"
		}

		nextRun := "scheduled"
		if job.State.NextRunAtMS != nil {
			nextTime := time.UnixMilli(*job.State.NextRunAtMS)
			nextRun = nextTime.Format("2006-01-02 15:04")
		}

		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}

		fmt.Printf("  %s (%s)\n", job.Name, job.ID)
		fmt.Printf("    Schedule: %s\n", schedule)
		fmt.Printf("    Status: %s\n", status)
		fmt.Printf("    Next run: %s\n", nextRun)

		if len(job.Skills) > 0 {
			fmt.Printf("    Skills: %s\n", strings.Join(job.Skills, ", "))
		}
		if job.NoAgent {
			fmt.Printf("    Mode: no-agent (script execution)\n")
		}
	}
}

func cronAddCmd(storePath string) {
	name := ""
	message := ""
	var everySec *int64
	cronExpr := ""
	deliver := false
	channel := ""
	to := ""
	skillsStr := ""
	noAgent := false

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-e", "--every":
			if i+1 < len(args) {
				var sec int64
				fmt.Sscanf(args[i+1], "%d", &sec)
				everySec = &sec
				i++
			}
		case "-c", "--cron":
			if i+1 < len(args) {
				cronExpr = args[i+1]
				i++
			}
		case "-d", "--deliver":
			deliver = true
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		case "--skills":
			if i+1 < len(args) {
				skillsStr = args[i+1]
				i++
			}
		case "--no-agent":
			noAgent = true
		}
	}

	if name == "" {
		fmt.Println("Error: --name is required")
		return
	}

	if message == "" {
		fmt.Println("Error: --message is required")
		return
	}

	if everySec == nil && cronExpr == "" {
		fmt.Println("Error: Either --every or --cron must be specified")
		return
	}

	var schedule cron.CronSchedule
	if everySec != nil {
		everyMS := *everySec * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}
	} else {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
		}
	}

	var skills []string
	if skillsStr != "" {
		skills = strings.Split(skillsStr, ",")
		for i := range skills {
			skills[i] = strings.TrimSpace(skills[i])
		}
	}

	cs := cron.NewCronService(storePath, nil, nil)
	job, err := cs.AddJobWithOptions(name, schedule, message, deliver, channel, to, nil, skills, noAgent, "")
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}

	fmt.Printf("✓ Added job '%s' (%s)\n", job.Name, job.ID)
}

func cronRemoveCmd(storePath, jobID string) {
	cs := cron.NewCronService(storePath, nil, nil)
	if cs.RemoveJob(jobID) {
		fmt.Printf("✓ Removed job %s\n", jobID)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func cronEnableCmd(storePath string, disable bool) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ghost cron enable/disable <job_id>")
		return
	}

	jobID := os.Args[3]
	cs := cron.NewCronService(storePath, nil, nil)
	enabled := !disable

	job := cs.EnableJob(jobID, enabled)
	if job != nil {
		status := "enabled"
		if disable {
			status = "disabled"
		}
		fmt.Printf("✓ Job '%s' %s\n", job.Name, status)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
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
	fmt.Println("  install <repo>          Install skill from GitHub")
	fmt.Println("  install-builtin          Install all builtin skills to workspace")
	fmt.Println("  list-builtin             List available builtin skills")
	fmt.Println("  remove <name>           Remove installed skill")
	fmt.Println("  search                  Search available skills")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ghost skills list")
	fmt.Println("  ghost skills install sipeed/ghost-skills/weather")
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

func skillsInstallCmd(installer *skills.SkillInstaller) {
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
}

func skillsRemoveCmd(installer *skills.SkillInstaller, skillName string) {
	fmt.Printf("Removing skill '%s'...\n", skillName)

	if err := installer.Uninstall(skillName); err != nil {
		fmt.Printf("✗ Failed to remove skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' removed successfully!\n", skillName)
}

func skillsInstallBuiltinCmd(workspace string) {
	builtinSkillsDir := "./ghost/skills"
	workspaceSkillsDir := filepath.Join(workspace, "skills")

	fmt.Printf("Copying builtin skills to workspace...\n")

	skillsToInstall := []string{
		"weather",
		"news",
		"stock",
		"calculator",
	}

	for _, skillName := range skillsToInstall {
		builtinPath := filepath.Join(builtinSkillsDir, skillName)
		workspacePath := filepath.Join(workspaceSkillsDir, skillName)

		if _, err := os.Stat(builtinPath); err != nil {
			fmt.Printf("⊘ Builtin skill '%s' not found: %v\n", skillName, err)
			continue
		}

		if err := os.MkdirAll(workspacePath, 0755); err != nil {
			fmt.Printf("✗ Failed to create directory for %s: %v\n", skillName, err)
			continue
		}

		if err := copyDirectory(builtinPath, workspacePath); err != nil {
			fmt.Printf("✗ Failed to copy %s: %v\n", skillName, err)
		}
	}

	fmt.Println("\n✓ All builtin skills installed!")
	fmt.Println("Now you can use them in your workspace.")
}

func skillsListBuiltinCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	builtinSkillsDir := filepath.Join(filepath.Dir(cfg.WorkspacePath()), "ghost", "skills")

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("Error reading builtin skills: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No builtin skills available.")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			skillName := entry.Name()
			skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")

			description := "No description"
			if _, err := os.Stat(skillFile); err == nil {
				data, err := os.ReadFile(skillFile)
				if err == nil {
					content := string(data)
					if idx := strings.Index(content, "\n"); idx > 0 {
						firstLine := content[:idx]
						if strings.Contains(firstLine, "description:") {
							descLine := strings.Index(content[idx:], "\n")
							if descLine > 0 {
								description = strings.TrimSpace(content[idx+descLine : idx+descLine])
							}
						}
					}
				}
			}
			status := "✓"
			fmt.Printf("  %s  %s\n", status, entry.Name())
			if description != "" {
				fmt.Printf("     %s\n", description)
			}
		}
	}
}

func skillsSearchCmd(installer *skills.SkillInstaller) {
	fmt.Println("Searching for available skills...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	availableSkills, err := installer.ListAvailableSkills(ctx)
	if err != nil {
		fmt.Printf("✗ Failed to fetch skills list: %v\n", err)
		return
	}

	if len(availableSkills) == 0 {
		fmt.Println("No skills available.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(availableSkills))
	fmt.Println("--------------------")
	for _, skill := range availableSkills {
		fmt.Printf("  📦 %s\n", skill.Name)
		fmt.Printf("     %s\n", skill.Description)
		fmt.Printf("     Repo: %s\n", skill.Repository)
		if skill.Author != "" {
			fmt.Printf("     Author: %s\n", skill.Author)
		}
		if len(skill.Tags) > 0 {
			fmt.Printf("     Tags: %v\n", skill.Tags)
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
