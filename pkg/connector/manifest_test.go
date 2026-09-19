package connector

import (
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
)

func validNative() *Manifest {
	return &Manifest{
		SchemaVersion: SchemaVersion,
		ID:            "acme-notes",
		DisplayName:   "Acme Notes",
		Kind:          KindNative,
		Version:       "1.0.0",
		Auth:          Auth{Kind: connectedapp.AuthAPIKey, Setup: connectedapp.SetupPasteKey},
		Capabilities:  []Capability{{ID: "notes.read", Risk: capability.RiskReadOnly}},
	}
}

func hasField(errs []ValidationError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestValidNativeManifest(t *testing.T) {
	if errs := validNative().Validate(); len(errs) != 0 {
		t.Fatalf("expected valid, got %v", errs)
	}
}

func TestRequiredFields(t *testing.T) {
	m := validNative()
	m.ID = ""
	m.DisplayName = "  "
	m.Version = ""
	errs := m.Validate()
	for _, f := range []string{"id", "display_name", "version"} {
		if !hasField(errs, f) {
			t.Fatalf("expected error on %q, got %v", f, errs)
		}
	}
}

func TestReadOnlyCannotNameMutatingTool(t *testing.T) {
	m := validNative()
	m.Capabilities[0].AllowedTools = []string{"notes_create"}
	if !hasField(m.Validate(), "capabilities[0].allowed_tools") {
		t.Fatalf("read_only with a mutating tool must fail: %v", m.Validate())
	}
	// A read tool is fine.
	m.Capabilities[0].AllowedTools = []string{"notes_list"}
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("read tool should pass, got %v", errs)
	}
}

func TestCapabilityIDs(t *testing.T) {
	m := validNative()
	m.Capabilities = []Capability{
		{ID: "nodot", Risk: capability.RiskReadOnly},
		{ID: "dup.read", Risk: capability.RiskReadOnly},
		{ID: "dup.read", Risk: capability.RiskReadOnly},
	}
	errs := m.Validate()
	if !hasField(errs, "capabilities[0].id") {
		t.Fatalf("expected namespacing error, got %v", errs)
	}
	if !hasField(errs, "capabilities[2].id") {
		t.Fatalf("expected duplicate error, got %v", errs)
	}
}

func TestAuthSetupMismatch(t *testing.T) {
	m := validNative()
	m.Auth.Kind = connectedapp.AuthOAuth
	m.Auth.Setup = connectedapp.SetupPasteKey
	if !hasField(m.Validate(), "auth.setup") {
		t.Fatalf("oauth with paste_key must fail: %v", m.Validate())
	}
}

func TestTransportRequirements(t *testing.T) {
	m := validNative()
	m.Kind = KindMCP
	if !hasField(m.Validate(), "mcp") {
		t.Fatalf("mcp without a command/url must fail: %v", m.Validate())
	}
	m.MCP = &MCPSpec{Command: "npx"}
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("mcp with a command should pass, got %v", errs)
	}

	m2 := validNative()
	m2.MCP = &MCPSpec{Command: "x"}
	if !hasField(m2.Validate(), "kind") {
		t.Fatalf("native with an mcp spec must fail: %v", m2.Validate())
	}
}

func TestSignedNeedsHash(t *testing.T) {
	m := validNative()
	m.Provenance.SignedBy = "alice"
	if !hasField(m.Validate(), "provenance.hash") {
		t.Fatalf("signed without hash must fail: %v", m.Validate())
	}
	m.Provenance.Hash = "sha256:abc"
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("signed with hash should pass, got %v", errs)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Acme Notes":      "acme-notes",
		"listContacts":    "list-contacts",
		"  Hello, World!": "hello-world",
		"AcmeCRM":         "acme-crm",
		"--x--":           "x",
		"":                "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadFromDir(t *testing.T) {
	dir := t.TempDir()
	m := validNative()
	if err := Save(m, filepath.Join(dir, FileName)); err != nil {
		t.Fatal(err)
	}
	loaded, verrs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(verrs) != 0 {
		t.Fatalf("expected valid, got %v", verrs)
	}
	if loaded.ID != "acme-notes" || loaded.Kind != KindNative {
		t.Fatalf("unexpected load: %+v", loaded)
	}
}
