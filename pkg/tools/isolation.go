package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/egress"
)

// IsolationMode controls OS-level isolation for model-initiated subprocesses.
type IsolationMode string

const (
	// IsolationOff disables namespace isolation (env sanitization still applies).
	IsolationOff IsolationMode = "off"
	// IsolationAuto uses isolation when the platform supports it, otherwise
	// falls back to running with env sanitization only.
	IsolationAuto IsolationMode = "auto"
	// IsolationRequire refuses to run unless isolation is available.
	IsolationRequire IsolationMode = "require"
)

// isolationMode reads GHOST_EXEC_ISOLATION (default require). Require is
// the fail-closed default: model-initiated subprocesses refuse to run
// without OS-level isolation rather than silently downgrading to
// unsandboxed execution. Operators on machines without bubblewrap opt out
// explicitly with GHOST_EXEC_ISOLATION=auto (isolate when possible) or
// =off (env sanitization only).
func isolationMode() IsolationMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GHOST_EXEC_ISOLATION"))) {
	case "off":
		return IsolationOff
	case "auto":
		return IsolationAuto
	default:
		return IsolationRequire
	}
}

// WrapArgv wraps a subprocess argv in OS-level isolation when available and
// enabled. It returns (argv, wrapped, error). wrapped reports whether the
// isolation wrapper was applied. On a platform without isolation, auto mode
// returns the original argv unchanged; require mode returns an error.
//
// The isolation (Linux/bubblewrap) gives a read-only root, a private /tmp,
// process/IPC/UTS namespaces, and — critically — does NOT bind the config
// directory, so provider credentials are not reachable from the sandbox.
// Ghost's own workspace is bound read-only: only the scratch directory and
// the owner-content areas (memory notes, knowledge, captures, learning)
// are writable, and HOME points at scratch. A user project outside the
// workspace is bound read-write so real work can happen there.
func WrapArgv(base []string, cwd, workspace string, allowNetwork bool) ([]string, bool, error) {
	if len(base) == 0 {
		return base, false, nil
	}
	mode := isolationMode()
	if mode == IsolationOff {
		return base, false, nil
	}
	if !bwrapAvailable() {
		if mode == IsolationRequire {
			return nil, false, fmt.Errorf("command isolation is required but bubblewrap (bwrap) is not available")
		}
		return base, false, nil
	}
	if pol := egress.FromEnv(); allowNetwork && pol.Enforced() {
		return wrapEgress(base, cwd, workspace, pol)
	}
	wrapped := append([]string{"bwrap"}, bwrapArgs(cwd, workspace, allowNetwork)...)
	wrapped = append(wrapped, base...)
	return wrapped, true, nil
}

// wrapEgress runs base in a sandbox with no network of its own. Its only way
// out is the host proxy, reached through a bridge that Ghost's own binary runs
// inside the sandbox (see package egress). If that cannot be set up the
// command does not run: a guard that fails open is not a guard.
func wrapEgress(base []string, cwd, workspace string, pol egress.Policy) ([]string, bool, error) {
	sock, err := egress.SocketPath(pol)
	if err != nil {
		return nil, false, fmt.Errorf("egress guard is on but its proxy could not start: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return nil, false, fmt.Errorf("egress guard is on but Ghost cannot find its own binary: %w", err)
	}
	const inDir = "/run/ghost-egress"
	args := bwrapArgs(cwd, workspace, false)
	args = append(args,
		"--ro-bind", self, inDir+"/ghost",
		"--bind", filepath.Dir(sock), inDir+"/sock",
	)
	wrapped := append([]string{"bwrap"}, args...)
	wrapped = append(wrapped, inDir+"/ghost", "__egress-exec", inDir+"/sock/"+filepath.Base(sock), "--")
	wrapped = append(wrapped, base...)
	return wrapped, true, nil
}

// IsolationActive reports whether subprocess isolation is currently in force
// (used by Doctor/tests). It does not consider the per-call network flag.
func IsolationActive() bool {
	return isolationMode() != IsolationOff && bwrapAvailable()
}
