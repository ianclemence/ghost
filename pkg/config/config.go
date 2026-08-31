package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/caarlos0/env/v11"
)

// FlexibleStringSlice is a []string that also accepts JSON numbers,
// so allow_from can contain both "123" and 123.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	// Try []string first
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*f = ss
		return nil
	}

	// Try []interface{} to handle mixed types
	var raw []interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	result := make([]string, 0, len(raw))
	for _, v := range raw {
		switch val := v.(type) {
		case string:
			result = append(result, val)
		case float64:
			result = append(result, fmt.Sprintf("%.0f", val))
		default:
			result = append(result, fmt.Sprintf("%v", val))
		}
	}
	*f = result
	return nil
}

type Config struct {
	Agents      AgentsConfig      `json:"agents"`
	Channels    ChannelsConfig    `json:"channels"`
	Providers   ProvidersConfig   `json:"providers"`
	Gateway     GatewayConfig     `json:"gateway"`
	Relay       RelayConfig       `json:"relay"`
	RAG         RAGConfig         `json:"rag"`
	Tools       ToolsConfig       `json:"tools"`
	Heartbeat   HeartbeatConfig   `json:"heartbeat"`
	Devices     DevicesConfig     `json:"devices"`
	Skills      SkillsConfig      `json:"skills"`
	Nudge       NudgeConfig       `json:"nudge"`
	Personality PersonalityConfig `json:"personality"`
	Toolsets    ToolsetsConfig    `json:"toolsets"`
	mu          sync.RWMutex
}

type SkillsConfig struct {
	ClawHub ClawHubConfig `json:"clawhub"`
	Honcho  HonchoConfig  `json:"honcho"`
}

type ClawHubConfig struct {
	BaseURL      string `json:"base_url" env:"GHOST_SKILLS_CLAWHUB_BASE_URL"`
	AuthToken    string `json:"auth_token" env:"GHOST_SKILLS_CLAWHUB_AUTH_TOKEN"`
	SearchPath   string `json:"search_path" env:"GHOST_SKILLS_CLAWHUB_SEARCH_PATH"`
	SkillsPath   string `json:"skills_path" env:"GHOST_SKILLS_CLAWHUB_SKILLS_PATH"`
	DownloadPath string `json:"download_path" env:"GHOST_SKILLS_CLAWHUB_DOWNLOAD_PATH"`
	Timeout      int    `json:"timeout" env:"GHOST_SKILLS_CLAWHUB_TIMEOUT"`
}

type HonchoConfig struct {
	Enabled   bool   `json:"enabled" env:"GHOST_HONCHO_ENABLED"`
	APIKey    string `json:"api_key" env:"GHOST_HONCHO_API_KEY"`
	ProjectID string `json:"project_id" env:"GHOST_HONCHO_PROJECT_ID"`
	BaseURL   string `json:"base_url" env:"GHOST_HONCHO_BASE_URL"`
}

type RAGConfig struct {
	Enabled        bool `json:"enabled" env:"GHOST_RAG_ENABLED"`
	M              int  `json:"m" env:"GHOST_RAG_M"`                             // Max connections per layer (default 16)
	EfConstruction int  `json:"ef_construction" env:"GHOST_RAG_EF_CONSTRUCTION"` // Size of dynamic candidate list during construction (default 200)
	EfSearch       int  `json:"ef_search" env:"GHOST_RAG_EF_SEARCH"`             // Size of dynamic candidate list during search (default 10)
}

type AgentsConfig struct {
	Defaults  AgentDefaults `json:"defaults"`
	Routing   RoutingConfig `json:"routing"`
	ModelList []ModelPreset `json:"model_list,omitempty"`
}

