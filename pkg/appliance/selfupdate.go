package appliance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This is Ghost's release client: resolve a release, download and verify its
// artifacts (sha256, and an Ed25519 signature when a public key is configured),
// and install atomically. It replaces the standalone ghost-update-daemon with
// one updater shared by `ghost update` and any future auto-update loop.

// Release describes a published Ghost release.
type Release struct {
	Version    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Notes      string  `json:"body"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// ReleaseClient resolves and downloads releases. An interface so tests can
// inject a fake without a network.
type ReleaseClient interface {
	Latest() (*Release, error)
	ByTag(tag string) (*Release, error)
	Download(url, dst string) error
}

// ErrNotFound is returned when a release or tag does not exist.
var ErrNotFound = errors.New("release not found")

// GitHubClient is the production client backed by GitHub Releases.
type GitHubClient struct {
	Repo   string
	HTTP   *http.Client
	APIURL string // override for tests
}

// NewGitHubClient builds a client for a repository.
func NewGitHubClient(repo string) *GitHubClient {
	return &GitHubClient{Repo: repo, HTTP: &http.Client{Timeout: 120 * time.Second}}
}

func (c *GitHubClient) api(path string) string {
	base := c.APIURL
	if base == "" {
		base = "https://api.github.com"
	}
	return strings.TrimSuffix(base, "/") + path
}

func (c *GitHubClient) get(url string, into any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ghost-selfupdate")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("release host returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// Latest fetches the newest stable release.
func (c *GitHubClient) Latest() (*Release, error) {
	var r Release
	if err := c.get(c.api("/repos/"+c.Repo+"/releases/latest"), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ByTag fetches a release by tag name.
func (c *GitHubClient) ByTag(tag string) (*Release, error) {
	var r Release
	if err := c.get(c.api("/repos/"+c.Repo+"/releases/tags/"+tag), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Download streams an asset to dst.
func (c *GitHubClient) Download(url, dst string) error {
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("asset download returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Sync()
}

// ---------- verification ----------

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

func parseSemver(s string) (maj, min, pat int, ok bool) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 0, 0, false
	}
	maj, _ = strconv.Atoi(m[1])
	min, _ = strconv.Atoi(m[2])
	pat, _ = strconv.Atoi(m[3])
	return maj, min, pat, true
}

// IsNewer reports whether candidate is a strictly newer semantic version than
// current. A non-version candidate is never an upgrade; a non-version current
// makes any valid candidate newer.
func IsNewer(candidate, current string) bool {
	cm, cn, cp, cok := parseSemver(candidate)
	if !cok {
		return false
	}
	um, un, up, uok := parseSemver(current)
	if !uok {
		return true
	}
	switch {
	case cm != um:
		return cm > um
	case cn != un:
		return cn > un
	default:
		return cp > up
	}
}

// NormalizeVersion trims a leading "v".
func NormalizeVersion(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }

// SHA256File returns the hex sha256 of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySHA256 checks a file against an expected hex digest (empty = skip).
func VerifySHA256(path, wantHex string) error {
	if wantHex == "" {
		return nil
	}
	got, err := SHA256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filepath.Base(path), got, wantHex)
	}
	return nil
}

// VerifyEd25519 checks an Ed25519 signature (hex) over the file's sha256
// digest. An empty public key or signature is a no-op (unsigned release).
func VerifyEd25519(path string, pubHex, sigHex string) error {
	pubHex = strings.TrimSpace(pubHex)
	sigHex = strings.TrimSpace(sigHex)
	if pubHex == "" || sigHex == "" {
		return nil
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid release public key")
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid release signature")
	}
	sum, err := SHA256File(path)
	if err != nil {
		return err
	}
	digest, err := hex.DecodeString(sum)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), digest, sig) {
		return fmt.Errorf("release signature verification failed")
	}
	return nil
}

// BaseVersion strips any git suffix ("-1-gabc1234", "-dirty") from a version
// string, returning just the leading x.y.z. A marker recorded from a dev build
// then still matches the corresponding changelog entry.
func BaseVersion(s string) string {
	s = NormalizeVersion(s)
	if maj, min, pat, ok := parseSemver(s); ok {
		return fmt.Sprintf("%d.%d.%d", maj, min, pat)
	}
	return s
}

// ChangelogEntry is one versioned section of a CHANGELOG file.
type ChangelogEntry struct {
	Version string
	Body    string
}

var changelogHeaderRe = regexp.MustCompile(`(?m)^##\s+\[?v?(\d+\.\d+\.\d+)\]?.*$`)

// ParseChangelog splits a CHANGELOG.md into versioned entries, newest first.
func ParseChangelog(md string) []ChangelogEntry {
	idx := changelogHeaderRe.FindAllStringSubmatchIndex(md, -1)
	var out []ChangelogEntry
	for i, loc := range idx {
		ver := md[loc[2]:loc[3]]
		start := loc[1]
		end := len(md)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, ChangelogEntry{Version: ver, Body: strings.TrimSpace(md[start:end])})
	}
	return out
}

// NewEntries returns the entries newer than lastSeen (exclusive), newest
// first, comparing by base version.
func NewEntries(entries []ChangelogEntry, lastSeen string) []ChangelogEntry {
	if strings.TrimSpace(lastSeen) == "" {
		return entries
	}
	base := BaseVersion(lastSeen)
	var out []ChangelogEntry
	for _, e := range entries {
		if e.Version == base {
			break
		}
		out = append(out, e)
	}
	return out
}

// AtomicInstall installs src as dst via a sibling .new file and a rename, so a
// running binary is never partially overwritten. srcFile's mode is preserved as
// executable.
func AtomicInstall(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		os.Remove(tmp) // never leave a truncated .new behind
		return err
	}
	// A short write (disk full) must fail loudly rather than install a
	// truncated binary that then fails at exec time.
	if fi, err := os.Stat(tmp); err != nil {
		os.Remove(tmp)
		return err
	} else if fi.Size() != int64(len(data)) {
		os.Remove(tmp)
		return fmt.Errorf("install %s: truncated write (%d of %d bytes) — check free disk space", dst, fi.Size(), len(data))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
