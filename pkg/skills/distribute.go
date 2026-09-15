// Package-manager distribution for Ghost skills.
//
// The legacy installer fetches one SKILL.md over HTTPS with no identity,
// no lock, and no update story. The distributor replaces it with:
//   - owner/repo[@skill][/subpath][#ref] identity plus git URLs and local paths
//   - a canonical content-addressed store with symlinks into scopes
//     (single source of truth, trivial updates)
//   - two locks: a machine-global tree-SHA lock and a checked-in
//     content-hash project lock for reproducible workspaces
//   - three-tier fetch (well-known index → snapshot tarball → shallow
//     clone) with hard caps, and lazy-anonymous-first git auth that
//     never exports credentials (no gh-token reads, no terminal prompts)
package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Fetch caps: a skill is documentation plus small scripts, never an OS
// image. Well-known download caps mirror the snapshot caps.
const (
	distMaxBytes = 10 << 20 // 10 MiB per download
	distMaxFiles = 1000
	distTimeout  = 60 * time.Second
)

type sourceKind int

const (
	sourceGitHub sourceKind = iota
	sourceGit
	sourceLocal
	sourceWellKnown
)

// DistSource is a parsed install source.
type DistSource struct {
	Kind    sourceKind
	Host    string // git host, default github.com
	Owner   string
	Repo    string
	Subpath string // skill dir inside the repo, "" = repo root
	Ref     string // branch/tag, default main
	Skill   string // @skill name filter, "" = discover
	Local   string // local path when Kind == sourceLocal
	Index   string // well-known index URL when Kind == sourceWellKnown
	RawURL  string // verbatim URL when Kind == sourceGit from file:// or explicit git URL
}

// ParseDistSource accepts:
// owner/repo, owner/repo/path, owner/repo@skill, owner/repo/path@skill,
// owner/repo#ref, https://github.com/o/r/tree/<ref>/<path>,
// git@host:o/r(.git), https://host/o/r(.git), ./local, /abs/path,
// https://host (well-known index discovery), host.tld@skill.
func ParseDistSource(s string) (DistSource, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DistSource{}, fmt.Errorf("empty skill source")
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || filepath.IsAbs(s) {
		return DistSource{Kind: sourceLocal, Local: s}, nil
	}
	if strings.HasPrefix(s, "git@") || strings.HasSuffix(s, ".git") || strings.Contains(s, "://") {
		return parseRemoteSource(s)
	}
	// Bare host with a skill filter: well-known discovery.
	if at := strings.Index(s, "@"); at > 0 && !strings.Contains(s, "/") && strings.Contains(s[:at], ".") {
		return DistSource{Kind: sourceWellKnown, Host: s[:at], Skill: s[at+1:],
			Index: "https://" + s[:at] + "/.well-known/agent-skills/index.json"}, nil
	}
	return parseShorthand(s)
}

func parseShorthand(s string) (DistSource, error) {
	src := DistSource{Kind: sourceGitHub, Host: "github.com", Ref: "main"}
	rest := s
	if i := strings.Index(rest, "#"); i >= 0 {
		src.Ref = rest[i+1:]
		rest = rest[:i]
		if src.Ref == "" {
			return src, fmt.Errorf("empty ref in %q", s)
		}
	}
	if i := strings.Index(rest, "@"); i >= 0 {
		src.Skill = rest[i+1:]
		rest = rest[:i]
		if src.Skill == "" {
			return src, fmt.Errorf("empty skill filter in %q", s)
		}
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return src, fmt.Errorf("want owner/repo[@skill][/path][#ref], got %q", s)
	}
	src.Owner, src.Repo = parts[0], parts[1]
	if len(parts) > 2 {
		src.Subpath = filepath.Join(parts[2:]...)
	}
	if err := checkPathSafe(src.Subpath); err != nil {
		return src, err
	}
	return src, nil
}

