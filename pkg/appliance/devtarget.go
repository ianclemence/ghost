package appliance

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// DefaultBinDir is the single canonical directory the `ghost` binary is
// installed to. Every service ExecStart and every documented install path uses
// it; a second copy elsewhere on PATH is what caused updates to appear not to
// take.
const DefaultBinDir = "/usr/local/bin"

// Development vs production separation.
//
// A Ghost source checkout and an installed Ghost must never share a config,
// workspace, or database, or prototype work silently reaches production. The
// dev target is a fully isolated instance rooted at a dev directory with its
// own config, workspace, data, and port. `ghost dev` runs the checkout against
// this target; `ghost update` deploys tagged releases to the production target
// (/var/ghost). The two never touch each other.

// Dev environment defaults. Overridable by env so a developer can place the
// instance anywhere.
const (
	DefaultDevGhostDir = ".ghost-dev"
	DefaultDevPort     = 8877
)

// DevTarget describes an isolated development instance.
type DevTarget struct {
	GhostDir  string
	ConfigDir string
	Workspace string
	DataDir   string
	Port      int
}

// ResolveDevTarget computes the isolated dev paths for a checkout rooted at
// home (the user's home directory) and a requested port. An empty port uses
// GHOST_DEV_PORT if set, else DefaultDevPort. It deliberately does NOT consult
// GHOST_API_PORT: the production .env sets that to the production port, and a
// dev instance must never collide with production. It performs no I/O so it is
// fully testable.
//
// Precedence for the dev root: GHOST_DEV_DIR env, else home/.ghost-dev.
func ResolveDevTarget(getenv func(string) string, home string, port int) DevTarget {
	root := getenv("GHOST_DEV_DIR")
	if root == "" {
		root = filepath.Join(home, DefaultDevGhostDir)
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(home, root)
	}
	if port <= 0 {
		if p := getenv("GHOST_DEV_PORT"); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				port = n
			}
		}
	}
	if port <= 0 {
		port = DefaultDevPort
	}
	return DevTarget{
		GhostDir:  root,
		ConfigDir: filepath.Join(root, "config"),
		Workspace: filepath.Join(root, "workspace"),
		DataDir:   filepath.Join(root, "data"),
		Port:      port,
	}
}

// ConflictsWithProduction reports whether the dev target would operate on the
// same directory as the installed Ghost. It is the safety check behind
// `ghost dev`: a dev instance must never point at /var/ghost.
func (t DevTarget) ConflictsWithProduction() bool {
	return cleanAbs(t.GhostDir) == cleanAbs(DefaultGhostDir) ||
		cleanAbs(t.ConfigDir) == cleanAbs(DefaultConfigDir) ||
		cleanAbs(t.Workspace) == cleanAbs(DefaultWorkspaceDir)
}

func cleanAbs(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// Describe renders a short human summary for the `ghost dev` banner.
func (t DevTarget) Describe() string {
	return fmt.Sprintf("dir=%s port=%d", t.GhostDir, t.Port)
}
