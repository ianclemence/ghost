package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ghostroot "github.com/ianclemence/ghost"
	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/changelog"
)

// The release channel.
//
// `ghost update` resolves the latest GitHub release and deploys exactly that
// release, never whatever happens to be in a local checkout:
//
//  1. If the release carries a prebuilt binary for this platform, download it
//     and verify it fail-closed: checksums.txt must carry a signature that
//     checks out under a key this binary already trusts, and each binary must
//     match the checksum that signed list gives it. A release that fails any of that is refused, and
//     is never replaced by a source build (that would let a tampered release
//     choose the weaker path).
//  2. If it carries none (a platform we don't build for), fetch the release
//     tag into a throwaway checkout and build that.
//
// Everything is staged while Ghost is still running. Only the swap happens
// with the services stopped, after the recovery snapshot.

// embeddedReleaseKeys are the public keys, hex, that sign Ghost releases. The
// private half lives with whoever cuts releases (`ghost-release keygen`). More
// than one may be listed, comma separated, so the key can be rotated: ship a
// release signed by the old key that also trusts the new one, then switch.
const embeddedReleaseKeys = "90421b3fdf6defda468205032028fcb88f8296ffdb657c35f37f58b7b45a0f38"

// errNoAsset means the release has no prebuilt binary for this platform.
var errNoAsset = errors.New("this release has no prebuilt binary for this platform")

func trustedReleaseKeys() ([]ed25519.PublicKey, error) {
	s := embeddedReleaseKeys
	if extra := strings.TrimSpace(os.Getenv("GHOST_RELEASE_PUBKEY")); extra != "" {
		s += "," + extra
	}
	return appliance.ParseTrustedKeys(s)
}

// releaseClient is the GitHub client. GHOST_RELEASE_API points it at a mirror
// (or, in tests, a local server); downloads follow the URLs the release names.
func releaseClient() *appliance.GitHubClient {
	c := appliance.NewGitHubClient(ghostRepo)
	if v := strings.TrimSpace(os.Getenv("GHOST_RELEASE_API")); v != "" {
		c.APIURL = v
	}
	return c
}

func sourceURL() string {
	if v := strings.TrimSpace(os.Getenv("GHOST_SOURCE_URL")); v != "" {
		return v
	}
	return "https://github.com/" + ghostRepo + ".git"
}

// stagedRelease is a release downloaded (or built), verified and waiting to
// be swapped in.
type stagedRelease struct {
	version string
	how     string // "download" or "source"
	ghost   string
	web     string // empty when the console binary is not part of this install
	cleanup func()
}

// updateEnv is everything an update does to the machine besides moving files,
// so a test can run the whole flow without touching services or backups.
type updateEnv struct {
	plan     func() error
	snapshot func() error
	migrate  func() error
	service  func(scope appliance.ScopePaths, action string, svcs ...string)
	restart  func(scope appliance.ScopePaths)
	sudo     func(name string, args ...string) error
}

func realUpdateEnv() updateEnv {
	if os.Getenv("GHOST_UPDATE_SANDBOX") != "" {
		// For rehearsing an update inside a throwaway root (scripts/e2e-update.sh
		// runs it in a bubblewrap sandbox with an empty /usr/local): move and
		// verify files, but leave services, snapshots and the workspace alone.
		// It must never be set on a real install, where it would skip the
		// recovery snapshot.
		return updateEnv{
			plan:     func() error { return nil },
			snapshot: func() error { return nil },
			migrate:  func() error { return nil },
			service:  func(appliance.ScopePaths, string, ...string) {},
			restart:  func(appliance.ScopePaths) {},
			sudo:     runSudo,
		}
	}
	return updateEnv{
		plan: func() error {
			fmt.Println("1. Validating workspace layout (services still running)...")
			return appliance.CheckWorkspaceMigration(appliance.DefaultGhostDir)
		},
		snapshot: func() error {
			fmt.Println("2. Taking recovery snapshot (services still running)...")
			return appliance.PreUpdateSnapshot()
		},
		migrate: func() error { return migrateApplianceWorkspace(false) },
		service: serviceCtl,
		restart: restartScope,
		sudo:    runSudo,
	}
}

