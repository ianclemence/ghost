package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/connector"
)

// connectorCmd is the authoring surface for Ghost's portable connectors:
//
//	ghost connector validate <path>        validate a connector.json (or dir)
//	ghost connector init <id>              scaffold a manifest
//	ghost connector from-openapi <spec>    generate a draft from OpenAPI
//	ghost connector list                   first-party + installed connectors
func connectorCmd() {
	if len(os.Args) < 3 {
		connectorHelp()
		return
	}
	switch os.Args[2] {
	case "validate":
		connectorValidateCmd(os.Args[3:])
	case "init":
		connectorInitCmd(os.Args[3:])
	case "from-openapi":
		connectorFromOpenAPICmd(os.Args[3:])
	case "list":
		connectorListCmd(os.Args[3:])
	case "review":
		connectorReviewCmd(os.Args[3:])
	case "install":
		connectorInstallCmd(os.Args[3:])
	case "call":
		connectorCallCmd(os.Args[3:])
	default:
		fmt.Printf("Unknown connector command: %s\n", os.Args[2])
		connectorHelp()
	}
}

func connectorHelp() {
	fmt.Println("Usage: ghost connector <validate|review|init|from-openapi|install|list>")
	fmt.Println()
	fmt.Println("  validate <path>          Validate connector.json (or a directory containing it)")
	fmt.Println("  review <path>            Schema + capability-risk + provenance audit")
	fmt.Println("  init <id> [--dir=path]   Scaffold a connector manifest")
	fmt.Println("  from-openapi <spec>      Generate a draft connector from an OpenAPI document")
	fmt.Println("                           [--out=connector.json] [--id=] [--version=] [--url=] [--keyless]")
	fmt.Println("  install <path>           Install into <workspace>/connectors [--dir=] [--force]")
	fmt.Println("  call <path> <cap> [k=v]  Execute one capability (openapi connectors)")
	fmt.Println("  list [--dir=path]        List first-party and installed connectors")
}

func connectorCallCmd(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: ghost connector call <path> <capability-id> [key=value ...]")
		os.Exit(1)
	}
	path, capID := args[0], args[1]
	m, verrs, err := connector.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	if len(verrs) > 0 {
		fmt.Fprintf(os.Stderr, "connector is invalid:\n%s\n", connector.FormatErrors(verrs))
		os.Exit(1)
	}
	if m.Kind != connector.KindOpenAPI || m.OpenAPI == nil {
		fmt.Fprintln(os.Stderr, "only openapi connectors can be called from the CLI")
		os.Exit(1)
	}
	var found *connector.Capability
	for i := range m.Capabilities {
		if m.Capabilities[i].ID == capID {
			found = &m.Capabilities[i]
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "unknown capability %q\n", capID)
		os.Exit(1)
	}
	callArgs := map[string]interface{}{}
	for _, a := range args[2:] {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "bad argument %q (want key=value)\n", a)
			os.Exit(1)
		}
		callArgs[k] = v
	}
	exec := &connector.OpenAPIExecutor{BaseURL: m.OpenAPI.BaseURL}
	out, err := exec.Execute(context.Background(), *found, callArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "call failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

func connectorReviewCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector review <path>")
		os.Exit(1)
	}
	m, _, err := connector.Load(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	findings := connector.Review(m)
	fmt.Printf("Review: %s v%s (%s)\n", m.ID, m.Version, m.Kind)
	for _, f := range findings {
		mark := "·"
		switch f.Level {
		case "error":
			mark = "✗"
		case "warning":
			mark = "!"
		}
		field := ""
		if f.Field != "" {
			field = " " + f.Field + ":"
		}
		fmt.Printf("  %s%s %s\n", mark, field, f.Message)
	}
	if connector.HasErrors(findings) {
		os.Exit(1)
	}
}

func connectorInstallCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector install <path> [--dir=<workspace>/connectors] [--force]")
		os.Exit(1)
	}
	path := args[0]
	dir := ""
	force := false
	for _, a := range args[1:] {
		switch {
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		case a == "--force":
			force = true
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			os.Exit(1)
		}
	}
	if dir == "" {
		if cfg, err := loadConfig(); err == nil {
			if ws := cfg.WorkspacePath(); ws != "" {
				dir = filepath.Join(ws, "connectors")
			}
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "no install dir; pass --dir=<path>")
		os.Exit(1)
	}
	dest, err := connector.Install(path, dir, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("installed to %s\n", dest)
}

func connectorValidateCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector validate <path>")
		os.Exit(1)
	}
	m, verrs, err := connector.Load(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	if len(verrs) > 0 {
		fmt.Printf("✗ %s — %d problem(s)\n", m.ID, len(verrs))
		for _, e := range verrs {
			fmt.Printf("  - %s\n", e.Error())
		}
		os.Exit(1)
	}
	fmt.Printf("✓ %s v%s — %s (%s), %d capabilities\n",
		m.ID, m.Version, m.DisplayName, m.Kind, len(m.Capabilities))
}

func connectorInitCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector init <id> [--dir=path]")
		os.Exit(1)
	}
	id := connector.Slugify(args[0])
	if id == "" {
		fmt.Fprintln(os.Stderr, "id must contain at least one letter or digit")
		os.Exit(1)
	}
	dir := id
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "--dir=") {
			dir = strings.TrimPrefix(a, "--dir=")
		}
	}
	m := &connector.Manifest{
		SchemaVersion: connector.SchemaVersion,
		ID:            id,
		DisplayName:   args[0],
		Kind:          connector.KindNative,
		Version:       "0.1.0",
		Auth:          connector.Auth{Kind: connectedapp.AuthAPIKey, Setup: connectedapp.SetupPasteKey},
		Capabilities: []connector.Capability{{
			ID:              id + ".read",
			Title:           "Read",
			Description:     "Describe what this connector reads.",
			Risk:            capability.RiskReadOnly,
			NetworkRequired: true,
		}},
	}
	out := filepath.Join(dir, connector.FileName)
	if err := connector.Save(m, out); err != nil {
		fmt.Fprintf(os.Stderr, "could not write %s: %v\n", out, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s — edit it, then `ghost connector validate %s`\n", out, dir)
}

func connectorFromOpenAPICmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector from-openapi <spec.json> [--out=connector.json] [--id=] [--version=] [--url=] [--keyless]")
		os.Exit(1)
	}
	spec := args[0]
	out := connector.FileName
	opts := connector.OpenAPIOptions{Path: spec}
	keyless := false
	for _, a := range args[1:] {
		switch {
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "--id="):
			opts.ID = strings.TrimPrefix(a, "--id=")
		case strings.HasPrefix(a, "--version="):
			opts.Version = strings.TrimPrefix(a, "--version=")
		case strings.HasPrefix(a, "--url="):
			opts.URL = strings.TrimPrefix(a, "--url=")
		case a == "--keyless":
			keyless = true
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			os.Exit(1)
		}
	}
	raw, err := os.ReadFile(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read %s: %v\n", spec, err)
		os.Exit(1)
	}
	if keyless {
		zero := connector.Auth{}
		opts.Auth = &zero
	}
	m, err := connector.FromOpenAPI(raw, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not generate connector: %v\n", err)
		os.Exit(1)
	}
	if err := connector.Save(m, out); err != nil {
		fmt.Fprintf(os.Stderr, "could not write %s: %v\n", out, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s — %d capabilities from %s\n", out, len(m.Capabilities), spec)
	if verrs := m.Validate(); len(verrs) > 0 {
		fmt.Println("review these before publishing:")
		for _, e := range verrs {
			fmt.Printf("  - %s\n", e.Error())
		}
	}
}

func connectorListCmd(args []string) {
	dir := ""
	for _, a := range args {
		if strings.HasPrefix(a, "--dir=") {
			dir = strings.TrimPrefix(a, "--dir=")
		}
	}
	fmt.Println("First-party connectors:")
	for _, c := range connectedapp.FirstParty() {
		fmt.Printf("  %-16s %-18s %s\n", c.ID, c.DisplayName, strings.Join(c.Capabilities, ", "))
	}
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read %s: %v\n", dir, err)
		os.Exit(1)
	}
	fmt.Printf("\nInstalled connectors in %s:\n", dir)
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, verrs, err := connector.Load(filepath.Join(dir, e.Name()))
		if err != nil || len(verrs) > 0 {
			continue
		}
		found = true
		fmt.Printf("  %-16s %-18s %s\n", m.ID, m.DisplayName, m.Kind)
	}
	if !found {
		fmt.Println("  (none)")
	}
}
