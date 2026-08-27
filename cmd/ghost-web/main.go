package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/skills"
)

//go:embed all:web
var webFiles embed.FS

var (
	version = "dev"
	fb      *appliance.SetupState
	// waitingOnSystemd is true when running in -wait mode under the
	// ghost-web.service unit, whose ExecStartPost starts the ghost
	// service. When false, handleConfigure must start ghost itself.
	waitingOnSystemd bool
	// forceMode is true when the wizard was launched with -force, meaning
	// re-running setup is permitted after authentication.
	forceMode bool
	// boundPort is the port the wizard actually bound to (may differ from the
	// requested -port when it fell back, e.g. 80 -> 8080).
	boundPort int
// sessions tracks authenticated admin sessions for wizard re-runs.
sessions = newSessionStore()
// loginThrottle tracks consecutive failed login attempts per client IP.
loginThrottle = newLoginThrottle()
)

// sessionStore keeps issued admin session tokens with an expiry.
type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

const sessionTTL = 30 * time.Minute
const rememberMeTTL = 7 * 24 * time.Hour

// loginThrottle limits failed login attempts per client IP to slow brute-force.
type loginThrottler struct {
	mu           sync.Mutex
	failures     map[string]time.Time
	attemptCounts map[string]int
}

const (
	maxLoginAttempts    = 5
	loginCooldownPeriod = 10 * time.Minute
)

func newLoginThrottle() *loginThrottler {
	return &loginThrottler{
		failures:      make(map[string]time.Time),
		attemptCounts: make(map[string]int),
	}
}

// allowed reports whether ip may attempt a login and the remaining wait.
func (t *loginThrottler) allowed(ip string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.attemptCounts[ip] >= maxLoginAttempts {
		first, ok := t.failures[ip]
		if ok {
			wait := loginCooldownPeriod - time.Since(first)
			if wait > 0 {
				return false, wait
			}
		}
		// Cooldown expired: reset and allow.
		delete(t.failures, ip)
		delete(t.attemptCounts, ip)
	}
	return true, 0
}

func (t *loginThrottler) recordFailure(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.failures[ip]; !ok {
		t.failures[ip] = time.Now()
	}
	t.attemptCounts[ip]++
}

func (t *loginThrottler) recordSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, ip)
	delete(t.attemptCounts, ip)
}

func (t *loginThrottler) attempts(ip string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attemptCounts[ip]
}

// failedLogin tracks recent failed login attempts for dashboard visibility.
type failedLogin struct {
	IP        string    `json:"ip"`
	Timestamp time.Time `json:"time"`
}

var (
	recentFailedLoginsMu sync.Mutex
	recentFailedLogins   []failedLogin
	maxRecentLogins      = 20
)

func recordFailedLogin(ip string) {
	recentFailedLoginsMu.Lock()
	defer recentFailedLoginsMu.Unlock()
	recentFailedLogins = append(recentFailedLogins, failedLogin{IP: ip, Timestamp: time.Now().UTC()})
	if len(recentFailedLogins) > maxRecentLogins {
		recentFailedLogins = recentFailedLogins[len(recentFailedLogins)-maxRecentLogins:]
	}
}

func getRecentFailedLogins() []failedLogin {
	recentFailedLoginsMu.Lock()
	defer recentFailedLoginsMu.Unlock()
	out := make([]failedLogin, len(recentFailedLogins))
	copy(out, recentFailedLogins)
	return out
}

func clearRecentFailedLogins() {
	recentFailedLoginsMu.Lock()
	defer recentFailedLoginsMu.Unlock()
	recentFailedLogins = nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func newSessionStore() *sessionStore {
	return &sessionStore{tokens: make(map[string]time.Time)}
}

// issue creates a new random session token valid for sessionTTL.
func (s *sessionStore) issue() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Prune expired tokens opportunistically.
	for k, exp := range s.tokens {
		if time.Now().After(exp) {
			delete(s.tokens, k)
		}
	}
	s.tokens[token] = time.Now().Add(sessionTTL)
	return token, nil
}

// valid reports whether the given token is currently valid.
func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, token)
		return false
	}
	return true
}

// revokeAll invalidates every session (e.g. after setup completes).
func (s *sessionStore) revokeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = make(map[string]time.Time)
}