// ModelPreset is a named, user-selectable model configuration (Picoclaw-style).
// Provider and Model follow the same conventions as AgentDefaults.
type ModelPreset struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type AgentDefaults struct {
	Workspace           string   `json:"workspace" env:"GHOST_AGENTS_DEFAULTS_WORKSPACE"`
	RestrictToWorkspace bool     `json:"restrict_to_workspace" env:"GHOST_AGENTS_DEFAULTS_RESTRICT_TO_WORKSPACE"`
	SearchEnabled       bool     `json:"search_enabled" env:"GHOST_AGENTS_DEFAULTS_SEARCH_ENABLED"`
	Provider            string   `json:"provider" env:"GHOST_AGENTS_DEFAULTS_PROVIDER"`
	Model               string   `json:"model" env:"GHOST_AGENTS_DEFAULTS_MODEL"`
	MaxTokens           int      `json:"max_tokens" env:"GHOST_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature         float64  `json:"temperature" env:"GHOST_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations   int      `json:"max_tool_iterations" env:"GHOST_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
	EmbeddingModel      string   `json:"embedding_model" env:"GHOST_AGENTS_DEFAULTS_EMBEDDING_MODEL"`
	FallbackModels      []string `json:"fallback_models" env:"GHOST_AGENTS_DEFAULTS_FALLBACK_MODELS"`
	FallbackCooldown    int      `json:"fallback_cooldown_seconds" env:"GHOST_AGENTS_DEFAULTS_FALLBACK_COOLDOWN_SECONDS"`
	SessionStore        string   `json:"session_store" env:"GHOST_AGENTS_DEFAULTS_SESSION_STORE"`
}

type RoutingConfig struct {
	LightModel string  `json:"light_model" env:"GHOST_AGENTS_ROUTING_LIGHT_MODEL"`
	Threshold  float64 `json:"threshold" env:"GHOST_AGENTS_ROUTING_THRESHOLD"`
	// User-facing preferences surfaced in the Web Console. These are
	// hints; the router still falls back to local when the cloud provider
	// is unavailable regardless of these toggles.
	PreferLocal         bool `json:"prefer_local"`
	AllowCloud          bool `json:"allow_cloud"`
	CloudWhenLocalFails bool `json:"cloud_when_local_fails"`
}

type ChannelsConfig struct {
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Telegram TelegramConfig `json:"telegram"`
	Discord  DiscordConfig  `json:"discord"`
	Slack    SlackConfig    `json:"slack"`
	LINE     LINEConfig     `json:"line"`
	Email    EmailConfig    `json:"email"`
	SMS      SMSConfig      `json:"sms"`
	WeChat   WeChatConfig   `json:"wechat"`
}

type SMSConfig struct {
	Enabled    bool                `json:"enabled" env:"GHOST_CHANNELS_SMS_ENABLED"`
	AccountSID string              `json:"account_sid" env:"GHOST_CHANNELS_SMS_ACCOUNT_SID"`
	AuthToken  string              `json:"auth_token" env:"GHOST_CHANNELS_SMS_AUTH_TOKEN"`
	From       string              `json:"from" env:"GHOST_CHANNELS_SMS_FROM"`
	WebhookURL string              `json:"webhook_url" env:"GHOST_CHANNELS_SMS_WEBHOOK_URL"`
	AllowFrom  FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_SMS_ALLOW_FROM"`
}

type WeChatConfig struct {
	Enabled        bool                `json:"enabled" env:"GHOST_CHANNELS_WECHAT_ENABLED"`
	CorpID         string              `json:"corp_id" env:"GHOST_CHANNELS_WECHAT_CORP_ID"`
	AgentID        string              `json:"agent_id" env:"GHOST_CHANNELS_WECHAT_AGENT_ID"`
	Secret         string              `json:"secret" env:"GHOST_CHANNELS_WECHAT_SECRET"`
	Token          string              `json:"token" env:"GHOST_CHANNELS_WECHAT_TOKEN"`
	EncodingAESKey string              `json:"encoding_aes_key" env:"GHOST_CHANNELS_WECHAT_ENCODING_AES_KEY"`
	AllowFrom      FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_WECHAT_ALLOW_FROM"`
}

type WhatsAppConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_WHATSAPP_ENABLED"`
	BridgeURL string              `json:"bridge_url" env:"GHOST_CHANNELS_WHATSAPP_BRIDGE_URL"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_WHATSAPP_ALLOW_FROM"`
}

type TelegramConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_TELEGRAM_ENABLED"`
	Token     string              `json:"token" env:"GHOST_CHANNELS_TELEGRAM_TOKEN"`
	Proxy     string              `json:"proxy" env:"GHOST_CHANNELS_TELEGRAM_PROXY"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_TELEGRAM_ALLOW_FROM"`
}

type DiscordConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_DISCORD_ENABLED"`
	Token     string              `json:"token" env:"GHOST_CHANNELS_DISCORD_TOKEN"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_DISCORD_ALLOW_FROM"`
}

type SlackConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_SLACK_ENABLED"`
	BotToken  string              `json:"bot_token" env:"GHOST_CHANNELS_SLACK_BOT_TOKEN"`
	AppToken  string              `json:"app_token" env:"GHOST_CHANNELS_SLACK_APP_TOKEN"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_SLACK_ALLOW_FROM"`
}

type LINEConfig struct {
	Enabled            bool                `json:"enabled" env:"GHOST_CHANNELS_LINE_ENABLED"`
	ChannelSecret      string              `json:"channel_secret" env:"GHOST_CHANNELS_LINE_CHANNEL_SECRET"`
	ChannelAccessToken string              `json:"channel_access_token" env:"GHOST_CHANNELS_LINE_CHANNEL_ACCESS_TOKEN"`
	WebhookHost        string              `json:"webhook_host" env:"GHOST_CHANNELS_LINE_WEBHOOK_HOST"`
	WebhookPort        int                 `json:"webhook_port" env:"GHOST_CHANNELS_LINE_WEBHOOK_PORT"`
	WebhookPath        string              `json:"webhook_path" env:"GHOST_CHANNELS_LINE_WEBHOOK_PATH"`
	AllowFrom          FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_LINE_ALLOW_FROM"`
}

type EmailConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_EMAIL_ENABLED"`
	SMTPHost  string              `json:"smtp_host" env:"GHOST_CHANNELS_EMAIL_SMTP_HOST"`
	SMTPPort  int                 `json:"smtp_port" env:"GHOST_CHANNELS_EMAIL_SMTP_PORT"`
	Username  string              `json:"username" env:"GHOST_CHANNELS_EMAIL_USERNAME"`
	Password  string              `json:"password" env:"GHOST_CHANNELS_EMAIL_PASSWORD"`
	From      string              `json:"from" env:"GHOST_CHANNELS_EMAIL_FROM"`
	To        string              `json:"to" env:"GHOST_CHANNELS_EMAIL_TO"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_EMAIL_ALLOW_FROM"`
}

type HeartbeatConfig struct {
	Enabled  bool `json:"enabled" env:"GHOST_HEARTBEAT_ENABLED"`
	Interval int  `json:"interval" env:"GHOST_HEARTBEAT_INTERVAL"` // minutes, min 5
}

type DevicesConfig struct {
	Enabled    bool `json:"enabled" env:"GHOST_DEVICES_ENABLED"`
	MonitorUSB bool `json:"monitor_usb" env:"GHOST_DEVICES_MONITOR_USB"`
}

type ProvidersConfig struct {
	Anthropic     ProviderConfig `json:"anthropic"`
	OpenAI        ProviderConfig `json:"openai"`
	OpenRouter    ProviderConfig `json:"openrouter"`
	Groq          ProviderConfig `json:"groq"`
	Zhipu         ProviderConfig `json:"zhipu"`
	VLLM          ProviderConfig `json:"vllm"`
	Gemini        ProviderConfig `json:"gemini"`
	Nvidia        ProviderConfig `json:"nvidia"`
	Moonshot      ProviderConfig `json:"moonshot"`
	ShengSuanYun  ProviderConfig `json:"shengsuanyun"`
	DeepSeek      ProviderConfig `json:"deepseek"`
	Qwen          ProviderConfig `json:"qwen"`
	GitHubCopilot ProviderConfig `json:"github_copilot"`
	Ollama        ProviderConfig `json:"ollama"`
}

type ProviderConfig struct {
	APIKey      string `json:"api_key" env:"GHOST_PROVIDERS_{{.Name}}_API_KEY"`
	APIBase     string `json:"api_base" env:"GHOST_PROVIDERS_{{.Name}}_API_BASE"`
	Proxy       string `json:"proxy,omitempty" env:"GHOST_PROVIDERS_{{.Name}}_PROXY"`
	AuthMethod  string `json:"auth_method,omitempty" env:"GHOST_PROVIDERS_{{.Name}}_AUTH_METHOD"`
	ConnectMode string `json:"connect_mode,omitempty" env:"GHOST_PROVIDERS_{{.Name}}_CONNECT_MODE"` //only for Github Copilot, `stdio` or `grpc`
}

type GatewayConfig struct {
	Host string `json:"host" env:"GHOST_GATEWAY_HOST"`
	Port int    `json:"port" env:"GHOST_GATEWAY_PORT"`
}

