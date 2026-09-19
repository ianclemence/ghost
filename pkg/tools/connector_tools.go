package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/connector"
)

// RegisterConnectorTools loads installed connectors and registers their
// executable capabilities as tools. It returns the number of tools registered.
//
// native/skill connectors are skipped: their capabilities are already
// fulfilled by existing tools. MCP connectors are handled by the MCP manager.
// Only openapi connectors add tools here, each backed by an HTTP executor.
//
// headers, when non-nil, supplies the auth headers for a connector (the caller
// owns the vault). A nil headers func means keyless connectors only; an
// authenticated connector then fails honestly at call time rather than
// silently.
func RegisterConnectorTools(reg *ToolRegistry, workspace string, headers func(*connector.Manifest) (map[string]string, error)) (int, error) {
	if reg == nil {
		return 0, fmt.Errorf("nil tool registry")
	}
	manifests, err := connector.LoadInstalled(workspace)
	if err != nil {
		return 0, err
	}
	registered := 0
	for _, m := range manifests {
		m := m
		if m.Kind != connector.KindOpenAPI {
			continue
		}
		if m.OpenAPI == nil || strings.TrimSpace(m.OpenAPI.BaseURL) == "" {
			continue
		}
		exec := &connector.OpenAPIExecutor{
			BaseURL: m.OpenAPI.BaseURL,
			Headers: func() (map[string]string, error) {
				if headers == nil {
					return nil, nil
				}
				return headers(m)
			},
		}
		for _, c := range m.Capabilities {
			if c.Operation == nil || c.Operation.Method == "" || c.Operation.Path == "" {
				continue
			}
			reg.Register(newConnectorTool(m, c, exec))
			registered++
		}
	}
	return registered, nil
}

// connectorTool adapts one connector capability to Ghost's tool interface, so
// an installed connector participates in the same governed execution path as
// every built-in tool (broker gating, evidence, honest errors).
type connectorTool struct {
	manifest *connector.Manifest
	cap      connector.Capability
	exec     connector.Executor
}

func newConnectorTool(m *connector.Manifest, c connector.Capability, e connector.Executor) *connectorTool {
	return &connectorTool{manifest: m, cap: c, exec: e}
}

func (t *connectorTool) Name() string { return t.cap.ID }

func (t *connectorTool) Description() string {
	d := strings.TrimSpace(t.cap.Description)
	if d == "" {
		d = strings.TrimSpace(t.cap.Title)
	}
	if d == "" {
		d = "Connector capability " + t.cap.ID
	}
	return fmt.Sprintf("%s (connector: %s)", d, t.manifest.DisplayName)
}

func (t *connectorTool) Parameters() map[string]interface{} {
	props := map[string]interface{}{}
	for _, k := range t.cap.RequiredInput {
		props[k] = map[string]interface{}{"type": "string"}
	}
	for _, k := range t.cap.OptionalInput {
		props[k] = map[string]interface{}{"type": "string"}
	}
	return map[string]interface{}{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": true,
	}
}

func (t *connectorTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	out, err := t.exec.Execute(ctx, t.cap, args)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return NewToolResult(out)
}