// sessionToken reads the admin session token from the request cookie.
func sessionToken(r *http.Request) string {
	c, err := r.Cookie("ghost_admin_session")
	if err != nil {
		return ""
	}
	return c.Value
}

func main() {
	port := flag.Int("port", 80, "HTTP port for setup wizard")
	ghostDir := flag.String("dir", "/var/ghost", "Ghost installation directory")
	waitMode := flag.Bool("wait", false, "Block until setup is complete (for oneshot systemd)")
	forceFlag := flag.Bool("force", false, "Re-run setup even if already complete")
	flag.Parse()

	forceMode = *forceFlag
	fb = appliance.NewSetupState()
	fb.GhostDir = *ghostDir
	fb.ConfigDir = filepath.Join(*ghostDir, "config")
	fb.DataDir = filepath.Join(*ghostDir, "data")
	fb.Workspace = appliance.ResolveWorkspaceDir(*ghostDir)
	fb.ConfigPath = filepath.Join(*ghostDir, "config", "config.json")
	fb.EnvPath = filepath.Join(*ghostDir, ".env")

	// Check if setup is needed
	if !*forceFlag && !fb.NeedsSetup() {
		flagPath := filepath.Join(fb.GhostDir, appliance.SetupCompleteFlag)
		log.Println("Setup already complete. The wizard only runs before setup.")
		log.Println("To re-open the wizard, run with -force, or remove " + flagPath + " and restart the ghost-web service.")
		os.Exit(0)
	}

	log.Printf("Ghost Web Console v%s", version)
	log.Printf("Starting setup wizard on port %d...", *port)

	// Ensure directories exist
	if err := fb.EnsureDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}

	// Reconcile bundled skills against the runtime workspace. On a fresh
	// checkout layout this seeds the wizard's skills tab; on every start it
	// refreshes unchanged bundled skills and always preserves user edits. On
	// installed layouts there is no bundled source here — the gateway seeds
	// from its embedded copy on first start instead.
	if src := bundledSkillsSourceDir(); src != "" {
		if report, err := skills.SyncBundled(src, filepath.Join(fb.Workspace, "skills")); err == nil {
			if len(report.Seeded) > 0 {
				log.Printf("Seeded bundled skills: %s", strings.Join(report.Seeded, ", "))
			}
			if len(report.Updated) > 0 {
				log.Printf("Updated bundled skills: %s", strings.Join(report.Updated, ", "))
			}
			if len(report.UserModified) > 0 {
				log.Printf("Preserved user-modified skills: %s", strings.Join(report.UserModified, ", "))
			}
		}
	}

	// Start the wizard server
	mux := http.NewServeMux()

	// Serve static assets (CSS, JS, fonts) from the embedded web directory.
	assetsFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("Failed to load embedded assets: %v", err)
	}
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))

	mux.HandleFunc("/", handleWizardIndex)
	mux.HandleFunc("/api/scan-wifi", handleScanWiFi)
	mux.HandleFunc("/api/connect-wifi", handleConnectWiFi)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/configure", handleConfigure)
	mux.HandleFunc("/api/pairing-code", handlePairingCode)
	mux.HandleFunc("/api/ollama/models", handleOllamaModels)
	mux.HandleFunc("/api/ollama/pull", handleOllamaPull)
	mux.HandleFunc("/api/ollama/delete", handleOllamaDelete)

	// Admin dashboard API (Phase 1-4)
	mux.HandleFunc("/api/admin/status", handleSystemStatus)
	mux.HandleFunc("/api/admin/doctor", handleDoctor)
	mux.HandleFunc("/api/admin/update", handleUpdateStart)
	mux.HandleFunc("/api/admin/update/status", handleUpdateStatus)
	mux.HandleFunc("/api/admin/config", handleConfigGet)
	mux.HandleFunc("/api/admin/config/save", handleConfigSet)
	mux.HandleFunc("/api/admin/channels", handleChannelsGet)
	mux.HandleFunc("/api/admin/channels/save", handleChannelsSet)
	mux.HandleFunc("/api/admin/network", handleNetworkStatus)
	mux.HandleFunc("/api/admin/hostname", handleSetHostname)
	mux.HandleFunc("/api/admin/backup", handleBackup)
	mux.HandleFunc("/api/admin/reboot", handleReboot)
	mux.HandleFunc("/api/admin/password", handleChangePassword)
	mux.HandleFunc("/api/admin/skills", handleSkillsList)
	mux.HandleFunc("/api/admin/skills/install", handleSkillInstall)
	mux.HandleFunc("/api/admin/skills/remove", handleSkillRemove)
	mux.HandleFunc("/api/admin/skills/toggle", handleSkillToggle)
	mux.HandleFunc("/api/admin/skills/sync", handleSkillsSync)
	mux.HandleFunc("/api/admin/skills/read", handleSkillRead)
	mux.HandleFunc("/api/admin/skills/save", handleSkillSave)
	mux.HandleFunc("/api/admin/skills/clawhub/search", handleClawHubSearch)
	mux.HandleFunc("/api/admin/skills/clawhub/install", handleClawHubInstall)
	mux.HandleFunc("/api/admin/tools", handleToolsGet)
	mux.HandleFunc("/api/admin/tools/save", handleToolsSet)
	mux.HandleFunc("/api/admin/gateway", handleGatewayGet)
	mux.HandleFunc("/api/admin/gateway/save", handleGatewaySet)
	mux.HandleFunc("/api/admin/advanced", handleAdvancedGet)
	mux.HandleFunc("/api/admin/advanced/save", handleAdvancedSet)
	mux.HandleFunc("/api/admin/personality", handlePersonalityGet)
	mux.HandleFunc("/api/admin/personality/save", handlePersonalitySet)
	mux.HandleFunc("/api/admin/personality/create", handlePersonalityCreate)
	mux.HandleFunc("/api/admin/personality/delete", handlePersonalityDelete)
	mux.HandleFunc("/api/admin/logs", handleLogs)
	mux.HandleFunc("/api/admin/toolsets", handleToolsetsGet)
	mux.HandleFunc("/api/admin/toolsets/save", handleToolsetsSet)
	mux.HandleFunc("/api/admin/auth/meta", handleAdminMeta)
	mux.HandleFunc("/api/admin/auth/check", handleAuthCheck)
	mux.HandleFunc("/api/admin/auth/failed-logins", handleFailedLogins)

	// Gateway API proxy — forwards requests to the Ghost gateway (port 8766)
	mux.HandleFunc("/api/proxy/", handleGatewayProxy)

	// Try ports in order: 80, 8080, 8888, 9090
	ports := []int{*port, 8080, 8888, 9090}
	if *port != 80 {
		ports = append([]int{*port}, ports...)
	}

	var listener net.Listener
	for _, p := range ports {
		addr := fmt.Sprintf("0.0.0.0:%d", p)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// Distinguish the real failure so users aren't misled.
			switch {
			case errors.Is(err, syscall.EADDRINUSE):
				log.Printf("Port %d is already in use by another process, trying next...", p)
			case errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM):
				if p < 1024 {
					log.Printf("Cannot bind port %d: permission denied. Ports below 1024 require root.", p)
					log.Printf("Run as root (sudo ghost-web -force), or use -port 8080 / another unprivileged port.")
				} else {
					log.Printf("Cannot bind port %d: permission denied (%v)", p, err)
				}
			default:
				log.Printf("Cannot bind port %d: %v", p, err)
			}
			continue
		}
		listener = ln
		boundPort = p
		// Open the firewall for the port we actually bound (may differ from
		// -port when port 80 fell back). Best-effort.
		openFirewallPort(boundPort)
		log.Printf("Setup wizard running at http://ghost.local:%d", boundPort)
		log.Printf("Also available at http://<your-pi-ip>:%d", boundPort)
		break
	}

	if listener == nil {
		log.Fatalf("All ports failed to bind: %v", ports)
	}

	// Remember how we're running so handleConfigure knows whether systemd's
	// ExecStartPost will start the ghost service (wait mode) or whether we
	// must do it ourselves (manual -force runs).
	waitingOnSystemd = *waitMode

	if *waitMode {
		// In wait mode, run the server in a goroutine and block until setup completes.
		// This is used by the oneshot systemd service to ensure setup finishes before
		// the main Ghost service starts.
		go func() {
			if err := http.Serve(listener, mux); err != nil {
				log.Printf("Wizard server error: %v", err)
			}
		}()

		log.Println("Waiting for setup to complete...")
		waitForSetupComplete()
		log.Println("Setup complete, exiting.")
	} else {
		if err := http.Serve(listener, mux); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}
}

