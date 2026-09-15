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

	if dryRun {
		fmt.Println("[dry-run] Migration preview (no changes made):")
		if err := migrateApplianceWorkspace(true); err != nil {
			fmt.Printf("Workspace migration check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Dry run complete.")
		return
	}

	services := []string{"ghost", "ghost-web", "ghost-speech"}
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
			for _, svc := range services {
				exec.Command("systemctl", "stop", svc).Run()
			}
		},
		Apply: func() error {
			// Migrate the workspace out of the install tree if the
			// running install still uses the legacy layout.
			fmt.Println("4. Checking workspace layout...")
			if err := migrateApplianceWorkspace(false); err != nil {
				return err
			}
			// Make install
			fmt.Println("5. Building and installing...")
			cmd := exec.Command("make", "-C", ghostDir, "install-ghost")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		Start: func() error {
			fmt.Println("Update failed — restarting services...")
			var first error
			for _, svc := range services {
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

	// Restart services
	fmt.Println("6. Restarting services...")
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
