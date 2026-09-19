package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/connector"
)

func TestConnectorAuthHeaders(t *testing.T) {
	t.Setenv("ACME_API_KEY", "secret")

	// Default: Authorization: Bearer <key>.
	h, err := connectorAuthHeaders(&connector.Manifest{ID: "acme"})
	if err != nil || h["Authorization"] != "Bearer secret" {
		t.Fatalf("unexpected headers: %v %v", h, err)
	}
	// A custom header carries the raw key (no scheme).
	h, _ = connectorAuthHeaders(&connector.Manifest{ID: "acme", Auth: connector.Auth{Header: "X-API-Key"}})
	if h["X-API-Key"] != "secret" {
		t.Fatalf("expected raw key in X-API-Key, got %v", h)
	}
	// Explicit scheme is honored.
	h, _ = connectorAuthHeaders(&connector.Manifest{ID: "acme", Auth: connector.Auth{Header: "X-API-Key", Scheme: "Token"}})
	if h["X-API-Key"] != "Token secret" {
		t.Fatalf("expected Token scheme, got %v", h)
	}
	// Keyless connector: no headers.
	if h, err := connectorAuthHeaders(&connector.Manifest{ID: "nokey"}); err != nil || len(h) != 0 {
		t.Fatalf("expected no headers, got %v %v", h, err)
	}
	// nil manifest.
	if h, err := connectorAuthHeaders(nil); err != nil || h != nil {
		t.Fatalf("nil manifest must yield nil, nil")
	}
}
