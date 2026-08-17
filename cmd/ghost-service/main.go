package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	ghostDir := "/var/ghost"
	if dir := os.Getenv("GHOST_DIR"); dir != "" {
		ghostDir = dir
	}

	setupComplete := filepath.Join(ghostDir, ".setup-complete")

	// Check if setup is complete
	if _, err := os.Stat(setupComplete); os.IsNotExist(err) {
		// Setup not complete - start web console (setup wizard + dashboard)
		fmt.Println("Setup not complete. Starting setup wizard...")
		fmt.Println("Open http://<your-pi-ip> on your phone to continue.")
		cmd := exec.Command("systemctl", "start", "ghost-web")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("Failed to start setup wizard: %v\n", err)
			fmt.Println("Try: sudo systemctl start ghost-web")
		}
		return
	}

	// Setup complete - start ghost
	fmt.Println("Starting Ghost...")
	cmd := exec.Command("systemctl", "start", "ghost")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to start Ghost: %v\n", err)
		fmt.Println("Try: sudo systemctl start ghost")
		return
	}
	fmt.Println("Ghost started. Access at http://<your-pi-ip>:8766")
}
