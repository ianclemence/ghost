// Ghost - Ultra-lightweight personal AI agent
// Inspired by and based on GHOST: https://github.com/ianclemence/ghost
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/commands"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/constants"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/doctor"
	"github.com/ianclemence/ghost/pkg/evolution"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/mcp"
	"github.com/ianclemence/ghost/pkg/media"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/rag"
	"github.com/ianclemence/ghost/pkg/routing"
	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/state"
	"github.com/ianclemence/ghost/pkg/telemetry"
	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/utils"
)

type AgentLoop struct {
	bus              *bus.MessageBus
	provider         providers.LLMProvider
	workspace        string
	model            string
	temperature      float64
	maxTokens        int // Maximum output tokens for the LLM
	contextWindow    int // Maximum context window size in tokens
	maxIterations    int
	sessions         *session.SessionManager
	state            *state.Manager
	media            media.MediaStore
	contextBuilder   *ContextBuilder
	tools            *tools.ToolRegistry
	toolProfile      tools.ToolProfile
	commands         *commands.Registry
	commandExec      *commands.Executor
	router           *routing.Router
	fallback         *providers.FallbackChain
	fallbackModels   []providers.FallbackCandidate
	installer        *skills.SkillInstaller
	providersByModel map[string]providers.LLMProvider
	cfg              *config.Config
	configPath       string
	doctor           *doctor.Doctor
	running          atomic.Bool
	summarizing      sync.Map // Tracks which sessions are currently being summarized
	curator          *tools.Curator
	nudge            *NudgeManager
	evolution        *evolution.EvolutionManager
	steering         *SteeringManager
	// pcStore is the Personal Context store, opened once for the life of the
	// agent. It is a derived-memory layer: if opening it fails, the agent still
	// runs and extraction is skipped rather than blocking the turn.
	pcStore *personalcontext.Store
	db      *db.DB
}

// processOptions configures how a message is processed
type processOptions struct {
	SessionKey      string // Session identifier for history/context
	Channel         string // Target channel for tool execution
	ChatID          string // Target chat ID for tool execution
	ToolProfile     tools.ToolProfile
	IsCronTriggered bool
	UserMessage     string // User message content (may include prefix)
	DefaultResponse string // Response when LLM returns empty
	EnableSummary   bool   // Whether to trigger summarization
	SendResponse    bool   // Whether to send response via bus
	NoHistory       bool   // If true, don't load session history (for heartbeat)
	Media           []string
	Thinking        bool
	OnChunk         func(string)                   // New: callback for streaming chunks
	OnToolCall      func(name string, args string) // New: callback for tool calls
	RequestID       string                         // Unique request identifier for tracing
}

