package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPManager interface {
	CallTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error)
}

type MCPTool struct {
	manager    MCPManager
	serverName string
	tool       *mcp.Tool
}

func NewMCPTool(manager MCPManager, serverName string, tool *mcp.Tool) *MCPTool {
	return &MCPTool{
		manager:    manager,
		serverName: serverName,
		tool:       tool,
	}
}

func (t *MCPTool) Name() string {
	sServer := sanitizeIdentifierComponent(t.serverName)
	sTool := sanitizeIdentifierComponent(t.tool.Name)
	base := fmt.Sprintf("mcp_%s_%s", sServer, sTool)
	lossless := strings.ToLower(t.serverName) == sServer && strings.ToLower(t.tool.Name) == sTool
	if lossless && len(base) <= 64 {
		return base
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.serverName + "\x00" + t.tool.Name))
	suffix := fmt.Sprintf("%08x", h.Sum32())
	if len(base) > 55 {
		base = strings.TrimRight(base[:55], "_")
	}
	return base + "_" + suffix
}

func (t *MCPTool) Description() string {
	desc := t.tool.Description
	if desc == "" {
		desc = "MCP tool"
	}
	return fmt.Sprintf("[MCP:%s] %s", t.serverName, desc)
}

func (t *MCPTool) Parameters() map[string]interface{} {
	schema := t.tool.InputSchema
	if schema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		}
	}
	if schemaMap, ok := schema.(map[string]interface{}); ok {
		return schemaMap
	}
	if raw, ok := schema.(json.RawMessage); ok {
		var result map[string]interface{}
		if err := json.Unmarshal(raw, &result); err == nil {
			return result
		}
	}
	if rawBytes, ok := schema.([]byte); ok {
		var result map[string]interface{}
		if err := json.Unmarshal(rawBytes, &result); err == nil {
			return result
		}
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.manager == nil {
		return ErrorResult(fmt.Errorf("mcp manager not configured"))
	}
	result, err := t.manager.CallTool(ctx, t.tool.Name, args)
	if err != nil {
		return ErrorResult(err)
	}
	return TextResult(result)
}

func sanitizeIdentifierComponent(s string) string {
	const maxLen = 64
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevUnderscore := false
	for _, r := range s {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isAllowed {
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
			continue
		}
		if r == '_' {
			if prevUnderscore {
				continue
			}
			prevUnderscore = true
		} else {
			prevUnderscore = false
		}
		b.WriteRune(r)
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		result = "unnamed"
	}
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}
