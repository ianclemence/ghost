package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/appliance"
)

// fakeBinary is a script that behaves like `ghost version` for the updater.
func fakeBinary(version string) []byte {
	return []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo \"👻 Ghost " + version + " (git: test)\"; fi\n")
}

type relOpts struct {
	version      string // tag of the release
	binVersion   string // what the shipped binary says (defaults to version)
	omitSig      bool
	omitSums     bool
	tamper       bool // serve a different binary than the signed list names
	tamperSums   bool // serve a different checksum list than the one signed
	signWith     ed25519.PrivateKey
	omitPlatform bool // no binary for this platform
}

type fakeRel struct {
	rel   *appliance.Release
	trust ed25519.PublicKey // the key to put in GHOST_RELEASE_PUBKEY
	srv   *httptest.Server
}

func newFakeRelease(t *testing.T, o relOpts) *fakeRel {
	t.Helper()
	if o.binVersion == "" {
		o.binVersion = o.version
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if o.signWith != nil {
		priv = o.signWith
	}
	files := map[string][]byte{}
	name := appliance.BinaryAssetName("ghost", runtimeGOOS(), runtimeArch())
	good := fakeBinary(o.binVersion)
	binSum := sha256.Sum256(good)
	signedList := []byte(hex.EncodeToString(binSum[:]) + "  " + name + "\n")
	listDigest := sha256.Sum256(signedList)
	sig := hex.EncodeToString(ed25519.Sign(priv, listDigest[:]))

	served := good
	if o.tamper {
		served = append(append([]byte{}, good...), []byte("\necho pwned\n")...)
	}
	if !o.omitPlatform {
		files[name] = served
	}
	if !o.omitSums {
		list := signedList
		if o.tamperSums {
			evil := sha256.Sum256(served)
			list = []byte(hex.EncodeToString(evil[:]) + "  " + name + "\n")
		}
		files["checksums.txt"] = list
		if !o.omitSig {
			files["checksums.txt.sig"] = []byte(sig + "\n")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	rel := &appliance.Release{Version: o.version, Notes: "- a change"}
	for n, b := range files {
		rel.Assets = append(rel.Assets, appliance.Asset{Name: n, URL: srv.URL + "/" + n, Size: int64(len(b))})
	}
	return &fakeRel{rel: rel, trust: pub, srv: srv}
}

// recorder is an updateEnv that records what the update did to the machine.
type recorder struct {
	calls []string
}

func (r *recorder) env() updateEnv {
	return updateEnv{
		plan:     func() error { r.calls = append(r.calls, "plan"); return nil },
		snapshot: func() error { r.calls = append(r.calls, "snapshot"); return nil },
		migrate:  func() error { r.calls = append(r.calls, "migrate"); return nil },
		service: func(_ appliance.ScopePaths, action string, svcs ...string) {
			r.calls = append(r.calls, action+" "+strings.Join(svcs, ","))
		},
		restart: func(appliance.ScopePaths) { r.calls = append(r.calls, "restart") },
		sudo:    func(string, ...string) error { return errors.New("no sudo in tests") },
	}
}

func (r *recorder) did(prefix string) bool {
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func userScope(t *testing.T) (appliance.ScopePaths, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	old := fakeBinary("v1.0.0")
	if err := os.WriteFile(filepath.Join(bin, "ghost"), old, 0o755); err != nil {
		t.Fatal(err)
	}
	return appliance.ScopePaths{Scope: appliance.ScopeUser, BinDir: bin, UserBinDir: bin}, filepath.Join(bin, "ghost")
}

func trust(t *testing.T, f *fakeRel) {
	t.Helper()
	t.Setenv("GHOST_RELEASE_PUBKEY", hex.EncodeToString(f.trust))
	t.Setenv("GHOST_RELEASE_API", f.srv.URL)
}

func installedVersion(t *testing.T, path string) string {
	t.Helper()
	v, err := binaryVersion(path)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDeployInstallsAVerifiedRelease(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0"})
	trust(t, f)
	scope, ghost := userScope(t)
	var r recorder
	if err := deployRelease(r.env(), scope, f.rel); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if got := installedVersion(t, ghost); got != "v2.0.0" {
		t.Fatalf("installed %s, want v2.0.0", got)
	}
	// The order that makes an update recoverable: snapshot, then stop, then swap, then restart.
	want := []string{"plan", "snapshot", "stop ghost", "stop ghost-web", "migrate", "restart"}
	if strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v, want %v", r.calls, want)
	}
}

// Refusals must happen while Ghost is still running and before anything is
// replaced: the installed binary is untouched and the services never stop.
func assertRefused(t *testing.T, err error, r *recorder, ghost, wantInErr string) {
	t.Helper()
	if err == nil {
		t.Fatal("the release was installed; it must have been refused")
	}
	if wantInErr != "" && !strings.Contains(err.Error(), wantInErr) {
		t.Errorf("error %q does not mention %q", err, wantInErr)
	}
	if got := installedVersion(t, ghost); got != "v1.0.0" {
		t.Errorf("installed binary changed to %s", got)
	}
	if r.did("stop") || r.did("snapshot") || r.did("restart") {
		t.Errorf("services were touched before the refusal: %v", r.calls)
	}
}

func TestDeployRefusesATamperedBinary(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0", tamper: true})
	trust(t, f)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "checksum mismatch")
}

// An attacker who can edit release assets swaps in a binary AND a checksum
// list that blesses it. The signature was made over the original list.
func TestDeployRefusesASwappedChecksumList(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0", tamper: true, tamperSums: true})
	trust(t, f)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "trusted key")
}

func TestDeployRefusesAReleaseWithNoSignature(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0", omitSig: true})
	trust(t, f)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "no signature")
}

func TestDeployRefusesAReleaseWithNoChecksums(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0", omitSums: true})
	trust(t, f)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "checksums.txt")
}