// createToolRegistry creates a tool registry with common tools.
// This is shared between main agent and subagents.
func createToolRegistry(workspace string, restrict bool, cfg *config.Config, msgBus *bus.MessageBus) *tools.ToolRegistry {
	registry := tools.NewToolRegistry()

	// File system tools
	registry.Register(tools.NewReadFileTool(workspace, false))
	registry.Register(tools.NewWriteFileTool(workspace, false))
	registry.Register(tools.NewListDirTool(workspace, false))
	registry.Register(tools.NewEditFileTool(workspace, false))
	registry.Register(tools.NewAppendFileTool(workspace, false))

	// Shell execution
	registry.RegisterHidden(tools.NewExecTool(workspace, false), 6*time.Hour)
	registry.Register(tools.NewUpdateTool(workspace))

	// Oracle context bundling
	registry.Register(tools.NewOracleTool(workspace, false))

	// Video frames extraction
	registry.Register(tools.NewVideoFramesTool(workspace, false))

	// Lanes (Isolated Contexts)
	registry.Register(tools.NewLaneTool(func(lane string) {
		// This callback will be handled by the agent loop if needed,
		// but for now, the tool just returns a success message to the LLM.
		// The LLM can then decide to use this information.
	}))

	// Interactive Browser Automation (CDP-based)
	registry.RegisterHidden(tools.NewBrowserTool(workspace, "navigate"), 2*time.Hour)
	registry.RegisterHidden(tools.NewBrowserTool(workspace, "snapshot"), 2*time.Hour)
	registry.RegisterHidden(tools.NewBrowserTool(workspace, "click"), 2*time.Hour)
	registry.RegisterHidden(tools.NewBrowserTool(workspace, "type"), 2*time.Hour)
	registry.RegisterHidden(tools.NewBrowserTool(workspace, "press"), 2*time.Hour)

	// Sandbox Execution Tool (Safe code running)
	registry.RegisterHidden(tools.NewSandboxTool(workspace), 2*time.Hour)

	// Networking & Discovery Tool (Tailscale, Bonjour)
	registry.Register(tools.NewNetworkingTool(workspace))

	// Voice Wake Word Control (Always-Listening)
	registry.Register(tools.NewVoiceWakeTool(func(active bool) {
		// This would ideally signal a background voice-processing service.
	}))

	// Context Compaction Tool (Proactive Session Summarization)
	registry.Register(tools.NewCompactionTool(func() error {
		// We'll trigger the summarization logic manually.
		// In a real implementation, we'd need to access the current sessionID and loop.
		return nil // For now, we'll just return a success message.
	}))

	// Canvas visual presence
	canvasTool := tools.NewCanvasTool(workspace, func(html string) {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: "system",
			ChatID:  "canvas",
			Content: html,
			Metadata: map[string]interface{}{
				"type": "canvas_update",
			},
		})
	})
	registry.Register(canvasTool)

	if searchTool := tools.NewWebSearchTool(tools.WebSearchToolOptions{
		BraveAPIKey:          cfg.Tools.Web.Brave.APIKey,
		BraveMaxResults:      cfg.Tools.Web.Brave.MaxResults,
		BraveEnabled:         cfg.Tools.Web.Brave.Enabled,
		DuckDuckGoMaxResults: cfg.Tools.Web.DuckDuckGo.MaxResults,
		DuckDuckGoEnabled:    cfg.Tools.Web.DuckDuckGo.Enabled,
	}); searchTool != nil {
		registry.Register(searchTool)
	}
	registry.Register(tools.NewWebFetchTool(50000))

	// Vision tool - image analysis
	registry.Register(tools.NewVisionTool(workspace))

	// Image Generation tool - DALL-E image creation
	openaiKey := cfg.Providers.OpenAI.APIKey
	if openaiKey == "" {
		openaiKey = os.Getenv("OPENAI_API_KEY")
	}
	if openaiKey != "" {
		imageGen := tools.NewImageGenTool(workspace, openaiKey, "")
		registry.Register(imageGen)
	}

	// Hardware tools (I2C, SPI) - Linux only, returns error on other platforms
	registry.Register(tools.NewI2CTool())
	registry.Register(tools.NewSPITool())

	// Message tool - available to both agent and subagent
	// Subagent uses it to communicate directly with user
	messageTool := tools.NewMessageTool()
	messageTool.SetSendCallback(func(channel, chatID, content string) error {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: content,
			Metadata: map[string]interface{}{
				"type": "assistant_message",
			},
		})
		return nil
	})
	registry.Register(messageTool)

	// Self-improving Skills — agent can create/patch/delete skills autonomously
	registry.Register(tools.NewSkillManageTool(workspace))

	// Bounded Curated Memory — agent can maintain a persistent, curated profile of the user and project
	registry.Register(tools.NewMemoryCurateTool(workspace))

	// Todo Tool — task decomposition and tracking
	registry.Register(tools.NewTodoTool())

	// Clarify Tool — interactive user questions with choices
	registry.Register(tools.NewClarifyTool(msgBus))

	// TTS Tool — text-to-speech conversion
	ttsConfig := tools.TTSConfig{
		Enabled:      true,
		Provider:     "edge-tts",
		DefaultVoice: "en-US-GuyNeural",
		OutputFormat: "mp3",
	}
	registry.Register(tools.NewTTSTool(ttsConfig, workspace))

	// Document Parser — .docx/.xlsx/.ipynb parsing
	registry.Register(tools.NewDocParserTool(workspace))

	if cfg.Tools.MCP.Enabled {
		manager := mcp.NewManager()
		if err := manager.LoadFromConfig(context.Background(), cfg); err == nil {
			for _, info := range manager.ListToolInfos() {
				registry.Register(tools.NewMCPTool(manager, info.Server, info.Tool))
			}
		}
	}

	// registry.SetToolEnabledForChannel("mobile", "exec", false)
	// registry.SetToolEnabledForChannel("mobile", "write_file", false)
	// registry.SetToolEnabledForChannel("telegram", "exec", false)
	// registry.SetToolEnabledForChannel("telegram", "write_file", false)

	return registry
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider) (*AgentLoop, error) {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	restrict := cfg.Agents.Defaults.RestrictToWorkspace

	// Create tool registry for main agent
	toolsRegistry := createToolRegistry(workspace, restrict, cfg, msgBus)

	// Create subagent manager with its own tool registry
	subagentManager := tools.NewSubagentManager(provider, cfg.Agents.Defaults.Model, workspace, msgBus)
	subagentTools := createToolRegistry(workspace, restrict, cfg, msgBus)
	// Subagent doesn't need spawn/subagent tools to avoid recursion
	subagentManager.SetTools(subagentTools)

	// Register spawn tool (for main agent)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)

	// Register subagent tool (synchronous execution)
	subagentTool := tools.NewSubagentTool(subagentManager)
	toolsRegistry.Register(subagentTool)

	// Register batch delegate tool (parallel execution)
	if cfg.Tools.Delegation.Enabled {
		batchDelegate := tools.NewBatchDelegateTool(subagentManager)
		toolsRegistry.Register(batchDelegate)
	}

	// Initialize DB
	database, err := db.NewDB(workspace)
	if err != nil {
		return nil, fmt.Errorf("initialize db: %w", err)
	}

	if cfg.Agents.Defaults.SearchEnabled {
		searchTool := tools.NewSessionSearchTool(database.DB)
		toolsRegistry.Register(searchTool)
		subagentTools.Register(searchTool)
	}

	// Initialize RAG
	var ragStore *rag.Store
	if cfg.RAG.Enabled {
		if embedder, ok := provider.(providers.EmbeddingProvider); ok {
			ragStore = rag.NewStore(database, embedder, cfg.RAG)
			// Load index asynchronously
			go func() {
				if err := ragStore.LoadIndex(context.Background()); err != nil {
					logger.ErrorCF("agent", "Failed to load RAG index", map[string]interface{}{"error": err.Error()})
				}
			}()
		} else {
			logger.WarnC("agent", "Provider does not support embeddings, RAG disabled")
		}
	} else {
		logger.InfoC("agent", "RAG is disabled in config")
	}

	if ragStore != nil {
		toolsRegistry.Register(tools.NewRememberTool(workspace, ragStore))
	}

	var store session.Store
	if strings.ToLower(cfg.Agents.Defaults.SessionStore) == "jsonl" {
		store = session.NewJSONLStore(workspace)
	} else {
		store = session.NewSQLiteStore(database)
	}
	sessionsManager := session.NewSessionManager(store, ragStore)

	// Create state manager for atomic state persistence
	stateManager := state.NewManager(workspace)

	// Open the Personal Context store once and reuse it for the life of the
	// agent. Personal Context is a derived layer: if it cannot open, the agent
	// still runs and extraction is skipped for this process.
	pcStore, err := personalcontext.Open(workspace)
	if err != nil {
		logger.WarnCF("agent", "Personal Context unavailable", map[string]interface{}{"error": err.Error()})
	}

	// Expose Personal Context as an on-demand query tool. The agent calls it
	// explicitly when it needs a belief; nothing is injected automatically.
	// Registered only when the store is available so a missing store just means
	// the tool is absent, not a broken agent.
	if pcStore != nil {
		contextGetTool := tools.NewContextGetTool(pcStore)
		toolsRegistry.Register(contextGetTool)
		subagentTools.Register(contextGetTool)
	}

	// Create media store
	mediaStore := media.NewFileMediaStoreWithCleanup(media.MediaCleanerConfig{
		Enabled:  true,
		MaxAge:   24 * time.Hour,
		Interval: 1 * time.Hour,
	})
	mediaStore.Start()

	// Create context builder and set tools registry
	contextBuilder := NewContextBuilder(workspace)
	contextBuilder.SetToolsRegistry(toolsRegistry)
	contextBuilder.SetPersonalContext(pcStore)

	// Create skill installer
	installer := skills.NewSkillInstaller(workspace)

	cmdRegistry := commands.NewRegistry(commands.DefaultDefinitions())
	doctorRunner := doctor.New(database.DB, provider, toolsRegistry, workspace)

	router := routing.NewRouter(cfg.Agents.Routing.LightModel, cfg.Agents.Routing.Threshold)
	fallback := providers.NewFallbackChain(time.Duration(cfg.Agents.Defaults.FallbackCooldown) * time.Second)
	providersByModel := map[string]providers.LLMProvider{
		cfg.Agents.Defaults.Model: provider,
	}
	fallbackCandidates := []providers.FallbackCandidate{
		{Name: cfg.Agents.Defaults.Model, Provider: provider, Model: cfg.Agents.Defaults.Model},
	}
	for _, model := range cfg.Agents.Defaults.FallbackModels {
		if model == "" || providersByModel[model] != nil {
			continue
		}
		p, err := providers.CreateProviderForModel(cfg, model)
		if err != nil {
			continue
		}
		providersByModel[model] = p
		fallbackCandidates = append(fallbackCandidates, providers.FallbackCandidate{Name: model, Provider: p, Model: model})
	}
	if cfg.Agents.Routing.LightModel != "" {
		if _, ok := providersByModel[cfg.Agents.Routing.LightModel]; !ok {
			if p, err := providers.CreateProviderForModel(cfg, cfg.Agents.Routing.LightModel); err == nil {
				providersByModel[cfg.Agents.Routing.LightModel] = p
			}
		}
	}

	// Initialize curator
	curator := tools.NewCurator(database.DB, tools.CuratorConfig{
		Enabled:           cfg.Tools.Curator.Enabled,
		StaleAfterDays:    cfg.Tools.Curator.StaleAfterDays,
		ArchiveAfterDays:  cfg.Tools.Curator.ArchiveAfterDays,
		CheckIntervalMins: cfg.Tools.Curator.CheckIntervalMins,
	})
	if err := curator.EnsureSchema(); err != nil {
		logger.WarnCF("agent", "Failed to initialize curator schema: %v", map[string]interface{}{"error": err.Error()})
	}

	// Initialize nudge manager
	nudgeMgr := NewNudgeManager(NudgeConfig{
		Enabled:        cfg.Nudge.Enabled,
		MemoryInterval: cfg.Nudge.MemoryInterval,
		SkillInterval:  cfg.Nudge.SkillInterval,
	}, sessionsManager)

	// Initialize the evolution pipeline (self-improvement / autonomous skill
	// creation). Enabled by default; cold-path analysis runs periodically.
	evolveCfg := evolution.DefaultEvolutionConfig()
	evolveCfg.Enabled = true
	evolutionMgr := evolution.NewEvolutionManager(workspace, evolveCfg)
	if err := evolutionMgr.Load(); err != nil {
		logger.WarnCF("agent", "Failed to load evolution state: %v", map[string]interface{}{"error": err.Error()})
	}

	al := &AgentLoop{
		bus:              msgBus,
		provider:         provider,
		workspace:        workspace,
		model:            cfg.Agents.Defaults.Model,
		temperature:      cfg.Agents.Defaults.Temperature,
		maxTokens:        cfg.Agents.Defaults.MaxTokens,
		contextWindow:    cfg.Agents.Defaults.MaxTokens, // Restore context window for summarization
		maxIterations:    cfg.Agents.Defaults.MaxToolIterations,
		sessions:         sessionsManager,
		state:            stateManager,
		media:            mediaStore,
		contextBuilder:   contextBuilder,
		tools:            toolsRegistry,
		toolProfile:      tools.ProfileFull,
		commands:         cmdRegistry,
		router:           router,
		fallback:         fallback,
		fallbackModels:   fallbackCandidates,
		installer:        installer,
		providersByModel: providersByModel,
		cfg:              cfg,
		doctor:           doctorRunner,
		summarizing:      sync.Map{},
		curator:          curator,
		nudge:            nudgeMgr,
		evolution:        evolutionMgr,
		steering:         NewSteeringManager(),
		pcStore:          pcStore,
		db:               database,
	}

	cmdRuntime := &commands.Runtime{
		Tools:           toolsRegistry,
		Sessions:        sessionsManager,
		Bus:             msgBus,
		Commands:        cmdRegistry,
		Doctor:          doctorRunner,
		Model:           cfg.Agents.Defaults.Model,
		ModelPresets:    al.ModelPresets(),
		CurrentModel:    al.GetCurrentModel,
		SetActiveModel:  al.SetModel,
		PersonalContext: pcStore,
	}
	cmdExec := commands.NewExecutor(cmdRegistry, cmdRuntime)
	al.commandExec = cmdExec

	return al, nil
}