func parseRemoteSource(s string) (DistSource, error) {
	src := DistSource{Ref: "main"}
	rest := s
	// file:// URLs clone verbatim (local mirrors, air-gapped registries,
	// and tests). No host parsing: the path is the identity.
	if strings.HasPrefix(rest, "file://") {
		src.Kind = sourceGit
		src.RawURL = rest
		return src, nil
	}
	if i := strings.LastIndex(rest, "#"); i >= 0 && !strings.HasSuffix(rest[:i], ".git") {
		src.Ref = rest[i+1:]
		rest = rest[:i]
	}
	// github.com/o/r/tree/<ref>/<path>
	if strings.Contains(rest, "github.com/") && strings.Contains(rest, "/tree/") {
		rest = strings.TrimPrefix(strings.TrimPrefix(rest, "https://"), "http://")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		ti := -1
		for i, p := range parts {
			if p == "tree" {
				ti = i
				break
			}
		}
		if ti < 0 || ti < 3 || ti+1 >= len(parts) {
			return src, fmt.Errorf("bad github tree URL %q", s)
		}
		src.Kind = sourceGitHub
		src.Host, src.Owner, src.Repo = "github.com", parts[ti-2], parts[ti-1]
		src.Ref = parts[ti+1]
		if ti+2 < len(parts) {
			src.Subpath = filepath.Join(parts[ti+2:]...)
		}
		if err := checkPathSafe(src.Subpath); err != nil {
			return src, err
		}
		return src, nil
	}
	// Bare host: well-known index discovery.
	trimmed := strings.TrimSuffix(strings.TrimSpace(rest), "/")
	if trimmed == "https://"+hostOf(trimmed) || trimmed == "http://"+hostOf(trimmed) {
		src.Kind = sourceWellKnown
		src.Host = hostOf(trimmed)
		scheme := "https"
		if strings.HasPrefix(trimmed, "http://") {
			scheme = "http"
		}
		src.Index = scheme + "://" + src.Host + "/.well-known/agent-skills/index.json"
		return src, nil
	}
	// Generic git URL or scp-like syntax.
	if strings.HasPrefix(rest, "git@") {
		src.Kind = sourceGit
		colon := strings.Index(rest, ":")
		if colon < 0 {
			return src, fmt.Errorf("bad scp-like git URL %q", s)
		}
		src.Host = rest[4:colon]
		path := strings.TrimSuffix(rest[colon+1:], ".git")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 2 {
			return src, fmt.Errorf("bad scp-like git URL %q", s)
		}
		src.Owner, src.Repo = parts[0], parts[1]
		if len(parts) > 2 {
			src.Subpath = filepath.Join(parts[2:]...)
		}
	} else {
		src.Kind = sourceGit
		without := strings.TrimPrefix(strings.TrimPrefix(rest, "https://"), "http://")
		without = strings.TrimSuffix(without, ".git")
		parts := strings.Split(strings.Trim(without, "/"), "/")
		if len(parts) < 3 {
			return src, fmt.Errorf("bad git URL %q", s)
		}
		src.Host, src.Owner, src.Repo = parts[0], parts[1], parts[2]
		if len(parts) > 3 {
			src.Subpath = filepath.Join(parts[3:]...)
		}
	}
	if err := checkPathSafe(src.Subpath); err != nil {
		return src, err
	}
	return src, nil
}

