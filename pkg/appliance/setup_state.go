package appliance

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

const (
	SetupCompleteFlag = ".setup-complete"
	DefaultGhostDir   = "/var/ghost"
	DefaultConfigDir  = "/var/ghost/config"
	DefaultDataDir    = "/var/ghost/data"
	DefaultWorkspace  = "/var/ghost/workspace"
)

// SetupState detects whether Ghost has been configured.
type SetupState struct {
	GhostDir   string
	ConfigDir  string
	DataDir    string
	Workspace  string
	ConfigPath string
	EnvPath    string
}

// NewSetupState creates a SetupState with default paths.
func NewSetupState() *SetupState {
	ghostDir := os.Getenv("GHOST_DIR")
	if ghostDir == "" {
		ghostDir = DefaultGhostDir
	}

	return &SetupState{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}
}

// NeedsSetup returns true if Ghost has not been configured yet.
func (fb *SetupState) NeedsSetup() bool {
	// Check 1: setup-complete flag file
	flagPath := filepath.Join(fb.GhostDir, SetupCompleteFlag)
	if _, err := os.Stat(flagPath); err == nil {
		return false
	}

	// Check 2: config exists and has been customized
	if _, err := os.Stat(fb.ConfigPath); err == nil {
		cfg, err := config.LoadConfig(fb.ConfigPath)
		if err == nil && isConfigCustomized(cfg) {
			return false
		}
	}

	return true
}

// MarkSetupComplete writes the flag file to indicate setup is done.
func (fb *SetupState) MarkSetupComplete() error {
	flagPath := filepath.Join(fb.GhostDir, SetupCompleteFlag)
	return os.WriteFile(flagPath, []byte("setup complete\n"), 0644)
}

// ResetSetup removes the flag file and resets config to defaults.
func (fb *SetupState) ResetSetup() error {
	flagPath := filepath.Join(fb.GhostDir, SetupCompleteFlag)
	os.Remove(flagPath)
	return nil
}

// EnsureDirectories creates the required directory structure.
func (fb *SetupState) EnsureDirectories() error {
	dirs := []string{
		fb.GhostDir,
		fb.ConfigDir,
		fb.DataDir,
		fb.Workspace,
		filepath.Join(fb.Workspace, "skills"),
		filepath.Join(fb.Workspace, "memory"),
		filepath.Join(fb.Workspace, "sessions"),
		filepath.Join(fb.Workspace, "knowledge"),
		filepath.Join(fb.Workspace, "cron"),
		filepath.Join(fb.Workspace, "journal"),
		filepath.Join(fb.Workspace, "state"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// GenerateBridgeSecret creates a random 32-byte hex string.
func GenerateBridgeSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isConfigCustomized checks if config has been changed from defaults.
func isConfigCustomized(cfg *config.Config) bool {
	// Check if provider is still default
	defaultCfg := config.DefaultConfig()

	// If model is different from default, it's customized
	if cfg.Agents.Defaults.Model != defaultCfg.Agents.Defaults.Model {
		return true
	}

	// If any channel is enabled with a token, it's customized
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.Token != "" {
		return true
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.Token != "" {
		return true
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.BotToken != "" {
		return true
	}

	// If gateway secret is set and not the default empty
	if cfg.Gateway.BridgeSecret != "" && cfg.Gateway.BridgeSecret != defaultCfg.Gateway.BridgeSecret {
		return true
	}

	return false
}

// GeneratePairingCode creates a short code for mobile app pairing.
func GeneratePairingCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := hex.EncodeToString(b)
	// Format: XXXX-XXXX
	return strings.ToUpper(code[:4] + "-" + code[4:]), nil
}
