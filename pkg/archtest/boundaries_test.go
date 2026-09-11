package archtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repository root relative to this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// scanGoFiles walks the repo and calls fn for every non-test .go file.
func scanGoFiles(t *testing.T, fn func(path, rel string, src []byte)) {
	t.Helper()
	root := repoRoot(t)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "build" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fn(path, rel, src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestArchitecture_NoActiveCronScheduler proves the legacy second scheduler
// package no longer exists: a single authoritative scheduler remains.
func TestArchitecture_NoActiveCronScheduler(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "pkg", "cron")); err == nil {
		t.Fatal("pkg/cron must not exist: one authoritative scheduler only")
	}
	// No production code may import a cron engine.
	scanGoFiles(t, func(path, rel string, src []byte) {
		if strings.Contains(string(src), `"github.com/ianclemence/ghost/pkg/cron"`) {
			t.Errorf("%s imports the retired pkg/cron scheduler", rel)
		}
	})
}

// TestArchitecture_CredentialStorageOwnedByVault proves no package outside
// the credential boundary reads or writes the raw secret store.
func TestArchitecture_CredentialStorageOwnedByVault(t *testing.T) {
	allowed := map[string]bool{
		"pkg/credentials/credentials.go": true,
		"pkg/credentials/provider_keys.go": true,
		"pkg/config/secrets.go":           true,
		"pkg/config/vault.go":             true,
	}
	scanGoFiles(t, func(path, rel string, src []byte) {
		if allowed[rel] {
			return
		}
		s := string(src)
		if strings.Contains(s, "config.LoadSecrets(") || strings.Contains(s, "config.SaveSecrets(") {
			t.Errorf("%s reads/writes credential storage directly; route through pkg/credentials.Vault", rel)
		}
	})
}

// TestArchitecture_OnePermissionBroker proves there is exactly one broker
// constructor and no second broker implementation.
func TestArchitecture_OnePermissionBroker(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "pkg", "permissions"))
	if err != nil {
		t.Fatal(err)
	}
	brokers := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, "pkg", "permissions", e.Name()))
		if err != nil {
			continue
		}
		brokers += strings.Count(string(src), "type Broker struct")
	}
	if brokers != 1 {
		t.Fatalf("expected exactly one Broker implementation, found %d", brokers)
	}
}