func (al *AgentLoop) Config() *config.Config {
	return al.cfg
}

func (al *AgentLoop) DB() *sql.DB {
	// Expose the raw DB connection if available (for internal API)
	if sqlStore, ok := al.sessions.Store().(*session.SQLiteStore); ok {
		return sqlStore.DB()
	}
	return nil
}

func (al *AgentLoop) GetLastActiveSession() (string, string) {
	return al.state.GetLastActiveSession()
}

func (al *AgentLoop) Bus() *bus.MessageBus {
	return al.bus
}

// Steering exposes the mid-turn steering manager so external callers (e.g.
// the mobile API) can inject redirect/interrupt/abort messages into a running
// agent turn.
func (al *AgentLoop) Steering() *SteeringManager {
	return al.steering
}

// Tools exposes the tool registry so external callers (e.g. the mobile API)
// can reach interactive tools such as clarify.
func (al *AgentLoop) Tools() *tools.ToolRegistry {
	return al.tools
}

func (al *AgentLoop) Doctor() *doctor.Doctor {
	return al.doctor
}

func (al *AgentLoop) GetToolProfile() tools.ToolProfile {
	return al.toolProfile
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running.Store(true)

	// Start curator background goroutine
	go al.curator.Start(ctx)

	// Start the evolution cold path (periodic skill-draft generation).
	if al.evolution != nil {
		go al.evolutionColdPath(ctx)
	}

	for al.running.Load() {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				continue
			}

			response, err := al.processMessage(ctx, msg, nil, nil)
			if err != nil {
				response = fmt.Sprintf("Error processing message: %v", err)
			}

			if response != "" {
				// Check if the message tool already sent a response during this round.
				// If so, skip publishing to avoid duplicate messages to the user.
				alreadySent := false
				if tool, ok := al.tools.Get("message"); ok {
					if mt, ok := tool.(*tools.MessageTool); ok {
						alreadySent = mt.HasSentInRound()
					}
				}

				if !alreadySent {
					al.bus.PublishOutbound(bus.OutboundMessage{
						Channel: msg.Channel,
						ChatID:  msg.ChatID,
						Content: response,
						Metadata: map[string]interface{}{
							"type": "assistant_message",
						},
					})
				}
			}
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running.Store(false)
	al.curator.Stop()
	if al.db != nil {
		al.db.Close()
	}
}

func (al *AgentLoop) RegisterTool(tool tools.Tool) {
	al.tools.Register(tool)
}

// evolutionColdPath periodically runs the evolution pipeline's cold-path
// analysis, which clusters successful tasks into patterns and drafts new
// skills. Drafts are saved to state for review; ApplyDraft (or the skills
// tooling) is what actually creates the SKILL.md.
func (al *AgentLoop) evolutionColdPath(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if al.evolution == nil {
				return
			}
			if err := al.evolution.RunColdPath(); err != nil {
				logger.ErrorCF("agent", "Evolution cold path failed", map[string]interface{}{"error": err.Error()})
				continue
			}
			if err := al.evolution.Save(); err != nil {
				logger.ErrorCF("agent", "Failed to save evolution state", map[string]interface{}{"error": err.Error()})
			}
			drafts := al.evolution.GetDrafts()
			if len(drafts) > 0 {
				logger.InfoCF("agent", "Evolution generated skill drafts", map[string]interface{}{"count": len(drafts)})
			}
		}
	}
}

func (al *AgentLoop) CommandDefinitions() []commands.Definition {
	if al.commands == nil {
		return nil
	}
	return al.commands.Definitions()
}

// RecordLastChannel records the last active channel for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChannel(channel string) error {
	return al.state.SetLastChannel(channel)
}

// RecordLastChatID records the last active chat ID for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChatID(chatID string) error {
	return al.state.SetLastChatID(chatID)
}

// RecordLastActiveSession records the last active session (channel/chat) for this workspace.
func (al *AgentLoop) RecordLastActiveSession(channel, chatID string) error {
	return al.state.SetLastActiveSession(channel, chatID)
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	return al.ProcessDirectWithChannel(ctx, content, sessionKey, "cli", "direct", nil, nil, nil)
}

func (al *AgentLoop) ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string, media []string, onChunk func(string), onToolCall func(string, string)) (string, error) {
	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "mobile",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
		Media:      media,
	}

	return al.processMessage(ctx, msg, onChunk, onToolCall)
}