// serviceCtl runs systemctl in the install's scope. Failures are ignored on
// purpose: stopping a unit that isn't there is not an error.
func serviceCtl(scope appliance.ScopePaths, action string, svcs ...string) {
	base := []string{"systemctl"}
	if scope.Scope == appliance.ScopeUser {
		base = []string{"systemctl", "--user"}
	}
	exec.Command(base[0], append(append(base[1:], action), svcs...)...).Run()
}

// wantsWebBinary reports whether the console binary is part of this install:
// it is a second, root-owned system unit, and leaving it behind meant an
// updated daemon served by an old console.
func wantsWebBinary(scope appliance.ScopePaths) bool {
	return scope.Scope == appliance.ScopeSystem && systemUnitExists("ghost-web")
}

func stageRoot() string { return os.TempDir() }

// stageRelease prepares the release for installation while Ghost keeps
// running.
func stageRelease(rel *appliance.Release, scope appliance.ScopePaths) (*stagedRelease, error) {
	st, err := stageAssets(rel, scope)
	if errors.Is(err, errNoAsset) {
		fmt.Println("  No prebuilt binary for this platform; building the release tag from source.")
		return stageSource(rel, scope)
	}
	return st, err
}

// stageAssets downloads and verifies the prebuilt binaries. Any problem other
// than "there is no binary for this platform" is a refusal.
func stageAssets(rel *appliance.Release, scope appliance.ScopePaths) (*stagedRelease, error) {
	goos, goarch := runtimeGOOS(), runtimeArch()
	if _, ok := rel.FindAsset(appliance.BinaryAssetName("ghost", goos, goarch)); !ok {
		return nil, errNoAsset
	}
	keys, err := trustedReleaseKeys()
	if err != nil {
		return nil, fmt.Errorf("release key configuration: %w", err)
	}
	if len(keys) == 0 {
		return nil, appliance.ErrNoTrustedKeys
	}
	binaries := []string{"ghost"}
	if wantsWebBinary(scope) {
		binaries = append(binaries, "ghost-web")
	}

	dir, err := os.MkdirTemp(stageRoot(), "ghost-rel-")
	if err != nil {
		return nil, err
	}
	st := &stagedRelease{version: rel.Version, how: "download", cleanup: func() { os.RemoveAll(dir) }}
	fail := func(err error) (*stagedRelease, error) {
		st.cleanup()
		return nil, err
	}

	var total int64
	for _, b := range binaries {
		if a, ok := rel.FindAsset(appliance.BinaryAssetName(b, goos, goarch)); ok {
			total += a.Size
		}
	}
	need := uint64(total) * 2
	if need < 64<<20 {
		need = 64 << 20
	}
	if err := appliance.EnsureDiskSpace(dir, need); err != nil {
		return fail(err)
	}
	if _, err := os.Stat(scope.BinDir); err == nil {
		if err := appliance.EnsureDiskSpace(scope.BinDir, need); err != nil {
			return fail(err)
		}
	}

	client := releaseClient()
	sumAsset, ok := rel.FindAsset(appliance.ChecksumsAssetName)
	if !ok {
		return fail(fmt.Errorf("%w: the release has no %s", appliance.ErrNoChecksum, appliance.ChecksumsAssetName))
	}
	sigAsset, ok := rel.FindAsset(appliance.ChecksumsSigAssetName)
	if !ok {
		return fail(fmt.Errorf("%w: the release has no %s", appliance.ErrUnsigned, appliance.ChecksumsSigAssetName))
	}
	sumPath := filepath.Join(dir, appliance.ChecksumsAssetName)
	sigPath := filepath.Join(dir, appliance.ChecksumsSigAssetName)
	if err := client.Download(sumAsset.URL, sumPath); err != nil {
		return fail(fmt.Errorf("downloading %s: %w", appliance.ChecksumsAssetName, err))
	}
	if err := client.Download(sigAsset.URL, sigPath); err != nil {
		return fail(fmt.Errorf("downloading %s: %w", appliance.ChecksumsSigAssetName, err))
	}
	// The signature authenticates the checksum list; the list then binds each
	// binary. Verify the list first, then trust nothing it does not name.
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return fail(err)
	}
	if err := appliance.VerifyDetached(sumPath, string(sig), keys); err != nil {
		return fail(fmt.Errorf("refusing to install %s: %w", rel.Version, err))
	}
	fmt.Printf("  Verified the signature on %s.\n", appliance.ChecksumsAssetName)
	sumData, err := os.ReadFile(sumPath)
	if err != nil {
		return fail(err)
	}
	sums := appliance.ParseChecksums(string(sumData))

	for _, b := range binaries {
		name := appliance.BinaryAssetName(b, goos, goarch)
		asset, ok := rel.FindAsset(name)
		if !ok {
			return fail(fmt.Errorf("the release is incomplete: it has no %s", name))
		}
		dst := filepath.Join(dir, b)
		fmt.Printf("  Downloading %s...\n", name)
		if err := client.Download(asset.URL, dst); err != nil {
			return fail(fmt.Errorf("downloading %s: %w", name, err))
		}
		if err := appliance.VerifyChecksum(dst, sums[name]); err != nil {
			return fail(fmt.Errorf("refusing to install %s: %w", name, err))
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return fail(err)
		}
		fmt.Printf("  Verified %s.\n", name)
		if b == "ghost" {
			st.ghost = dst
		} else {
			st.web = dst
		}
	}

	// The tag name is not signed; the binary is. A release re-published
	// under a newer tag but carrying an older signed binary must not install.
	if got, err := binaryVersion(st.ghost); err != nil {
		fmt.Printf("  Could not run the staged binary to confirm its version (%v); the signature and checksum did verify.\n", err)
	} else if got != rel.Version {
		return fail(fmt.Errorf("the release is tagged %s but its binary says it is %s; refusing to install it", rel.Version, got))
	}
	return st, nil
}

