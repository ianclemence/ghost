package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func updateCmd() {
	requireRoot()
	ghostDir := findGhostDir()

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

	// Build and deploy
	fmt.Println("2. Building and deploying...")
	cmd = exec.Command("make", "-C", ghostDir, "install-ghost")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error building: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Update complete!")
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

	// Build and deploy
	cmd = exec.Command("make", "-C", ghostDir, "install-ghost")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error building: %v\n", err)
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

	// Try common locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "ghost"),
		filepath.Join(home, ".ghost"),
		"/var/ghost",
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
