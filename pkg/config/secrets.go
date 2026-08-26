package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Secrets holds sensitive values that must never live in config.json.
// It is persisted to .secrets.json next to the config file with 0600
// permissions, using atomic temp-file + rename writes.
//
// This is the strict secrets boundary: config.json stores configuration,
// .secrets.json stores credentials. Mirrors the admin.hash pattern used
// for the admin credential.
type Secrets struct {
	ProviderAPIKeys map[string]string `json:"provider_api_keys,omitempty"`

	TelegramToken        string `json:"telegram_token,omitempty"`
	DiscordToken         string `json:"discord_token,omitempty"`
	SlackBotToken        string `json:"slack_bot_token,omitempty"`
	SlackAppToken        string `json:"slack_app_token,omitempty"`
	LINEChannelSecret    string `json:"line_channel_secret,omitempty"`
	LINEChannelAccessTok string `json:"line_channel_access_token,omitempty"`
	EmailPassword        string `json:"email_password,omitempty"`
	SMSAccountSID        string `json:"sms_account_sid,omitempty"`
	SMSAuthToken         string `json:"sms_auth_token,omitempty"`
	WeChatSecret         string `json:"wechat_secret,omitempty"`
	WeChatToken          string `json:"wechat_token,omitempty"`
	WeChatEncodingAESKey string `json:"wechat_encoding_aes_key,omitempty"`

	BridgeSecret      string `json:"bridge_secret,omitempty"` // Deprecated: kept for migration
	ClawHubAuthToken  string `json:"clawhub_auth_token,omitempty"`
	HonchoAPIKey      string `json:"honcho_api_key,omitempty"`
	FirecrawlAPIKey   string `json:"firecrawl_api_key,omitempty"`
	BraveAPIKey       string `json:"brave_api_key,omitempty"`
	RelayDeviceSecret string `json:"relay_device_secret,omitempty"`
}

// SecretsPath returns the path to the secrets file for a given config path.
func SecretsPath(configPath string) string {
	dir := filepath.Dir(configPath)
	return filepath.Join(dir, ".secrets.json")
}

// LoadSecrets reads secrets from disk. A missing file returns an empty
// Secrets (no error) so the boundary works on first boot.
func LoadSecrets(path string) (*Secrets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Secrets{}, nil
		}
		return nil, err
	}
	var s Secrets
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.ProviderAPIKeys == nil {
		s.ProviderAPIKeys = map[string]string{}
	}
	return &s, nil
}

// SaveSecrets writes secrets atomically with 0600 permissions.
func SaveSecrets(path string, s *Secrets) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create secrets dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".secrets-*")
	if err != nil {
		return fmt.Errorf("create temp secrets file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write secrets: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod secrets: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close secrets: %w", err)
	}
	return os.Rename(tmpName, path)
}

// extractSecrets pulls every secret field out of the config into a Secrets.
// Called before writing config.json so credentials never persist there.
func extractSecrets(cfg *Config) *Secrets {
	s := &Secrets{ProviderAPIKeys: map[string]string{}}

	providers := map[string]*ProviderConfig{
		"anthropic":     &cfg.Providers.Anthropic,
		"openai":        &cfg.Providers.OpenAI,
		"openrouter":    &cfg.Providers.OpenRouter,
		"groq":          &cfg.Providers.Groq,
		"zhipu":         &cfg.Providers.Zhipu,
		"vllm":          &cfg.Providers.VLLM,
		"gemini":        &cfg.Providers.Gemini,
		"nvidia":        &cfg.Providers.Nvidia,
		"moonshot":      &cfg.Providers.Moonshot,
		"shengsuanyun":  &cfg.Providers.ShengSuanYun,
		"deepseek":      &cfg.Providers.DeepSeek,
		"githubcopilot": &cfg.Providers.GitHubCopilot,
		"ollama":        &cfg.Providers.Ollama,
	}
	for name, p := range providers {
		if p.APIKey != "" {
			s.ProviderAPIKeys[name] = p.APIKey
		}
	}

	s.TelegramToken = cfg.Channels.Telegram.Token
	s.DiscordToken = cfg.Channels.Discord.Token
	s.SlackBotToken = cfg.Channels.Slack.BotToken
	s.SlackAppToken = cfg.Channels.Slack.AppToken
	s.LINEChannelSecret = cfg.Channels.LINE.ChannelSecret
	s.LINEChannelAccessTok = cfg.Channels.LINE.ChannelAccessToken
	s.EmailPassword = cfg.Channels.Email.Password
	s.SMSAccountSID = cfg.Channels.SMS.AccountSID
	s.SMSAuthToken = cfg.Channels.SMS.AuthToken
	s.WeChatSecret = cfg.Channels.WeChat.Secret
	s.WeChatToken = cfg.Channels.WeChat.Token
	s.WeChatEncodingAESKey = cfg.Channels.WeChat.EncodingAESKey

	s.RelayDeviceSecret = cfg.Relay.DeviceSecret
	s.ClawHubAuthToken = cfg.Skills.ClawHub.AuthToken
	s.HonchoAPIKey = cfg.Skills.Honcho.APIKey
	s.FirecrawlAPIKey = cfg.Tools.Web.Firecrawl.APIKey
	s.BraveAPIKey = cfg.Tools.Web.Brave.APIKey

	return s
}

