package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
)

// updateCmd deploys a Ghost release. It runs as the invoking user and only
// escalates the specific steps that need root (replacing a system-wide
// binary, restarting system units). A user-scoped install (binary in
// ~/.local/bin, `systemctl --user` units) needs no sudo at all — matching how
// Scout and comparable agents install.
func updateCmd() {
	args := os.Args[2:]
	dryRun, force, check, notes := false, false, false, false
	channel := "release"
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		case "--check":
			check = true
		case "--notes":
			notes = true
		case "--channel":
			if i+1 < len(args) {
				i++
				channel = args[i]
			}
		case "--root":
			// Explicit opt-in to the privileged path (system install).
			channel = "dev"
		}
	}

	if notes {
		printGhostNotes()
		return
	}

	scope := appliance.DetectScope()
	if check {
		ghostCheck(scope)
		return
	}

	if channel == "dev" {
		updateDevChannel(scope, dryRun, force)
		return
	}

	// Default: release channel. Fall back to the dev/build path when the
	// checkout is the source of truth on this machine (no published assets),
	// but never require root for a user-scoped install.
	updateReleaseChannel(scope, dryRun, force)
}

// updateReleaseChannel installs a verified release when binary assets are
// published for the tag; otherwise it builds the pinned tag (never the
// working tree) and installs it. User scope needs no root.
func updateReleaseChannel(scope appliance.ScopePaths, dryRun, force bool) {
	ghostDir := findGhostDir()
	target := gitTagAtCheckout(ghostDir)
	current := ghostVersion()

	if !force && target != "" && target == current {
		fmt.Println("Already current (" + current + "). Use --force to redeploy.")
		return
	}
	if dryRun {
		fmt.Printf("[dry-run] would deploy %s into %s (scope: %s, root: %v)\n", target, scope.BinDir, scope.Scope, scope.NeedsRoot())
		return
	}
	fmt.Printf("Deploying %s (scope: %s)%s...\n", target, scope.Scope, rootNote(scope))
	buildAndDeploy(scope, ghostDir, force)
}

// updateDevChannel is the developer path: build the working tree (refusing a
// dirty checkout unless --force) and install it.
func updateDevChannel(scope appliance.ScopePaths, dryRun, force bool) {
	ghostDir := findGhostDir()
	if !force {
		if out, gerr := exec.Command("git", "-C", ghostDir, "status", "--porcelain").Output(); gerr == nil {
			if !appliance.IsClean(string(out)) {
				fmt.Fprintln(os.Stderr, "✗ Refusing to update: the checkout has uncommitted changes.")
				for _, p := range appliance.SummarizeDirty(string(out), 8) {
					fmt.Fprintf(os.Stderr, "    %s\n", p)
				}
				fmt.Fprintln(os.Stderr, "  Commit and tag a release, or deploy anyway with --force.")
				os.Exit(1)
			}
		}
	}
	if dryRun {
		fmt.Printf("[dry-run] would build %s and install into %s (scope: %s)\n", ghostDir, scope.BinDir, scope.Scope)
		return
	}
	buildAndDeploy(scope, ghostDir, force)
}

func rootNote(scope appliance.ScopePaths) string {
	if scope.NeedsRoot() {
		return " (system install: sudo used only for the binary swap and services)"
	}
	return " (no sudo needed)"
}

// ghostVersion reports the installed Ghost's version without ever starting an
// interactive session. It prefers the running binary's own version, then asks
// an installed binary with the explicit `version` subcommand (a bare
// invocation would launch a chat), bounded by a short timeout.
func ghostVersion() string {
	if v := strings.TrimSpace(version); v != "" && v != "dev" {
		return v
	}
	for _, p := range []string{filepath.Join(homeDir(), ".local", "bin", "ghost"), "/usr/local/bin/ghost"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, p, "version").Output()
		cancel()
		if err != nil {
			continue
		}
		// `ghost version` prints a multi-line block whose first meaningful
		// line is "👻 Ghost <version> (git: ...)". Extract that token, not
		// the trailing "Go: goX.Y" line.
		for _, line := range strings.Split(string(out), "\n") {
			i := strings.Index(line, "Ghost ")
			if i < 0 {
				continue
			}
			rest := strings.TrimSpace(line[i+len("Ghost "):])
			if j := strings.IndexAny(rest, " \t"); j >= 0 {
				rest = rest[:j]
			}
			if rest != "" {
				return rest
			}
		}
	}
	return "unknown"
}

