package connector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
)

func levelOf(findings []Finding, field, level string) bool {
	for _, f := range findings {
		if f.Field == field && f.Level == level {
			return true
		}
	}
	return false
}

func TestReviewProvenanceWarning(t *testing.T) {
	m := validNative() // no provenance
	if !levelOf(Review(m), "provenance.source", "warning") {
		t.Fatalf("expected a provenance warning: %v", Review(m))
	}
	m.Provenance.Source = "local"
	if levelOf(Review(m), "provenance.source", "warning") {
		t.Fatalf("provenance set should clear the warning: %v", Review(m))
	}
}

func TestReviewOAuthScopesWarning(t *testing.T) {
	m := validNative()
	m.Auth.Kind = connectedapp.AuthOAuth
	m.Auth.Setup = connectedapp.SetupConsoleOAuth
	m.Provenance.Source = "local"
	if !levelOf(Review(m), "auth.scopes", "warning") {
		t.Fatalf("expected an oauth scopes warning: %v", Review(m))
	}
	m.Auth.Scopes = []string{"read"}
	if levelOf(Review(m), "auth.scopes", "warning") {
		t.Fatalf("scopes set should clear the warning: %v", Review(m))
	}
}

func TestReviewHighImpactWarning(t *testing.T) {
	m := validNative()
	m.Capabilities = []Capability{
		{ID: "a.one", Risk: capability.RiskHighImpact},
		{ID: "a.two", Risk: capability.RiskHighImpact},
		{ID: "a.three", Risk: capability.RiskHighImpact},
	}
	m.Provenance.Source = "local"
	if !levelOf(Review(m), "capabilities", "warning") {
		t.Fatalf("expected a high-impact warning: %v", Review(m))
	}
}

func TestReviewInvalidBlocks(t *testing.T) {
	m := validNative()
	m.ID = ""
	findings := Review(m)
	if !HasErrors(findings) {
		t.Fatalf("expected errors: %v", findings)
	}
}

func TestInstallAndProvenance(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, FileName)
	if err := Save(validNative(), src); err != nil {
		t.Fatal(err)
	}
	destRoot := t.TempDir()
	dest, err := Install(src, destRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, FileName)); err != nil {
		t.Fatalf("manifest not installed: %v", err)
	}
	rec, err := os.ReadFile(filepath.Join(dest, SourceRecordName))
	if err != nil {
		t.Fatalf("provenance not written: %v", err)
	}
	if len(rec) == 0 {
		t.Fatal("empty provenance record")
	}
}

func TestInstallRefusesDifferentSameID(t *testing.T) {
	srcDir := t.TempDir()
	v1 := validNative()
	if err := Save(v1, filepath.Join(srcDir, "v1", FileName)); err != nil {
		t.Fatal(err)
	}
	v2 := validNative()
	v2.Version = "2.0.0"
	if err := Save(v2, filepath.Join(srcDir, "v2", FileName)); err != nil {
		t.Fatal(err)
	}

	destRoot := t.TempDir()
	if _, err := Install(filepath.Join(srcDir, "v1"), destRoot, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(filepath.Join(srcDir, "v2"), destRoot, false); err == nil {
		t.Fatal("expected a refusal when replacing a different connector without --force")
	}
	if _, err := Install(filepath.Join(srcDir, "v2"), destRoot, true); err != nil {
		t.Fatalf("--force should replace: %v", err)
	}
}

func TestInstallRejectsInvalid(t *testing.T) {
	srcDir := t.TempDir()
	bad := validNative()
	bad.Kind = KindMCP // no mcp spec
	src := filepath.Join(srcDir, FileName)
	if err := Save(bad, src); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(src, t.TempDir(), false); err == nil {
		t.Fatal("expected install to reject an invalid connector")
	}
}