// ProcessHeartbeat processes a heartbeat request without session history.
// Each heartbeat is independent and doesn't accumulate context.
func (al *AgentLoop) ProcessHeartbeat(ctx context.Context, content, channel, chatID string) (string, error) {
	return al.runAgentLoop(ctx, processOptions{
		SessionKey:      "heartbeat",
		Channel:         channel,
		ChatID:          chatID,
		ToolProfile:     tools.ProfileHeartbeatSafe,
		UserMessage:     content,
		DefaultResponse: "I've completed processing but have no response to give.",
		EnableSummary:   false,
		SendResponse:    false,
		NoHistory:       true, // Don't load session history for heartbeat
	})
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage, onChunk func(string), onToolCall func(string, string)) (string, error) {
	// Ensure request ID exists for tracing
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]string)
	}
	requestID := msg.Metadata["request_id"]
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		msg.Metadata["request_id"] = requestID
	}

	// Record initial trace
	telemetry.Global.Record(msg.SessionKey, requestID, "queued", msg.Channel, msg.ChatID, "")

	// Add message preview to log (show full content for error messages)
	var logContent string
	if strings.Contains(msg.Content, "Error:") || strings.Contains(msg.Content, "error") {
		logContent = msg.Content // Full content for errors
	} else {
		logContent = utils.Truncate(msg.Content, 80)
	}
	logger.InfoCF("agent", fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, logContent),
		map[string]interface{}{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
		})

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	thinking := false
	if strings.HasPrefix(msg.Content, "/think") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(msg.Content, "/think"))
		if trimmed != "" {
			thinking = true
			msg.Content = trimmed
		}
	}

	if strings.HasPrefix(msg.Content, "/") {
		var replies []string
		req := commands.Request{
			Text:       msg.Content,
			Channel:    msg.Channel,
			ChatID:     msg.ChatID,
			SessionKey: msg.SessionKey,
			Reply: func(text string) error {
				replies = append(replies, text)
				return nil
			},
		}
		if al.commandExec != nil {
			result := al.commandExec.Execute(ctx, req)
			if result.Err != nil {
				return "", result.Err
			}
			if result.Outcome == commands.OutcomeHandled {
				resp := strings.Join(replies, "\n")
				if onChunk != nil && resp != "" {
					onChunk(resp)
				}
				return resp, nil
			}
		}
	}

	// Process as user message
	isCronTriggered := strings.HasPrefix(msg.SessionKey, "cron-")
	profile := channels.DetectToolProfile(msg.Channel, "", msg.SessionKey, false)
	if isCronTriggered {
		profile = tools.ProfileHeartbeatSafe
	}
	response, err := al.runAgentLoop(ctx, processOptions{
		SessionKey:      msg.SessionKey,
		Channel:         msg.Channel,
		ChatID:          msg.ChatID,
		ToolProfile:     profile,
		IsCronTriggered: isCronTriggered,
		UserMessage:     msg.Content,
		DefaultResponse: "I've completed processing but have no response to give.",
		EnableSummary:   true,
		SendResponse:    false,
		Media:           msg.Media,
		Thinking:        thinking,
		OnChunk:         onChunk,
		OnToolCall:      onToolCall,
		RequestID:       requestID,
	})

	// Cleanup temporary media files using MediaStore ReleaseAll if possible
	if len(msg.Media) > 0 {
		go func() {
			// Wait a bit to ensure no race conditions with async logging or other consumers
			time.Sleep(10 * time.Second)
			if al.media != nil {
				// The media paths are registered under sessionKey scope in ProcessDirectWithChannel
				// or they are just raw paths here.
				// For safety, we still do manual cleanup for paths not in media store
				for _, path := range msg.Media {
					_ = os.Remove(path)
				}
			}
		}()
	}

	return response, err
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]interface{}{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	// Parse origin channel from chat_id (format: "channel:chat_id")
	var originChannel string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
	} else {
		// Fallback
		originChannel = "cli"
	}

	// Extract subagent result from message content
	// Format: "Task 'label' completed.\n\nResult:\n<actual content>"
	content := msg.Content
	if idx := strings.Index(content, "Result:\n"); idx >= 0 {
		content = content[idx+8:] // Extract just the result part
	}

	// Skip internal channels - only log, don't send to user
	if constants.IsInternalChannel(originChannel) {
		logger.InfoCF("agent", "Subagent completed (internal channel)",
			map[string]interface{}{
				"sender_id":   msg.SenderID,
				"content_len": len(content),
				"channel":     originChannel,
			})
		return "", nil
	}

	// Agent acts as dispatcher only - subagent handles user interaction via message tool
	// Don't forward result here, subagent should use message tool to communicate with user
	logger.InfoCF("agent", "Subagent completed",
		map[string]interface{}{
			"sender_id":   msg.SenderID,
			"channel":     originChannel,
			"content_len": len(content),
		})

	// Agent only logs, does not respond to user
	return "", nil
}

// extractPersonalContext runs the Personal Context extractor over the user
// message that was just persisted. It is a derived-memory layer: failures are
// logged and never break or roll back the conversation turn.
// StartLearningWorker runs a conservative, background Personal Context
// consolidation (see personalcontext.Compact). It is local-first and
// disposable: a low-priority goroutine that runs once shortly after start and
// then roughly daily. It only strips exact duplicate memories (preserving
// provenance) and never blocks a conversation or requires cloud AI. If it
// fails, Ghost keeps working and the next run retries.
func (al *AgentLoop) StartLearningWorker(ctx context.Context) {
	go func() {
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		al.consolidatePersonalContext()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				al.consolidatePersonalContext()
			case <-ticker.C:
				al.consolidatePersonalContext()
			}
		}
	}()
}

// consolidatePersonalContext runs the conservative memory consolidation. It is
// a maintenance operation only; errors are logged and never break Ghost.
func (al *AgentLoop) consolidatePersonalContext() {
	if al.pcStore == nil {
		return
	}
	rejected, decayed, err := personalcontext.Compact(al.pcStore)
	if err != nil {
		logger.WarnCF("agent", "Personal Context consolidation failed", map[string]interface{}{"error": err.Error()})
	} else if rejected > 0 || decayed > 0 {
		logger.InfoCF("agent", "Personal Context consolidated", map[string]interface{}{"rejected": rejected, "decayed": decayed})
	}
	// Keep the curated (always-injected) profile in sync with Ghost's
	// structured memory, so the curated layer is actually used.
	if n, err := personalcontext.MaterializeCuratedProfile(al.workspace, al.pcStore); err != nil {
		logger.WarnCF("agent", "Curated profile materialization failed", map[string]interface{}{"error": err.Error()})
	} else if n > 0 {
		logger.InfoCF("agent", "Curated profile updated", map[string]interface{}{"facts": n})
	}
}

func (al *AgentLoop) extractPersonalContext(opts processOptions) {
	// Deterministic quick-capture for explicit "Remember/note/capture this"
	// directives so the note persists even when the model only acknowledges it.
	al.captureQuickNote(opts.UserMessage, opts.Channel)

	if al.pcStore == nil {
		return
	}

	// MessageID: the session store does not expose a persisted message id
	// through its public API, so the turn's existing per-message request id is
	// reused as the provenance message id rather than inventing a new one.
	msgID := opts.RequestID
	if msgID == "" {
		msgID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}

	in := personalcontext.Input{
		SessionID:    opts.SessionKey,
		MessageID:    msgID,
		Text:         opts.UserMessage,
		Timestamp:    time.Now().UTC(),
		PreviousText: previousUserMessage(al.sessions.GetHistory(opts.SessionKey)),
	}
	if _, err := personalcontext.Apply(al.pcStore, in); err != nil {
		logger.WarnCF("agent", "Personal Context extraction failed", map[string]interface{}{
			"session_key": opts.SessionKey,
			"error":       err.Error(),
		})
	}
}

// previousUserMessage returns the content of the immediately preceding USER
// message in the session history, or "" when there is none. The just-persisted
// current message is the last element of history, so scanning starts one
// position before it.
// quickCaptureRE matches explicit capture directives. Task/note captures live
// only under these; declaration-style directives ("remember that I prefer X")
// are intentionally left to the Personal Context extractor so a preference is
// stored once as a structured memory, not twice as raw text.
var quickCaptureRE = regexp.MustCompile(`(?i)^\s*(?:remember this|note that|note this|capture this|save this|write (?:this|that) down|jot (?:this|that) down|keep this)\s*[:,\s]+(.+?)\s*$`)

// captureQuickNote appends an explicit capture directive ("Remember this: X")
// to workspace/data/captures.md deterministically, matching the quick-capture
// skill's format, so a note is never lost when the model only acknowledges it.
func (al *AgentLoop) captureQuickNote(msg, channel string) {
	m := quickCaptureRE.FindStringSubmatch(strings.TrimSpace(msg))
	if m == nil {
		return
	}
	content := strings.TrimSpace(m[1])
	if content == "" {
		return
	}
	path := filepath.Join(al.workspace, "data", "captures.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		logger.WarnCF("agent", "Failed to create captures dir", map[string]interface{}{"error": err.Error()})
		return
	}
	if channel == "" {
		channel = "chat"
	}
	ts := time.Now().Format("2006-01-02 15:04")
	line := fmt.Sprintf("## %s (from %s)\n%s\n", ts, channel, content)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logger.WarnCF("agent", "Failed to open captures.md", map[string]interface{}{"error": err.Error()})
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		logger.WarnCF("agent", "Failed to write capture", map[string]interface{}{"error": err.Error()})
		return
	}
	logger.InfoCF("agent", "Quick capture saved", map[string]interface{}{"chars": len(content)})
}

