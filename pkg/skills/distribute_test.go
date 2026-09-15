package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDistSource(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		repo  string
		sub   string
		ref   string
		skill string
		kind  sourceKind
		host  string
	}{
		{"acme/skills", "acme", "skills", "", "main", "", sourceGitHub, "github.com"},
		{"acme/skills/nested/dir", "acme", "skills", "nested/dir", "main", "", sourceGitHub, "github.com"},
		{"acme/skills@web-design", "acme", "skills", "", "main", "web-design", sourceGitHub, "github.com"},
		{"acme/skills/path@x#v2", "acme", "skills", "path", "v2", "x", sourceGitHub, "github.com"},
		{"acme/skills#develop", "acme", "skills", "", "develop", "", sourceGitHub, "github.com"},
		{"https://github.com/acme/skills/tree/main/path/to", "acme", "skills", "path/to", "main", "", sourceGitHub, "github.com"},
		{"git@github.com:acme/skills.git", "acme", "skills", "", "main", "", sourceGit, "github.com"},
		{"https://git.example.com/acme/skills", "acme", "skills", "", "main", "", sourceGit, "git.example.com"},
		{"./local", "", "", "", "", "", sourceLocal, ""},
		{"/abs/path", "", "", "", "", "", sourceLocal, ""},
		{"https://example.com", "", "", "", "main", "", sourceWellKnown, "example.com"},
		{"example.com@my-skill", "", "", "", "", "my-skill", sourceWellKnown, "example.com"},
		{"file:///tmp/mirror.git", "", "", "", "main", "", sourceGit, ""},
	}
	for _, tc := range cases {
		src, err := ParseDistSource(tc.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.in, err)
			continue
		}
		if src.Kind != tc.kind || src.Owner != tc.owner || src.Repo != tc.repo ||
			src.Subpath != tc.sub || src.Ref != tc.ref || src.Skill != tc.skill || src.Host != tc.host {
			t.Errorf("%q: got %+v", tc.in, src)
		}
	}
	bad := []string{"", "justowner", "a/b/../../c", "acme/skills@", "git@github.com:nocolon"}
	for _, in := range bad {
		if _, err := ParseDistSource(in); err == nil {
			t.Errorf("%q: want error, got nil", in)
		}
	}
}

// mkSkillTree builds a minimal skill dir for offline tests.
func mkSkillTree(t *testing.T, parent, name, desc string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\nDo things thoroughly and carefully every time.\n", name, desc, name)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocalInstallAndLocks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := mkSkillTree(t, root, "demo", "demo skill for tests")
	dataDir := filepath.Join(root, "data")
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0755); err != nil {
		t.Fatal(err)
	}
	slug, err := InstallSource(ctx, dataDir, ws, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "skills", slug)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("default install must symlink to the canonical store")
	}
	if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
		t.Fatal("linked SKILL.md must resolve")
	}
	glock, err := readGlobalLock(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := glock[slug]; !ok {
		t.Fatal("global lock must record the install")
	}
	plock, err := readProjectLock(ws)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := plock[slug]
	if !ok || entry.ContentHash == "" {
		t.Fatal("project lock must record a content hash")
	}
	hash, err := ContentHash(origin)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ContentHash != hash {
		t.Fatal("project lock hash must match content")
	}
	// No-change update check.
	_, _, changed, err := UpdateCheck(ctx, dataDir, slug)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unchanged skill must report no change")
	}
	// Mutate origin: change detected.
	if err := os.WriteFile(filepath.Join(origin, "extra.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, changed, err = UpdateCheck(ctx, dataDir, slug)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("mutated skill must report change")
	}
	// Copy mode installs real files.
	ws2 := filepath.Join(root, "ws2")
	if err := os.MkdirAll(ws2, 0755); err != nil {
		t.Fatal(err)
	}
	// Remove extra.md so reinstall is deterministic for copy check.
	_ = os.Remove(filepath.Join(origin, "extra.md"))
	slug2, err := InstallSource(ctx, dataDir, ws2, origin, true)
	if err != nil {
		t.Fatal(err)
	}
	fi, err = os.Lstat(filepath.Join(ws2, "skills", slug2))
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("copy install must not symlink")
	}
}

