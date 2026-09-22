package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/changelog"
)

// updateCmd deploys a Ghost release. It runs as the invoking user and only
// escalates the specific steps that need root (replacing a system-wide
// binary, restarting system units). A user-scoped install (binary in
// ~/.local/bin, `systemctl --user` units) needs no sudo at all — matching how
// Scout and comparable agents install.
func updateCmd() {
	args := os.Args[2:]
	dryRun, force, check, notes := false, false, false, false
	channel := "release"
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		case "--check":
			check = true
		case "--notes":
			notes = true
		case "--channel":
			if i+1 < len(args) {
				i++
				channel = args[i]
			}
		case "--root":
			// Explicit opt-in to the privileged path (system install).
			channel = "dev"
		}
	}

	if notes {
		printGhostChangelog()
		return
	}

	scope := appliance.DetectScope()
	if check {
		ghostCheck(scope)
		return
	}

	if channel == "dev" {
		updateDevChannel(scope, dryRun, force)
		return
	}

	// Default: release channel. Fall back to the dev/build path when the
	// checkout is the source of truth on this machine (no published assets),
	// but never require root for a user-scoped install.
	updateReleaseChannel(scope, dryRun, force)
}

// updateReleaseChannel installs a verified release. It resolves the target
// from GitHub Releases; if a binary asset is published it downloads and
// verifies it (sha256 + optional Ed25519) and installs that, otherwise it
// falls back to building the pinned tag from a detached checkout. Either way
// it never builds the dirty working tree. User scope needs no root.
func updateReleaseChannel(scope appliance.ScopePaths, dryRun, force bool) {
	ghostDir := findGhostDir()
	current := ghostVersion()

	rel, err := resolveRelease(offline())
	if err != nil {
		// No release channel reachable: fall back to the local tag.
		target := gitTagAtCheckout(ghostDir)
		if target == "" {
			fmt.Printf("Could not resolve a release and no local tag found: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Release channel unavailable (%v); deploying local tag %s.\n", err, target)
		if !force && target == current {
			fmt.Println("Already current (" + current + "). Use --force to redeploy.")
			return
		}
		if dryRun {
			return
		}
		buildAndDeploy(scope, ghostDir, force)
		return
	}

	target := rel.Version
	fmt.Printf("Installed: %s\nAvailable: %s\n", current, target)
	if !force && !appliance.IsNewer(target, current) {
		fmt.Println("Already current.")
		printGhostNotes(current)
		return
	}
	if dryRun {
		kind := "build the pinned tag"
		if asset, ok := pickGhostAsset(rel); ok {
			kind = "download and verify " + asset.Name + ""
		}
		fmt.Printf("[dry-run] would %s and install into %s (scope: %s, root: %v)\n", kind, scope.BinDir, scope.Scope, scope.NeedsRoot())
		return
	}
	fmt.Printf("Deploying %s...\n", target)

	if asset, ok := pickGhostAsset(rel); ok {
		if err := installReleaseAsset(scope, rel, asset); err != nil {
			return
		}
		printNotesFor(target)
		return
	}

	// No published asset: build the pinned tag (detached, never the working
	// tree) so the install is still reproducible and version-stamped.
	buildAndDeploy(scope, ghostDir, force)
}

// ghostRepo is the release repository.
const ghostRepo = "ianclemence/ghost"

func offline() bool {
	return os.Getenv("GHOST_OFFLINE") != ""
}

// resolveRelease fetches the latest Ghost release (or a pinned one via
// GHOST_VERSION) from GitHub. When offline it returns an error so the caller
// can fall back to the local tag.
func resolveRelease(isOffline bool) (*appliance.Release, error) {
	if isOffline {
		return nil, fmt.Errorf("offline")
	}
	client := appliance.NewGitHubClient(ghostRepo)
	if v := strings.TrimSpace(os.Getenv("GHOST_VERSION")); v != "" {
		return client.ByTag(v)
	}
	return client.Latest()
}

// pickGhostAsset selects the platform binary asset for this machine.
func pickGhostAsset(rel *appliance.Release) (appliance.Asset, bool) {
	want := "ghost_" + runtimeGOOS() + "_" + runtimeArch()
	for _, a := range rel.Assets {
		if strings.Contains(strings.ToLower(a.Name), want) || strings.Contains(strings.ToLower(a.Name), "ghost-linux-"+runtimeArch()) {
			return a, true
		}
	}
	return appliance.Asset{}, false
}

// installReleaseAsset downloads, verifies, and atomically installs a release
// binary, then restarts the service in the matching scope.
func installReleaseAsset(scope appliance.ScopePaths, rel *appliance.Release, asset appliance.Asset) error {
	stage, err := os.MkdirTemp("", "ghost-rel-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	dst := filepath.Join(stage, "ghost")
	client := appliance.NewGitHubClient(ghostRepo)
	fmt.Printf("  Downloading %s...\n", asset.Name)
	if err := client.Download(asset.URL, dst); err != nil {
		return err
	}

	if sum, ok := pickAsset(rel, "checksums"); ok {
		sumPath := filepath.Join(stage, sum.Name)
		if err := client.Download(sum.URL, sumPath); err == nil {
			if data, rerr := os.ReadFile(sumPath); rerr == nil {
				if want := parseChecksums(string(data))[asset.Name]; want != "" {
					if err := appliance.VerifySHA256(dst, want); err != nil {
						return err
					}
					fmt.Println("  Checksum verified.")
				}
			}
		}
	}

	if sig, ok := pickAsset(rel, ".sig"); ok {
		pub := strings.TrimSpace(os.Getenv("GHOST_RELEASE_PUBKEY"))
		sigPath := filepath.Join(stage, sig.Name)
		if pub != "" && client.Download(sig.URL, sigPath) == nil {
			if data, rerr := os.ReadFile(sigPath); rerr == nil {
				if err := appliance.VerifyEd25519(dst, pub, strings.TrimSpace(string(data))); err != nil {
					return err
				}
				fmt.Println("  Signature verified.")
			}
		}
	}

	target := scope.BinDir + "/ghost"
	if scope.NeedsRoot() && os.Geteuid() != 0 {
		if err := runSudo("install", "-m", "0755", dst, target); err != nil {
			return err
		}
	} else {
		if err := appliance.AtomicInstall(dst, target); err != nil {
			return err
		}
	}
	fmt.Printf("  Installed %s\n", target)
	restartScope(scope)
	_ = changelog.MarkSeen(ghostDataDir(), rel.Version)
	fmt.Printf("Updated %s → %s\n", ghostVersion(), rel.Version)
	return nil
}

func pickAsset(rel *appliance.Release, substr string) (appliance.Asset, bool) {
	for _, a := range rel.Assets {
		if strings.Contains(strings.ToLower(a.Name), strings.ToLower(substr)) {
			return a, true
		}
	}
	return appliance.Asset{}, false
}

// parseChecksums parses a sha256sums file into name->hex.
func parseChecksums(data string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out
}

// restartScope reloads and restarts the service in the detected scope.
func restartScope(scope appliance.ScopePaths) {
	if scope.Scope == appliance.ScopeUser {
		exec.Command("systemctl", "--user", "daemon-reload").Run()
		exec.Command("systemctl", "--user", "restart", "ghost").Run()
	} else {
		_ = runSudo("systemctl", "daemon-reload")
		_ = runSudo("systemctl", "restart", "ghost")
	}
}

func runtimeGOOS() string { return runtime.GOOS }
func runtimeArch() string { return runtime.GOARCH }

// updateDevChannel is the developer path: build the working tree (refusing a
// dirty checkout unless --force) and install it.
func updateDevChannel(scope appliance.ScopePaths, dryRun, force bool) {
	ghostDir := findGhostDir()
	if !force {
		if out, gerr := exec.Command("git", "-C", ghostDir, "status", "--porcelain").Output(); gerr == nil {
			if !appliance.IsClean(string(out)) {
				fmt.Fprintln(os.Stderr, "✗ Refusing to update: the checkout has uncommitted changes.")
				for _, p := range appliance.SummarizeDirty(string(out), 8) {
					fmt.Fprintf(os.Stderr, "    %s\n", p)
				}
				fmt.Fprintln(os.Stderr, "  Commit and tag a release, or deploy anyway with --force.")
				os.Exit(1)
			}
		}
	}
	if dryRun {
		fmt.Printf("[dry-run] would build %s and install into %s (scope: %s)\n", ghostDir, scope.BinDir, scope.Scope)
		return
	}
	buildAndDeploy(scope, ghostDir, force)
}

// ghostVersion reports the installed Ghost's version without ever starting an
// interactive session. It prefers the running binary's own version, then asks
// an installed binary with the explicit `version` subcommand (a bare
// invocation would launch a chat), bounded by a short timeout.
func ghostVersion() string {
	if v := strings.TrimSpace(version); v != "" && v != "dev" {
		return v
	}
	for _, p := range []string{filepath.Join(homeDir(), ".local", "bin", "ghost"), "/usr/local/bin/ghost"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, p, "version").Output()
		cancel()
		if err != nil {
			continue
		}
		// `ghost version` prints a multi-line block whose first meaningful
		// line is "👻 Ghost <version> (git: ...)". Extract that token, not
		// the trailing "Go: goX.Y" line.
		for _, line := range strings.Split(string(out), "\n") {
			i := strings.Index(line, "Ghost ")
			if i < 0 {
				continue
			}
			rest := strings.TrimSpace(line[i+len("Ghost "):])
			if j := strings.IndexAny(rest, " \t"); j >= 0 {
				rest = rest[:j]
			}
			if rest != "" {
				return rest
			}
		}
	}
	return "unknown"
}

func ghostCheck(scope appliance.ScopePaths) {
	current := ghostVersion()
	target := ""
	if rel, err := resolveRelease(offline()); err == nil {
		target = rel.Version
	} else {
		target = gitTagAtCheckout(findGhostDir())
	}
	fmt.Printf("Installed: %s\n", current)
	if target != "" {
		fmt.Printf("Available: %s\n", target)
	}
	fmt.Printf("Scope: %s (%s, root: %v)\n", scope.Scope, scope.BinDir, scope.NeedsRoot())
	if target == "" {
		fmt.Println("No release channel reachable; run `ghost update --channel dev` to build locally.")
		return
	}
	if !appliance.IsNewer(target, current) {
		fmt.Println("Already current.")
		printGhostNotes(current)
		return
	}
	fmt.Println("Run `ghost update` to deploy.")
}

// ghostDataDir is where the changelog marker lives (the install root).
func ghostDataDir() string {
	if v := os.Getenv("GHOST_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("GHOST_DIR"); v != "" {
		return v
	}
	if _, err := os.Stat(appliance.DefaultGhostDir); err == nil {
		return appliance.DefaultGhostDir
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "ghost")
}

// printGhostNotes prints changelog entries newer than the given version (or
// nothing on a fresh install, which just records the current version).
func printGhostNotes(version string) {
	for _, e := range changelog.NewSince(ghostDataDir(), version) {
		fmt.Printf("\nWhat's new in %s:\n\n%s\n", e.Version, e.Body)
	}
}

// printNotesFor prints the notes for an exact version after an update.
func printNotesFor(version string) {
	if body := changelog.ForVersion(version); body != "" {
		fmt.Printf("\nWhat's new in %s:\n\n%s\n", version, body)
	}
}

func gitTagAtCheckout(dir string) string {
	out, err := exec.Command("git", "-C", dir, "describe", "--tags", "--always").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// printGhostChangelog prints the full embedded changelog (ghost update --notes).
func printGhostChangelog() {
	fmt.Println(changelog.Raw())
}

// buildAndDeploy runs the crash-safe update sequence for the detected scope.
// Read-only planning happens while services are up; the build/install step is
// the only one that escalates, and only when the install is system-scoped.
func buildAndDeploy(scope appliance.ScopePaths, ghostDir string, force bool) {
	fmt.Println("Updating Ghost...")

	systemSvc := func(action string, svcs ...string) {
		args := append([]string{action}, svcs...)
		base := []string{"systemctl"}
		if scope.Scope == appliance.ScopeUser {
			base = []string{"systemctl", "--user"}
		}
		exec.Command(base[0], append(base[1:], args...)...).Run()
	}

	steps := appliance.UpdateSteps{
		Pull: func() error {
			// Release channel deploys a pinned tag; the dev channel already
			// validated the tree. No network pull happens here so updates
			// never touch the working tree implicitly.
			return nil
		},
		Plan: func() error {
			fmt.Println("1. Validating workspace layout (services still running)...")
			return appliance.CheckWorkspaceMigration(appliance.DefaultGhostDir)
		},
		Snapshot: func() error {
			fmt.Println("2. Taking recovery snapshot (services still running)...")
			return appliance.PreUpdateSnapshot()
		},
		Stop: func() {
			fmt.Println("3. Stopping services...")
			systemSvc("stop", "ghost")
			systemSvc("stop", "ghost-web")
		},
		Apply: func() error {
			fmt.Println("4. Building and installing...")
			if err := migrateApplianceWorkspace(false); err != nil {
				return err
			}
			return installScope(scope, ghostDir, force)
		},
		Start: func() error {
			fmt.Println("Update failed — restarting services...")
			var first error
			for _, svc := range []string{"ghost", "ghost-web"} {
				systemSvc("start", svc)
			}
			return first
		},
	}
	if err := appliance.RunUpdate(steps); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Update complete!")
}

// migrateApplianceWorkspace moves the Ghost workspace from the legacy
// <ghostDir>/workspace location to the runtime location when needed. The
// Ghost runs from DefaultGhostDir regardless of where the checkout (and
// therefore the git pull) lives, so this always targets the Ghost dir.
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

// installScope builds Ghost and installs it into the detected scope. The
// user scope is fully unprivileged: build to a temp file, then atomically
// rename into ~/.local/bin and (re)start the per-user service. The system
// scope escalates only the binary replacement and service restart, via sudo,
// and preserves ownership of the runtime data for the invoking user.
func installScope(scope appliance.ScopePaths, ghostDir string, force bool) error {
	tag := gitTagAtCheckout(ghostDir)
	staged, cleanup, err := buildGhostBinary(ghostDir, tag)
	if err != nil {
		return err
	}
	defer cleanup()

	target := scope.BinDir + "/ghost"
	if scope.NeedsRoot() && os.Geteuid() != 0 {
		// System install: run only the swap under sudo.
		fmt.Println("  (system install: using sudo for the binary swap)")
		if err := runSudo("install", "-m", "0755", staged, target); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(scope.BinDir, 0o755); err != nil {
			return err
		}
		// Atomic: write alongside, then rename. Never overwrite a running
		// binary in place.
		tmp := target + ".new"
		data, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		if err := os.WriteFile(tmp, data, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	fmt.Printf("  Installed %s\n", target)
	restartScope(scope)
	return nil
}

// buildGhostBinary builds the ghost binary for the current platform into a
// temp file and returns its path plus a cleanup func. It injects the version
// so the installed binary reports the tag, not a bare hash.
func buildGhostBinary(ghostDir, tag string) (string, func(), error) {
	f, err := os.CreateTemp("", "ghost-build-*")
	if err != nil {
		return "", func() {}, err
	}
	f.Close()
	path := f.Name()
	cleanup := func() { os.Remove(path) }
	ldflags := "-s -w"
	if tag != "" {
		ldflags += " -X main.version=" + tag
	}
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", path, "./cmd/ghost")
	cmd.Dir = ghostDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("build failed: %w", err)
	}
	return path, cleanup, nil
}

// runSudo runs one command under sudo, inheriting the invoking user so any
// user-scoped paths the command touches resolve correctly.
func runSudo(name string, args ...string) error {
	full := append([]string{name}, args...)
	cmd := exec.Command("sudo", full...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func updaterCmd() {
	if wantsHelp(os.Args[2:]) {
		fmt.Println("Usage: ghost auto-update [--interval DURATION]")
		fmt.Println()
		fmt.Println("Periodically check GitHub Releases and install a newer release using the")
		fmt.Println("same verified, user-scoped updater as 'ghost update'.")
		fmt.Println()
		fmt.Println("  --interval/-i DURATION   how often to check (default 6h, e.g. 30m, 24h)")
		fmt.Println()
		fmt.Println("For a one-shot deploy, use 'ghost update'.")
		return
	}

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

// checkAndUpdate is the periodic tick for `ghost auto-update`. It uses the
// same release-channel updater as `ghost update`, so there is exactly one
// code path: resolve a release, verify, install, restart.
func checkAndUpdate() {
	fmt.Println("Checking for updates...")
	scope := appliance.DetectScope()
	updateReleaseChannel(scope, false, false)
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

	// Try common locations (including non-root home dirs when running as root)
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "ghost"),
		filepath.Join(home, ".ghost"),
		"/var/ghost",
		"/home/ianclemence/ghost",
	}

	// When running as root, also check /home/*  for the repo
	if home == "/root" {
		if entries, err := os.ReadDir("/home"); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					candidates = append(candidates, filepath.Join("/home", e.Name(), "ghost"))
				}
			}
		}
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