// applySecrets overlays secrets back onto the config. Called after loading
// config.json so the running process sees credentials.
func applySecrets(cfg *Config, s *Secrets) {
	for name, key := range s.ProviderAPIKeys {
		if key == "" {
			continue
		}
		switch name {
		case "anthropic":
			cfg.Providers.Anthropic.APIKey = key
		case "openai":
			cfg.Providers.OpenAI.APIKey = key
		case "openrouter":
			cfg.Providers.OpenRouter.APIKey = key
		case "groq":
			cfg.Providers.Groq.APIKey = key
		case "zhipu":
			cfg.Providers.Zhipu.APIKey = key
		case "vllm":
			cfg.Providers.VLLM.APIKey = key
		case "gemini":
			cfg.Providers.Gemini.APIKey = key
		case "nvidia":
			cfg.Providers.Nvidia.APIKey = key
		case "moonshot":
			cfg.Providers.Moonshot.APIKey = key
		case "shengsuanyun":
			cfg.Providers.ShengSuanYun.APIKey = key
		case "deepseek":
			cfg.Providers.DeepSeek.APIKey = key
		case "githubcopilot":
			cfg.Providers.GitHubCopilot.APIKey = key
		case "ollama":
			cfg.Providers.Ollama.APIKey = key
		}
	}

	if s.TelegramToken != "" {
		cfg.Channels.Telegram.Token = s.TelegramToken
	}
	if s.DiscordToken != "" {
		cfg.Channels.Discord.Token = s.DiscordToken
	}
	if s.SlackBotToken != "" {
		cfg.Channels.Slack.BotToken = s.SlackBotToken
	}
	if s.SlackAppToken != "" {
		cfg.Channels.Slack.AppToken = s.SlackAppToken
	}
	if s.LINEChannelSecret != "" {
		cfg.Channels.LINE.ChannelSecret = s.LINEChannelSecret
	}
	if s.LINEChannelAccessTok != "" {
		cfg.Channels.LINE.ChannelAccessToken = s.LINEChannelAccessTok
	}
	if s.EmailPassword != "" {
		cfg.Channels.Email.Password = s.EmailPassword
	}
	if s.SMSAccountSID != "" {
		cfg.Channels.SMS.AccountSID = s.SMSAccountSID
	}
	if s.SMSAuthToken != "" {
		cfg.Channels.SMS.AuthToken = s.SMSAuthToken
	}
	if s.WeChatSecret != "" {
		cfg.Channels.WeChat.Secret = s.WeChatSecret
	}
	if s.WeChatToken != "" {
		cfg.Channels.WeChat.Token = s.WeChatToken
	}
	if s.WeChatEncodingAESKey != "" {
		cfg.Channels.WeChat.EncodingAESKey = s.WeChatEncodingAESKey
	}
	if s.RelayDeviceSecret != "" {
		cfg.Relay.DeviceSecret = s.RelayDeviceSecret
	}
	if s.ClawHubAuthToken != "" {
		cfg.Skills.ClawHub.AuthToken = s.ClawHubAuthToken
	}
	if s.HonchoAPIKey != "" {
		cfg.Skills.Honcho.APIKey = s.HonchoAPIKey
	}
	if s.FirecrawlAPIKey != "" {
		cfg.Tools.Web.Firecrawl.APIKey = s.FirecrawlAPIKey
	}
	if s.BraveAPIKey != "" {
		cfg.Tools.Web.Brave.APIKey = s.BraveAPIKey
	}
}

// clearSecrets zeroes secret fields so config.json never persists credentials.
func clearSecrets(cfg *Config) {
	providers := []*ProviderConfig{
		&cfg.Providers.Anthropic, &cfg.Providers.OpenAI,
		&cfg.Providers.OpenRouter, &cfg.Providers.Groq,
		&cfg.Providers.Zhipu, &cfg.Providers.VLLM,
		&cfg.Providers.Gemini, &cfg.Providers.Nvidia,
		&cfg.Providers.Moonshot, &cfg.Providers.ShengSuanYun,
		&cfg.Providers.DeepSeek, &cfg.Providers.GitHubCopilot,
		&cfg.Providers.Ollama,
	}
	for _, p := range providers {
		p.APIKey = ""
	}

	cfg.Channels.Telegram.Token = ""
	cfg.Channels.Discord.Token = ""
	cfg.Channels.Slack.BotToken = ""
	cfg.Channels.Slack.AppToken = ""
	cfg.Channels.LINE.ChannelSecret = ""
	cfg.Channels.LINE.ChannelAccessToken = ""
	cfg.Channels.Email.Password = ""
	cfg.Channels.SMS.AccountSID = ""
	cfg.Channels.SMS.AuthToken = ""
	cfg.Channels.WeChat.Secret = ""
	cfg.Channels.WeChat.Token = ""
	cfg.Channels.WeChat.EncodingAESKey = ""
	cfg.Relay.DeviceSecret = ""
	cfg.Skills.ClawHub.AuthToken = ""
	cfg.Skills.Honcho.APIKey = ""
	cfg.Tools.Web.Firecrawl.APIKey = ""
	cfg.Tools.Web.Brave.APIKey = ""
}
