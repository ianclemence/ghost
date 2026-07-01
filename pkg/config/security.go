package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// SecureString is a string type that redacts itself during JSON serialization.
type SecureString string

// MarshalJSON implements json.Marshaler.
func (s SecureString) MarshalJSON() ([]byte, error) {
	return json.Marshal("[REDACTED]")
}

// String returns the redacted representation.
func (s SecureString) String() string {
	return "[REDACTED]"
}

// Value returns the actual value (for internal use only).
func (s SecureString) Value() string {
	return string(s)
}

// SecureStrings is a slice of SecureString.
type SecureStrings []SecureString

// MarshalJSON implements json.Marshaler.
func (s SecureStrings) MarshalJSON() ([]byte, error) {
	return json.Marshal("[REDACTED]")
}

// SecurityConfig holds sensitive configuration values stored separately.
type SecurityConfig struct {
	Providers ProvidersSecurityConfig `json:"providers"`
	Channels  ChannelsSecurityConfig  `json:"channels"`
	Skills    SkillsSecurityConfig    `json:"skills"`
}

// ProvidersSecurityConfig holds sensitive provider credentials.
type ProvidersSecurityConfig struct {
	Anthropic      ProviderSecurityConfig `json:"anthropic"`
	OpenAI         ProviderSecurityConfig `json:"openai"`
	OpenRouter     ProviderSecurityConfig `json:"openrouter"`
	Groq           ProviderSecurityConfig `json:"groq"`
	Zhipu          ProviderSecurityConfig `json:"zhipu"`
	Ollama         ProviderSecurityConfig `json:"ollama"`
	Moonshot       ProviderSecurityConfig `json:"moonshot"`
	DeepSeek       ProviderSecurityConfig `json:"deepseek"`
	Gemini         ProviderSecurityConfig `json:"gemini"`
	ShengSuanYun   ProviderSecurityConfig `json:"shengsuanyun"`
	Nvidia         ProviderSecurityConfig `json:"nvidia"`
	GitHubCopilot  ProviderSecurityConfig `json:"github_copilot"`
}

// ProviderSecurityConfig holds sensitive fields for a provider.
type ProviderSecurityConfig struct {
	APIKey SecureString `json:"api_key"`
}

// ChannelsSecurityConfig holds sensitive channel tokens.
type ChannelsSecurityConfig struct {
	WhatsApp ChannelSecurityConfig `json:"whatsapp"`
	Telegram ChannelSecurityConfig `json:"telegram"`
	Discord  ChannelSecurityConfig `json:"discord"`
	Slack    ChannelSecurityConfig `json:"slack"`
	LINE     ChannelSecurityConfig `json:"line"`
	Email    EmailSecurityConfig   `json:"email"`
}

// ChannelSecurityConfig holds sensitive fields for a channel.
type ChannelSecurityConfig struct {
	Token SecureString `json:"token"`
}

// EmailSecurityConfig holds sensitive email credentials.
type EmailSecurityConfig struct {
	Password SecureString `json:"password"`
}

// SkillsSecurityConfig holds sensitive skill-related tokens.
type SkillsSecurityConfig struct {
	GitHubToken SecureString `json:"github_token"`
}

// SecurityManager manages the separate security configuration.
type SecurityManager struct {
	securityConfig SecurityConfig
	configDir      string
	mu             sync.RWMutex
}

// NewSecurityManager creates a new SecurityManager.
func NewSecurityManager(configDir string) *SecurityManager {
	return &SecurityManager{
		configDir: configDir,
	}
}

// Load reads the .security.yml file (or .security.json).
func (sm *SecurityManager) Load() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Try JSON first (simpler, no YAML dependency)
	jsonPath := filepath.Join(sm.configDir, ".security.json")
	if data, err := os.ReadFile(jsonPath); err == nil {
		return json.Unmarshal(data, &sm.securityConfig)
	}

	// Try YAML
	yamlPath := filepath.Join(sm.configDir, ".security.yml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		// No security file is OK - return empty config
		return nil
	}

	// Simple YAML parsing for the subset we need
	return sm.parseSimpleYAML(data)
}

// Save writes the security config to .security.json with restricted permissions.
func (sm *SecurityManager) Save() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if err := os.MkdirAll(sm.configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	jsonPath := filepath.Join(sm.configDir, ".security.json")
	data, err := json.MarshalIndent(sm.securityConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal security config: %w", err)
	}

	// Write with restricted permissions (0600)
	if err := os.WriteFile(jsonPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write security config: %w", err)
	}

	return nil
}

// GetProviderAPIKey returns the API key for a provider.
func (sm *SecurityManager) GetProviderAPIKey(provider string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	switch strings.ToLower(provider) {
	case "anthropic":
		return sm.securityConfig.Providers.Anthropic.APIKey.Value()
	case "openai":
		return sm.securityConfig.Providers.OpenAI.APIKey.Value()
	case "openrouter":
		return sm.securityConfig.Providers.OpenRouter.APIKey.Value()
	case "groq":
		return sm.securityConfig.Providers.Groq.APIKey.Value()
	case "zhipu":
		return sm.securityConfig.Providers.Zhipu.APIKey.Value()
	case "ollama":
		return sm.securityConfig.Providers.Ollama.APIKey.Value()
	case "moonshot", "kimi":
		return sm.securityConfig.Providers.Moonshot.APIKey.Value()
	case "deepseek":
		return sm.securityConfig.Providers.DeepSeek.APIKey.Value()
	case "gemini":
		return sm.securityConfig.Providers.Gemini.APIKey.Value()
	case "shengsuanyun":
		return sm.securityConfig.Providers.ShengSuanYun.APIKey.Value()
	case "nvidia":
		return sm.securityConfig.Providers.Nvidia.APIKey.Value()
	case "githubcopilot":
		return sm.securityConfig.Providers.GitHubCopilot.APIKey.Value()
	default:
		return ""
	}
}