func previousUserMessage(history []providers.Message) string {
	for i := len(history) - 2; i >= 0; i-- {
		if history[i].Role == "user" {
			return history[i].Content
		}
	}
	return ""
}

// runAgentLoop is the core message processing logic.
// It handles context building, LLM calls, tool execution, and response handling.
func (al *AgentLoop) runAgentLoop(ctx context.Context, opts processOptions) (string, error) {
	startTime := time.Now()

	// 0. Record last channel for heartbeat notifications (skip internal channels)
	if opts.Channel != "" && opts.ChatID != "" {
		// Don't record internal channels (cli, system, subagent)
		if !constants.IsInternalChannel(opts.Channel) {
			channelKey := fmt.Sprintf("%s:%s", opts.Channel, opts.ChatID)
			if err := al.RecordLastChannel(channelKey); err != nil {
				logger.WarnCF("agent", "Failed to record last channel: %v", map[string]interface{}{"error": err.Error()})
			}
			// Also record specifically as active session for smart routing
			if err := al.RecordLastActiveSession(opts.Channel, opts.ChatID); err != nil {
				logger.WarnCF("agent", "Failed to record last active session: %v", map[string]interface{}{"error": err.Error()})
			}
		}
	}

	// 1. Update tool contexts
	al.updateToolContexts(opts.Channel, opts.ChatID)

	// 2. Build messages (skip history for heartbeat)
	var history []providers.Message
	var summary string
	if !opts.NoHistory {
		history = al.sessions.GetHistory(opts.SessionKey)
		summary = al.sessions.GetSummary(opts.SessionKey)

		// Inject RAG context into summary
		ragContext := al.sessions.GetContext(ctx, opts.UserMessage)
		if ragContext != "" {
			if summary != "" {
				summary += "\n\n" + ragContext
			} else {
				summary = ragContext
			}
		}
	}
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		opts.UserMessage,
		opts.Media,
		opts.Channel,
		opts.ChatID,
		al.provider,
	)

	// 3. Save user message to session (only if not a slash command)
	isSlashCommand := strings.HasPrefix(opts.UserMessage, "/")
	if !isSlashCommand {
		al.sessions.AddMessage(opts.SessionKey, "user", opts.UserMessage)
		// Track user turn for memory nudge
		al.nudge.OnUserTurn(opts.SessionKey)
		// Derive Personal Context from the persisted user message. Runs after
		// persistence and before the LLM iteration, and never blocks the turn.
		if opts.SessionKey != "heartbeat" {
			al.extractPersonalContext(opts)
		}
	}

	// 4. Run LLM iteration loop
	telemetry.Global.Record(opts.SessionKey, opts.RequestID, "agent_processing", opts.Channel, opts.ChatID, "")
	finalContent, iteration, err := al.runLLMIteration(ctx, messages, opts)
	if err != nil {
		return "", err
	}

	// If last tool had ForUser content and we already sent it, we might not need to send final response
	// This is controlled by the tool's Silent flag and ForUser content

	// 5. Handle empty response
	if finalContent == "" {
		finalContent = opts.DefaultResponse
	}

	// 6. Save final assistant message to session (only if it's a real response, not a tool result turn or slash command)
	// We don't save iterations that were just tool calls here because runLLMIteration
	// already handles AddFullMessage for tool turns.
	if !isSlashCommand {
		al.sessions.AddMessage(opts.SessionKey, "assistant", finalContent)
		al.sessions.Save(opts.SessionKey)
	}

	// 7. Optional: summarization
	if opts.EnableSummary {
		al.maybeSummarize(opts.SessionKey)
	}

	// 8. Nudge system: inject memory/skill review prompts if thresholds met
	if !isSlashCommand && opts.SessionKey != "heartbeat" {
		if al.nudge.ShouldReviewMemory() {
			history := al.sessions.GetHistory(opts.SessionKey)
			if prompt := al.nudge.BuildMemoryPrompt(history); prompt != "" {
				al.sessions.AddMessage(opts.SessionKey, "system", prompt)
			}
		}
		if al.nudge.ShouldReviewSkills() {
			// Collect tools used in this turn from history
			history := al.sessions.GetHistory(opts.SessionKey)
			var toolsUsed []string
			for _, msg := range history {
				if msg.Role == "assistant" {
					for _, tc := range msg.ToolCalls {
						toolsUsed = append(toolsUsed, tc.Name)
					}
				}
			}
			if prompt := al.nudge.BuildSkillPrompt(toolsUsed); prompt != "" {
				al.sessions.AddMessage(opts.SessionKey, "system", prompt)
			}
		}
	}

	// 9. Optional: send response via bus
	if opts.SendResponse {
		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: opts.Channel,
			ChatID:  opts.ChatID,
			Content: finalContent,
			Metadata: map[string]interface{}{
				"type": "assistant_message",
			},
		})
	}

	// 9. Log response
	responsePreview := utils.Truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response: %s", responsePreview),
		map[string]interface{}{
			"session_key":  opts.SessionKey,
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	// 10. Auto-journaling
	if !opts.NoHistory && opts.SessionKey != "heartbeat" && !strings.HasPrefix(opts.UserMessage, "/") {
		go al.autoJournal(opts.SessionKey)
	}

	// 11. Record turn for the evolution pipeline (autonomous skill creation)
	if !isSlashCommand && al.evolution != nil {
		al.evolution.RecordTurn(evolution.LearningRecord{
			TaskKind:   classifyTaskKind(opts.UserMessage),
			Summary:    utils.Truncate(opts.UserMessage, 300),
			ToolsUsed:  al.collectToolsUsed(opts.SessionKey),
			Success:    finalContent != "",
			Duration:   time.Since(startTime),
			SessionKey: opts.SessionKey,
			Timestamp:  time.Now().UTC(),
			Metadata: map[string]string{
				"model": al.model,
			},
		})
	}

	telemetry.Global.Record(opts.SessionKey, opts.RequestID, "agent_completed", opts.Channel, opts.ChatID, "")

	return finalContent, nil
}