type RelayConfig struct {
	Enabled      bool   `json:"enabled" env:"GHOST_RELAY_ENABLED"`
	Server       string `json:"server" env:"GHOST_RELAY_SERVER"`
	GatewayURL   string `json:"gateway_url" env:"GHOST_RELAY_GATEWAY_URL"`
	ReconnectMin int    `json:"reconnect_min_s" env:"GHOST_RELAY_RECONNECT_MIN"`
	ReconnectMax int    `json:"reconnect_max_s" env:"GHOST_RELAY_RECONNECT_MAX"`
	DeviceSecret string `json:"-"` // loaded from .secrets.json, never in config.json
}

type BraveConfig struct {
	Enabled    bool   `json:"enabled" env:"GHOST_TOOLS_WEB_BRAVE_ENABLED"`
	APIKey     string `json:"api_key" env:"GHOST_TOOLS_WEB_BRAVE_API_KEY"`
	MaxResults int    `json:"max_results" env:"GHOST_TOOLS_WEB_BRAVE_MAX_RESULTS"`
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled" env:"GHOST_TOOLS_WEB_DUCKDUCKGO_ENABLED"`
	MaxResults int  `json:"max_results" env:"GHOST_TOOLS_WEB_DUCKDUCKGO_MAX_RESULTS"`
}

type FirecrawlConfig struct {
	Enabled    bool   `json:"enabled" env:"GHOST_TOOLS_WEB_FIRECRAWL_ENABLED"`
	APIKey     string `json:"api_key" env:"GHOST_TOOLS_WEB_FIRECRAWL_API_KEY"`
	MaxResults int    `json:"max_results" env:"GHOST_TOOLS_WEB_FIRECRAWL_MAX_RESULTS"`
}

type WebToolsConfig struct {
	Firecrawl  FirecrawlConfig  `json:"firecrawl"`
	Brave      BraveConfig      `json:"brave"`
	DuckDuckGo DuckDuckGoConfig `json:"duckduckgo"`
}

type NudgeConfig struct {
	Enabled        bool `json:"enabled" env:"GHOST_NUDGE_ENABLED"`
	MemoryInterval int  `json:"memory_interval" env:"GHOST_NUDGE_MEMORY_INTERVAL"`
	SkillInterval  int  `json:"skill_interval" env:"GHOST_NUDGE_SKILL_INTERVAL"`
}

type PersonalityConfig struct {
	Active string `json:"active" env:"GHOST_PERSONALITY_ACTIVE"`
}

type ToolsetsConfig struct {
	Active string `json:"active" env:"GHOST_TOOLSETS_ACTIVE"`
}

type CuratorConfig struct {
	Enabled           bool `json:"enabled" env:"GHOST_CURATOR_ENABLED"`
	StaleAfterDays    int  `json:"stale_after_days" env:"GHOST_CURATOR_STALE_AFTER_DAYS"`
	ArchiveAfterDays  int  `json:"archive_after_days" env:"GHOST_CURATOR_ARCHIVE_AFTER_DAYS"`
	CheckIntervalMins int  `json:"check_interval_mins" env:"GHOST_CURATOR_CHECK_INTERVAL_MINS"`
}

type DelegationConfig struct {
	Enabled       bool `json:"enabled" env:"GHOST_DELEGATION_ENABLED"`
	MaxConcurrent int  `json:"max_concurrent" env:"GHOST_DELEGATION_MAX_CONCURRENT"`
	MaxTasks      int  `json:"max_tasks" env:"GHOST_DELEGATION_MAX_TASKS"`
	BudgetTokens  int  `json:"budget_tokens" env:"GHOST_DELEGATION_BUDGET_TOKENS"`
}

type ToolsConfig struct {
	Web        WebToolsConfig   `json:"web"`
	MCP        MCPConfig        `json:"mcp"`
	Curator    CuratorConfig    `json:"curator"`
	Delegation DelegationConfig `json:"delegation"`
}

type MCPConfig struct {
	Enabled bool                       `json:"enabled" env:"GHOST_TOOLS_MCP_ENABLED"`
	Servers map[string]MCPServerConfig `json:"servers"`
}

