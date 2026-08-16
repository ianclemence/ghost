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
	case "duplicate":
		cmdDuplicate(manager, args)
	case "delete":
		cmdDelete(manager, args)
	case "env":
		cmdEnv(manager, args)
	case "group":
		cmdGroup(manager, args)
	case "groups":
		cmdGroups(manager)
	case "avatar":
		cmdAvatar(manager, args)
	case "channel":
		cmdChannel(manager, args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: ghost-profiles <command> [args]")
	fmt.Println()
	fmt.Println("Profile Commands:")
	fmt.Println("  list                        List all profiles")
	fmt.Println("  current                     Show active profile")
	fmt.Println("  switch <name>               Switch to a profile")
	fmt.Println("  create <name> [desc]        Create a new profile")
	fmt.Println("  duplicate <name>            Duplicate a profile")
	fmt.Println("  delete <name>               Delete a profile")
	fmt.Println("  env <name>                  Show profile environment variables")
	fmt.Println()
	fmt.Println("Group Commands:")
	fmt.Println("  groups                      List all groups")
	fmt.Println("  group set <name> <group>    Set profile's group")
	fmt.Println("  group roster                Show grouped roster")
	fmt.Println()
	fmt.Println("Avatar Commands:")
	fmt.Println("  avatar set <name> <shape> <color>  Set avatar")
	fmt.Println()
	fmt.Println("Channel Commands:")
	fmt.Println("  channel create <name> --members <m1,m2>  Create channel")
	fmt.Println("  channel list <profile>                   List channels")
	fmt.Println("  channel send <id> <sender> <message>     Send message")
	fmt.Println("  channel history <id>                     Read history")
}

func cmdList(manager *profiles.Manager) {
	profileList, err := manager.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(profileList) == 0 {
		fmt.Println("No profiles found.")
		return
	}

	active := manager.ActiveProfile()
	for _, p := range profileList {
		marker := "  "
		if p.Name == active {
			marker = "* "
		}
		group := ""
		if p.Group != "" {
			group = fmt.Sprintf(" [%s]", p.Group)
		}
		title := p.Title
		if title == "" {
			title = p.Name
		}
		fmt.Printf("%s%s%s - %s\n", marker, p.Name, group, title)
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
	fmt.Printf("Title: %s\n", p.Title)
	fmt.Printf("Description: %s\n", p.Description)
	fmt.Printf("Group: %s\n", p.Group)
	fmt.Printf("Ghost Home: %s\n", p.GhostHome)
	fmt.Printf("Workspace: %s\n", p.WorkspacePath())
	if p.Avatar != nil {
		fmt.Printf("Avatar: shape=%s color=%s\n", p.Avatar.Shape, p.Avatar.Color)
	}
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

func cmdDuplicate(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles duplicate <name>\n")
		os.Exit(1)
	}

	name := args[0]
	newName := manager.UniqueName(name)
	p, err := manager.Duplicate(name, newName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Duplicated '%s' → '%s'\n", name, p.Name)
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

func cmdGroup(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles group <set|roster> [args]\n")
		os.Exit(1)
	}

	switch args[0] {
	case "set":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: ghost-profiles group set <name> <group>\n")
			os.Exit(1)
		}
		if err := manager.SetGroup(args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Moved '%s' to group '%s'\n", args[1], args[2])

	case "roster":
		ungrouped, groups, err := manager.GroupRoster()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if len(ungrouped) > 0 {
			fmt.Println("Ungrouped:")
			for _, p := range ungrouped {
				fmt.Printf("  %s\n", p.Name)
			}
		}

		for group, members := range groups {
			fmt.Printf("\n%s:\n", group)
			for _, p := range members {
				fmt.Printf("  %s\n", p.Name)
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown group command: %s\n", args[0])
	}
}

func cmdGroups(manager *profiles.Manager) {
	groups, err := manager.ListGroups()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(groups) == 0 {
		fmt.Println("No groups found.")
		return
	}

	for _, g := range groups {
		fmt.Println(g)
	}
}

func cmdAvatar(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles avatar set <name> <shape> <color>\n")
		os.Exit(1)
	}

	switch args[0] {
	case "set":
		if len(args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: ghost-profiles avatar set <name> <shape> <color>\n")
			fmt.Fprintf(os.Stderr, "Shapes: circle, squircle, pill, triangle, hexagon, cloud, drop\n")
			fmt.Fprintf(os.Stderr, "Colors: #f5f5f4, #8d6748, #ef4444, #f97316, #22c55e, #3b82f6, #8b5cf6, #ec4899\n")
			os.Exit(1)
		}
		avatar := &profiles.Avatar{
			Shape: args[2],
			Color: args[3],
		}
		if err := manager.UpdateAvatar(args[1], avatar); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated avatar for '%s': shape=%s, color=%s\n", args[1], args[2], args[3])

	default:
		fmt.Fprintf(os.Stderr, "Unknown avatar command: %s\n", args[0])
	}
}

func cmdChannel(manager *profiles.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: ghost-profiles channel <create|list|send|history> [args]\n")
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: ghost-profiles channel create <name> --members <m1,m2>\n")
			os.Exit(1)
		}
		name := args[1]
		topic := ""
		sender := ""
		var members []string

		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--topic":
				if i+1 < len(args) {
					topic = args[i+1]
					i++
				}
			case "--members":
				if i+1 < len(args) {
					members = strings.Split(args[i+1], ",")
					for j := range members {
						members[j] = strings.TrimSpace(members[j])
					}
					i++
				}
			case "--sender":
				if i+1 < len(args) {
					sender = args[i+1]
					i++
				}
			}
		}

		if sender == "" && len(members) > 0 {
			sender = members[0]
		}

		ch, err := manager.CreateChannel(name, topic, sender, members)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created channel '%s' (id=%s, members=%v)\n", ch.Name, ch.ID, ch.Members)

	case "list":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: ghost-profiles channel list <profile>\n")
			os.Exit(1)
		}
		channels, err := manager.ListChannels(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(channels) == 0 {
			fmt.Println("No channels found.")
			return
		}
		for _, ch := range channels {
			fmt.Printf("  %s (%s) - %s\n", ch.Name, ch.ID, strings.Join(ch.Members, ", "))
		}

	case "send":
		if len(args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: ghost-profiles channel send <channel_id> <sender> <message>\n")
			os.Exit(1)
		}
		if err := manager.SendChannelMessage(args[1], args[2], strings.Join(args[3:], " ")); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Message sent to channel '%s'\n", args[1])

	case "history":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: ghost-profiles channel history <channel_id>\n")
			os.Exit(1)
		}
		messages, err := manager.ReadChannelHistory(args[1], 50)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(messages) == 0 {
			fmt.Println("No messages.")
			return
		}
		for _, msg := range messages {
			fmt.Printf("[%s] %s | %s\n", msg.Timestamp.Format("15:04:05"), msg.Sender, msg.Content)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown channel command: %s\n", args[0])
	}
}
