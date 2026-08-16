package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if os.Geteuid() != 0 {
		fmt.Println("This command must be run as root (e.g. 'sudo ghost-update')")
		os.Exit(1)
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

	// Make install
	fmt.Println("2. Building and installing...")
	cmd = exec.Command("make", "-C", ghostDir, "install-ghost")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error building: %v\n", err)
		os.Exit(1)
	}

	// Restart services
	fmt.Println("3. Restarting services...")
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "restart", "ghost").Run()

	fmt.Println("Update complete!")
}
