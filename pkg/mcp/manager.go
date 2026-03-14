package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func loadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open env file: %w", err)
	}
	defer file.Close()

	envVars := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format at line %d: %s", lineNum, line)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid format at line %d: empty key", lineNum)
		}
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		envVars[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env file: %w", err)
	}
	return envVars, nil
}

type ServerConnection struct {
	Name    string
	Client  *mcp.Client
	Session *mcp.ClientSession
	Tools   []*mcp.Tool
}

type Manager struct {
	servers map[string]*ServerConnection
	mu      sync.RWMutex
	closed  atomic.Bool
	wg      sync.WaitGroup
}

type ToolInfo struct {
	Server string
	Tool   *mcp.Tool
}

func NewManager() *Manager {
	return &Manager{
		servers: make(map[string]*ServerConnection),
	}
}

func (m *Manager) LoadFromConfig(ctx context.Context, cfg *config.Config) error {
	return m.LoadFromMCPConfig(ctx, cfg.Tools.MCP, cfg.WorkspacePath())
}

func (m *Manager) LoadFromMCPConfig(ctx context.Context, mcpCfg config.MCPConfig, workspacePath string) error {
	if !mcpCfg.Enabled {
		return nil
	}
	if len(mcpCfg.Servers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(mcpCfg.Servers))

	for name, serverCfg := range mcpCfg.Servers {
		if !serverCfg.Enabled {
			continue
		}
		wg.Add(1)
		go func(name string, serverCfg config.MCPServerConfig, workspace string) {
			defer wg.Done()
			if serverCfg.EnvFile != "" && !filepath.IsAbs(serverCfg.EnvFile) {
				if workspace == "" {
					errs <- fmt.Errorf("workspace path is empty while resolving relative envFile %q for server %s", serverCfg.EnvFile, name)
					return
				}
				serverCfg.EnvFile = filepath.Join(workspace, serverCfg.EnvFile)
			}
			if err := m.ConnectServer(ctx, name, serverCfg); err != nil {
				errs <- fmt.Errorf("failed to connect to server %s: %w", name, err)
			}
		}(name, serverCfg, workspacePath)
	}

	wg.Wait()
	close(errs)

	var errList []error
	for err := range errs {
		errList = append(errList, err)
	}
	if len(errList) > 0 {
		return errors.Join(errList...)
	}
	return nil
}

func (m *Manager) ConnectServer(ctx context.Context, name string, cfg config.MCPServerConfig) error {
	if name == "" {
		return fmt.Errorf("server name is required")
	}
	if cfg.Command == "" && !cfg.HTTP {
		return fmt.Errorf("server command is required")
	}

	envVars := make(map[string]string)
	if cfg.EnvFile != "" {
		loaded, err := loadEnvFile(cfg.EnvFile)
		if err != nil {
			return err
		}
		for k, v := range loaded {
			envVars[k] = v
		}
	}
	for k, v := range cfg.Env {
		envVars[k] = v
	}

	var client *mcp.Client
	var session *mcp.ClientSession
	var tools []*mcp.Tool

	if cfg.HTTP {
		headers := cfg.Headers
		client = mcp.NewClient(&mcp.Implementation{Name: "ghost"}, nil)
		transport := &mcp.SSEClientTransport{
			Endpoint: cfg.HTTPURL,
			HTTPClient: &http.Client{
				Transport: &headerTransport{
					headers: headers,
				},
			},
		}
		handshakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		sess, err := client.Connect(handshakeCtx, transport, nil)
		if err != nil {
			return err
		}
		session = sess
	} else {
		cmd := exec.Command(cfg.Command, cfg.Args...)
		if cfg.Workdir != "" {
			cmd.Dir = cfg.Workdir
		}
		if len(envVars) > 0 {
			envList := os.Environ()
			for k, v := range envVars {
				envList = append(envList, fmt.Sprintf("%s=%s", k, v))
			}
			cmd.Env = envList
		}
		client = mcp.NewClient(&mcp.Implementation{Name: "ghost"}, nil)
		transport := &mcp.CommandTransport{Command: cmd}
		handshakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		sess, err := client.Connect(handshakeCtx, transport, nil)
		if err != nil {
			return err
		}
		session = sess
	}

	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return err
	}
	tools = toolsResult.Tools

	conn := &ServerConnection{
		Name:    name,
		Client:  client,
		Session: session,
		Tools:   tools,
	}

	m.mu.Lock()
	m.servers[name] = conn
	m.mu.Unlock()

	logger.InfoCF("mcp", "Connected MCP server", map[string]any{
		"server": name,
		"tools":  len(tools),
	})
	return nil
}

func (m *Manager) ListTools() []*mcp.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []*mcp.Tool
	for _, s := range m.servers {
		all = append(all, s.Tools...)
	}
	return all
}

func (m *Manager) ListToolInfos() []ToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []ToolInfo
	for name, s := range m.servers {
		for _, t := range s.Tools {
			all = append(all, ToolInfo{Server: name, Tool: t})
		}
	}
	return all
}

func (m *Manager) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	m.wg.Add(1)
	defer m.wg.Done()
	if m.closed.Load() {
		return "", fmt.Errorf("mcp manager is closed")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.servers {
		for _, t := range s.Tools {
			if t.Name == toolName {
				result, err := s.Session.CallTool(ctx, &mcp.CallToolParams{
					Name:      toolName,
					Arguments: args,
				})
				if err != nil {
					return "", err
				}
				return flattenMCPContent(result.Content), nil
			}
		}
	}
	return "", fmt.Errorf("tool not found: %s", toolName)
}

func (m *Manager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for name, s := range m.servers {
		if s.Session != nil {
			if err := s.Session.Close(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
		}
	}
	m.servers = map[string]*ServerConnection{}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func flattenMCPContent(content []mcp.Content) string {
	var sb strings.Builder
	for _, c := range content {
		switch v := c.(type) {
		case *mcp.TextContent:
			sb.WriteString(v.Text)
		default:
			if data, err := json.Marshal(v); err == nil {
				sb.WriteString(string(data))
			}
		}
	}
	return sb.String()
}
