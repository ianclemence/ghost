package appliance

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

const (
	SetupCompleteFlag = ".setup-complete"
	// SetupCodeFileName holds the one-time setup code that gates the first
	// configure. It proves local presence: only someone who can read the
	// device's console output (journal/terminal) or its filesystem can claim
	// an unconfigured Ghost.
	SetupCodeFileName = ".setup-code"
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
	// Sensitive directories use 0700 (owner-only access).
	sensitiveDirs := []string{
		fb.GhostDir,
		fb.ConfigDir,
		fb.DataDir,
	}
	for _, dir := range sensitiveDirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	// Workspace directories use 0755 (owner full, others read/execute).
	workspaceDirs := []string{
		fb.Workspace,
		filepath.Join(fb.Workspace, "skills"),
		filepath.Join(fb.Workspace, "memory"),
		filepath.Join(fb.Workspace, "sessions"),
		filepath.Join(fb.Workspace, "knowledge"),
		filepath.Join(fb.Workspace, "cron"),
		filepath.Join(fb.Workspace, "journal"),
		filepath.Join(fb.Workspace, "state"),
	}
	for _, dir := range workspaceDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
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

	return false
}

// RotateSetupCode mints a fresh setup code and persists it (0600) so the first
// configure can prove local presence. It is called on every ghost-web start
// while Ghost is unconfigured, so a code leaked in an older log is invalidated
// by the next restart.
func (fb *SetupState) RotateSetupCode() (string, error) {
	code, err := randomSetupCode()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(fb.GhostDir, SetupCodeFileName), []byte(code+"\n"), 0600); err != nil {
		return "", err
	}
	return code, nil
}

// VerifySetupCode reports whether code matches the stored setup code, compared
// in constant time. A missing or empty code file never verifies.
func (fb *SetupState) VerifySetupCode(code string) bool {
	stored, err := os.ReadFile(filepath.Join(fb.GhostDir, SetupCodeFileName))
	if err != nil {
		return false
	}
	want := strings.TrimSpace(string(stored))
	got := strings.TrimSpace(code)
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// ClearSetupCode removes the setup code once setup is complete.
func (fb *SetupState) ClearSetupCode() {
	os.Remove(filepath.Join(fb.GhostDir, SetupCodeFileName))
}

// randomSetupCode returns a zero-padded 6-digit code from crypto/rand.
func randomSetupCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
