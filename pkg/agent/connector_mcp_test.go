package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/connector"
)

func TestConnectorMCPConfig(t *testing.T) {
	cmd := &connector.Manifest{
		ID:   "srv",
		Kind: connector.KindMCP,
		MCP:  &connector.MCPSpec{Command: "npx", Args: []string{"-y", "server"}, Env: map[string]string{"K": "V"}},
	}
	cfg := connectorMCPConfig(cmd)
	if !cfg.Enabled || cfg.Command != "npx" || len(cfg.Args) != 2 || cfg.Env["K"] != "V" || cfg.HTTP {
		t.Fatalf("unexpected command config: %+v", cfg)
	}

	httpSrv := &connector.Manifest{
		ID:   "web",
		Kind: connector.KindMCP,
		MCP:  &connector.MCPSpec{URL: "https://mcp.example/sse"},
	}
	cfg = connectorMCPConfig(httpSrv)
	if !cfg.HTTP || cfg.HTTPURL != "https://mcp.example/sse" || cfg.Command != "" {
		t.Fatalf("unexpected http config: %+v", cfg)
	}

	if cfg := connectorMCPConfig(nil); cfg.Enabled {
		t.Fatal("nil manifest must yield a zero config")
	}
}
