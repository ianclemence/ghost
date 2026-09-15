package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
)

func updateCmd() {
	requireRoot()
	ghostDir := findGhostDir()

	dryRun := false
	for _, arg := range os.Args[2:] {
		if arg == "--dry-run" {
			dryRun = true
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
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		Plan: func() error {
			fmt.Println("2. Validating workspace layout (services still running)...")
			return appliance.CheckWorkspaceMigration(appliance.DefaultGhostDir)
		},
		Stop: func() {
			// Quiesce the appliance before touching its runtime
			// workspace, so the move never happens under a running
			// gateway with the DB open.
			fmt.Println("3. Stopping services...")
			exec.Command("systemctl", "stop", "ghost").Run()
			exec.Command("systemctl", "stop", "ghost-web").Run()
		},
		Apply: func() error {
			// Migrate the workspace out of the install tree if the
			// running install still uses the legacy layout. This must
			// happen before install-ghost restarts services with
			// GHOST_WORKSPACE_DIR pointing at /var/lib/ghost.
			fmt.Println("4. Checking workspace layout...")
			if err := migrateApplianceWorkspace(false); err != nil {
				return err
			}
			// Build and deploy (install-ghost restarts services).
			fmt.Println("5. Building and deploying...")
			cmd := exec.Command("make", "-C", ghostDir, "install-ghost")
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

// migrateApplianceWorkspace moves the appliance workspace from the legacy
// <ghostDir>/workspace location to the runtime location when needed. The
// appliance runs from DefaultGhostDir regardless of where the checkout (and
// therefore the git pull) lives, so this always targets the appliance dir.
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
		Stop: func() {
			// Quiesce the appliance before touching its runtime workspace.
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
			cmd := exec.Command("make", "-C", ghostDir, "install-ghost")
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
