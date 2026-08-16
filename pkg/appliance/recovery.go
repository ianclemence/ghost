package appliance

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
)

// RecoveryServer provides a minimal web UI for recovery mode.
// It runs when Ghost fails to start properly, allowing users to
// view logs, reset config, or retry starting Ghost.
type RecoveryServer struct {
	Port        int
	GhostDir    string
	ConfigPath  string
	LogsCommand string
}

// NewRecoveryServer creates a RecoveryServer with default settings.
func NewRecoveryServer() *RecoveryServer {
	port := 8766
	ghostDir := os.Getenv("GHOST_DIR")
	if ghostDir == "" {
		ghostDir = DefaultGhostDir
	}

	return &RecoveryServer{
		Port:        port,
		GhostDir:    ghostDir,
		ConfigPath:  filepath.Join(ghostDir, "config", "config.json"),
		LogsCommand: "journalctl -u ghost --no-pager -n 100",
	}
}

// RecoveryStatus holds the current system status for the recovery UI.
type RecoveryStatus struct {
	Version     string `json:"version"`
	Uptime      string `json:"uptime"`
	ErrorCount  int    `json:"error_count"`
	LastError   string `json:"last_error"`
	ConfigExists bool  `json:"config_exists"`
	GhostRunning bool  `json:"ghost_running"`
}

// Start begins listening for recovery requests.
func (rs *RecoveryServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", rs.handleIndex)
	mux.HandleFunc("/api/status", rs.handleStatus)
	mux.HandleFunc("/api/logs", rs.handleLogs)
	mux.HandleFunc("/api/config", rs.handleConfig)
	mux.HandleFunc("/api/reset", rs.handleReset)
	mux.HandleFunc("/api/restart", rs.handleRestart)

	addr := fmt.Sprintf("0.0.0.0:%d", rs.Port)
	log.Printf("Recovery mode active at http://ghost.local:%d", rs.Port)
	return http.ListenAndServe(addr, mux)
}

func (rs *RecoveryServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, recoveryHTML)
}