// waitForSetupComplete polls for the .setup-complete flag file.
func waitForSetupComplete() {
	flagPath := filepath.Join(fb.GhostDir, appliance.SetupCompleteFlag)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := os.Stat(flagPath); err == nil {
			return
		}
	}
}

func handleWizardIndex(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Serve index.html for SPA routes (non-API, non-asset paths)
	if path == "/" || (!strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/assets/")) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := webFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "Failed to load wizard", http.StatusInternalServerError)
			return
		}
		w.Write(data)
		return
	}
	http.NotFound(w, r)
}

// handleGatewayProxy forwards requests to the Ghost gateway API on localhost:8766.
// The gateway binds to localhost only, so no authentication is needed for proxied requests.
func handleGatewayProxy(w http.ResponseWriter, r *http.Request) {
	// Load config to get gateway port
	cfgPath := filepath.Join(fb.GhostDir, "config", "config.json")
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		http.Error(w, `{"error":"failed to load config"}`, http.StatusInternalServerError)
		return
	}

	gatewayPort := cfg.Gateway.Port
	if gatewayPort == 0 {
		gatewayPort = 8766
	}

	// Strip /api/proxy prefix to get the gateway path
	gatewayPath := strings.TrimPrefix(r.URL.Path, "/api/proxy")
	if gatewayPath == "" {
		gatewayPath = "/"
	}
	if r.URL.RawQuery != "" {
		gatewayPath += "?" + r.URL.RawQuery
	}

	gatewayURL := fmt.Sprintf("http://127.0.0.1:%d%s", gatewayPort, gatewayPath)

	// Create the proxied request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, gatewayURL, r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to create proxy request"}`, http.StatusInternalServerError)
		return
	}

	// Copy headers from original request
	for key, values := range r.Header {
		for _, v := range values {
			proxyReq.Header.Add(key, v)
		}
	}

	// Execute the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, `{"error":"gateway unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream response body (supports SSE)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
}