type MCPServerConfig struct {
	Enabled bool              `json:"enabled"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Workdir string            `json:"workdir"`
	Env     map[string]string `json:"env"`
	EnvFile string            `json:"env_file"`
	Headers map[string]string `json:"headers"`
	HTTP    bool              `json:"http"`
	HTTPURL string            `json:"http_url"`
}

func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:           "~/.ghost/workspace",
				RestrictToWorkspace: true,
				SearchEnabled:       true,
				Provider:            "moonshot",
				Model:               "kimi-k2.5",
				MaxTokens:           8192,
				Temperature:         0.7,
				MaxToolIterations:   20,
				FallbackModels:      []string{},
				FallbackCooldown:    30,
				SessionStore:        "sqlite",
			},
			Routing: RoutingConfig{
				LightModel:          "",
				Threshold:           0.35,
				PreferLocal:         true,
				AllowCloud:          true,
				CloudWhenLocalFails: true,
			},
		},
		Channels: ChannelsConfig{
			WhatsApp: WhatsAppConfig{
				Enabled:   false,
				BridgeURL: "ws://localhost:3001",
				AllowFrom: FlexibleStringSlice{},
			},
			Telegram: TelegramConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: FlexibleStringSlice{},
			},
			Discord: DiscordConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: FlexibleStringSlice{},
			},
			Slack: SlackConfig{
				Enabled:   false,
				BotToken:  "",
				AppToken:  "",
				AllowFrom: FlexibleStringSlice{},
			},
			LINE: LINEConfig{
				Enabled:            false,
				ChannelSecret:      "",
				ChannelAccessToken: "",
				WebhookHost:        "0.0.0.0",
				WebhookPort:        18791,
				WebhookPath:        "/webhook/line",
				AllowFrom:          FlexibleStringSlice{},
			},
			Email: EmailConfig{
				Enabled:   false,
				SMTPHost:  "",
				SMTPPort:  587,
				Username:  "",
				Password:  "",
				From:      "",
				To:        "",
				AllowFrom: FlexibleStringSlice{},
			},
		},
		Providers: ProvidersConfig{
			Anthropic:    ProviderConfig{},
			OpenAI:       ProviderConfig{},
			OpenRouter:   ProviderConfig{},
			Groq:         ProviderConfig{},
			Zhipu:        ProviderConfig{},
			VLLM:         ProviderConfig{},
			Gemini:       ProviderConfig{},
			Nvidia:       ProviderConfig{},
			Moonshot:     ProviderConfig{},
			ShengSuanYun: ProviderConfig{},
			Qwen:         ProviderConfig{},
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 8766,
		},
		Relay: RelayConfig{
			Enabled:      false,
			Server:       "",
			GatewayURL:   "",
			ReconnectMin: 1,
			ReconnectMax: 60,
		},
		RAG: RAGConfig{
			Enabled:        true,
			M:              16,
			EfConstruction: 200,
			EfSearch:       10,
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
				Firecrawl: FirecrawlConfig{
					Enabled:    false,
					APIKey:     "",
					MaxResults: 5,
				},
				Brave: BraveConfig{
					Enabled:    false,
					APIKey:     "",
					MaxResults: 5,
				},
				DuckDuckGo: DuckDuckGoConfig{
					Enabled:    true,
					MaxResults: 5,
				},
			},
			MCP: MCPConfig{
				Enabled: false,
				Servers: map[string]MCPServerConfig{},
			},
			Curator: CuratorConfig{
				Enabled:           true,
				StaleAfterDays:    30,
				ArchiveAfterDays:  90,
				CheckIntervalMins: 60,
			},
			Delegation: DelegationConfig{
				Enabled:       true,
				MaxConcurrent: 5,
				MaxTasks:      5,
				BudgetTokens:  100000,
			},
		},
		Nudge: NudgeConfig{
			Enabled:        true,
			MemoryInterval: 20,
			SkillInterval:  15,
		},
		Heartbeat: HeartbeatConfig{
			Enabled:  true,
			Interval: 30, // default 30 minutes
		},
		Devices: DevicesConfig{
			Enabled:    false,
			MonitorUSB: true,
		},
		Skills: SkillsConfig{
			ClawHub: ClawHubConfig{
				BaseURL: "https://clawhub.ai",
				Timeout: 30,
			},
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	// Manual override for providers since env tags with templates might not work
	if key := os.Getenv("KIMI_API_KEY"); key != "" {
		cfg.Providers.Moonshot.APIKey = key
	}
	if base := os.Getenv("KIMI_API_BASE"); base != "" {
		cfg.Providers.Moonshot.APIBase = base
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.Providers.Anthropic.APIKey = key
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		cfg.Providers.OpenAI.APIKey = key
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		cfg.Providers.Gemini.APIKey = key
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		cfg.Providers.OpenRouter.APIKey = key
	}
	if key := os.Getenv("ZHIPU_API_KEY"); key != "" {
		cfg.Providers.Zhipu.APIKey = key
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		cfg.Providers.Groq.APIKey = key
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		cfg.Providers.DeepSeek.APIKey = key
	}

	// Apply the strict secrets boundary: overlay .secrets.json (0600) so
	// credentials stored there win over any that slipped into config.json.
	secrets, err := LoadSecrets(SecretsPath(path))
	if err != nil {
		return nil, err
	}
	applySecrets(cfg, secrets)

	return cfg, nil
}

// MarshalSanitized serializes cfg with every secret field cleared, ready for
// an export where credentials must never be embedded (even inside an
// encrypted archive that will be shared between machines).
func MarshalSanitized(cfg *Config) ([]byte, error) {
	cfg.mu.RLock()
	raw, err := json.Marshal(cfg)
	cfg.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	var clean Config
	if err := json.Unmarshal(raw, &clean); err != nil {
		return nil, err
	}
	clearSecrets(&clean)
	return json.MarshalIndent(&clean, "", "  ")
}

// SaveConfig persists config.json (0600, atomic) and splits every secret out
// into .secrets.json (0600, atomic). config.json never stores credentials.
func SaveConfig(path string, cfg *Config) error {
	cfg.mu.RLock()

	// Serialize once, then rebuild a clean copy without the mutex (copying
	// the struct directly would copy the embedded sync.RWMutex).
	raw, err := json.Marshal(cfg)
	if err != nil {
		cfg.mu.RUnlock()
		return err
	}
	secrets := extractSecrets(cfg)
	cfg.mu.RUnlock()

	var clean Config
	if err := json.Unmarshal(raw, &clean); err != nil {
		return err
	}
	clearSecrets(&clean)

	data, err := json.MarshalIndent(&clean, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	if err := writeFileAtomic(path, data, 0600); err != nil {
		return err
	}

	return SaveSecrets(SecretsPath(path), secrets)
}

// writeFileAtomic writes data to path via temp file + rename, with the given
// permissions, so a crash never leaves a truncated file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ghost-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (c *Config) WorkspacePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return expandHome(c.Agents.Defaults.Workspace)
}

func (c *Config) GetAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Providers.OpenRouter.APIKey != "" {
		return c.Providers.OpenRouter.APIKey
	}
	if c.Providers.Anthropic.APIKey != "" {
		return c.Providers.Anthropic.APIKey
	}
	if c.Providers.OpenAI.APIKey != "" {
		return c.Providers.OpenAI.APIKey
	}
	if c.Providers.Gemini.APIKey != "" {
		return c.Providers.Gemini.APIKey
	}
	if c.Providers.Zhipu.APIKey != "" {
		return c.Providers.Zhipu.APIKey
	}
	if c.Providers.Groq.APIKey != "" {
		return c.Providers.Groq.APIKey
	}
	if c.Providers.VLLM.APIKey != "" {
		return c.Providers.VLLM.APIKey
	}
	if c.Providers.ShengSuanYun.APIKey != "" {
		return c.Providers.ShengSuanYun.APIKey
	}
	return ""
}

func (c *Config) GetAPIBase() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Providers.OpenRouter.APIKey != "" {
		if c.Providers.OpenRouter.APIBase != "" {
			return c.Providers.OpenRouter.APIBase
		}
		return "https://openrouter.ai/api/v1"
	}
	if c.Providers.Zhipu.APIKey != "" {
		return c.Providers.Zhipu.APIBase
	}
	if c.Providers.VLLM.APIKey != "" && c.Providers.VLLM.APIBase != "" {
		return c.Providers.VLLM.APIBase
	}
	return ""
}

// FindModelPreset returns the named model preset, or nil if not found.
func (c *Config) FindModelPreset(name string) *ModelPreset {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Agents.ModelList {
		if c.Agents.ModelList[i].Name == name {
			p := c.Agents.ModelList[i]
			return &p
		}
	}
	return nil
}

// SetActiveModel updates the active provider/model on the defaults, and
// returns a canonical "provider:model" string describing the selection.
func (c *Config) SetActiveModel(provider, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if provider != "" {
		c.Agents.Defaults.Provider = provider
	}
	if model != "" {
		c.Agents.Defaults.Model = model
	}
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}
