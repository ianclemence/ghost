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

// OAuthToken holds OAuth2 credentials for an MCP server.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// SchemaCache caches tool schemas to avoid repeated ListTools calls.
type SchemaCache struct {
	tools     map[string][]*mcp.Tool // server name -> tools
	updatedAt map[string]time.Time
	mu        sync.RWMutex
	ttl       time.Duration
}

func NewSchemaCache(ttl time.Duration) *SchemaCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &SchemaCache{
		tools:     make(map[string][]*mcp.Tool),
		updatedAt: make(map[string]time.Time),
		ttl:       ttl,
	}
}

func (c *SchemaCache) Get(server string) ([]*mcp.Tool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tools, ok := c.tools[server]
	if !ok {
		return nil, false
	}
	if time.Since(c.updatedAt[server]) > c.ttl {
		return nil, false
	}
	return tools, true
}

func (c *SchemaCache) Set(server string, tools []*mcp.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools[server] = tools
	c.updatedAt[server] = time.Now()
}

func (c *SchemaCache) Invalidate(server string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tools, server)
	delete(c.updatedAt, server)
}

func (c *SchemaCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = make(map[string][]*mcp.Tool)
	c.updatedAt = make(map[string]time.Time)
}

// ServerHealth tracks the health status of a stdio MCP server.
type ServerHealth struct {
	Connected   bool      `json:"connected"`
	LastError   string    `json:"last_error,omitempty"`
	LastCheck   time.Time `json:"last_check"`
	RestartCount int      `json:"restart_count"`
}

type ServerConnection struct {
	Name      string
	Client    *mcp.Client
	Session   *mcp.ClientSession
	Tools     []*mcp.Tool
	OAuth     *OAuthToken
	Health    ServerHealth
	cfg       config.MCPServerConfig
	cancelFn  context.CancelFunc
}

type Manager struct {
	servers    map[string]*ServerConnection
	schemaCache *SchemaCache
	oauthTokens map[string]*OAuthToken // server name -> token
	mu         sync.RWMutex
	closed     atomic.Bool
	wg         sync.WaitGroup
}

type ToolInfo struct {
	Server string
	Tool   *mcp.Tool
}

func NewManager() *Manager {
	return &Manager{
		servers:     make(map[string]*ServerConnection),
		schemaCache: NewSchemaCache(5 * time.Minute),
		oauthTokens: make(map[string]*OAuthToken),
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
	var cancelFn context.CancelFunc

	if cfg.HTTP {
		headers := cfg.Headers

		// Inject OAuth token if available
		if token, ok := m.oauthTokens[name]; ok && token.ExpiresAt.After(time.Now()) {
			if headers == nil {
				headers = make(map[string]string)
			}
			headers["Authorization"] = token.TokenType + " " + token.AccessToken
		}

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
		cancelFn = cancel
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
		cancelFn = cancel
	}

	// Check schema cache first
	if cachedTools, ok := m.schemaCache.Get(name); ok {
		tools = cachedTools
		logger.DebugCF("mcp", "Using cached schemas", map[string]any{"server": name})
	} else {
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			return err
		}
		tools = toolsResult.Tools
		m.schemaCache.Set(name, tools)
	}

	conn := &ServerConnection{
		Name:     name,
		Client:   client,
		Session:  session,
		Tools:    tools,
		cfg:      cfg,
		cancelFn: cancelFn,
		Health: ServerHealth{
			Connected: true,
			LastCheck: time.Now(),
		},
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

// SetOAuthToken stores an OAuth token for a specific MCP server.
func (m *Manager) SetOAuthToken(serverName string, token *OAuthToken) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oauthTokens[serverName] = token
}

// GetOAuthToken retrieves the stored OAuth token for a server.
func (m *Manager) GetOAuthToken(serverName string) (*OAuthToken, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	token, ok := m.oauthTokens[serverName]
	return token, ok
}

// RefreshOAuthToken calls the server's token endpoint to refresh an expired token.
func (m *Manager) RefreshOAuthToken(ctx context.Context, serverName string, tokenURL string, clientID string, clientSecret string) error {
	m.mu.RLock()
	token, ok := m.oauthTokens[serverName]
	m.mu.RUnlock()
	if !ok || token.RefreshToken == "" {
		return fmt.Errorf("no refresh token available for server %s", serverName)
	}

	// Build refresh request
	data := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s&client_id=%s&client_secret=%s",
		token.RefreshToken, clientID, clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("token refresh failed: HTTP %d", resp.StatusCode)
	}

	newToken := &OAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if newToken.RefreshToken == "" {
		newToken.RefreshToken = token.RefreshToken
	}

	m.SetOAuthToken(serverName, newToken)
	logger.InfoCF("mcp", "OAuth token refreshed", map[string]any{"server": serverName})
	return nil
}

// ReconnectServer closes and re-establishes connection to a server.
func (m *Manager) ReconnectServer(ctx context.Context, name string) error {
	m.mu.RLock()
	conn, ok := m.servers[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("server %q not found", name)
	}

	// Close existing connection
	if conn.cancelFn != nil {
		conn.cancelFn()
	}
	if conn.Session != nil {
		conn.Session.Close()
	}

	// Invalidate schema cache
	m.schemaCache.Invalidate(name)

	// Reconnect using stored config
	if err := m.ConnectServer(ctx, name, conn.cfg); err != nil {
		return err
	}

	logger.InfoCF("mcp", "Server reconnected", map[string]any{"server": name})
	return nil
}

// CheckHealth checks connectivity to all servers and reconnects if needed.
func (m *Manager) CheckHealth(ctx context.Context) map[string]ServerHealth {
	m.mu.RLock()
	servers := make(map[string]*ServerConnection)
	for k, v := range m.servers {
		servers[k] = v
	}
	m.mu.RUnlock()

	healthMap := make(map[string]ServerHealth)
	for name, conn := range servers {
		health := conn.Health
		health.LastCheck = time.Now()

		// Try a simple ListTools call to verify connectivity
		_, err := conn.Session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			health.Connected = false
			health.LastError = err.Error()
			logger.WarnCF("mcp", "Server unhealthy, attempting reconnect", map[string]any{
				"server": name,
				"error":  err.Error(),
			})

			if reconnectErr := m.ReconnectServer(ctx, name); reconnectErr == nil {
				health.Connected = true
				health.LastError = ""
				health.RestartCount++
			} else {
				health.LastError = reconnectErr.Error()
			}
		} else {
			health.Connected = true
			health.LastError = ""
		}

		healthMap[name] = health
	}
	return healthMap
}

// GetServerHealth returns health status for a specific server.
func (m *Manager) GetServerHealth(name string) (ServerHealth, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.servers[name]
	if !ok {
		return ServerHealth{}, false
	}
	return conn.Health, true
}

// InvalidateSchemaCache forces a schema refresh for the next tool call.
func (m *Manager) InvalidateSchemaCache(server string) {
	if server == "" {
		m.schemaCache.InvalidateAll()
	} else {
		m.schemaCache.Invalidate(server)
	}
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
