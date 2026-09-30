package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/mcp"
)

func init() {
	// MCP servers configured with a "vault:<id>" header get their key from the
	// sealed vault, at connect time, so the key is never kept in config.json.
	mcp.HeaderSecretResolver = func(id string) string {
		out := ""
		_ = connectionVault().Use(id, func(secret string) error { out = secret; return nil })
		return out
	}
}

var toolServerNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// toolServerURLOK allows https anywhere, and plain http only to a machine on
// the owner's own network (a server they run at home).
func toolServerURLOK(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", false
	}
	switch u.Scheme {
	case "https":
		return u.String(), true
	case "http":
		host := u.Hostname()
		if host == "localhost" || strings.HasSuffix(host, ".local") {
			return u.String(), true
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
			return u.String(), true
		}
	}
	return "", false
}

// registerToolServerRoutes lets an owner add outside tool servers (MCP) from
// the app or the console. Only web-addressed servers are accepted here: a
// server that runs a command on the Pod stays a terminal decision, because
// that is running code. The API key is sealed in the vault, the connection is
// tested before anything is saved, and no secret is ever returned.
func registerToolServerRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	mux.HandleFunc("/v1/tool-servers", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := config.LoadConfig(getConfigPath())
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "config", "couldn't read settings")
				return
			}
			var names []string
			for n := range cfg.Tools.MCP.Servers {
				names = append(names, n)
			}
			sort.Strings(names)
			out := []map[string]interface{}{}
			for _, n := range names {
				s := cfg.Tools.MCP.Servers[n]
				connected, count, lastErr := al.ToolServerStatus(n)
				kind, target := "command", ""
				if s.HTTP {
					kind, target = "web", s.HTTPURL
				}
				status := "not connected"
				if connected {
					status = "connected"
				}
				out = append(out, map[string]interface{}{
					"name": n, "kind": kind, "url": target, "enabled": s.Enabled,
					"status": status, "tools": count, "error": lastErr,
					"has_key": len(s.Headers) > 0,
				})
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "servers": out})

		case http.MethodPost:
			var req struct {
				Name   string `json:"name"`
				URL    string `json:"url"`
				APIKey string `json:"api_key"`
				Header string `json:"header"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid request")
				return
			}
			name := strings.ToLower(strings.TrimSpace(req.Name))
			if !toolServerNameRE.MatchString(name) {
				jsonError(w, http.StatusBadRequest, "invalid_request", "Give it a short name using letters, numbers, dashes or underscores, like \"notion\".")
				return
			}
			addr, ok := toolServerURLOK(req.URL)
			if !ok {
				jsonError(w, http.StatusBadRequest, "invalid_request", "That address should start with https:// (or http:// for a server on your own network).")
				return
			}
			server := config.MCPServerConfig{Enabled: true, HTTP: true, HTTPURL: addr}
			key := strings.TrimSpace(req.APIKey)
			vaultID := "mcp:" + name
			if key != "" {
				header := strings.TrimSpace(req.Header)
				if header == "" {
					header = "Authorization"
				}
				value := key
				if strings.EqualFold(header, "Authorization") && !strings.Contains(key, " ") {
					value = "Bearer " + key
				}
				if err := connectionVault().Store(vaultID, value); err != nil {
					jsonError(w, http.StatusInternalServerError, "vault", "couldn't store the key safely")
					return
				}
				server.Headers = map[string]string{header: mcp.VaultRef(vaultID)}
			}
			ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
			defer cancel()
			names, err := al.ConnectToolServer(ctx, name, server)
			if err != nil {
				if key != "" {
					_ = connectionVault().Disconnect(vaultID)
				}
				jsonError(w, http.StatusBadRequest, "connect_failed", "Couldn't connect to that server: "+scrubKey(err.Error(), key)+" Check the address and key.")
				return
			}
			cfg, lerr := config.LoadConfig(getConfigPath())
			if lerr == nil {
				cfg.Tools.MCP.Enabled = true
				if cfg.Tools.MCP.Servers == nil {
					cfg.Tools.MCP.Servers = map[string]config.MCPServerConfig{}
				}
				cfg.Tools.MCP.Servers[name] = server
				lerr = config.SaveConfig(getConfigPath(), cfg)
			}
			if lerr != nil {
				al.DisconnectToolServer(name)
				jsonError(w, http.StatusInternalServerError, "config", "connected, but couldn't save it; nothing was changed")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": name, "tools": len(names)})

		case http.MethodDelete:
			name := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("name")))
			cfg, err := config.LoadConfig(getConfigPath())
			if err != nil || name == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "name is required")
				return
			}
			if _, exists := cfg.Tools.MCP.Servers[name]; !exists {
				jsonError(w, http.StatusNotFound, "not_found", "no tool server with that name")
				return
			}
			al.DisconnectToolServer(name)
			delete(cfg.Tools.MCP.Servers, name)
			if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
				jsonError(w, http.StatusInternalServerError, "config", "couldn't save settings")
				return
			}
			_ = connectionVault().Disconnect("mcp:" + name)
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})

		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET, POST or DELETE")
		}
	}))
}

// scrubKey keeps a pasted key out of an error message.
func scrubKey(msg, key string) string {
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "«key»")
	}
	return msg
}