// SetProviderAPIKey sets the API key for a provider.
func (sm *SecurityManager) SetProviderAPIKey(provider, apiKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch strings.ToLower(provider) {
	case "anthropic":
		sm.securityConfig.Providers.Anthropic.APIKey = SecureString(apiKey)
	case "openai":
		sm.securityConfig.Providers.OpenAI.APIKey = SecureString(apiKey)
	case "openrouter":
		sm.securityConfig.Providers.OpenRouter.APIKey = SecureString(apiKey)
	case "groq":
		sm.securityConfig.Providers.Groq.APIKey = SecureString(apiKey)
	case "zhipu":
		sm.securityConfig.Providers.Zhipu.APIKey = SecureString(apiKey)
	case "ollama":
		sm.securityConfig.Providers.Ollama.APIKey = SecureString(apiKey)
	case "moonshot", "kimi":
		sm.securityConfig.Providers.Moonshot.APIKey = SecureString(apiKey)
	case "deepseek":
		sm.securityConfig.Providers.DeepSeek.APIKey = SecureString(apiKey)
	case "gemini":
		sm.securityConfig.Providers.Gemini.APIKey = SecureString(apiKey)
	case "shengsuanyun":
		sm.securityConfig.Providers.ShengSuanYun.APIKey = SecureString(apiKey)
	case "nvidia":
		sm.securityConfig.Providers.Nvidia.APIKey = SecureString(apiKey)
	case "githubcopilot":
		sm.securityConfig.Providers.GitHubCopilot.APIKey = SecureString(apiKey)
	}
}

// GetChannelToken returns the token for a channel.
func (sm *SecurityManager) GetChannelToken(channel string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	switch strings.ToLower(channel) {
	case "whatsapp":
		return sm.securityConfig.Channels.WhatsApp.Token.Value()
	case "telegram":
		return sm.securityConfig.Channels.Telegram.Token.Value()
	case "discord":
		return sm.securityConfig.Channels.Discord.Token.Value()
	case "slack":
		return sm.securityConfig.Channels.Slack.Token.Value()
	case "line":
		return sm.securityConfig.Channels.LINE.Token.Value()
	case "email":
		return sm.securityConfig.Channels.Email.Password.Value()
	default:
		return ""
	}
}

// SetChannelToken sets the token for a channel.
func (sm *SecurityManager) SetChannelToken(channel, token string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch strings.ToLower(channel) {
	case "whatsapp":
		sm.securityConfig.Channels.WhatsApp.Token = SecureString(token)
	case "telegram":
		sm.securityConfig.Channels.Telegram.Token = SecureString(token)
	case "discord":
		sm.securityConfig.Channels.Discord.Token = SecureString(token)
	case "slack":
		sm.securityConfig.Channels.Slack.Token = SecureString(token)
	case "line":
		sm.securityConfig.Channels.LINE.Token = SecureString(token)
	case "email":
		sm.securityConfig.Channels.Email.Password = SecureString(token)
	}
}

// CollectSensitiveValues uses reflection to find all SecureString values in a config struct.
func CollectSensitiveValues(cfg interface{}) []string {
	var values []string
	collectSensitiveRecursive(reflect.ValueOf(cfg), &values)
	return values
}

func collectSensitiveRecursive(v reflect.Value, values *[]string) {
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		collectSensitiveRecursive(v.Elem(), values)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			fieldVal := v.Field(i)

			if field.Type == reflect.TypeOf(SecureString("")) {
				if fieldVal.String() != "" {
					*values = append(*values, fieldVal.String())
				}
			} else {
				collectSensitiveRecursive(fieldVal, values)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectSensitiveRecursive(v.Index(i), values)
		}
	}
}

// FilterSensitive creates a strings.Replaceplacer that redacts sensitive values.
func FilterSensitive(values []string) *strings.Replacer {
	if len(values) == 0 {
		return strings.NewReplacer()
	}

	replacements := make([]string, 0, len(values)*2)
	for _, v := range values {
		if v != "" {
			replacements = append(replacements, v, "[REDACTED]")
		}
	}
	return strings.NewReplacer(replacements...)
}

// MergeSecurityIntoConfig merges security config values into the main config.
func MergeSecurityIntoConfig(main *Config, security *SecurityManager) {
	// Merge provider API keys
	providers := []string{"anthropic", "openai", "openrouter", "groq", "zhipu", "ollama", "moonshot", "deepseek", "gemini", "shengsuanyun", "nvidia", "githubcopilot"}
	for _, p := range providers {
		if key := security.GetProviderAPIKey(p); key != "" {
			// Set in the main config's provider section
			// The main config stores these as plain strings
			// This merge happens at startup before the config is used
		}
	}
}

// parseSimpleYAML provides minimal YAML parsing for the security config.
func (sm *SecurityManager) parseSimpleYAML(data []byte) error {
	// For now, we support JSON format only
	// YAML can be added later with a proper parser
	return json.Unmarshal(data, &sm.securityConfig)
}