func (rs *RecoveryServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := RecoveryStatus{
		Version:      "unknown",
		ConfigExists: fileExists(rs.ConfigPath),
		GhostRunning: isGhostRunning(),
	}

	// Try to get version from binary
	if out, err := exec.Command("ghost", "version").Output(); err == nil {
		status.Version = string(out)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (rs *RecoveryServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if runtime.GOOS == "linux" {
		out, err := exec.Command("journalctl", "-u", "ghost", "--no-pager", "-n", "200").CombinedOutput()
		if err != nil {
			fmt.Fprintf(w, "Error fetching logs: %v\n", err)
			return
		}
		fmt.Fprint(w, string(out))
		return
	}

	// Fallback: read log file if it exists
	logPath := filepath.Join(rs.GhostDir, "ghost.log")
	if data, err := os.ReadFile(logPath); err == nil {
		// Show last 200 lines
		lines := splitLines(string(data))
		start := 0
		if len(lines) > 200 {
			start = len(lines) - 200
		}
		for _, line := range lines[start:] {
			fmt.Fprintln(w, line)
		}
		return
	}

	fmt.Fprint(w, "No logs available. Start Ghost to generate logs.")
}

func (rs *RecoveryServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		data, err := os.ReadFile(rs.ConfigPath)
		if err != nil {
			http.Error(w, `{"error":"config not found"}`, http.StatusNotFound)
			return
		}
		w.Write(data)
		return
	}

	if r.Method == http.MethodPut {
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(rs.ConfigPath, data, 0644); err != nil {
			http.Error(w, `{"error":"failed to write config"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (rs *RecoveryServer) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fb := NewFirstBoot()
	fb.GhostDir = rs.GhostDir
	if err := fb.ResetSetup(); err != nil {
		http.Error(w, `{"error":"failed to reset"}`, http.StatusInternalServerError)
		return
	}

	// Remove config
	os.Remove(rs.ConfigPath)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true,"message":"Setup reset. Restart to begin setup wizard."}`)
}

func (rs *RecoveryServer) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if runtime.GOOS == "linux" {
		go func() {
			time.Sleep(1 * time.Second)
			exec.Command("systemctl", "restart", "ghost").Run()
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true,"message":"Restarting..."}`)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isGhostRunning() bool {
	if runtime.GOOS == "linux" {
		out, err := exec.Command("systemctl", "is-active", "ghost").Output()
		return err == nil && string(out) == "active\n"
	}
	return false
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

const recoveryHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ghost Recovery Mode</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, monospace;
            background: #09090b;
            color: #e4e4e7;
            min-height: 100vh;
            padding: 20px;
        }
        .container { max-width: 800px; margin: 0 auto; }
        h1 {
            font-size: 24px;
            margin-bottom: 8px;
            color: #ef4444;
        }
        .subtitle {
            color: #71717a;
            margin-bottom: 24px;
            font-size: 14px;
        }
        .card {
            background: #18181b;
            border: 1px solid #27272a;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 16px;
        }
        .card h2 {
            font-size: 16px;
            margin-bottom: 12px;
            color: #a1a1aa;
        }
        .status-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 12px;
        }
        .status-item {
            background: #09090b;
            padding: 12px;
            border-radius: 6px;
        }
        .status-item .label {
            font-size: 12px;
            color: #71717a;
            text-transform: uppercase;
        }
        .status-item .value {
            font-size: 18px;
            font-weight: bold;
            margin-top: 4px;
        }
        .status-item .value.ok { color: #4ade80; }
        .status-item .value.error { color: #ef4444; }
        .status-item .value.warning { color: #fbbf24; }
        .btn {
            background: #27272a;
            color: #e4e4e7;
            border: 1px solid #3f3f46;
            padding: 10px 20px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 14px;
            margin-right: 8px;
            margin-bottom: 8px;
        }
        .btn:hover { background: #3f3f46; }
        .btn.danger { border-color: #ef4444; color: #ef4444; }
        .btn.danger:hover { background: #ef4444; color: white; }
        .btn.primary { border-color: #4ade80; color: #4ade80; }
        .btn.primary:hover { background: #4ade80; color: #09090b; }
        pre {
            background: #09090b;
            padding: 12px;
            border-radius: 6px;
            overflow-x: auto;
            font-size: 12px;
            line-height: 1.5;
            max-height: 400px;
            overflow-y: auto;
            font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
        }
        .loading { color: #71717a; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Ghost Recovery Mode</h1>
        <p class="subtitle">Something went wrong. Use this panel to diagnose and fix the issue.</p>

        <div class="card">
            <h2>System Status</h2>
            <div class="status-grid" id="status">
                <div class="status-item">
                    <div class="label">Status</div>
                    <div class="value loading">Loading...</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Actions</h2>
            <button class="btn primary" onclick="restartGhost()">Restart Ghost</button>
            <button class="btn" onclick="loadLogs()">Refresh Logs</button>
            <button class="btn danger" onclick="resetSetup()">Reset Setup</button>
        </div>

        <div class="card">
            <h2>Logs</h2>
            <pre id="logs">Loading logs...</pre>
        </div>
    </div>

    <script>
        async function loadStatus() {
            try {
                const res = await fetch('/api/status');
                const data = await res.json();
                document.getElementById('status').innerHTML = `
                    <div class="status-item">
                        <div class="label">Ghost Running</div>
                        <div class="value ${data.ghost_running ? 'ok' : 'error'}">${data.ghost_running ? 'Yes' : 'No'}</div>
                    </div>
                    <div class="status-item">
                        <div class="label">Config Exists</div>
                        <div class="value ${data.config_exists ? 'ok' : 'warning'}">${data.config_exists ? 'Yes' : 'No'}</div>
                    </div>
                    <div class="status-item">
                        <div class="label">Version</div>
                        <div class="value">${data.version || 'unknown'}</div>
                    </div>
                `;
            } catch (e) {
                console.error('Failed to load status:', e);
            }
        }

        async function loadLogs() {
            try {
                const res = await fetch('/api/logs');
                const text = await res.text();
                document.getElementById('logs').textContent = text || 'No logs available.';
            } catch (e) {
                document.getElementById('logs').textContent = 'Failed to load logs.';
            }
        }

        async function restartGhost() {
            if (!confirm('Restart Ghost service?')) return;
            try {
                await fetch('/api/restart', { method: 'POST' });
                alert('Restarting... The page will reload in 5 seconds.');
                setTimeout(() => location.reload(), 5000);
            } catch (e) {
                alert('Failed to restart.');
            }
        }

        async function resetSetup() {
            if (!confirm('This will reset Ghost to factory defaults. All configuration will be lost. Continue?')) return;
            if (!confirm('Are you absolutely sure? This cannot be undone.')) return;
            try {
                const res = await fetch('/api/reset', { method: 'POST' });
                const data = await res.json();
                alert(data.message || 'Setup has been reset. Restart Ghost to begin the setup wizard.');
            } catch (e) {
                alert('Failed to reset.');
            }
        }

        loadStatus();
        loadLogs();
        setInterval(loadStatus, 10000);
    </script>
</body>
</html>`
