package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
)

func updateCmd() {
	requireRoot()
	ghostDir := findGhostDir()

	dryRun := false
	force := false
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		}
	}

	// Release guard: production must run a release, not a working tree.
	// A dirty checkout would build and deploy uncommitted (prototype) work
	// straight into the running install. Refuse unless --force.
	if !force {
		if out, gerr := exec.Command("git", "-C", ghostDir, "status", "--porcelain").Output(); gerr == nil {
			if !appliance.IsClean(string(out)) {
				fmt.Fprintln(os.Stderr, "✗ Refusing to update: the checkout has uncommitted changes.")
				fmt.Fprintln(os.Stderr, "  Production must run a release, not a working tree. Uncommitted paths:")
				for _, p := range appliance.SummarizeDirty(string(out), 8) {
					fmt.Fprintf(os.Stderr, "    %s\n", p)
				}
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, "  Commit and tag a release, then run `ghost update`; or")
				fmt.Fprintln(os.Stderr, "  test changes in an isolated instance with `ghost dev`; or")
				fmt.Fprintln(os.Stderr, "  deploy anyway with `ghost update --force`.")
				os.Exit(1)
			}
		}
	}

	fmt.Println("Updating Ghost...")

	if dryRun {
		fmt.Println("[dry-run] Migration preview (no changes made):")
		if err := migrateApplianceWorkspace(true); err != nil {
			fmt.Printf("Workspace migration check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Dry run complete.")
		return
	}

	// Crash-safe sequencing (pkg/appliance.RunUpdate): read-only
	// migration planning runs while services are still up, so a sealed
	// config or unreadable layout aborts before anything stops; any
	// failure after the stop triggers a best-effort restart.
	steps := appliance.UpdateSteps{
		Pull: func() error {
			fmt.Println("1. Pulling latest changes...")
			cmd := exec.Command("git", "-C", ghostDir, "pull")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return err
			}
			// No-change updates used to snapshot, stop, rebuild, and
			// restart every service for zero benefit. Exit here instead;
			// --force still redeploys on demand. Print exactly one
			// verdict line: git's own "Already up to date." is swallowed
			// so it never appears twice.
			if !force && strings.Contains(out.String(), "Already up to date.") {
				fmt.Println("Already up to date — nothing to deploy. Use --force to redeploy anyway.")
				os.Exit(0)
			}
			fmt.Print(out.String())
			return nil
		},
		Plan: func() error {
			fmt.Println("2. Validating workspace layout (services still running)...")
			return appliance.CheckWorkspaceMigration(appliance.DefaultGhostDir)
		},
		Snapshot: func() error {
			fmt.Println("3. Taking recovery snapshot (services still running)...")
			return appliance.PreUpdateSnapshot()
		},
		Stop: func() {
			// Quiesce the personal AI before touching its runtime
			// workspace, so the move never happens under a running
			// gateway with the DB open.
			fmt.Println("4. Stopping services...")
			exec.Command("systemctl", "stop", "ghost").Run()
			exec.Command("systemctl", "stop", "ghost-web").Run()
		},
		Apply: func() error {
			// Migrate the workspace out of the install tree if the
			// running install still uses the legacy layout. This must
			// happen before install-ghost restarts services with
			// GHOST_WORKSPACE_DIR pointing at /var/lib/ghost.
			fmt.Println("5. Applying workspace layout, building and deploying...")
			if err := migrateApplianceWorkspace(false); err != nil {
				return err
			}
			// Build and deploy (install-ghost restarts services).
			cmd := makeInstallGhost(ghostDir)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		Start: func() error {
			fmt.Println("Update failed — restarting services...")
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

func requireRoot() {
	if os.Geteuid() != 0 {
		fmt.Println("This command must be run as root (e.g. 'sudo ghost update')")
		os.Exit(1)
	}
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
