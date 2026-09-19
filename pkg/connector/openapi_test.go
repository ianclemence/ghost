package connector

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
)

const crmDoc = `{
  "info": {"title": "Acme CRM", "version": "2.1.0", "description": "Contacts and deals."},
  "servers": [{"url": "https://api.acme.example"}],
  "paths": {
    "/contacts": {
      "get": {"operationId": "listContacts", "summary": "List contacts"},
      "post": {"operationId": "createContact", "summary": "Create a contact"}
    },
    "/contacts/{id}": {
      "delete": {"operationId": "deleteContact", "summary": "Delete a contact"}
    }
  }
}`

func TestFromOpenAPI(t *testing.T) {
	m, err := FromOpenAPI([]byte(crmDoc), OpenAPIOptions{Path: "crm.json"})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "acme-crm" || m.Version != "2.1.0" || m.Kind != KindOpenAPI {
		t.Fatalf("unexpected manifest header: %+v", m)
	}
	if m.OpenAPI == nil || m.OpenAPI.BaseURL != "https://api.acme.example" || m.OpenAPI.Path != "crm.json" {
		t.Fatalf("unexpected openapi block: %+v", m.OpenAPI)
	}
	if m.Auth.Kind != connectedapp.AuthAPIKey || m.Auth.Setup != connectedapp.SetupPasteKey {
		t.Fatalf("unexpected default auth: %+v", m.Auth)
	}

	byID := map[string]Capability{}
	for _, c := range m.Capabilities {
		byID[c.ID] = c
	}
	if c, ok := byID["acme-crm.list-contacts"]; !ok || c.Risk != capability.RiskReadOnly {
		t.Fatalf("expected read_only list-contacts, got %+v", byID)
	}
	if c, ok := byID["acme-crm.create-contact"]; !ok || c.Risk != capability.RiskConsequential {
		t.Fatalf("expected consequential create-contact, got %+v", byID)
	}
	if c, ok := byID["acme-crm.delete-contact"]; !ok || c.Risk != capability.RiskConsequential {
		t.Fatalf("expected consequential delete-contact, got %+v", byID)
	}

	if verrs := m.Validate(); len(verrs) != 0 {
		t.Fatalf("generated draft should validate, got %v", verrs)
	}
}

func TestFromOpenAPIKeyless(t *testing.T) {
	zero := Auth{}
	m, err := FromOpenAPI([]byte(crmDoc), OpenAPIOptions{ID: "crm", Path: "crm.json", Auth: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if m.Auth.Kind != "" {
		t.Fatalf("expected keyless auth, got %+v", m.Auth)
	}
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("keyless connector should validate, got %v", errs)
	}
}

func TestFromOpenAPINoOperations(t *testing.T) {
	if _, err := FromOpenAPI([]byte(`{"info":{"title":"Empty"}}`), OpenAPIOptions{}); err == nil {
		t.Fatal("expected an error for a document with no operations")
	}
}