func hostOf(raw string) string {
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if i := strings.Index(raw, "/"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// checkPathSafe rejects traversal and absolute subpaths.
func checkPathSafe(p string) error {
	if p == "" {
		return nil
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute subpath %q", p)
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return fmt.Errorf("traversal subpath %q", p)
		}
	}
	return nil
}

// Slug is the filesystem identity: owner-repo[-skill], kebab-cased.
func (s DistSource) Slug() string {
	if s.Kind == sourceWellKnown {
		return kebab(strings.ToLower(s.Host) + "-" + strings.ToLower(s.Skill))
	}
	owner, repo := s.Owner, strings.TrimSuffix(s.Repo, ".git")
	if owner == "" && s.RawURL != "" {
		base := strings.TrimSuffix(filepath.Base(strings.TrimSuffix(s.RawURL, "/")), ".git")
		if base == "" || base == "." || base == "/" {
			base = "skill"
		}
		owner, repo = "local", base
	}
	base := strings.ToLower(owner + "-" + repo)
	if s.Skill != "" {
		base += "-" + strings.ToLower(s.Skill)
	} else if s.Subpath != "" {
		base += "-" + strings.ToLower(filepath.Base(s.Subpath))
	}
	return kebab(base)
}

func kebab(s string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// CloneURL returns the authenticated-neutral clone URL: plain git, no
// tokens. Credential helpers/SSH agent apply outside this process;
// GIT_TERMINAL_PROMPT=0 guarantees no interactive prompts.
func (s DistSource) CloneURL() string {
	if s.RawURL != "" {
		return s.RawURL
	}
	if s.Kind == sourceGitHub {
		return fmt.Sprintf("https://%s/%s/%s.git", s.Host, s.Owner, s.Repo)
	}
	return ""
}

// StoreDir is the canonical store location for a source.
func StoreDir(dataDir string, src DistSource) string {
	if src.Kind == sourceLocal {
		return ""
	}
	if src.Kind == sourceWellKnown {
		return filepath.Join(dataDir, "skill-store", src.Host, "well-known", kebab(src.Skill))
	}
	host := src.Host
	if host == "" {
		host = "git"
	}
	return filepath.Join(dataDir, "skill-store", host, src.Slug()+"@"+src.Ref)
}

// StoreBaseDir is the canonical store root for a workspace: a sibling
// .skill-store directory, so user content stays clean and the store can
// be wiped without touching memory, sessions, or skills the user edits.
func StoreBaseDir(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), ".skill-store")
}

// --- locks ------------------------------------------------------------

// GlobalLockEntry pins one installed skill machine-wide by tree SHA.
type GlobalLockEntry struct {
	Source    string `json:"source"`
	Ref       string `json:"ref"`
	SkillPath string `json:"skill_path"`
	TreeSHA   string `json:"tree_sha"`
	Installed string `json:"installed_at"`
	Updated   string `json:"updated_at"`
}

// ProjectLockEntry pins one skill for a workspace by content hash:
// reproducible installs without trusting the network.
type ProjectLockEntry struct {
	Source      string `json:"source"`
	Ref         string `json:"ref"`
	SkillPath   string `json:"skill_path"`
	ContentHash string `json:"content_hash"`
}

func globalLockPath(dataDir string) string {
	return filepath.Join(dataDir, "skill-store", ".skill-lock.json")
}

// ProjectLockPath is checked into the workspace for reproducible installs.
func ProjectLockPath(workspace string) string {
	return filepath.Join(workspace, "skills-lock.json")
}

func readGlobalLock(dataDir string) (map[string]GlobalLockEntry, error) {
	out := map[string]GlobalLockEntry{}
	raw, err := os.ReadFile(globalLockPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse global skill lock: %w", err)
	}
	return out, nil
}

func writeGlobalLock(dataDir string, lock map[string]GlobalLockEntry) error {
	raw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(globalLockPath(dataDir)), 0755); err != nil {
		return err
	}
	return os.WriteFile(globalLockPath(dataDir), raw, 0644)
}

