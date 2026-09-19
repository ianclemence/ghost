package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/connector"
)

func TestConnectorView(t *testing.T) {
	m := &connector.Manifest{
		SchemaVersion: connector.SchemaVersion,
		ID:            "acme",
		DisplayName:   "Acme",
		Kind:          connector.KindOpenAPI,
		Version:       "1.0.0",
		Capabilities:  []connector.Capability{{ID: "acme.list", Title: "List", Risk: capability.RiskReadOnly}},
	}
	v := connectorView(m, "installed")
	if v["id"] != "acme" || v["kind"] != "openapi" || v["source"] != "installed" {
		t.Fatalf("unexpected view: %+v", v)
	}
	caps, _ := v["capabilities"].([]map[string]interface{})
	if len(caps) != 1 || caps[0]["risk"] != "read_only" {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
}

func TestFilterConnectors(t *testing.T) {
	in := []map[string]interface{}{
		{"id": "acme", "display_name": "Acme", "capabilities": []map[string]interface{}{{"id": "acme.list"}}},
		{"id": "gmail", "display_name": "Gmail", "capabilities": []map[string]interface{}{{"id": "email.send"}}},
	}
	if got := filterConnectors(in, "email"); len(got) != 1 || got[0]["id"] != "gmail" {
		t.Fatalf("capability filter failed: %+v", got)
	}
	if got := filterConnectors(in, "acme"); len(got) != 1 || got[0]["id"] != "acme" {
		t.Fatalf("id filter failed: %+v", got)
	}
	if got := filterConnectors(in, "zzz"); len(got) != 0 {
		t.Fatalf("expected no matches, got %+v", got)
	}
}

func TestFirstPartyView(t *testing.T) {
	v := firstPartyView(connectedapp.FirstParty()[0])
	if v["kind"] != "native" || v["source"] != "first-party" {
		t.Fatalf("unexpected first-party view: %+v", v)
	}
}
