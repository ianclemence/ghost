package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ianclemence/ghost/pkg/appliance"
)

func main() {
	if os.Geteuid() != 0 {
		fmt.Println("This command must be run as root (e.g. 'sudo ghost-update')")
		os.Exit(1)
	}

	dryRun := false
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" {
			dryRun = true
		}
	}

	ghostDir := "/home/ianclemence/ghost"
	if dir := os.Getenv("GHOST_DIR"); dir != "" {
		ghostDir = dir
	}

	// Find ghost directory
	if _, err := os.Stat(filepath.Join(ghostDir, ".git")); os.IsNotExist(err) {
		// Try current directory
		if _, err := os.Stat(".git"); err == nil {
			ghostDir, _ = os.Getwd()
		} else {
			fmt.Println("Error: Ghost directory not found")
			os.Exit(1)
		}
	}

	fmt.Println("Updating Ghost...")

	// Git pull
	fmt.Println("1. Pulling latest changes...")
	cmd := exec.Command("git", "-C", ghostDir, "pull")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error pulling changes: %v\n", err)
		os.Exit(1)
	}

	if dryRun {
		fmt.Println("2. [dry-run] Migration preview (no changes made):")
		if err := migrateApplianceWorkspace(true); err != nil {
			fmt.Printf("Workspace migration check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Dry run complete.")
		return
	}

	// Quiesce the appliance before touching its runtime workspace, so the
	// move never happens under a running gateway with the DB open.
	fmt.Println("2. Stopping services...")
	exec.Command("systemctl", "stop", "ghost").Run()
	exec.Command("systemctl", "stop", "ghost-web").Run()

	// Migrate the workspace out of the install tree if the running install
	// still uses the legacy layout. This must happen before install-ghost
	// restarts services with GHOST_WORKSPACE_DIR pointing at /var/lib/ghost.
	fmt.Println("3. Checking workspace layout...")
	if err := migrateApplianceWorkspace(false); err != nil {
		fmt.Printf("Workspace migration failed: %v\n", err)
		os.Exit(1)
	}

	// Make install
	fmt.Println("4. Building and installing...")
	cmd = exec.Command("make", "-C", ghostDir, "install-ghost")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error building: %v\n", err)
		os.Exit(1)
	}

	// Restart services
	fmt.Println("5. Restarting services...")
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "restart", "ghost").Run()

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