func readProjectLock(workspace string) (map[string]ProjectLockEntry, error) {
	out := map[string]ProjectLockEntry{}
	raw, err := os.ReadFile(ProjectLockPath(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse project skill lock: %w", err)
	}
	return out, nil
}

// WriteProjectLock sorts keys for clean merges.
func WriteProjectLock(workspace string, lock map[string]ProjectLockEntry) error {
	keys := make([]string, 0, len(lock))
	for k := range lock {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]byte, 0)
	ordered = append(ordered, '{', '\n')
	for i, k := range keys {
		entry, _ := json.MarshalIndent(lock[k], "    ", "  ")
		kb, _ := json.Marshal(k)
		ordered = append(ordered, []byte("  ")...)
		ordered = append(ordered, kb...)
		ordered = append(ordered, []byte(": ")...)
		ordered = append(ordered, entry...)
		if i+1 < len(keys) {
			ordered = append(ordered, ',')
		}
		ordered = append(ordered, '\n')
	}
	ordered = append(ordered, '}', '\n')
	return os.WriteFile(ProjectLockPath(workspace), ordered, 0644)
}

// ContentHash walks dir (sorted, symlink-free) and hashes paths +
// contents: the reproducibility anchor for project locks. A symlinked
// root (the standard linked install) is resolved first; symlinks
// *inside* content are rejected.
func ContentHash(dir string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in skill content: %s", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// LockedSlugs lists slugs in the machine-global lock, sorted.
func LockedSlugs(dataDir string) ([]string, error) {
	lock, err := readGlobalLock(dataDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(lock))
	for slug := range lock {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out, nil
}

// LockedSource returns the recorded source string for a slug.
func LockedSource(dataDir, slug string) (string, error) {
	lock, err := readGlobalLock(dataDir)
	if err != nil {
		return "", err
	}
	entry, ok := lock[slug]
	if !ok {
		return "", fmt.Errorf("skill %q not in global lock", slug)
	}
	return entry.Source, nil
}

// InstalledCopyMode reports whether a workspace install is a real copy
// (true) or a store symlink (false).
func InstalledCopyMode(workspace, slug string) bool {
	fi, err := os.Lstat(filepath.Join(workspace, "skills", slug))
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink == 0
}

// --- fetch ------------------------------------------------------------

// gitEnv is the lazy-anonymous-first environment: no terminal prompts,
// ssh batch mode (no password prompts), restricted protocols, and —
// critically — no gh-token reads. Whatever credential helper or SSH
// agent the operator configured applies; nothing is exported.
func gitEnv() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
		"GIT_ALLOW_PROTOCOL=https:http:ssh:git:file",
	}
}

func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), gitEnv()...)
	return cmd
}

// FetchSkill materializes a source into the canonical store and returns
// the store dir plus a tree/content identity. Tiers: local copy →
// well-known index → snapshot tarball (github shorthand) → shallow clone.
func FetchSkill(ctx context.Context, dataDir string, src DistSource) (storeDir, identity string, err error) {
	if src.Kind == sourceLocal {
		return fetchLocal(dataDir, src)
	}
	storeDir = StoreDir(dataDir, src)
	if src.Kind == sourceWellKnown {
		return fetchWellKnown(ctx, storeDir, src)
	}
	if src.Kind == sourceGitHub {
		if dir, id, ferr := fetchSnapshot(ctx, storeDir, src); ferr == nil {
			return dir, id, nil
		} else {
			err = ferr
		}
	}
	dir, id, cerr := fetchClone(ctx, storeDir, src)
	if cerr != nil {
		if err != nil {
			return "", "", fmt.Errorf("snapshot: %v; clone: %w", err, cerr)
		}
		return "", "", cerr
	}
	return dir, id, nil
}

// originDir resolves a local source to its skill dir (bare SKILL.md dir,
// or the single nested dir containing one).
func originDir(local string) (string, error) {
	info, err := os.Stat(local)
	if err != nil {
		return "", fmt.Errorf("local skill: %w", err)
	}
	if !info.IsDir() {
		return filepath.Dir(local), nil
	}
	if _, err := os.Stat(filepath.Join(local, "SKILL.md")); err == nil {
		return local, nil
	}
	// Skill folders often nest one level deep.
	entries, derr := os.ReadDir(local)
	if derr != nil || len(entries) != 1 || !entries[0].IsDir() {
		return "", fmt.Errorf("local skill %q has no SKILL.md", local)
	}
	return filepath.Join(local, entries[0].Name()), nil
}