// runLLMIteration executes the LLM call loop with tool handling.
// Returns the final content, iteration count, and any error.
func (al *AgentLoop) runLLMIteration(ctx context.Context, messages []providers.Message, opts processOptions) (string, int, error) {
	iteration := 0
	var finalContent string
	activeProfile := opts.ToolProfile
	if activeProfile == "" {
		activeProfile = al.toolProfile
	}
	activeTools := tools.FilterRegistryByProfile(al.tools, activeProfile)

	for iteration < al.maxIterations {
		iteration++

		logger.DebugCF("agent", "LLM iteration",
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		// Build tool definitions
		providerToolDefs := activeTools.ToProviderDefs()

		selectedModel, _ := al.selectModel(opts, messages)
		logger.DebugCF("agent", "LLM request",
			map[string]interface{}{
				"iteration":         iteration,
				"model":             selectedModel,
				"messages_count":    len(messages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        8192,
				"temperature":       al.temperature,
				"system_prompt_len": len(messages[0].Content),
			})

		// Log full messages (detailed)
		logger.DebugCF("agent", "Full LLM request",
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		response, err := al.callLLM(ctx, selectedModel, messages, providerToolDefs, opts)

		if err != nil {
			logger.ErrorCF("agent", "LLM call failed",
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return "", iteration, fmt.Errorf("LLM call failed: %w", err)
		}

		// Check if no tool calls - we're done
		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
				map[string]interface{}{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		// Log tool calls
		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
			if opts.OnToolCall != nil {
				argsJSON, _ := json.Marshal(tc.Arguments)
				opts.OnToolCall(tc.Name, string(argsJSON))
			}
		}
		logger.InfoCF("agent", "LLM requested tool calls",
			map[string]interface{}{
				"tools":     toolNames,
				"count":     len(response.ToolCalls),
				"iteration": iteration,
			})

		// Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:             "assistant",
			Content:          response.Content,
			ReasoningContent: response.ReasoningContent,
		}
		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		// Save assistant message with tool calls to session
		al.sessions.AddFullMessage(opts.SessionKey, assistantMsg)

		// Execute tool calls
		for _, tc := range response.ToolCalls {
			// Log tool call with arguments preview
			argsJSON, _ := json.Marshal(tc.Arguments)
			argsPreview := utils.Truncate(string(argsJSON), 200)
			logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
				map[string]interface{}{
					"tool":      tc.Name,
					"iteration": iteration,
				})

			// Create async callback for tools that implement AsyncTool
			// NOTE: Following openclaw's design, async tools do NOT send results directly to users.
			// Instead, they notify the agent via PublishInbound, and the agent decides
			// whether to forward the result to the user (in processSystemMessage).
			asyncCallback := func(callbackCtx context.Context, result *tools.ToolResult) {
				// Log the async completion but don't send directly to user
				// The agent will handle user notification via processSystemMessage
				if !result.Silent && result.ForUser != "" {
					logger.InfoCF("agent", "Async tool completed, agent will handle notification",
						map[string]interface{}{
							"tool":        tc.Name,
							"content_len": len(result.ForUser),
						})
				}
			}

			if !activeProfile.Allows(tc.Name) {
				toolResultMsg := providers.Message{
					Role:       "tool",
					Content:    fmt.Sprintf("tool %s not available in profile %s", tc.Name, activeProfile),
					ToolCallID: tc.ID,
				}
				messages = append(messages, toolResultMsg)
				al.sessions.AddFullMessage(opts.SessionKey, toolResultMsg)
				continue
			}

			toolCtx := tools.WithSubagentDepth(ctx, tools.SubagentDepth(ctx))
			toolResult := activeTools.ExecuteWithContext(toolCtx, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey, asyncCallback)

			// Send ForUser content to user immediately if not Silent
			if !toolResult.Silent && toolResult.ForUser != "" && opts.SendResponse {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: opts.Channel,
					ChatID:  opts.ChatID,
					Content: toolResult.ForUser,
				})
				logger.DebugCF("agent", "Sent tool result to user",
					map[string]interface{}{
						"tool":        tc.Name,
						"content_len": len(toolResult.ForUser),
					})
			}

			// Determine content for LLM based on tool result
			contentForLLM := toolResult.ForLLM
			if contentForLLM == "" && toolResult.Err != nil {
				contentForLLM = toolResult.Err.Error()
			}

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    contentForLLM,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)

			// Save tool result message to session
			al.sessions.AddFullMessage(opts.SessionKey, toolResultMsg)

			// Record tool usage for curator
			al.curator.RecordUsage(tc.Name)

			// Track tool iteration for skill creation nudge
			al.nudge.OnToolIteration(opts.SessionKey)

			// Reset skill counter if skill-related tool used
			if tc.Name == "skill_manage" || tc.Name == "remember" {
				al.nudge.OnSkillToolUsed(opts.SessionKey)
			}
		}

		// Inject any mid-turn steering messages queued by the user (e.g. an
		// interrupt-and-redirect while a long tool-call sequence is running).
		if al.steering != nil {
			pending := al.steering.DrainPending(opts.SessionKey)
			if len(pending) > 0 {
				steerText := FormatForPrompt(pending)
				if steerText != "" {
					messages = append(messages, providers.Message{
						Role:    "system",
						Content: steerText,
					})
				}
				// A hard abort ends the iteration immediately.
				for _, m := range pending {
					if m.IsHardAbort {
						if finalContent == "" {
							finalContent = "Aborted by user."
						}
						return finalContent, iteration, nil
					}
				}
			}
			if al.steering.CheckInterrupt(opts.SessionKey) {
				if finalContent == "" {
					finalContent = "Interrupted by user."
				}
				return finalContent, iteration, nil
			}
		}
	}

	return finalContent, iteration, nil
}

func (al *AgentLoop) selectModel(opts processOptions, messages []providers.Message) (string, float64) {
	model := al.model
	conf := 1.0
	if al.router != nil {
		model, conf = al.router.SelectModel(opts.UserMessage, messages, len(opts.Media) > 0, al.model)
	}
	// If the turn carries an image, prefer a vision-capable model so the
	// provider actually receives it (e.g. DeepSeek's default flash model does
	// not accept images; the vision model does).
	if messagesContainImages(messages) {
		if vm := visionModelFor(model); vm != "" {
			return vm, conf
		}
	}
	return model, conf
}

// visionModelFor maps a configured provider:model to a vision-capable model of
// the same provider, when the configured one cannot see images. Providers whose
// models are already multimodal (OpenAI, Anthropic, Gemini, Groq, etc.) are
// left untouched.
func visionModelFor(model string) string {
	p, _ := splitProviderModel(model)
	switch p {
	case "deepseek":
		return "deepseek:deepseek-v4-flash-vision-exp"
	}
	return ""
}

// splitProviderModel separates a "provider:model" or "provider/model" id.
func splitProviderModel(model string) (string, string) {
	for _, sep := range []string{":", "/"} {
		if i := strings.Index(model, sep); i >= 0 {
			return model[:i], model[i+1:]
		}
	}
	return "", model
}

func messagesContainImages(messages []providers.Message) bool {
	for _, m := range messages {
		for _, p := range m.MultiContent {
			if p.ImageURL != nil && p.ImageURL.URL != "" {
				return true
			}
		}
	}
	return false
}

// classifyTaskKind produces a coarse task category for the evolution
// pipeline, which clusters similar successful tasks into skill candidates.
func classifyTaskKind(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "write") || strings.Contains(m, "create") || strings.Contains(m, "edit") || strings.Contains(m, "code"):
		return "code"
	case strings.Contains(m, "search") || strings.Contains(m, "research") || strings.Contains(m, "find"):
		return "research"
	case strings.Contains(m, "summarize") || strings.Contains(m, "summarise") || strings.Contains(m, "brief"):
		return "summarize"
	case strings.Contains(m, "schedule") || strings.Contains(m, "remind") || strings.Contains(m, "timer"):
		return "scheduling"
	case strings.Contains(m, "email") || strings.Contains(m, "message") || strings.Contains(m, "reply"):
		return "communication"
	case strings.Contains(m, "backup") || strings.Contains(m, "update") || strings.Contains(m, "install"):
		return "system"
	default:
		return "general"
	}
}

// collectToolsUsed returns the set of tool names used in a session's recent
// assistant messages, for evolution pattern matching.
func (al *AgentLoop) collectToolsUsed(sessionKey string) []string {
	seen := map[string]bool{}
	var out []string
	for _, msg := range al.sessions.GetHistory(sessionKey) {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if !seen[tc.Name] {
				seen[tc.Name] = true
				out = append(out, tc.Name)
			}
		}
	}
	return out
}

func (al *AgentLoop) callLLM(ctx context.Context, model string, messages []providers.Message, tools []providers.ToolDefinition, opts processOptions) (*providers.LLMResponse, error) {
	candidates := al.buildCandidates(model)
	if al.fallback != nil && len(candidates) > 0 {
		return al.fallback.Execute(ctx, candidates, func(c providers.FallbackCandidate) (*providers.LLMResponse, error) {
			return al.invokeProvider(ctx, c.Provider, c.Model, messages, tools, opts)
		})
	}
	if len(candidates) == 0 {
		return al.invokeProvider(ctx, al.provider, model, messages, tools, opts)
	}
	return al.invokeProvider(ctx, candidates[0].Provider, candidates[0].Model, messages, tools, opts)
}