// Signed by a key this binary has never been told to trust.
func TestDeployRefusesAnUntrustedSigner(t *testing.T) {
	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	f := newFakeRelease(t, relOpts{version: "v2.0.0", signWith: stranger})
	trust(t, f) // trusts f.trust, which is NOT the stranger's key
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "trusted key")
}

// An old signed binary re-published under a newer tag must not install: the
// tag is unsigned, the binary's own version is.
func TestDeployRefusesATagThatLiesAboutTheBinary(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0", binVersion: "v0.5.0"})
	trust(t, f)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "tagged v2.0.0")
}

// The real signing key is baked in, so a build with no extra key still has
// something to verify against, and a stranger's signature is refused by it.
func TestEmbeddedReleaseKeyIsRealAndEnforced(t *testing.T) {
	t.Setenv("GHOST_RELEASE_PUBKEY", "")
	keys, err := trustedReleaseKeys()
	if err != nil || len(keys) != 1 {
		t.Fatalf("embedded keys: %v, %v", keys, err)
	}
	f := newFakeRelease(t, relOpts{version: "v2.0.0"}) // signed by a throwaway key
	t.Setenv("GHOST_RELEASE_API", f.srv.URL)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "trusted key")
}

// A failed verification must never be answered by building from source: that
// would let whoever tampered with the release pick the weaker path.
func TestFailedVerificationDoesNotFallBackToSource(t *testing.T) {
	f := newFakeRelease(t, relOpts{version: "v2.0.0", tamper: true})
	trust(t, f)
	t.Setenv("GHOST_SOURCE_URL", filepath.Join(t.TempDir(), "no-such-repo"))
	scope, ghost := userScope(t)
	var r recorder
	err := deployRelease(r.env(), scope, f.rel)
	assertRefused(t, err, &r, ghost, "checksum mismatch")
	if strings.Contains(err.Error(), "fetching") {
		t.Errorf("tried the source path after a verification failure: %v", err)
	}
}

// A platform with no prebuilt binary builds the release TAG from a throwaway
// checkout, not the owner's tree and not a later commit.
func TestNoPrebuiltBinaryBuildsTheTagNotTheCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := tinyGhostRepo(t)
	t.Setenv("GHOST_SOURCE_URL", repo)
	f := newFakeRelease(t, relOpts{version: "v2.0.0", omitPlatform: true})
	t.Setenv("GHOST_RELEASE_API", f.srv.URL)
	scope, ghost := userScope(t)
	var r recorder
	if err := deployRelease(r.env(), scope, f.rel); err != nil {
		t.Fatalf("deploy from source: %v", err)
	}
	out, err := exec.Command(ghost).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "built from the v2.0.0 tag, stamped v2.0.0" {
		t.Fatalf("the installed binary is %q: it was built from the wrong source", got)
	}
}

func TestSourceFallbackNeedsTheTagToExist(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GHOST_SOURCE_URL", tinyGhostRepo(t))
	f := newFakeRelease(t, relOpts{version: "v9.9.9", omitPlatform: true})
	t.Setenv("GHOST_RELEASE_API", f.srv.URL)
	scope, ghost := userScope(t)
	var r recorder
	assertRefused(t, deployRelease(r.env(), scope, f.rel), &r, ghost, "v9.9.9")
}

// tinyGhostRepo is a throwaway git repository with a v2.0.0 tag and a LATER
// commit on main that must not be built.
func tinyGhostRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(msg string) {
		src := fmt.Sprintf("package main\n\nimport \"fmt\"\n\nvar version = \"dev\"\n\nfunc main() { fmt.Printf(\"%s, stamped %%s\\n\", version) }\n", msg)
		if err := os.MkdirAll(filepath.Join(dir, "cmd", "ghost"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmd", "ghost", "main.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tiny\n\ngo 1.21\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("built from the v2.0.0 tag")
	git("add", ".")
	git("commit", "-q", "-m", "release")
	git("tag", "v2.0.0")
	write("built from an unreleased commit")
	git("add", ".")
	git("commit", "-q", "-m", "unreleased work")
	return dir
}

func TestParseVersionOutput(t *testing.T) {
	cases := map[string]string{
		"👻 Ghost v0.24.105 (git: abc)\nGo: go1.26": "v0.24.105",
		"Ghost v1.2.3": "v1.2.3",
		"nothing here": "",
	}
	for in, want := range cases {
		if got := parseVersionOutput(in); got != want {
			t.Errorf("parseVersionOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReleaseCarriesItsOwnUnitTemplates(t *testing.T) {
	for _, name := range []string{"ghost", "ghost-web"} {
		out, err := exec.Command("go", "run", ".", "__unit", name).CombinedOutput()
		if err != nil {
			t.Fatalf("__unit %s: %v\n%s", name, err, out)
		}
		if !strings.Contains(string(out), "[Service]") || !strings.Contains(string(out), "__BIN_DIR__") {
			t.Errorf("the %s template looks wrong: %q", name, out)
		}
	}
}

func TestReleaseJSONDecodesTheWayGitHubSendsIt(t *testing.T) {
	var rel appliance.Release
	body := `{"tag_name":"v1.2.3","body":"notes","prerelease":false,"assets":[{"name":"ghost_linux_arm64","browser_download_url":"https://x/y","size":5}]}`
	if err := json.Unmarshal([]byte(body), &rel); err != nil {
		t.Fatal(err)
	}
	if a, ok := rel.FindAsset("ghost_linux_arm64"); !ok || a.URL != "https://x/y" || rel.Version != "v1.2.3" {
		t.Fatalf("decoded %+v", rel)
	}
}