func fetchLocal(dataDir string, src DistSource) (string, string, error) {
	_ = dataDir
	origin, err := originDir(src.Local)
	if err != nil {
		return "", "", err
	}
	id, err := ContentHash(origin)
	if err != nil {
		return "", "", err
	}
	return origin, "content:" + id, nil
}

// wellKnownEntry is one skill in a /.well-known/agent-skills index.
type wellKnownEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Download    string `json:"download"`
	Digest      string `json:"digest"` // optional "sha256:<hex>"
}

// fetchWellKnown resolves a skill through the host's agent-skills index
// and downloads the tarball with digest verification when advertised.
func fetchWellKnown(ctx context.Context, storeDir string, src DistSource) (string, string, error) {
	if src.Skill == "" {
		return "", "", fmt.Errorf("well-known source needs a skill (host@skill)")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", src.Index, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("X-Skills-Update-Check", "1")
	client := &http.Client{Timeout: distTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("well-known index HTTP %d", resp.StatusCode)
	}
	var index struct {
		Skills []wellKnownEntry `json:"skills"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&index); err != nil {
		return "", "", fmt.Errorf("well-known index: %w", err)
	}
	var match *wellKnownEntry
	for i := range index.Skills {
		if strings.EqualFold(index.Skills[i].Name, src.Skill) {
			match = &index.Skills[i]
			break
		}
	}
	if match == nil || match.Download == "" {
		return "", "", fmt.Errorf("skill %q not in %s index", src.Skill, src.Host)
	}
	dreq, err := http.NewRequestWithContext(ctx, "GET", match.Download, nil)
	if err != nil {
		return "", "", err
	}
	dresp, err := client.Do(dreq)
	if err != nil {
		return "", "", err
	}
	defer dresp.Body.Close()
	if dresp.StatusCode != 200 {
		return "", "", fmt.Errorf("well-known download HTTP %d", dresp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(dresp.Body, distMaxBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(raw) > distMaxBytes {
		return "", "", fmt.Errorf("skill download exceeds %d bytes", distMaxBytes)
	}
	if match.Digest != "" {
		want := strings.TrimPrefix(match.Digest, "sha256:")
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != strings.ToLower(want) {
			return "", "", fmt.Errorf("well-known digest mismatch for %q", src.Skill)
		}
	}
	tmp, err := os.MkdirTemp("", "ghost-skill-wk-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	if _, err := extractTarGz(bytes.NewReader(raw), tmp); err != nil {
		return "", "", err
	}
	fake := src
	fake.Subpath = ""
	return stageStore(storeDir, tmp, fake, "wellknown:"+match.Digest)
}

// fetchSnapshot downloads the codeload tarball: no git history, bounded.
func fetchSnapshot(ctx context.Context, storeDir string, src DistSource) (string, string, error) {
	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", src.Owner, strings.TrimSuffix(src.Repo, ".git"), src.Ref)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", err
	}
	client := &http.Client{Timeout: distTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("snapshot HTTP %d", resp.StatusCode)
	}
	tmp, err := os.MkdirTemp("", "ghost-skill-snap-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	identity, err := extractTarGz(io.LimitReader(resp.Body, distMaxBytes+1), tmp)
	if err != nil {
		return "", "", err
	}
	return stageStore(storeDir, tmp, src, "snapshot:"+identity)
}

// fetchClone does a shallow, single-branch clone with no prompts.
func fetchClone(ctx context.Context, storeDir string, src DistSource) (string, string, error) {
	cloneURL := src.CloneURL()
	if cloneURL == "" {
		return "", "", fmt.Errorf("no clone URL for source kind %d", src.Kind)
	}
	tmp, err := os.MkdirTemp("", "ghost-skill-git-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	clone := gitCmd(ctx, "", "clone", "--depth", "1", "--branch", src.Ref, "--single-branch", cloneURL, tmp)
	if out, err := clone.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("clone: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	rev := gitCmd(ctx, tmp, "rev-parse", "HEAD")
	out, err := rev.Output()
	if err != nil {
		return "", "", fmt.Errorf("rev-parse: %w", err)
	}
	return stageStore(storeDir, tmp, src, "git:"+strings.TrimSpace(string(out)))
}

// stageStore moves the fetched tree (or its subpath) into the canonical
// store atomically and validates skill bounds.
func stageStore(storeDir, tmp string, src DistSource, identity string) (string, string, error) {
	root := tmp
	// Trees often wrap in one top-level dir (tarballs always, repos with
	// a single skill folder). Descend only past a lone visible dir;
	// dotfiles like .git never count as content.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", "", err
	}
	var visible []os.DirEntry
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			visible = append(visible, e)
		}
	}
	if len(visible) == 1 && visible[0].IsDir() && src.Subpath == "" && !hasSkillMD(tmp) {
		root = filepath.Join(tmp, visible[0].Name())
	}
	if src.Subpath != "" {
		root = filepath.Join(root, src.Subpath)
	}
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err != nil {
		return "", "", fmt.Errorf("no SKILL.md at %q", src.Subpath)
	}
	if err := validateSkillBoundsWalk(root); err != nil {
		return "", "", err
	}
	parent := filepath.Dir(storeDir)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", "", err
	}
	staging := storeDir + ".staging"
	_ = os.RemoveAll(staging)
	if err := copyDir(root, staging); err != nil {
		return "", "", err
	}
	if err := os.Rename(staging, storeDir); err != nil {
		_ = os.RemoveAll(staging)
		return "", "", err
	}
	return storeDir, identity, nil
}

func hasSkillMD(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

// extractTarGz unpacks a bounded tar.gz: no absolute paths, no
// traversal, no symlinks, no devices; returns the sha256 of the stream.
func extractTarGz(r io.Reader, dest string) (string, error) {
	h := sha256.New()
	tee := io.TeeReader(r, h)
	gz, err := gzip.NewReader(tee)
	if err != nil {
		return "", fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := 0
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // skip symlinks, dirs handled implicitly, devices rejected
		}
		files++
		if files > distMaxFiles {
			return "", fmt.Errorf("skill archive exceeds %d files", distMaxFiles)
		}
		name := hdr.Name
		// Strict rejection (not silent sanitizing): absolute paths and
		// any ".." segment fail the whole archive.
		if filepath.IsAbs(name) {
			return "", fmt.Errorf("archive absolute path: %q", name)
		}
		for _, seg := range strings.Split(filepath.ToSlash(name), "/") {
			if seg == ".." {
				return "", fmt.Errorf("archive traversal: %q", name)
			}
		}
		name = filepath.Clean("/" + name)[1:]
		if name == "" || name == "." {
			continue
		}
		if total+hdr.Size > distMaxBytes {
			return "", fmt.Errorf("skill archive exceeds %d bytes", distMaxBytes)
		}
		out := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return "", err
		}
		n, err := io.Copy(f, io.LimitReader(tr, distMaxBytes-total+1))
		f.Close()
		if err != nil {
			return "", err
		}
		total += n
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- install ----------------------------------------------------------

type distInstaller struct {
	dataDir   string
	workspace string
	copy      bool // copy instead of symlink
}

// InstallSource fetches, links, and locks one skill source. It returns
// the workspace skill name (slug).
func InstallSource(ctx context.Context, dataDir, workspace, raw string, copy bool) (string, error) {
	src, err := ParseDistSource(raw)
	if err != nil {
		return "", err
	}
	storeDir, identity, err := FetchSkill(ctx, dataDir, src)
	if err != nil {
		return "", err
	}
	slug := src.Slug()
	if src.Kind == sourceLocal && src.Skill == "" {
		if base := filepath.Base(storeDir); base != "." && base != "/" {
			slug = kebab(strings.ToLower(base))
		}
	}
	linkDir := filepath.Join(workspace, "skills", slug)
	if _, err := os.Lstat(linkDir); err == nil {
		_ = os.RemoveAll(linkDir)
	}
	if err := os.MkdirAll(filepath.Dir(linkDir), 0755); err != nil {
		return "", err
	}
	if copy {
		if err := copyDir(storeDir, linkDir); err != nil {
			return "", err
		}
	} else {
		if err := os.Symlink(storeDir, linkDir); err != nil {
			return "", err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	glock, err := readGlobalLock(dataDir)
	if err != nil {
		return "", err
	}
	glock[slug] = GlobalLockEntry{Source: raw, Ref: src.Ref, SkillPath: src.Subpath, TreeSHA: identity, Installed: now, Updated: now}
	if err := writeGlobalLock(dataDir, glock); err != nil {
		return "", err
	}
	plock, err := readProjectLock(workspace)
	if err != nil {
		return "", err
	}
	// Provenance lands in the installed dir only for copies: a symlinked
	// install shares the canonical store, and store content stays
	// pristine so content hashes reproduce. Symlinked installs carry
	// provenance in the locks instead (global entry + project entry).
	if copy {
		_ = WriteProvenance(linkDir, Provenance{
			Type: "dist", Owner: src.Owner, Repo: src.Repo, Branch: src.Ref, Path: src.Subpath,
		})
	}
	// The project lock pins the pristine store content: what was
	// installed, independent of later workspace edits or copies.
	hash, err := ContentHash(storeDir)
	if err != nil {
		return "", err
	}
	plock[slug] = ProjectLockEntry{Source: raw, Ref: src.Ref, SkillPath: src.Subpath, ContentHash: hash}
	if err := WriteProjectLock(workspace, plock); err != nil {
		return "", err
	}
	return slug, nil
}

// UpdateCheck reports whether the upstream moved: git ls-remote HEAD for
// git sources (anonymous, no prompts), content re-hash otherwise.
func UpdateCheck(ctx context.Context, dataDir string, slug string) (current, latest string, changed bool, err error) {
	glock, err := readGlobalLock(dataDir)
	if err != nil {
		return "", "", false, err
	}
	entry, ok := glock[slug]
	if !ok {
		return "", "", false, fmt.Errorf("skill %q not in global lock", slug)
	}
	src, err := ParseDistSource(entry.Source)
	if err != nil {
		return "", "", false, err
	}
	current = entry.TreeSHA
	if src.Kind == sourceLocal {
		origin, oerr := originDir(src.Local)
		if oerr != nil {
			return current, "", false, oerr
		}
		hash, herr := ContentHash(origin)
		if herr != nil {
			return current, "", false, herr
		}
		return current, "content:" + hash, current != "content:"+hash, nil
	}
	if src.Kind == sourceWellKnown {
		hash, herr := ContentHash(StoreDir(dataDir, src))
		if herr != nil {
			return current, "", false, herr
		}
		return current, "content:" + hash, current != "content:"+hash, nil
	}
	ls := gitCmd(ctx, "", "ls-remote", src.CloneURL(), "HEAD")
	out, err := ls.Output()
	if err != nil {
		return current, "", false, fmt.Errorf("ls-remote: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return current, "", false, fmt.Errorf("empty ls-remote")
	}
	latest = "git:" + fields[0]
	return current, latest, current != latest, nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in skill content: %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// validateSkillBoundsWalk enforces the install bounds over a
// staged tree (file count, per-file and total sizes, blocked suffixes).
func validateSkillBoundsWalk(root string) error {
	names := []string{}
	loads := map[string]func(string) ([]byte, error){}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		names = append(names, rel)
		p := path
		loads[rel] = func(string) ([]byte, error) { return os.ReadFile(p) }
		return nil
	})
	if err != nil {
		return err
	}
	return ValidateSkillDownloadBounds(names, func(rel string) ([]byte, error) {
		return loads[rel](rel)
	})
}