// stageSource fetches the release tag into a throwaway checkout and builds
// it. It never reads the owner's own checkout.
func stageSource(rel *appliance.Release, scope appliance.ScopePaths) (*stagedRelease, error) {
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("building %s from source needs %s on this machine, and the release has no prebuilt binary for %s/%s", rel.Version, tool, runtimeGOOS(), runtimeArch())
		}
	}
	dir, err := os.MkdirTemp(stageRoot(), "ghost-src-")
	if err != nil {
		return nil, err
	}
	st := &stagedRelease{version: rel.Version, how: "source", cleanup: func() { os.RemoveAll(dir) }}
	fail := func(err error) (*stagedRelease, error) {
		st.cleanup()
		return nil, err
	}

	src := filepath.Join(dir, "src")
	fmt.Printf("  Fetching %s...\n", rel.Version)
	clone := exec.Command("git", "clone", "--quiet", "--depth", "1", "--branch", rel.Version, sourceURL(), src)
	clone.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	clone.Stdout, clone.Stderr = os.Stdout, os.Stderr
	if err := clone.Run(); err != nil {
		return fail(fmt.Errorf("fetching the %s tag: %w", rel.Version, err))
	}
	out, err := exec.Command("git", "-C", src, "describe", "--tags", "--exact-match").Output()
	if err != nil || strings.TrimSpace(string(out)) != rel.Version {
		return fail(fmt.Errorf("the fetched source is not the %s tag", rel.Version))
	}

	fmt.Println("  Building (this takes a few minutes on a small machine)...")
	ghost, cleanGhost, err := buildBinary(src, rel.Version, "./cmd/ghost")
	if err != nil {
		return fail(err)
	}
	st.ghost = ghost
	st.cleanup = chain(st.cleanup, cleanGhost)
	if wantsWebBinary(scope) {
		web, cleanWeb, err := buildBinary(src, rel.Version, "./cmd/ghost-web")
		if err != nil {
			return fail(err)
		}
		st.web = web
		st.cleanup = chain(st.cleanup, cleanWeb)
	}
	return st, nil
}

func chain(a, b func()) func() { return func() { a(); b() } }