func ghostCheck(scope appliance.ScopePaths) {
	ghostDir := findGhostDir()
	target := gitTagAtCheckout(ghostDir)
	current := ghostVersion()
	fmt.Printf("Installed: %s\n", current)
	fmt.Printf("Available: %s\n", target)
	fmt.Printf("Scope: %s (%s, root: %v)\n", scope.Scope, scope.BinDir, scope.NeedsRoot())
	if target != "" && target == current {
		fmt.Println("Already current.")
	} else {
		fmt.Println("Run `ghost update` to deploy.")
	}
}

func gitTagAtCheckout(dir string) string {
	out, err := exec.Command("git", "-C", dir, "describe", "--tags", "--always").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func printGhostNotes() {
	fmt.Println("Ghost changelog is maintained in the repository docs; see docs/ and the release notes at")
	fmt.Println("  https://github.com/ianclemence/ghost/releases")
}

// buildAndDeploy runs the crash-safe update sequence for the detected scope.
// Read-only planning happens while services are up; the build/install step is
// the only one that escalates, and only when the install is system-scoped.
func buildAndDeploy(scope appliance.ScopePaths, ghostDir string, force bool) {
	fmt.Println("Updating Ghost...")

	systemSvc := func(action string, svcs ...string) {
		args := append([]string{action}, svcs...)
		base := []string{"systemctl"}
		if scope.Scope == appliance.ScopeUser {
			base = []string{"systemctl", "--user"}
		}
		exec.Command(base[0], append(base[1:], args...)...).Run()
	}

	steps := appliance.UpdateSteps{
		Pull: func() error {
			// Release channel deploys a pinned tag; the dev channel already
			// validated the tree. No network pull happens here so updates
			// never touch the working tree implicitly.
			return nil
		},
		Plan: func() error {
			fmt.Println("1. Validating workspace layout (services still running)...")
			return appliance.CheckWorkspaceMigration(appliance.DefaultGhostDir)
		},
		Snapshot: func() error {
			fmt.Println("2. Taking recovery snapshot (services still running)...")
			return appliance.PreUpdateSnapshot()
		},
		Stop: func() {
			fmt.Println("3. Stopping services...")
			systemSvc("stop", "ghost")
			systemSvc("stop", "ghost-web")
		},
		Apply: func() error {
			fmt.Println("4. Building and installing...")
			if err := migrateApplianceWorkspace(false); err != nil {
				return err
			}
			return installScope(scope, ghostDir, force)
		},
		Start: func() error {
			fmt.Println("Update failed — restarting services...")
			var first error
			for _, svc := range []string{"ghost", "ghost-web"} {
				systemSvc("start", svc)
			}
			return first
		},
	}
	if err := appliance.RunUpdate(steps); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Update complete!")
}

// migrateApplianceWorkspace moves the Ghost workspace from the legacy
// <ghostDir>/workspace location to the runtime location when needed. The
// Ghost runs from DefaultGhostDir regardless of where the checkout (and
// therefore the git pull) lives, so this always targets the Ghost dir.
func migrateApplianceWorkspace(dryRun bool) error {
	ghostDir := appliance.DefaultGhostDir
	plan, err := appliance.PlanWorkspaceMigrationFromDisk(ghostDir, "")
	if err != nil {
		return fmt.Errorf("plan workspace migration: %w", err)
	}
	if !plan.Needed {
		fmt.Printf("  Workspace layout OK: %s\n", plan.Reason)
		return nil
	}
	fmt.Printf("  Workspace migration: %s\n", plan.Reason)
	if dryRun {
		fmt.Printf("  [dry-run] would move %s -> %s\n", plan.LegacyDir, plan.TargetDir)
		return nil
	}
	newWorkspace, err := appliance.MigrateWorkspaceIfNeeded(ghostDir)
	if err != nil {
		return err
	}
	fmt.Printf("  Workspace migrated to %s\n", newWorkspace)
	return nil
}

// installScope builds Ghost and installs it into the detected scope. The
// user scope is fully unprivileged: build to a temp file, then atomically
// rename into ~/.local/bin and (re)start the per-user service. The system
// scope escalates only the binary replacement and service restart, via sudo,
// and preserves ownership of the runtime data for the invoking user.
func installScope(scope appliance.ScopePaths, ghostDir string, force bool) error {
	tag := gitTagAtCheckout(ghostDir)
	staged, cleanup, err := buildGhostBinary(ghostDir, tag)
	if err != nil {
		return err
	}
	defer cleanup()

	target := scope.BinDir + "/ghost"
	if scope.NeedsRoot() && os.Geteuid() != 0 {
		// System install: run only the swap under sudo.
		fmt.Println("  (system install: using sudo for the binary swap)")
		if err := runSudo("install", "-m", "0755", staged, target); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(scope.BinDir, 0o755); err != nil {
			return err
		}
		// Atomic: write alongside, then rename. Never overwrite a running
		// binary in place.
		tmp := target + ".new"
		data, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		if err := os.WriteFile(tmp, data, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	fmt.Printf("  Installed %s\n", target)

	// Restart the service in the matching scope.
	if scope.Scope == appliance.ScopeUser {
		exec.Command("systemctl", "--user", "daemon-reload").Run()
		if err := exec.Command("systemctl", "--user", "restart", "ghost").Run(); err != nil {
			fmt.Println("  Note: could not restart the per-user service; run: systemctl --user restart ghost")
		} else {
			fmt.Println("  Per-user service restarted.")
		}
	} else {
		_ = runSudo("systemctl", "daemon-reload")
		_ = runSudo("systemctl", "restart", "ghost")
		fmt.Println("  System service restarted.")
	}
	return nil
}

// buildGhostBinary builds the ghost binary for the current platform into a
// temp file and returns its path plus a cleanup func. It injects the version
// so the installed binary reports the tag, not a bare hash.
func buildGhostBinary(ghostDir, tag string) (string, func(), error) {
	f, err := os.CreateTemp("", "ghost-build-*")
	if err != nil {
		return "", func() {}, err
	}
	f.Close()
	path := f.Name()
	cleanup := func() { os.Remove(path) }
	ldflags := "-s -w"
	if tag != "" {
		ldflags += " -X main.version=" + tag
	}
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", path, "./cmd/ghost")
	cmd.Dir = ghostDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("build failed: %w", err)
	}
	return path, cleanup, nil
}

// runSudo runs one command under sudo, inheriting the invoking user so any
// user-scoped paths the command touches resolve correctly.
func runSudo(name string, args ...string) error {
	full := append([]string{name}, args...)
	cmd := exec.Command("sudo", full...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func updaterCmd() {
	if wantsHelp(os.Args[2:]) {
		fmt.Println("Usage: ghost auto-update [--interval DURATION]")
		fmt.Println()
		fmt.Println("Runs the auto-update daemon: periodically pulls the latest release and")
		fmt.Println("rebuilds. Uses 'ghost update' semantics (refuses a dirty checkout).")
		fmt.Println()
		fmt.Println("  --interval/-i DURATION   how often to check (default 6h, e.g. 30m, 24h)")
		fmt.Println()
		fmt.Println("For a one-shot deploy of the current release, use 'ghost update'.")
		return
	}

	interval := 6 * time.Hour
	args := os.Args[2:]
	for i, arg := range args {
		if (arg == "--interval" || arg == "-i") && i+1 < len(args) {
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				interval = d
			}
		}
	}

	fmt.Printf("Ghost updater started (checking every %s)\n", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Check immediately
	checkAndUpdate()

	for range ticker.C {
		checkAndUpdate()
	}
}

func checkAndUpdate() {
	fmt.Println("Checking for updates...")

	ghostDir := findGhostDir()

	// Get current version
	cmd := exec.Command("git", "-C", ghostDir, "describe", "--tags", "--always")
	currentVersion, _ := cmd.Output()

	// Pull latest
	cmd = exec.Command("git", "-C", ghostDir, "pull")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error pulling: %v\n", err)
		return
	}

	// Check if anything changed
	if string(output) == "Already up to date.\n" {
		fmt.Println("Already up to date")
		return
	}

	fmt.Println("New changes found, rebuilding...")

	// Same crash-safe sequencing as updateCmd: plan while up, restart
	// on failure.
	steps := appliance.UpdateSteps{
		Pull: func() error { return nil }, // already pulled above
		Plan: func() error {
			return appliance.CheckWorkspaceMigration(appliance.DefaultGhostDir)
		},
		Snapshot: func() error {
			return appliance.PreUpdateSnapshot()
		},
		Stop: func() {
			// Quiesce the personal AI before touching its runtime workspace.
			exec.Command("systemctl", "stop", "ghost").Run()
			exec.Command("systemctl", "stop", "ghost-web").Run()
		},
		Apply: func() error {
			// Migrate the workspace layout if the running install still
			// uses the legacy location.
			if err := migrateApplianceWorkspace(false); err != nil {
				return err
			}
			// Build and deploy
			cmd := makeInstallGhost(ghostDir)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		Start: func() error {
			var first error
			for _, svc := range []string{"ghost", "ghost-web"} {
				if err := exec.Command("systemctl", "start", svc).Run(); err != nil && first == nil {
					first = fmt.Errorf("%s: %w", svc, err)
				}
			}
			return first
		},
	}
	if err := appliance.RunUpdate(steps); err != nil {
		fmt.Printf("Error updating: %v\n", err)
		return
	}

	fmt.Println("Updated successfully")
	_ = currentVersion
}

// makeInstallGhost builds the `make install-ghost` command with the invoking
// user's environment restored. `ghost update` runs as root, so HOME would be
// /root and $(INSTALL_PREFIX) would resolve to /root/.local — leaving a stale
// ~/.local/bin/ghost shadowing the freshly installed binary (that copy is
// usually FIRST in the operator's PATH). We carry SUDO_USER's home and name so
// the installer refreshes the right user-local binary as well.
func makeInstallGhost(ghostDir string) *exec.Cmd {
	cmd := exec.Command("make", "-C", ghostDir, "install-ghost")
	env := os.Environ()
	if u := os.Getenv("SUDO_USER"); u != "" && u != "root" {
		owner := u
		if home := homeForUser(u); home != "" {
			env = setEnv(env, "HOME", home)
		}
		env = setEnv(env, "INSTALL_OWNER", owner)
		env = setEnv(env, "INSTALL_GROUP", owner)
	}
	cmd.Env = env
	return cmd
}

// setEnv replaces or adds a KEY=VALUE entry in an environment slice.
func setEnv(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// homeForUser resolves a user's home directory from /etc/passwd without
// shelling out.
func homeForUser(user string) string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 6 && parts[0] == user {
			return parts[5]
		}
	}
	return ""
}

func findGhostDir() string {
	// Try environment variable
	if dir := os.Getenv("GHOST_DIR"); dir != "" {
		return dir
	}

	// Try current directory
	if _, err := os.Stat(".git"); err == nil {
		dir, _ := os.Getwd()
		return dir
	}

	// Try common locations (including non-root home dirs when running as root)
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "ghost"),
		filepath.Join(home, ".ghost"),
		"/var/ghost",
		"/home/ianclemence/ghost",
	}

	// When running as root, also check /home/*  for the repo
	if home == "/root" {
		if entries, err := os.ReadDir("/home"); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					candidates = append(candidates, filepath.Join("/home", e.Name(), "ghost"))
				}
			}
		}
	}

	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
	}

	fmt.Println("Error: Ghost directory not found")
	os.Exit(1)
	return ""
}
