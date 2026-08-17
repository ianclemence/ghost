package appliance

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// DefaultWorkspaceDir is where an appliance keeps its runtime workspace.
// It lives outside the install tree so user data never mixes with the
// deployment and never interferes with git pulls in checkout layouts.
const DefaultWorkspaceDir = "/var/lib/ghost/workspace"

// ResolveWorkspaceDir returns the runtime workspace location. The
// GHOST_WORKSPACE_DIR environment variable wins (set by the systemd units);
// otherwise the legacy <ghostDir>/workspace location is used, which keeps
// manual and dev runs working unchanged.
func ResolveWorkspaceDir(ghostDir string) string {
	if d := os.Getenv("GHOST_WORKSPACE_DIR"); d != "" {
		return d
	}
	if ghostDir != "" {
		return filepath.Join(ghostDir, "workspace")
	}
	return DefaultWorkspaceDir
}

// WorkspaceMigrationPlan describes moving the appliance workspace from the
// legacy <ghostDir>/workspace location inside the install tree to
// DefaultWorkspaceDir.
type WorkspaceMigrationPlan struct {
	GhostDir        string
	LegacyDir       string
	TargetDir       string
	ConfigPath      string
	EnvPath         string
	ConfigWorkspace string
	Needed          bool
	RewriteConfig   bool
	RewriteEnv      bool
	Reason          string
}

// PlanWorkspaceMigration is a pure decision about whether a migration is
// needed. It never touches the filesystem. A migration is only needed when
// the configured workspace is exactly the legacy layout; anything custom is
// left alone.
func PlanWorkspaceMigration(currentWorkspace, legacyDir, targetDir string) *WorkspaceMigrationPlan {
	p := &WorkspaceMigrationPlan{
		LegacyDir:       legacyDir,
		TargetDir:       targetDir,
		ConfigWorkspace: currentWorkspace,
	}
	switch {
	case currentWorkspace == "":
		p.Needed = false
		p.Reason = "no configured workspace; nothing to migrate"
	case currentWorkspace == targetDir:
		p.Needed = false
		p.Reason = "workspace already at runtime location"
	case currentWorkspace != legacyDir:
		p.Needed = false
		p.Reason = fmt.Sprintf("workspace uses a custom location (%s); not the legacy layout", currentWorkspace)
	default:
		p.Needed = true
		p.RewriteConfig = true
		p.Reason = fmt.Sprintf("workspace lives inside the install tree at %s", legacyDir)
	}
	return p
}

// MigrationTarget returns where a migration should move the workspace to:
// the GHOST_WORKSPACE_DIR override when set (the systemd units), otherwise
// the default runtime location. Unlike ResolveWorkspaceDir it never falls
// back to the install tree, since migration exists precisely to escape it.
func MigrationTarget(ghostDir string) string {
	if d := os.Getenv("GHOST_WORKSPACE_DIR"); d != "" {
		return d
	}
	return DefaultWorkspaceDir
}

// PlanWorkspaceMigrationFromDisk builds a full plan from the current on-disk
// state: the configured workspace, the environment file, and the target.
func PlanWorkspaceMigrationFromDisk(ghostDir, targetDir string) (*WorkspaceMigrationPlan, error) {
	legacyDir := filepath.Join(ghostDir, "workspace")
	if targetDir == "" {
		targetDir = MigrationTarget(ghostDir)
	}
	plan := &WorkspaceMigrationPlan{
		GhostDir:   ghostDir,
		LegacyDir:  legacyDir,
		TargetDir:  targetDir,
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}
	if _, err := os.Stat(plan.ConfigPath); err == nil {
		cfg, err := config.LoadConfig(plan.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load config for migration plan: %w", err)
		}
		plan.ConfigWorkspace = cfg.WorkspacePath()
	}
	decision := PlanWorkspaceMigration(plan.ConfigWorkspace, legacyDir, targetDir)
	plan.Needed = decision.Needed
	plan.RewriteConfig = decision.RewriteConfig
	plan.Reason = decision.Reason
	plan.RewriteEnv = envPointsAt(plan.EnvPath, legacyDir)
	return plan, nil
}

// MigrateWorkspaceIfNeeded moves the appliance workspace from the legacy
// <ghostDir>/workspace location to the runtime location when needed. It is
// safe to call on every gateway start and during updates: it only acts when
// the configured workspace is exactly the legacy layout, and it refuses to
// overwrite a non-empty target. Returns the new workspace path when a
// migration happened, or an empty string otherwise.
func MigrateWorkspaceIfNeeded(ghostDir string) (string, error) {
	plan, err := PlanWorkspaceMigrationFromDisk(ghostDir, "")
	if err != nil {
		return "", err
	}
	if !plan.Needed {
		return "", nil
	}
	if nonEmpty(plan.TargetDir) {
		return "", fmt.Errorf("refusing to migrate: target workspace %s is not empty", plan.TargetDir)
	}
	if err := ApplyWorkspaceMigration(plan); err != nil {
		return "", err
	}
	return plan.TargetDir, nil
}

// ApplyWorkspaceMigration executes a plan: physically move the workspace,
// then rewrite config.json and .env so everything points at the new location.
func ApplyWorkspaceMigration(plan *WorkspaceMigrationPlan) error {
	if !plan.Needed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0755); err != nil {
		return fmt.Errorf("create workspace parent: %w", err)
	}
	if err := moveDir(plan.LegacyDir, plan.TargetDir); err != nil {
		return fmt.Errorf("move workspace: %w", err)
	}
	if plan.RewriteConfig {
		if err := rewriteConfigWorkspace(plan.ConfigPath, plan.TargetDir); err != nil {
			return fmt.Errorf("rewrite config: %w", err)
		}
	}
	if plan.RewriteEnv {
		if err := rewriteEnvWorkspace(plan.EnvPath, plan.LegacyDir, plan.TargetDir); err != nil {
			return fmt.Errorf("rewrite env: %w", err)
		}
	}
	return nil
}

// moveDir renames src to dst, falling back to a copy+delete when the rename
// crosses a filesystem boundary (EXDEV).
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, d, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if ok := asLinkError(err, &linkErr); ok {
		return strings.Contains(linkErr.Err.Error(), "cross-device")
	}
	return false
}

func asLinkError(err error, target **os.LinkError) bool {
	if le, ok := err.(*os.LinkError); ok {
		*target = le
		return true
	}
	return false
}

// rewriteConfigWorkspace points the configured workspace at the runtime dir.
func rewriteConfigWorkspace(configPath, targetDir string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}
	cfg.Agents.Defaults.Workspace = targetDir
	return config.SaveConfig(configPath, cfg)
}

// rewriteEnvWorkspace rewrites values that point into the legacy workspace
// (GHOST_DB_PATH, MEMORY_DIR, etc.) to the target location. Missing files or
// files with no matching values are left untouched.
func rewriteEnvWorkspace(envPath, legacyDir, targetDir string) error {
	b, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(b), "\n")
	changed := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if val == legacyDir || strings.HasPrefix(val, legacyDir+string(filepath.Separator)) {
			lines[i] = key + "=" + targetDir + strings.TrimPrefix(val, legacyDir)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600)
}

// envPointsAt reports whether the .env file references the given path.
func envPointsAt(envPath, path string) bool {
	b, err := os.ReadFile(envPath)
	if err != nil {
		return false
	}
	for _, ln := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		_, val, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if val == path || strings.HasPrefix(val, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func nonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}