func handleScanWiFi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Scan for WiFi networks
	networks, err := scanWiFiNetworks()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"error":   err.Error(),
			"networks": []WiFiNetwork{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"networks": networks,
	})
}

func handleConnectWiFi(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := connectToWiFi(req.SSID, req.Password); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"needs_setup":       fb.NeedsSetup(),
		"admin_configured":  appliance.AdminConfigured(fb.GhostDir),
		"force":             forceMode,
		"version":           version,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	ip := clientIP(r)
	if ok, wait := loginThrottle.allowed(ip); !ok {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       false,
			"error":    fmt.Sprintf("too many failed attempts, try again in %d seconds", int(wait.Seconds())+1),
			"retry_in": int(wait.Seconds()) + 1,
		})
		return
	}

	ok, err := appliance.VerifyAdminPassword(fb.GhostDir, req.Password)
	if err != nil {
		http.Error(w, `{"ok":false,"error":"failed to verify password"}`, http.StatusInternalServerError)
		return
	}
	if !ok {
		loginThrottle.recordFailure(ip)
		recordFailedLogin(ip)
		log.Printf("Failed login attempt from %s", ip)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "invalid password",
		})
		return
	}

	loginThrottle.recordSuccess(ip)
	clearRecentFailedLogins()
	token, err := sessions.issue()
	if err != nil {
		http.Error(w, `{"ok":false,"error":"failed to create session"}`, http.StatusInternalServerError)
		return
	}

	maxAge := int(sessionTTL.Seconds())
	if req.RememberMe {
		maxAge = int(rememberMeTTL.Seconds())
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "ghost_admin_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "Logged in",
	})
}

func handleConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AdminPassword   string `json:"admin_password"`
		CurrentPassword string `json:"current_password"`
		Model           string `json:"model"`
		Provider        string `json:"provider"`
		OllamaURL       string `json:"ollama_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// If an admin password already exists, re-running setup requires an
	// authenticated session AND the current password. Fresh setup runs
	// (or a migration with no password yet) only need the new password.
	if appliance.AdminConfigured(fb.GhostDir) {
		if !sessions.valid(sessionToken(r)) {
			http.Error(w, `{"ok":false,"error":"session expired, please log in"}`, http.StatusUnauthorized)
			return
		}

		ok, err := appliance.VerifyAdminPassword(fb.GhostDir, req.CurrentPassword)
		if err != nil {
			http.Error(w, `{"ok":false,"error":"failed to verify current password"}`, http.StatusInternalServerError)
			return
		}
		if !ok {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "current password is incorrect",
			})
			return
		}

		// Optional: set a new password during a re-run.
		if req.AdminPassword != "" {
			if err := appliance.SetAdminPassword(fb.GhostDir, req.AdminPassword); err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"ok":    false,
					"error": "failed to update admin password: " + err.Error(),
				})
				return
			}
		}
	} else {
		if req.AdminPassword == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "admin password is required",
			})
			return
		}
		if err := appliance.SetAdminPassword(fb.GhostDir, req.AdminPassword); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "failed to save admin password: " + err.Error(),
			})
			return
		}
	}

	// Generate config
	if err := generateConfig(req.Model, req.Provider, req.OllamaURL); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	// Mark setup complete
	if err := fb.MarkSetupComplete(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "failed to mark setup complete: " + err.Error(),
		})
		return
	}

	// Invalidate all admin sessions: the configuration has changed.
	sessions.revokeAll()

	// Clean up the wizard firewall rule only in wait mode, where the oneshot
	// service exits after setup. In -force mode the wizard stays running as
	// an always-available login/management screen, so port 80 must stay open.
	if waitingOnSystemd {
		cleanupFirewall()
	}

	// Under systemd (wait mode), the ghost-web service's ExecStartPost
	// is responsible for starting ghost after setup. When running in
	// foreground / -force mode there is no ExecStartPost, so start it here.
	if !waitingOnSystemd {
		restartGhostService()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "Setup complete! Ghost will start shortly.",
	})
}

func handlePairingCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	code, err := appliance.GeneratePairingCode()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"code": code,
	})
}

func handleOllamaModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	models, err := listOllamaModels()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     false,
			"error":  err.Error(),
			"models": []string{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"models": models,
	})
}

func handleOllamaPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Pull model in background
	go pullOllamaModel(req.Model)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "Download started",
	})
}

// generateConfig creates the .env and config.json files. The admin password
// is never written here; it lives only as a bcrypt hash in data/admin.hash.
func generateConfig(model, provider, ollamaURL string) error {
	// Generate .env
	envContent := fmt.Sprintf(`# Ghost Appliance Configuration
# Generated by setup wizard

# API Server
GHOST_API_PORT=8766

# Data Paths
GHOST_DB_PATH=%s/ghost.db
MEMORY_DIR=%s/memory