// binaryVersion runs a staged binary's `version` and returns the version it
// reports.
func binaryVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return "", err
	}
	if v := parseVersionOutput(string(out)); v != "" {
		return v, nil
	}
	return "", errors.New("it printed no version")
}

// parseVersionOutput extracts the version token from `ghost version` output,
// whose first meaningful line is "👻 Ghost <version> (git: ...)".
func parseVersionOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
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
	return ""
}

// installBinary puts src at target atomically, escalating only when the
// install directory needs it.
func installBinary(env updateEnv, scope appliance.ScopePaths, src, target string) error {
	if scope.NeedsRoot() && os.Geteuid() != 0 {
		return env.sudo("install", "-m", "0755", src, target)
	}
	return appliance.AtomicInstall(src, target)
}

// installStaged swaps the staged release in. Services are already stopped.
func installStaged(env updateEnv, scope appliance.ScopePaths, st *stagedRelease) error {
	target := filepath.Join(scope.BinDir, "ghost")
	if err := installBinary(env, scope, st.ghost, target); err != nil {
		return err
	}
	fmt.Printf("  Installed %s\n", target)
	if st.web != "" {
		webTarget := filepath.Join(scope.BinDir, "ghost-web")
		if err := installBinary(env, scope, st.web, webTarget); err != nil {
			return err
		}
		fmt.Printf("  Installed %s\n", webTarget)
	}
	if scope.Scope == appliance.ScopeSystem {
		refreshUnitsFromBinary(env, st.ghost, scope.BinDir)
		home, _ := os.UserHomeDir()
		removeStaleShadows(scope.BinDir, []string{filepath.Join(home, ".local", "bin"), "/usr/local/sbin"})
	}
	env.restart(scope)
	return nil
}

// refreshUnitsFromBinary re-renders the system units from the templates the
// new release carries (the staged binary prints its own). The running
// updater is the old release, so it must not use its own copies.
func refreshUnitsFromBinary(env updateEnv, ghostBin, binDir string) {
	for _, name := range []string{"ghost", "ghost-web"} {
		out, err := exec.Command(ghostBin, "__unit", name).Output()
		if err != nil || len(out) == 0 {
			fmt.Printf("  Could not read the new %s unit template; leaving the installed unit as it is.\n", name)
			continue
		}
		refreshUnit(env.sudo, name, string(out), binDir)
	}
}

// unitCmd prints one of this binary's embedded unit templates.
func unitCmd(args []string) {
	if len(args) != 1 {
		os.Exit(2)
	}
	tpl, ok := ghostroot.UnitTemplate(args[0])
	if !ok {
		os.Exit(2)
	}
	fmt.Print(tpl)
}

// deployRelease is the crash-safe sequence for a resolved release: stage
// while running, plan, snapshot, stop, swap, restart. Any failure before the
// swap leaves Ghost exactly as it was.
func deployRelease(env updateEnv, scope appliance.ScopePaths, rel *appliance.Release) error {
	fmt.Println("Updating Ghost...")
	var st *stagedRelease
	defer func() {
		if st != nil {
			st.cleanup()
		}
	}()
	steps := appliance.UpdateSteps{
		Pull: func() error {
			var err error
			st, err = stageRelease(rel, scope)
			return err
		},
		Plan:     env.plan,
		Snapshot: env.snapshot,
		Stop: func() {
			fmt.Println("3. Stopping services...")
			env.service(scope, "stop", "ghost")
			env.service(scope, "stop", "ghost-web")
		},
		Apply: func() error {
			fmt.Println("4. Installing...")
			if err := env.migrate(); err != nil {
				return err
			}
			return installStaged(env, scope, st)
		},
		Start: func() error {
			fmt.Println("Update failed — restarting services...")
			env.service(scope, "start", "ghost")
			env.service(scope, "start", "ghost-web")
			return nil
		},
	}
	if err := appliance.RunUpdate(steps); err != nil {
		return err
	}
	_ = changelog.MarkSeen(ghostDataDir(), rel.Version)
	fmt.Println("Update complete!")
	if body := strings.TrimSpace(rel.Notes); body != "" {
		fmt.Printf("\nWhat's new in %s:\n\n%s\n", rel.Version, body)
	}
	return nil
}