func tarGz(t *testing.T, files map[string]string, symlink string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if symlink != "" {
		if err := tw.WriteHeader(&tar.Header{Name: symlink, Typeflag: tar.TypeSymlink, Linkname: "SKILL.md"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSnapshotFetch(t *testing.T) {
	blob := tarGz(t, map[string]string{
		"repo-abc/SKILL.md": "---\nname: s\ndescription: a reasonably long description for tests\n---\n\nBody text here.\n",
		"repo-abc/extra.md": "x",
	}, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(blob)
	}))
	defer srv.Close()
	_ = srv
	// fetchSnapshot builds its own codeload URL; exercise the extractor
	// and stager directly for hermetic coverage.
	tmp := t.TempDir()
	id, err := extractTarGz(bytes.NewReader(blob), tmp)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("extractor must return a stream identity")
	}
	store := filepath.Join(t.TempDir(), "store")
	src := DistSource{Kind: sourceGitHub, Host: "github.com", Owner: "o", Repo: "r", Ref: "main"}
	dir, identity, err := stageStore(store, tmp, src, "snapshot:"+id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(identity, "snapshot:") {
		t.Fatalf("identity %q", identity)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal("staged tree must hold SKILL.md")
	}
}

func TestExtractRejects(t *testing.T) {
	bad := tarGz(t, map[string]string{"../evil.md": "x"}, "")
	if _, err := extractTarGz(bytes.NewReader(bad), t.TempDir()); err == nil {
		t.Fatal("traversal must be rejected")
	}
	abs := tarGz(t, map[string]string{"/abs.md": "x"}, "")
	if _, err := extractTarGz(bytes.NewReader(abs), t.TempDir()); err == nil {
		t.Fatal("absolute paths must be rejected")
	}
	// Symlinks are skipped, not followed.
	withLink := tarGz(t, map[string]string{"SKILL.md": "---\nname: s\ndescription: a reasonably long description here\n---\n\nBody.\n"}, "link.md")
	dest := t.TempDir()
	if _, err := extractTarGz(bytes.NewReader(withLink), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link.md")); !os.IsNotExist(err) {
		t.Fatal("symlink entries must not materialize")
	}
}

func TestWellKnownFetch(t *testing.T) {
	ctx := context.Background()
	skillBlob := tarGz(t, map[string]string{"SKILL.md": "---\nname: wk\ndescription: well-known test skill with a sufficiently long description\n---\n\nBody text here for length.\n"}, "")
	sum := sha256.Sum256(skillBlob)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "index.json") {
			fmt.Fprintf(w, `{"skills":[{"name":"wk","download":"%s/dl.tgz","digest":%q}]}`, srv.URL, digest)
			return
		}
		w.Write(skillBlob)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	src := DistSource{Kind: sourceWellKnown, Host: host, Skill: "wk", Index: srv.URL + "/.well-known/agent-skills/index.json"}
	store := filepath.Join(t.TempDir(), "store")
	dir, id, err := fetchWellKnown(ctx, store, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "wellknown:") {
		t.Fatalf("identity %q", id)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal("downloaded skill must stage")
	}
	// Digest mismatch fails closed.
	srcBad := src
	_ = srcBad
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "index.json") {
			fmt.Fprintf(w, `{"skills":[{"name":"wk","download":"%s/x","digest":"sha256:0000"}]}`, srv.URL)
			return
		}
		w.Write(skillBlob)
	}))
	defer badSrv.Close()
	badHost := strings.TrimPrefix(badSrv.URL, "http://")
	bsrc := DistSource{Kind: sourceWellKnown, Host: badHost, Skill: "wk", Index: badSrv.URL + "/.well-known/agent-skills/index.json"}
	if _, _, err := fetchWellKnown(ctx, filepath.Join(t.TempDir(), "s2"), bsrc); err == nil {
		t.Fatal("digest mismatch must fail")
	}
}

func TestGitCloneFileURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	origin := t.TempDir()
	mkSkillTree(t, origin, "clone-me", "cloned skill")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run(origin, "init")
	run(origin, "add", "-A")
	run(origin, "commit", "-m", "init")
	run(origin, "branch", "-M", "main")
	dataDir := filepath.Join(t.TempDir(), "data")
	src, err := ParseDistSource("file://" + origin)
	if err != nil {
		t.Fatal(err)
	}
	storeDir, id, err := FetchSkill(ctx, dataDir, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "git:") {
		t.Fatalf("identity %q", id)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "SKILL.md")); err != nil {
		// Nested one level: mkSkillTree made origin/clone-me/SKILL.md.
		if _, err2 := os.Stat(filepath.Join(storeDir, "clone-me", "SKILL.md")); err2 != nil {
			t.Fatal("cloned store must hold the skill")
		}
	}
}