# Timezone
TZ=UTC
`, fb.Workspace, fb.Workspace)

	if err := os.WriteFile(fb.EnvPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Load default config and customize
	cfg := config.DefaultConfig()

	// Set model configuration
	if model != "" {
		cfg.Agents.Defaults.Model = model
	} else if strings.Contains(provider, "ollama") {
		cfg.Agents.Defaults.Model = "qwen3:0.6b"
	}
	if provider != "" {
		cfg.Agents.Defaults.Provider = provider
	}

	// Set workspace
	cfg.Agents.Defaults.Workspace = fb.Workspace

	// Configure Ollama
	if ollamaURL != "" {
		cfg.Providers.Ollama.APIBase = ollamaURL
	} else {
		cfg.Providers.Ollama.APIBase = "http://localhost:11434"
	}

	cfg.Gateway.Port = 8766

	// Enable heartbeat
	cfg.Heartbeat.Enabled = true
	cfg.Heartbeat.Interval = 30

	// Enable RAG
	cfg.RAG.Enabled = true

	// Save config. SaveConfig splits secrets into
	// .secrets.json at 0600 and writes config.json atomically at 0600.
	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		return fmt.Errorf("failed to write config.json: %w", err)
	}

	return nil
}

// WiFiNetwork represents a scanned WiFi network.
type WiFiNetwork struct {
	SSID       string `json:"ssid"`
	Signal     int    `json:"signal"`
	Encrypted  bool   `json:"encrypted"`
}

// scanWiFiNetworks scans for available WiFi networks.
func scanWiFiNetworks() ([]WiFiNetwork, error) {
	// Try iwlist scan
	out, err := exec.Command("iwlist", "wlan0", "scan").CombinedOutput()
	if err != nil {
		// Try nmcli as fallback
		return scanWiFiNmcli()
	}

	return parseIwlistOutput(string(out)), nil
}

func scanWiFiNmcli() ([]WiFiNetwork, error) {
	out, err := exec.Command("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY", "dev", "wifi", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to scan WiFi: %w", err)
	}

	var networks []WiFiNetwork
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		ssid := parts[0]
		if ssid == "" {
			continue
		}
		signal := 0
		fmt.Sscanf(parts[1], "%d", &signal)
		encrypted := len(parts) > 2 && parts[2] != ""
		networks = append(networks, WiFiNetwork{
			SSID:      ssid,
			Signal:    signal,
			Encrypted: encrypted,
		})
	}
	return networks, nil
}

func parseIwlistOutput(output string) []WiFiNetwork {
	var networks []WiFiNetwork
	seen := make(map[string]bool)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "ESSID:") {
			ssid := strings.TrimPrefix(line, "ESSID:\"")
			ssid = strings.TrimSuffix(ssid, "\"")
			if ssid != "" && !seen[ssid] {
				seen[ssid] = true
				networks = append(networks, WiFiNetwork{
					SSID:      ssid,
					Signal:    70, // Default signal strength
					Encrypted: true,
				})
			}
		}
	}
	return networks
}

// connectToWiFi connects to a WiFi network.
func connectToWiFi(ssid, password string) error {
	if password == "" {
		// Open network
		out, err := exec.Command("nmcli", "dev", "wifi", "connect", ssid).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to connect: %s", string(out))
		}
	} else {
		// Secured network - use connection clone with proper security
		// First try to connect with password
		out, err := exec.Command("nmcli", "dev", "wifi", "connect", ssid, "password", password, "key", "wpa-psk").CombinedOutput()
		if err != nil {
			// If that fails, try with wifi-sec key-mgmt
			out, err = exec.Command("nmcli", "connection", "add", "type", "wifi", "ssid", ssid, "wifi-sec.key-mgmt", "wpa-psk", "wifi-sec.psk", password, "connection.autoconnect", "yes").CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to connect: %s", string(out))
			}
			// Try to activate the connection
			out, err = exec.Command("nmcli", "connection", "up", ssid).CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to activate connection: %s", string(out))
			}
		}
	}
	return nil
}

// listOllamaModels lists locally installed Ollama models.
func listOllamaModels() ([]string, error) {
	out, err := exec.Command("ollama", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	var models []string
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			models = append(models, parts[0])
		}
	}
	return models, nil
}

// pullOllamaModel downloads an Ollama model.
func pullOllamaModel(model string) error {
	cmd := exec.Command("ollama", "pull", model)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runPrivileged runs a command, prefixing with sudo when we're not root so
// manual (non-systemd) runs of ghost-web can still manage ufw/systemd.
func runPrivileged(name string, args ...string) ([]byte, error) {
	if os.Geteuid() != 0 {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	return exec.Command(name, args...).CombinedOutput()
}

// cleanupFirewall removes the wizard port rule (whatever port was actually
// bound) after setup completes.
func cleanupFirewall() {
	if boundPort > 0 {
		runPrivileged("ufw", "delete", "allow", fmt.Sprintf("%d/tcp", boundPort))
	}
	// Also remove the common default in case setup was completed by an old
	// binary that only managed port 80.
	runPrivileged("ufw", "delete", "allow", "80/tcp")
}

// openFirewallPort opens the given TCP port on the firewall so the wizard is
// reachable from other devices. Best-effort: failures are logged and ignored
// (ufw may not be installed or active on the device).
func openFirewallPort(port int) {
	if out, err := runPrivileged("ufw", "allow", fmt.Sprintf("%d/tcp", port)); err != nil {
		log.Printf("Failed to open firewall port %d (ufw may not be active): %v: %s", port, err, strings.TrimSpace(string(out)))
	} else {
		log.Printf("Opened firewall port %d", port)
	}
}

// restartGhostService restarts the ghost service after setup.
func restartGhostService() {
	runPrivileged("systemctl", "daemon-reload")
	runPrivileged("systemctl", "restart", "ghost")
}