func (al *AgentLoop) invokeProvider(ctx context.Context, provider providers.LLMProvider, model string, messages []providers.Message, tools []providers.ToolDefinition, opts processOptions) (*providers.LLMResponse, error) {
	if model == "" {
		model = al.model
	}

	thinkingLevel := "off"
	if opts.Thinking {
		thinkingLevel = "medium" // Default to medium if thinking is enabled via prefix
	}

	options := map[string]interface{}{
		"max_tokens":     al.maxTokens,
		"temperature":    al.temperature,
		"thinking_level": thinkingLevel,
	}

	// Safeguard: Ensure OnChunk is only used for assistant content streaming.
	// We pass it to the provider, which is responsible for streaming the response.
	// The provider should NOT stream tool inputs/outputs, only the assistant's generation.
	if opts.OnChunk != nil {
		if sp, ok := provider.(providers.StreamingProvider); ok {
			safeOnChunk := func(chunk string) {
				if shouldFilterAssistantChunk(chunk) {
					return
				}
				opts.OnChunk(chunk)
			}
			// Only pass OnChunk if we are NOT in a thinking/tool-use phase that might leak.
			// Ideally, we should pass safeOnChunk, but if the provider is "chatty" with tools,
			// we might want to disable streaming for tool-heavy iterations.
			// For now, we use safeOnChunk.
			return sp.StreamChat(ctx, messages, tools, model, options, safeOnChunk)
		}
	}
	resp, err := provider.Chat(ctx, messages, tools, model, options)
	if err == nil && opts.OnChunk != nil && resp.Content != "" {
		opts.OnChunk(resp.Content)
	}
	return resp, err
}

func shouldFilterAssistantChunk(chunk string) bool {
	trimmed := strings.TrimSpace(chunk)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "tool_call:") || strings.HasPrefix(lower, "{\"tool") || strings.HasPrefix(lower, "running tool") {
		return true
	}
	if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[User]") {
		return true
	}
	if strings.Contains(lower, "<skills>") || strings.Contains(lower, "</skills>") {
		return true
	}
	if strings.Contains(lower, "skills/{skill-name}/skill.md") {
		return true
	}
	if strings.HasPrefix(lower, "name:") && strings.Contains(lower, "\ndescription:") {
		return true
	}
	if strings.Contains(lower, "\"metadata\"") && strings.Contains(lower, "\"homepage\"") && strings.Contains(lower, "\"description\"") {
		return true
	}
	return false
}

func (al *AgentLoop) buildCandidates(model string) []providers.FallbackCandidate {
	seen := map[string]bool{}
	var out []providers.FallbackCandidate
	if model != "" {
		p := al.resolveProviderForModel(model)
		out = append(out, providers.FallbackCandidate{Name: model, Provider: p, Model: model})
		seen[model] = true
	}
	for _, c := range al.fallbackModels {
		if seen[c.Name] {
			continue
		}
		out = append(out, c)
		seen[c.Name] = true
	}
	return out
}

// LearningsSummary returns a small, user-facing digest of Ghost's self-
// improvement: how many task turns were recorded, how many skill drafts the
// evolution pipeline produced, and recently proposed skill names. It surfaces
// the learning loop without exposing internals.
func (al *AgentLoop) LearningsSummary() map[string]interface{} {
	out := map[string]interface{}{
		"records":  0,
		"drafts":   0,
		"profiles": 0,
	}
	if al.evolution == nil {
		return out
	}
	records := al.evolution.GetRecords()
	out["records"] = len(records)
	drafts := al.evolution.GetDrafts()
	out["drafts"] = len(drafts)
	out["profiles"] = len(al.evolution.GetProfiles())

	var recent []map[string]string
	for _, d := range drafts {
		if len(recent) >= 6 {
			break
		}
		recent = append(recent, map[string]string{
			"skill":       d.SkillName,
			"change_kind": d.ChangeKind,
			"status":      d.Status,
		})
	}
	out["recent"] = recent
	return out
}

// isLocalModel reports whether the active model is a local (Ollama/vLLM) model,
// which is used to decide whether the cloud-dependent recalls should run.
func (al *AgentLoop) isLocalModel() bool {
	m := strings.ToLower(al.model)
	return strings.Contains(m, "ollama") || strings.Contains(m, "vllm")
}

type RecallResult struct {
	Summarized bool            `json:"summarized"`
	Summary    string          `json:"summary,omitempty"`
	Sessions   []RecallSession `json:"sessions"`
}

type RecallSession struct {
	SessionID string   `json:"session_id"`
	Messages  []string `json:"messages"`
}

// Recall answers "what did we talk about earlier?" by searching past sessions
// for the query and, when a cloud model is available, synthesizing a concise
// recall summary over them. Offline (local model), it returns the raw matched
// sessions.
func (al *AgentLoop) Recall(ctx context.Context, query string) RecallResult {
	if al.db == nil {
		return RecallResult{}
	}
	limit := 20
	rows, err := al.db.QueryContext(ctx, `
		SELECT m.session_id, m.role, m.content
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		  AND (m.archived IS NULL OR m.archived = 0)
		  AND m.role IN ('user','assistant')
		ORDER BY bm25(messages_fts)
		LIMIT ?
	`, query, limit)
	if err != nil {
		logger.WarnCF("agent", "Recall query failed", map[string]interface{}{"error": err.Error()})
		return RecallResult{}
	}
	defer rows.Close()

	grouped := map[string][]string{}
	order := []string{}
	for rows.Next() {
		var sid, role, content string
		if err := rows.Scan(&sid, &role, &content); err != nil {
			break
		}
		if _, exists := grouped[sid]; !exists {
			order = append(order, sid)
		}
		if len(grouped[sid]) < 3 {
			grouped[sid] = append(grouped[sid], role+": "+strings.TrimSpace(content))
		}
	}
	if err := rows.Err(); err != nil {
		return RecallResult{}
	}

	sessions := make([]RecallSession, 0, len(order))
	var digest strings.Builder
	for _, sid := range order {
		sessions = append(sessions, RecallSession{SessionID: sid, Messages: grouped[sid]})
		digest.WriteString(fmt.Sprintf("[%s]\n%s\n", sid, strings.Join(grouped[sid], "\n")))
	}
	if len(sessions) == 0 {
		return RecallResult{}
	}

	// Only synthesize with a cloud model; local models fall back to raw.
	if al.isLocalModel() {
		return RecallResult{Summarized: false, Sessions: sessions}
	}
	resp, err := al.provider.Chat(ctx, []providers.Message{{
		Role:    "user",
		Content: fmt.Sprintf("From Ghost's past conversations, synthesize a short, helpful recall summary answering: %q\n\nRelevant matches:\n%s", query, digest.String()),
	}}, nil, al.model, map[string]interface{}{"max_tokens": 400, "temperature": 0})
	if err != nil {
		logger.WarnCF("agent", "Recall summary failed", map[string]interface{}{"error": err.Error()})
		return RecallResult{Summarized: false, Sessions: sessions}
	}
	return RecallResult{Summarized: true, Summary: resp.Content, Sessions: sessions}
}

func (al *AgentLoop) resolveProviderForModel(model string) providers.LLMProvider {
	if model == "" {
		return al.provider
	}
	if p, ok := al.providersByModel[model]; ok && p != nil {
		return p
	}
	if al.cfg != nil {
		if p, err := providers.CreateProviderForModel(al.cfg, model); err == nil {
			if al.providersByModel != nil {
				al.providersByModel[model] = p
			}
			return p
		}
	}
	return al.provider
}

// SetConfigPath records the config file path so /model can persist selections.
func (al *AgentLoop) SetConfigPath(path string) {
	al.configPath = path
}

