package agent

import (
	"context"
	"sync"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/mcp"
	"github.com/ianclemence/ghost/pkg/tools"
)

// Tool servers are outside services (MCP) that give Ghost more tools. They can
// be added and removed while Ghost is running: the connection is made first,
// the tools appear at once, and nothing needs a restart.

var (
	mcpMu        sync.Mutex
	mcpShared    *mcp.Manager
	mcpToolNames = map[string][]string{} // server -> registered tool names
)

// sharedMCPManager is the one manager for the process.
func sharedMCPManager() *mcp.Manager {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	if mcpShared == nil {
		mcpShared = mcp.NewManager()
	}
	return mcpShared
}

// ConnectToolServer connects (or reconnects) a tool server and registers what
// it offers. It returns the names of the tools, and nothing is registered when
// the connection fails.
func (al *AgentLoop) ConnectToolServer(ctx context.Context, name string, cfg config.MCPServerConfig) ([]string, error) {
	m := sharedMCPManager()
	al.DisconnectToolServer(name)
	if err := m.ConnectServer(ctx, name, cfg); err != nil {
		return nil, err
	}
	var names []string
	for _, info := range m.ServerToolInfos(name) {
		t := tools.NewMCPTool(m, info.Server, info.Tool)
		al.tools.Register(t)
		names = append(names, t.Name())
	}
	mcpMu.Lock()
	mcpToolNames[name] = names
	mcpMu.Unlock()
	return names, nil
}

// DisconnectToolServer closes a tool server and removes its tools.
func (al *AgentLoop) DisconnectToolServer(name string) {
	sharedMCPManager().DisconnectServer(name)
	mcpMu.Lock()
	names := mcpToolNames[name]
	delete(mcpToolNames, name)
	mcpMu.Unlock()
	for _, n := range names {
		al.tools.Unregister(n)
	}
}

// ToolServerStatus reports whether a server is connected and how many tools it
// gave Ghost.
func (al *AgentLoop) ToolServerStatus(name string) (connected bool, toolCount int, lastError string) {
	m := sharedMCPManager()
	if h, ok := m.GetServerHealth(name); ok {
		connected, lastError = h.Connected, h.LastError
	}
	return connected, len(m.ServerToolInfos(name)), lastError
}
