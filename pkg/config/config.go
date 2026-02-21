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
	Agents    AgentsConfig    `json:"agents"`
	Channels  ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Gateway   GatewayConfig   `json:"gateway"`
	RAG       RAGConfig       `json:"rag"`
	Tools     ToolsConfig     `json:"tools"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
	Devices   DevicesConfig   `json:"devices"`
	mu        sync.RWMutex
}

type RAGConfig struct {
	Enabled        bool `json:"enabled" env:"GHOST_RAG_ENABLED"`
	M              int  `json:"m" env:"GHOST_RAG_M"`                           // Max connections per layer (default 16)
	EfConstruction int  `json:"ef_construction" env:"GHOST_RAG_EF_CONSTRUCTION"` // Size of dynamic candidate list during construction (default 200)
	EfSearch       int  `json:"ef_search" env:"GHOST_RAG_EF_SEARCH"`             // Size of dynamic candidate list during search (default 10)
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

type AgentDefaults struct {
	Workspace           string  `json:"workspace" env:"GHOST_AGENTS_DEFAULTS_WORKSPACE"`
	RestrictToWorkspace bool    `json:"restrict_to_workspace" env:"GHOST_AGENTS_DEFAULTS_RESTRICT_TO_WORKSPACE"`
	Provider            string  `json:"provider" env:"GHOST_AGENTS_DEFAULTS_PROVIDER"`
	Model               string  `json:"model" env:"GHOST_AGENTS_DEFAULTS_MODEL"`
	MaxTokens           int     `json:"max_tokens" env:"GHOST_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature         float64 `json:"temperature" env:"GHOST_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations   int     `json:"max_tool_iterations" env:"GHOST_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
	EmbeddingModel      string  `json:"embedding_model" env:"GHOST_AGENTS_DEFAULTS_EMBEDDING_MODEL"`
}

type ChannelsConfig struct {
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Telegram TelegramConfig `json:"telegram"`
	Feishu   FeishuConfig   `json:"feishu"`
	Discord  DiscordConfig  `json:"discord"`
	MaixCam  MaixCamConfig  `json:"maixcam"`
	QQ       QQConfig       `json:"qq"`
	DingTalk DingTalkConfig `json:"dingtalk"`
	Slack    SlackConfig    `json:"slack"`
	LINE     LINEConfig     `json:"line"`
	OneBot   OneBotConfig   `json:"onebot"`
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

type FeishuConfig struct {
	Enabled           bool                `json:"enabled" env:"GHOST_CHANNELS_FEISHU_ENABLED"`
	AppID             string              `json:"app_id" env:"GHOST_CHANNELS_FEISHU_APP_ID"`
	AppSecret         string              `json:"app_secret" env:"GHOST_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey        string              `json:"encrypt_key" env:"GHOST_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken string              `json:"verification_token" env:"GHOST_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom         FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_FEISHU_ALLOW_FROM"`
}

type DiscordConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_DISCORD_ENABLED"`
	Token     string              `json:"token" env:"GHOST_CHANNELS_DISCORD_TOKEN"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_DISCORD_ALLOW_FROM"`
}

type MaixCamConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_MAIXCAM_ENABLED"`
	Host      string              `json:"host" env:"GHOST_CHANNELS_MAIXCAM_HOST"`
	Port      int                 `json:"port" env:"GHOST_CHANNELS_MAIXCAM_PORT"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_MAIXCAM_ALLOW_FROM"`
}

type QQConfig struct {
	Enabled   bool                `json:"enabled" env:"GHOST_CHANNELS_QQ_ENABLED"`
	AppID     string              `json:"app_id" env:"GHOST_CHANNELS_QQ_APP_ID"`
	AppSecret string              `json:"app_secret" env:"GHOST_CHANNELS_QQ_APP_SECRET"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_QQ_ALLOW_FROM"`
}

