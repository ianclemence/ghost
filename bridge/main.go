//go:build !ghost_bridge_retired
// +build !ghost_bridge_retired

package main

import (
	"log"
	"os"
)

func main() {
	log.Println("ghost-bridge is retired. All functionality has been")
	log.Println("consolidated into the Ghost internal API on port 8766.")
	log.Println("You can safely remove the ghost-bridge service:")
	log.Println("  sudo systemctl stop ghost-bridge")
	log.Println("  sudo systemctl disable ghost-bridge")
	os.Exit(0)
}
