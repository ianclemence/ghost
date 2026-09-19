package agent

import (
	"context"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/connector"
	"github.com/ianclemence/ghost/pkg/mcp"
	"github.com/ianclemence/ghost/pkg/tools"
)

// connectorMCPConfig maps a connector's mcp spec to the MCP manager's server
// config. A URL means an HTTP server; otherwise a command is spawned.
func connectorMCPConfig(m *connector.Manifest) config.MCPServerConfig {
	if m == nil || m.MCP == nil {
		return config.MCPServerConfig{}
	}
	return config.MCPServerConfig{
		Enabled: true,
		Command: m.MCP.Command,
		Args:    m.MCP.Args,
		Env:     m.MCP.Env,
		HTTP:    strings.TrimSpace(m.MCP.URL) != "",
		HTTPURL: m.MCP.URL,
	}
}

// registerConnectorMCPTools connects installed mcp connectors to the shared MCP
// manager and registers each declared capability as an MCP-backed tool. It
// returns the number of tools registered. A connector whose server will not
// connect is skipped honestly (the rest still load); native/skill/openapi
// connectors are ignored here.
func registerConnectorMCPTools(registry *tools.ToolRegistry, manager *mcp.Manager, workspace string) int {
	if registry == nil || manager == nil {
		return 0
	}
	manifests, err := connector.LoadInstalled(workspace)
	if err != nil {
		return 0
	}
	registered := 0
	for _, m := range manifests {
		if m.Kind != connector.KindMCP || m.MCP == nil {
			continue
		}
		serverName := "connector-" + m.ID
		if err := manager.ConnectServer(context.Background(), serverName, connectorMCPConfig(m)); err != nil {
			continue
		}
		infos := manager.ListToolInfos()
		for _, c := range m.Capabilities {
			if c.Operation == nil || strings.TrimSpace(c.Operation.Tool) == "" {
				continue
			}
			for _, info := range infos {
				if info.Server == serverName && info.Tool.Name == c.Operation.Tool {
					registry.Register(tools.NewMCPTool(manager, serverName, info.Tool))
					registered++
					break
				}
			}
		}
	}
	return registered
}