// SetModel switches the active model at runtime and persists the selection to
// config.json. target may be a "provider:model" string or a named preset from
// config model_list.
func (al *AgentLoop) SetModel(target string) error {
	if al.cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	provider, model := "", target
	if preset := al.cfg.FindModelPreset(target); preset != nil {
		provider = preset.Provider
		model = preset.Model
	} else if strings.Contains(target, ":") {
		parts := strings.SplitN(target, ":", 2)
		if parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid format, use provider:model (e.g. openai:gpt-4o)")
		}
		provider = parts[0]
		model = parts[1]
	}

	// Update the live loop + config in memory.
	al.cfg.SetActiveModel(provider, model)
	al.model = model
	// Ensure the provider is resolvable before committing.
	canonical := model
	if provider != "" {
		canonical = provider + ":" + model
	}
	if _, err := providers.CreateProviderForModel(al.cfg, canonical); err != nil {
		return err
	}

	// Persist to config.json if a path is known.
	if al.configPath != "" {
		if err := config.SaveConfig(al.configPath, al.cfg); err != nil {
			return fmt.Errorf("persisted model but failed to save config: %w", err)
		}
	}
	return nil
}

// GetCurrentModel returns the active model for display.
func (al *AgentLoop) GetCurrentModel() string {
	if al.model != "" {
		return al.model
	}
	return "default"
}

// ModelPresets returns the list of named presets from config model_list,
// rendered as "provider:model" strings.
func (al *AgentLoop) ModelPresets() []string {
	if al.cfg == nil {
		return nil
	}
	var out []string
	for _, p := range al.cfg.Agents.ModelList {
		if p.Name != "" {
			out = append(out, p.Name)
		}
	}
	return out
}

// updateToolContexts updates the context for tools that need channel/chatID info.
func (al *AgentLoop) updateToolContexts(channel, chatID string) {
	// Use ContextualTool interface instead of type assertions
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(tools.ContextualTool); ok {
			mt.SetContext(channel, chatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(tools.ContextualTool); ok {
			st.SetContext(channel, chatID)
		}
	}
	if tool, ok := al.tools.Get("subagent"); ok {
		if st, ok := tool.(tools.ContextualTool); ok {
			st.SetContext(channel, chatID)
		}
	}
}

// maybeSummarize triggers summarization if the session history exceeds thresholds.
func (al *AgentLoop) maybeSummarize(sessionKey string) {
	newHistory := al.sessions.GetHistory(sessionKey)
	tokenEstimate := al.estimateTokens(newHistory)
	threshold := al.contextWindow * 75 / 100

	if len(newHistory) > 20 || tokenEstimate > threshold {
		if _, loading := al.summarizing.LoadOrStore(sessionKey, true); !loading {
			go func() {
				defer al.summarizing.Delete(sessionKey)
				al.summarizeSession(sessionKey)
			}()
		}
	}
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) GetStartupInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// Tools info
	tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(tools),
		"names": tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
}

// formatMessagesForLog formats messages for logging
func formatMessagesForLog(messages []providers.Message) string {
	if len(messages) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, msg := range messages {
		result += fmt.Sprintf("  [%d] Role: %s\n", i, msg.Role)
		if len(msg.ToolCalls) > 0 {
			result += "  ToolCalls:\n"
			for _, tc := range msg.ToolCalls {
				result += fmt.Sprintf("    - ID: %s, Type: %s, Name: %s\n", tc.ID, tc.Type, tc.Name)
				if tc.Function != nil {
					result += fmt.Sprintf("      Arguments: %s\n", utils.Truncate(tc.Function.Arguments, 200))
				}
			}
		}
		if msg.Content != "" {
			content := utils.Truncate(msg.Content, 200)
			result += fmt.Sprintf("  Content: %s\n", content)
		}
		if msg.ToolCallID != "" {
			result += fmt.Sprintf("  ToolCallID: %s\n", msg.ToolCallID)
		}
		result += "\n"
	}
	result += "]"
	return result
}

// formatToolsForLog formats tool definitions for logging
func formatToolsForLog(tools []providers.ToolDefinition) string {
	if len(tools) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, tool := range tools {
		result += fmt.Sprintf("  [%d] Type: %s, Name: %s\n", i, tool.Type, tool.Function.Name)
		result += fmt.Sprintf("      Description: %s\n", tool.Function.Description)
		if len(tool.Function.Parameters) > 0 {
			result += fmt.Sprintf("      Parameters: %s\n", utils.Truncate(fmt.Sprintf("%v", tool.Function.Parameters), 200))
		}
	}
	result += "]"
	return result
}

// summarizeSession summarizes the conversation history for a session.
func (al *AgentLoop) summarizeSession(sessionKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)

	// Keep last 4 messages for continuity
	if len(history) <= 4 {
		return
	}

	toSummarize := history[:len(history)-4]

	// Oversized Message Guard
	// Skip messages larger than 50% of context window to prevent summarizer overflow
	maxMessageTokens := al.contextWindow / 2
	validMessages := make([]providers.Message, 0)
	omitted := false

	for _, m := range toSummarize {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		// Estimate tokens for this message
		msgTokens := len(m.Content) / 4
		if msgTokens > maxMessageTokens {
			omitted = true
			continue
		}
		validMessages = append(validMessages, m)
	}

	if len(validMessages) == 0 {
		return
	}

	// Multi-Part Summarization
	// Split into two parts if history is significant
	var finalSummary string
	if len(validMessages) > 10 {
		mid := len(validMessages) / 2
		part1 := validMessages[:mid]
		part2 := validMessages[mid:]

		s1, _ := al.summarizeBatch(ctx, part1, "")
		s2, _ := al.summarizeBatch(ctx, part2, "")

		// Merge them
		mergePrompt := fmt.Sprintf("Merge these two conversation summaries into one cohesive summary:\n\n1: %s\n\n2: %s", s1, s2)
		resp, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: mergePrompt}}, nil, al.model, map[string]interface{}{
			"max_tokens":  1024,
			"temperature": 0.3,
		})
		if err == nil {
			finalSummary = resp.Content
		} else {
			finalSummary = s1 + " " + s2
		}
	} else {
		finalSummary, _ = al.summarizeBatch(ctx, validMessages, summary)
	}

	if omitted && finalSummary != "" {
		finalSummary += "\n[Note: Some oversized messages were omitted from this summary for efficiency.]"
	}

	if finalSummary != "" {
		al.sessions.SetSummary(sessionKey, finalSummary)
		al.sessions.TruncateHistory(sessionKey, 4)
		al.sessions.Save(sessionKey)
	}
}

// summarizeBatch summarizes a batch of messages.
func (al *AgentLoop) summarizeBatch(ctx context.Context, batch []providers.Message, existingSummary string) (string, error) {
	prompt := "Provide a concise summary of this conversation segment, preserving core context and key points.\n"
	if existingSummary != "" {
		prompt += "Existing context: " + existingSummary + "\n"
	}
	prompt += "\nCONVERSATION:\n"
	for _, m := range batch {
		prompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	response, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: prompt}}, nil, al.model, map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

// estimateTokens estimates the number of tokens in a message list.
// Uses rune count instead of byte length so that CJK and other multi-byte
// characters are not over-counted (a Chinese character is 3 bytes but roughly
// one token).
func (al *AgentLoop) estimateTokens(messages []providers.Message) int {
	total := 0
	for _, m := range messages {
		total += utf8.RuneCountInString(m.Content) / 3
	}
	return total
}

// autoJournal summarizes the session and appends it to the daily note.
func (al *AgentLoop) autoJournal(sessionKey string) {
	// Only journal if there's enough history
	history := al.sessions.GetHistory(sessionKey)
	if len(history) < 4 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := al.summarizeBatch(ctx, history, "")
	if err != nil || summary == "" {
		return
	}

	// Append to daily note via memory store
	entry := fmt.Sprintf("\n- [%s] (journal) %s", time.Now().Format("15:04"), summary)
	if al.contextBuilder != nil && al.contextBuilder.memory != nil {
		al.contextBuilder.memory.AppendToday(entry)
	}
}
