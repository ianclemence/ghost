package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/connector"
	"github.com/ianclemence/ghost/pkg/credentials"
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
	case "keygen":
		connectorKeygenCmd(os.Args[3:])
	case "sign":
		connectorSignCmd(os.Args[3:])
	case "verify":
		connectorVerifyCmd(os.Args[3:])
	default:
		fmt.Printf("Unknown connector command: %s\n", os.Args[2])
		connectorHelp()
	}
}

func connectorHelp() {
	fmt.Println("Usage: ghost connector <validate|review|init|from-openapi|install|list>")
	fmt.Println()
	fmt.Println("  validate <path>          Validate connector.json (or a directory containing it)")
	fmt.Println("  review <path> [--json]   Schema + capability-risk + provenance audit")
	fmt.Println("  init <id> [--dir=path]   Scaffold a connector manifest")
	fmt.Println("  from-openapi <spec>      Generate a draft connector from an OpenAPI document")
	fmt.Println("                           [--out=connector.json] [--id=] [--version=] [--url=] [--keyless]")
	fmt.Println("  install <path>           Install into <workspace>/connectors [--dir=] [--force]")
	fmt.Println("  call <path> <cap> [k=v]  Execute one capability (openapi connectors)")
	fmt.Println("  keygen [--out=dir]       Generate an ed25519 connector signing keypair")
	fmt.Println("  sign <path> --key= --by= Sign a connector manifest")
	fmt.Println("  verify <path> --key=     Verify a connector signature")
	fmt.Println("  list [--dir=path]        List first-party and installed connectors")
}

// connectorCLIAuthHeaders builds the auth header for a connector from the
// credential store, mirroring the agent's runtime behavior.
func connectorCLIAuthHeaders(m *connector.Manifest) func() (map[string]string, error) {
	return func() (map[string]string, error) {
		id := strings.TrimSpace(m.Auth.Provider)
		if id == "" {
			id = strings.TrimSpace(m.ID)
		}
		key := credentials.ProviderKey(id)
		if key == "" {
			return nil, nil
		}
		header := strings.TrimSpace(m.Auth.Header)
		if header == "" {
			header = "Authorization"
		}
		scheme := strings.TrimSpace(m.Auth.Scheme)
		if scheme == "" && header == "Authorization" {
			scheme = "Bearer"
		}
		value := key
		if scheme != "" {
			value = scheme + " " + key
		}
		return map[string]string{header: value}, nil
	}
}

// connectorManifestPath resolves a file or directory argument to the manifest
// file path (for reading and writing).
func connectorManifestPath(path string) string {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return filepath.Join(path, connector.FileName)
	}
	return path
}

func connectorKeygenCmd(args []string) {
	dir := "."
	for _, a := range args {
		if strings.HasPrefix(a, "--out=") {
			dir = strings.TrimPrefix(a, "--out=")
		}
	}
	pub, priv, err := connector.GenerateSigningKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen failed: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "could not create %s: %v\n", dir, err)
		os.Exit(1)
	}
	privPath := filepath.Join(dir, "connector-signing.key")
	pubPath := filepath.Join(dir, "connector-signing.pub")
	if err := os.WriteFile(privPath, []byte(connector.EncodePrivateKey(priv)+"\n"), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "could not write private key: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(pubPath, []byte(connector.EncodePublicKey(pub)+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "could not write public key: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (private, 0600) and %s (public)\n", privPath, pubPath)
}

func connectorSignCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector sign <path> --key=<private-key-file> --by=<name>")
		os.Exit(1)
	}
	path, keyFile, by := args[0], "", ""
	for _, a := range args[1:] {
		switch {
		case strings.HasPrefix(a, "--key="):
			keyFile = strings.TrimPrefix(a, "--key=")
		case strings.HasPrefix(a, "--by="):
			by = strings.TrimPrefix(a, "--by=")
		default:
			fmt.Printf("Unknown flag: %s\n", a)
			os.Exit(1)
		}
	}
	if keyFile == "" || strings.TrimSpace(by) == "" {
		fmt.Fprintln(os.Stderr, "both --key and --by are required")
		os.Exit(1)
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read key: %v\n", err)
		os.Exit(1)
	}
	priv, err := connector.DecodePrivateKey(string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	m, verrs, err := connector.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	if len(verrs) > 0 {
		fmt.Fprintf(os.Stderr, "connector is invalid:\n%s\n", connector.FormatErrors(verrs))
		os.Exit(1)
	}
	if err := connector.Sign(m, priv, by); err != nil {
		fmt.Fprintf(os.Stderr, "sign failed: %v\n", err)
		os.Exit(1)
	}
	if err := connector.Save(m, connectorManifestPath(path)); err != nil {
		fmt.Fprintf(os.Stderr, "could not write manifest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("signed %s as %s (%s)\n", m.ID, m.Provenance.SignedBy, m.Provenance.Hash)
}

func connectorVerifyCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghost connector verify <path> --key=<public-key-file>")
		os.Exit(1)
	}
	path, keyFile := args[0], ""
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "--key=") {
			keyFile = strings.TrimPrefix(a, "--key=")
		}
	}
	if keyFile == "" {
		fmt.Fprintln(os.Stderr, "--key is required")
		os.Exit(1)
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read key: %v\n", err)
		os.Exit(1)
	}
	pub, err := connector.DecodePublicKey(string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	m, _, err := connector.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	if err := connector.VerifySignature(m, pub); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s signed by %s\n", m.ID, m.Provenance.SignedBy)
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
	exec := &connector.OpenAPIExecutor{BaseURL: m.OpenAPI.BaseURL, Headers: connectorCLIAuthHeaders(m)}
	out, err := exec.Execute(context.Background(), *found, callArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "call failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

func connectorReviewCmd(args []string) {
	path := ""
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		} else {
			path = a
		}
	}
	if path == "" {
		fmt.Println("Usage: ghost connector review <path> [--json]")
		os.Exit(1)
	}
	m, _, err := connector.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	findings := connector.Review(m)
	if asJSON {
		raw, _ := json.MarshalIndent(map[string]interface{}{
			"id":         m.ID,
			"findings":   findings,
			"has_errors": connector.HasErrors(findings),
		}, "", "  ")
		fmt.Println(string(raw))
		if connector.HasErrors(findings) {
			os.Exit(1)
		}
		return
	}
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
		fmt.Println("Usage: ghost connector install <path|url> [--dir=] [--force] [--key=<pubfile>] [--require-signature]")
		os.Exit(1)
	}
	source := args[0]
	dir := ""
	force := false
	requireSig := false
	var keys []ed25519.PublicKey
	for _, a := range args[1:] {
		switch {
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		case a == "--force":
			force = true
		case a == "--require-signature":
			requireSig = true
		case strings.HasPrefix(a, "--key="):
			kf := strings.TrimPrefix(a, "--key=")
			raw, err := os.ReadFile(kf)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not read key %s: %v\n", kf, err)
				os.Exit(1)
			}
			k, err := connector.DecodePublicKey(string(raw))
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			keys = append(keys, k)
			requireSig = true
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

	var dest string
	var err error
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		dest, err = connector.FetchAndInstall(context.Background(), source, dir, force, connector.FetchOptions{
			RequireSignature: requireSig,
			TrustedKeys:      keys,
		})
	} else {
		dest, err = connector.Install(source, dir, force)
	}
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
