package appliance

import (
	"path/filepath"
	"testing"
)

func TestDetectConfigShadowNoInstalledGhost(t *testing.T) {
	_, ok := DetectConfigShadow("/home/dev/ghost/config/config.json", false, nil)
	if ok {
		t.Fatal("no installed Ghost means nothing to warn about")
	}
}

func TestDetectConfigShadowResolvedIsInstalled(t *testing.T) {
	// Resolved path IS the installed config: no shadow.
	resolved := filepath.Join(DefaultConfigDir, "config.json")
	_, ok := DetectConfigShadow(resolved, true, nil)
	if ok {
		t.Fatalf("resolving to the installed config is not a shadow")
	}
}

func TestDetectConfigShadowReportsWhenDifferent(t *testing.T) {
	sh, ok := DetectConfigShadow("/home/dev/checkout/config/config.json", true, func(p string) string {
		if filepath.Clean(p) == filepath.Clean(filepath.Join(DefaultConfigDir, "config.json")) {
			return "sk-secret"
		}
		return ""
	})
	if !ok {
		t.Fatal("a checkout config shadowing an installed Ghost must be reported")
	}
	if sh.ResolvedPath != "/home/dev/checkout/config/config.json" {
		t.Errorf("resolved path wrong: %q", sh.ResolvedPath)
	}
	if !sh.InstalledHasSecret {
		t.Errorf("installed config has a key; shadow should say so")
	}
}

func TestDetectConfigShadowInstalledWithoutSecret(t *testing.T) {
	sh, ok := DetectConfigShadow("/home/dev/checkout/config/config.json", true, func(string) string { return "" })
	if !ok {
		t.Fatal("installed Ghost exists, so the mismatch is still worth reporting")
	}
	if sh.InstalledHasSecret {
		t.Errorf("installed config has no key; shadow must not claim it does")
	}
}

func TestDetectConfigShadowEmptyResolved(t *testing.T) {
	if _, ok := DetectConfigShadow("", true, nil); ok {
		t.Fatal("empty resolved path is not a shadow")
	}
}

func TestFileExists(t *testing.T) {
	if FileExists("") {
		t.Error("empty path is never an existing file")
	}
	if FileExists("/definitely/not/here/xyz") {
		t.Error("nonexistent path must report false")
	}
}
