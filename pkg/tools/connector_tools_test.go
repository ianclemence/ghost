package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connector"
)

func TestRegisterConnectorTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from acme"))
	}))
	defer srv.Close()

	ws := t.TempDir()
	m := &connector.Manifest{
		SchemaVersion: connector.SchemaVersion,
		ID:            "acme",
		DisplayName:   "Acme",
		Kind:          connector.KindOpenAPI,
		Version:       "1.0.0",
		Capabilities: []connector.Capability{{
			ID:        "acme.list",
			Risk:      capability.RiskReadOnly,
			Operation: &connector.Operation{Method: "GET", Path: "/items"},
		}},
		OpenAPI: &connector.OpenAPISpec{BaseURL: srv.URL},
	}
	if err := connector.Save(m, filepath.Join(ws, connector.InstalledDir, "acme", connector.FileName)); err != nil {
		t.Fatal(err)
	}

	reg := NewToolRegistry()
	n, err := RegisterConnectorTools(reg, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 tool registered, got %d", n)
	}
	tool, ok := reg.Get("acme.list")
	if !ok {
		t.Fatal("connector tool not registered")
	}
	res := tool.Execute(context.Background(), map[string]interface{}{})
	if res.IsError || !strings.Contains(res.ForLLM, "hello from acme") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRegisterConnectorToolsSkipsNative(t *testing.T) {
	ws := t.TempDir()
	m := &connector.Manifest{
		SchemaVersion: connector.SchemaVersion,
		ID:            "notes",
		DisplayName:   "Notes",
		Kind:          connector.KindNative,
		Version:       "1.0.0",
		Capabilities:  []connector.Capability{{ID: "notes.read", Risk: capability.RiskReadOnly}},
	}
	if err := connector.Save(m, filepath.Join(ws, connector.InstalledDir, "notes", connector.FileName)); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry()
	n, err := RegisterConnectorTools(reg, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("native connectors must not register new tools, got %d", n)
	}
}