type DingTalkConfig struct {
	Enabled      bool                `json:"enabled" env:"GHOST_CHANNELS_DINGTALK_ENABLED"`
	ClientID     string              `json:"client_id" env:"GHOST_CHANNELS_DINGTALK_CLIENT_ID"`
	ClientSecret string              `json:"client_secret" env:"GHOST_CHANNELS_DINGTALK_CLIENT_SECRET"`
	AllowFrom    FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_DINGTALK_ALLOW_FROM"`
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

type OneBotConfig struct {
	Enabled            bool                `json:"enabled" env:"GHOST_CHANNELS_ONEBOT_ENABLED"`
	WSUrl              string              `json:"ws_url" env:"GHOST_CHANNELS_ONEBOT_WS_URL"`
	AccessToken        string              `json:"access_token" env:"GHOST_CHANNELS_ONEBOT_ACCESS_TOKEN"`
	ReconnectInterval  int                 `json:"reconnect_interval" env:"GHOST_CHANNELS_ONEBOT_RECONNECT_INTERVAL"`
	GroupTriggerPrefix []string            `json:"group_trigger_prefix" env:"GHOST_CHANNELS_ONEBOT_GROUP_TRIGGER_PREFIX"`
	AllowFrom          FlexibleStringSlice `json:"allow_from" env:"GHOST_CHANNELS_ONEBOT_ALLOW_FROM"`
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
	DeepSeek      ProviderConfig       `json:"deepseek"`
	GitHubCopilot ProviderConfig       `json:"github_copilot"`
	PicoLM        PicoLMProviderConfig `json:"picolm"`
}

type ProviderConfig struct {
	APIKey      string `json:"api_key" env:"GHOST_PROVIDERS_{{.Name}}_API_KEY"`
	APIBase     string `json:"api_base" env:"GHOST_PROVIDERS_{{.Name}}_API_BASE"`
	Proxy       string `json:"proxy,omitempty" env:"GHOST_PROVIDERS_{{.Name}}_PROXY"`
	AuthMethod  string `json:"auth_method,omitempty" env:"GHOST_PROVIDERS_{{.Name}}_AUTH_METHOD"`
	ConnectMode string `json:"connect_mode,omitempty" env:"GHOST_PROVIDERS_{{.Name}}_CONNECT_MODE"` //only for Github Copilot, `stdio` or `grpc`
}

type PicoLMProviderConfig struct {
	BinaryPath string `json:"binary" env:"GHOST_PROVIDERS_PICOLM_BINARY"`
	ModelPath  string `json:"model" env:"GHOST_PROVIDERS_PICOLM_MODEL"`
	Threads    int    `json:"threads" env:"GHOST_PROVIDERS_PICOLM_THREADS"`
	Context    int    `json:"context" env:"GHOST_PROVIDERS_PICOLM_CONTEXT"`
	Template   string `json:"template" env:"GHOST_PROVIDERS_PICOLM_TEMPLATE"`
}

type GatewayConfig struct {
	Host string `json:"host" env:"GHOST_GATEWAY_HOST"`
	Port int    `json:"port" env:"GHOST_GATEWAY_PORT"`
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

type WebToolsConfig struct {
	Brave      BraveConfig      `json:"brave"`
	DuckDuckGo DuckDuckGoConfig `json:"duckduckgo"`
}

type ToolsConfig struct {
	Web WebToolsConfig `json:"web"`
}

func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:           "~/.ghost/workspace",
				RestrictToWorkspace: true,
				Provider:            "",
				Model:               "glm-4.7",
				MaxTokens:           8192,
				Temperature:         0.7,
				MaxToolIterations:   20,
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
			Feishu: FeishuConfig{
				Enabled:           false,
				AppID:             "",
				AppSecret:         "",
				EncryptKey:        "",
				VerificationToken: "",
				AllowFrom:         FlexibleStringSlice{},
			},
			Discord: DiscordConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: FlexibleStringSlice{},
			},
			MaixCam: MaixCamConfig{
				Enabled:   false,
				Host:      "0.0.0.0",
				Port:      18790,
				AllowFrom: FlexibleStringSlice{},
			},
			QQ: QQConfig{
				Enabled:   false,
				AppID:     "",
				AppSecret: "",
				AllowFrom: FlexibleStringSlice{},
			},
			DingTalk: DingTalkConfig{
				Enabled:      false,
				ClientID:     "",
				ClientSecret: "",
				AllowFrom:    FlexibleStringSlice{},
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
			OneBot: OneBotConfig{
				Enabled:            false,
				WSUrl:              "ws://127.0.0.1:3001",
				AccessToken:        "",
				ReconnectInterval:  5,
				GroupTriggerPrefix: []string{},
				AllowFrom:          FlexibleStringSlice{},
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
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 18790,
		},
		RAG: RAGConfig{
			Enabled:        true,
			M:              16,
			EfConstruction: 200,
			EfSearch:       10,
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
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
		},
		Heartbeat: HeartbeatConfig{
			Enabled:  true,
			Interval: 30, // default 30 minutes
		},
		Devices: DevicesConfig{
			Enabled:    false,
			MonitorUSB: true,
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
	if key := os.Getenv("MOONSHOT_API_KEY"); key != "" {
		cfg.Providers.Moonshot.APIKey = key
	}
	if key := os.Getenv("KIMI_API_KEY"); key != "" {
		cfg.Providers.Moonshot.APIKey = key
	}

	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
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
