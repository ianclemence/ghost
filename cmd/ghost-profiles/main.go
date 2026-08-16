package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ianclemence/ghost/pkg/profiles"
)

func main() {
	homeDir, _ := os.UserHomeDir()
	manager := profiles.NewManager(homeDir)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "list":
		cmdList(manager)
	case "current":
		cmdCurrent(manager)
	case "switch":
		cmdSwitch(manager, args)
	case "create":
		cmdCreate(manager, args)
	case "delete":
		cmdDelete(manager, args)
	case "env":
		cmdEnv(manager, args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: ghost-profiles <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list              List all profiles")
	fmt.Println("  current           Show active profile")
	fmt.Println("  switch <name>     Switch to a profile")
	fmt.Println("  create <name> [desc]  Create a new profile")
	fmt.Println("  delete <name>     Delete a profile")
	fmt.Println("  env <name>        Show profile environment variables")
}

func cmdList(manager *profiles.Manager) {
	profiles, err := manager.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(profiles) == 0 {
		fmt.Println("No profiles found.")
		return
	}

	active := manager.ActiveProfile()
	for _, p := range profiles {
		marker := "  "
		if p.Name == active {
			marker = "* "
		}
		fmt.Printf("%s%s - %s\n", marker, p.Name, p.Description)
	}
}

func cmdCurrent(manager *profiles.Manager) {
	active := manager.ActiveProfile()
	p, err := manager.Get(active)
	if err != nil {
		fmt.Printf("Active profile: %s (not found)\n", active)
		return
	}
	fmt.Printf("Name: %s\n", p.Name)
	fmt.Printf("Description: %s\n", p.Description)
	fmt.Printf("Ghost Home: %s\n", p.GhostHome)
	fmt.Printf("Workspace: %s\n", p.WorkspacePath())
}

func cmdSwitch(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles switch <name>\n")
		os.Exit(1)
	}

	name := args[0]
	p, err := manager.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := manager.SetActive(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Switched to profile '%s'\n", p.Name)
}

func cmdCreate(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles create <name> [description]\n")
		os.Exit(1)
	}

	name := args[0]
	desc := ""
	if len(args) > 1 {
		desc = strings.Join(args[1:], " ")
	}

	p, err := manager.Create(name, desc, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created profile '%s' at %s\n", p.Name, p.GhostHome)
}

func cmdDelete(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles delete <name>\n")
		os.Exit(1)
	}

	name := args[0]
	if err := manager.Delete(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Deleted profile '%s'\n", name)
}

func cmdEnv(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles env <name>\n")
		os.Exit(1)
	}

	name := args[0]
	env, err := manager.ExportEnv(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for k, v := range env {
		fmt.Printf("%s=%s\n", k, v)
	}
}
