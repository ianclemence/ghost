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
	Timeout     time.Duration
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
		Timeout:     15 * time.Minute,
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

// Start begins listening for recovery requests. If Timeout > 0, the server
// automatically shuts down after that duration.
func (rs *RecoveryServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", rs.handleIndex)
	mux.HandleFunc("/api/status", rs.handleStatus)
	mux.HandleFunc("/api/logs", rs.handleLogs)
	mux.HandleFunc("/api/config", rs.handleConfig)
	mux.HandleFunc("/api/reset", rs.handleReset)
	mux.HandleFunc("/api/reset-password", rs.handleResetPassword)
	mux.HandleFunc("/api/restart", rs.handleRestart)

	addr := fmt.Sprintf("127.0.0.1:%d", rs.Port)
	log.Printf("Recovery mode active at http://127.0.0.1:%d (localhost only)", rs.Port)

	if rs.Timeout > 0 {
		log.Printf("Recovery server will auto-shutdown in %s", rs.Timeout)
		go func() {
			time.Sleep(rs.Timeout)
			log.Printf("Recovery server timeout reached, shutting down")
			os.Exit(0)
		}()
	}

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
		data, _ := json.MarshalIndent(&cfg, "", "  ")
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

	fb := NewSetupState()
	fb.GhostDir = rs.GhostDir
	if err := fb.ResetSetup(); err != nil {
		http.Error(w, `{"error":"failed to reset"}`, http.StatusInternalServerError)
		return
	}

	// Remove config and the admin credential so the setup wizard re-runs
	// from scratch (no stale password demanded on next boot).
	os.Remove(rs.ConfigPath)
	if err := RemoveAdminPassword(rs.GhostDir); err != nil {
		http.Error(w, `{"error":"failed to clear admin credential"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true,"message":"Setup reset. Restart to begin setup wizard."}`)
}

// handleResetPassword sets a fresh admin password without requiring the
// current one. Used from recovery mode when the password is forgotten.
func (rs *RecoveryServer) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	if req.Password != req.Confirm {
		http.Error(w, `{"ok":false,"error":"passwords do not match"}`, http.StatusBadRequest)
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		http.Error(w, `{"ok":false,"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	if err := SetAdminPassword(rs.GhostDir, req.Password); err != nil {
		http.Error(w, `{"ok":false,"error":"failed to set password"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Admin password reset from recovery mode")
	go func() {
		time.Sleep(1 * time.Second)
		exec.Command("systemctl", "restart", "ghost-web").Run()
	}()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true,"message":"Password reset. Log in with the new password."}`)
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
    <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
    <meta name="theme-color" content="#17130f">
    <title>Ghost Recovery Mode</title>
    <style>
        :root {
            --bg: #17130f;
            --surface: #241e17;
            --surface-2: #2b241b;
            --ink: #f1e9dc;
            --muted: #a3927f;
            --ember: #ffb45c;
            --sage: #86b28f;
            --clay: #e08667;
            --danger: #ff7b6b;
            --line: rgba(241, 233, 220, 0.08);
            --ring: rgba(255, 180, 92, 0.35);
            --ease-out: cubic-bezier(0.23, 1, 0.32, 1);
            --ease-in-out: cubic-bezier(0.77, 0, 0.175, 1);
            --s-1: 4px; --s-2: 8px; --s-3: 12px; --s-4: 16px; --s-5: 24px; --s-6: 32px; --s-7: 48px;
            --r-card: 16px; --r-field: 10px;
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            background:
                radial-gradient(80% 50% at 50% 0%, rgba(255, 180, 92, 0.06), transparent 70%),
                var(--bg);
            color: var(--ink);
            min-height: 100vh;
            padding: var(--s-6) var(--s-4) calc(var(--s-7) + env(safe-area-inset-bottom));
        }
        .container { max-width: 720px; margin: 0 auto; }

        .brand {
            display: flex; align-items: center; gap: var(--s-3);
            font-weight: 650; letter-spacing: -0.01em; font-size: 18px;
            margin-bottom: var(--s-2);
        }
        .brand .badge {
            font-size: 12px; font-weight: 600; text-transform: uppercase;
            letter-spacing: 0.06em; color: var(--ember);
            border: 1px solid rgba(255, 180, 92, 0.4);
            border-radius: 999px; padding: 2px 10px;
        }
        .subtitle { color: var(--muted); font-size: 15px; margin-bottom: var(--s-5); }

        .ember {
            position: relative; width: 14px; height: 14px; border-radius: 50%;
            background: var(--ember);
            box-shadow: 0 0 10px rgba(255, 180, 92, 0.65), 0 0 26px rgba(255, 180, 92, 0.3);
            animation: breathe 4.2s var(--ease-in-out) infinite;
            flex-shrink: 0;
        }
        .ember.ember--off { animation: none; background: var(--danger); box-shadow: 0 0 10px rgba(255, 123, 107, 0.5); }
        @keyframes breathe {
            0%, 100% { transform: scale(1); opacity: 1; }
            50% { transform: scale(1.15); opacity: 0.8; }
        }

        .card {
            background: var(--surface);
            border: 1px solid var(--line);
            border-radius: var(--r-card);
            padding: var(--s-5);
            margin-bottom: var(--s-4);
        }
        .card h2 {
            font-size: 15px; font-weight: 600; letter-spacing: -0.01em;
            margin-bottom: var(--s-4); display: flex; align-items: center; gap: var(--s-2);
        }
        .status-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
            gap: var(--s-3);
        }
        .status-item {
            background: var(--surface-2);
            border: 1px solid var(--line);
            border-radius: var(--r-field);
            padding: var(--s-3);
        }
        .status-item .label {
            font-size: 11px; color: var(--muted); text-transform: uppercase;
            letter-spacing: 0.06em;
        }
        .status-item .value {
            font-size: 18px; font-weight: 650; margin-top: 2px;
        }
        .status-item .value.ok { color: var(--sage); }
        .status-item .value.error { color: var(--danger); }
        .status-item .value.warning { color: var(--ember); }

        .actions { display: flex; flex-wrap: wrap; gap: var(--s-3); }
        .btn {
            font: inherit; font-size: 15px; font-weight: 600;
            color: var(--ink); background: var(--surface-2);
            border: 1px solid var(--line);
            border-radius: var(--r-field);
            padding: 11px 18px;
            cursor: pointer;
            transition: transform 120ms var(--ease-out), background-color 150ms var(--ease-out), border-color 150ms var(--ease-out), color 150ms var(--ease-out);
        }
        .btn:active { transform: scale(0.97); }
        .btn.primary {
            background: var(--ember); border-color: transparent; color: #1d1510;
        }
        .btn.primary:hover { background: #ffc47e; }
        .btn.danger {
            background: transparent; border-color: rgba(255, 123, 107, 0.5); color: var(--danger);
        }
        .btn.danger:hover { background: rgba(255, 123, 107, 0.12); border-color: var(--danger); }
        .btn:hover:not(.primary):not(.danger) { background: #342c22; border-color: rgba(241, 233, 220, 0.16); }
        .btn:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
        .btn:disabled { opacity: 0.5; cursor: default; }

        .log-box {
            background: #12100d;
            border: 1px solid var(--line);
            border-radius: var(--r-field);
            padding: var(--s-3) var(--s-4);
            font-family: ui-monospace, "SF Mono", "Cascadia Code", "Menlo", "Courier New", monospace;
            font-size: 12px; line-height: 1.55;
            max-height: 420px; overflow-y: auto;
            color: #cdbfa8;
            white-space: pre-wrap; word-break: break-word;
        }

        .field-input {
            font: inherit; font-size: 15px;
            color: var(--ink);
            background: var(--surface-2);
            border: 1px solid var(--line);
            border-radius: var(--r-field);
            padding: 11px 14px;
            width: 100%;
            box-sizing: border-box;
            outline: none;
            transition: border-color 150ms var(--ease-out), box-shadow 150ms var(--ease-out);
        }
        .field-input:focus { border-color: var(--ring); box-shadow: 0 0 0 3px rgba(255, 180, 92, 0.12); }
        .field-input::placeholder { color: #6f6253; }

        .toast {
            position: fixed; left: 50%; bottom: 24px;
            transform: translateX(-50%) translateY(16px);
            background: var(--surface-2); color: var(--ink);
            border: 1px solid var(--line);
            border-radius: 999px;
            padding: 12px 20px;
            font-size: 14px;
            opacity: 0;
            transition: transform 220ms var(--ease-out), opacity 220ms var(--ease-out);
            pointer-events: none;
            z-index: 50;
            max-width: min(92vw, 520px);
            text-align: center;
        }
        .toast.show { transform: translateX(-50%) translateY(0); opacity: 1; }

        .modal-backdrop {
            position: fixed; inset: 0; z-index: 40;
            background: rgba(10, 8, 6, 0.6);
            display: flex; align-items: center; justify-content: center;
            padding: var(--s-4);
            animation: fade-in 160ms var(--ease-out);
        }
        .modal {
            background: var(--surface-2);
            border: 1px solid var(--line);
            border-radius: var(--r-card);
            padding: var(--s-5);
            width: 100%; max-width: 380px;
            box-shadow: 0 20px 60px rgba(0, 0, 0, 0.45);
            animation: modal-in 220ms var(--ease-out);
        }
        .modal h3 { font-size: 17px; letter-spacing: -0.01em; margin-bottom: var(--s-2); }
        .modal p { color: var(--muted); font-size: 14px; line-height: 1.5; margin-bottom: var(--s-5); }
        .modal .actions { display: flex; gap: var(--s-3); justify-content: flex-end; }
        @keyframes fade-in { from { opacity: 0; } }
        @keyframes modal-in {
            from { opacity: 0; transform: translateY(10px) scale(0.98); }
        }
        @media (prefers-reduced-motion: reduce) {
            .ember, .toast, .modal, .modal-backdrop, .btn { animation: none; transition: none; }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="brand"><span class="ember" id="orb" aria-hidden="true"></span>Ghost<span class="badge">Recovery</span></div>
        <p class="subtitle">Something went wrong. Use this panel to look, then fix.</p>

        <div class="card">
            <h2>System status</h2>
            <div class="status-grid" id="status">
                <div class="status-item"><div class="label">Checking</div><div class="value">&hellip;</div></div>
            </div>
        </div>

        <div class="card">
            <h2>Actions</h2>
            <div class="actions">
                <button class="btn primary" id="btn-restart" type="button">Restart Ghost</button>
                <button class="btn" id="btn-refresh" type="button">Refresh logs</button>
                <button class="btn danger" id="btn-reset" type="button">Reset setup</button>
            </div>
        </div>

        <div class="card">
            <h2>Reset admin password</h2>
            <p class="subtitle" style="font-size:14px;margin-bottom:var(--s-4);">Forgot your admin password? Set a new one. No current password needed.</p>
            <input class="field-input" id="rp-password" type="password" placeholder="New admin password (at least 12 characters)" autocomplete="off">
            <input class="field-input" id="rp-confirm" type="password" placeholder="Confirm new password" autocomplete="off" style="margin-top:var(--s-3);">
            <div class="actions" style="margin-top:var(--s-4);">
                <button class="btn" id="btn-reset-password" type="button">Set new password</button>
            </div>
        </div>

        <div class="card">
            <h2>Logs</h2>
            <div class="log-box" id="logs">Loading logs&hellip;</div>
        </div>
    </div>

    <script>
        var toastTimer = null;
        function toast(message, ok) {
            var el = document.getElementById('toast');
            if (!el) {
                el = document.createElement('div');
                el.className = 'toast';
                el.id = 'toast';
                document.body.appendChild(el);
            }
            el.textContent = message;
            requestAnimationFrame(function () { el.classList.add('show'); });
            if (toastTimer) clearTimeout(toastTimer);
            toastTimer = setTimeout(function () { el.classList.remove('show'); }, ok ? 2600 : 5000);
        }

        function confirmModal(title, body, okLabel) {
            return new Promise(function (resolve) {
                var backdrop = document.createElement('div');
                backdrop.className = 'modal-backdrop';
                backdrop.innerHTML = '<div class="modal" role="alertdialog" aria-modal="true">' +
                    '<h3>' + title + '</h3><p>' + body + '</p>' +
                    '<div class="actions">' +
                    '<button class="btn" data-cancel type="button">Cancel</button>' +
                    '<button class="btn danger" data-ok type="button">' + okLabel + '</button></div></div>';
                var close = function (val) {
                    document.removeEventListener('keydown', onKey);
                    backdrop.remove();
                    resolve(val);
                };
                var onKey = function (e) { if (e.key === 'Escape') close(false); };
                backdrop.addEventListener('click', function (e) { if (e.target === backdrop) close(false); });
                backdrop.querySelector('[data-cancel]').addEventListener('click', function () { close(false); });
                backdrop.querySelector('[data-ok]').addEventListener('click', function () { close(true); });
                document.addEventListener('keydown', onKey);
                document.body.appendChild(backdrop);
                backdrop.querySelector('[data-ok]').focus();
            });
        }

        function esc(s) {
            return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
                return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
            });
        }

        async function loadStatus() {
            try {
                var res = await fetch('/api/status');
                var data = await res.json();
                var items = '';
                items += statusItem('Ghost running', data.ghost_running ? 'Yes' : 'No', data.ghost_running ? 'ok' : 'error');
                items += statusItem('Config exists', data.config_exists ? 'Yes' : 'No', data.config_exists ? 'ok' : 'warning');
                items += statusItem('Version', data.version || 'unknown', '');
                items += statusItem('Uptime', data.uptime || '—', '');
                if (data.error_count > 0) {
                    items += statusItem('Errors', String(data.error_count), 'error');
                    if (data.last_error) items += '<div class="status-item" style="grid-column:1/-1"><div class="label">Last error</div><div class="value" style="font-size:13px;font-weight:500;white-space:pre-wrap">' + esc(data.last_error) + '</div></div>';
                }
                document.getElementById('status').innerHTML = items;
                document.getElementById('orb').classList.toggle('ember--off', !data.ghost_running);
            } catch (e) {
                document.getElementById('status').innerHTML = '<div class="status-item"><div class="label">Checking</div><div class="value error">Unreachable</div></div>';
            }
        }
        function statusItem(label, value, cls) {
            return '<div class="status-item"><div class="label">' + label + '</div><div class="value ' + (cls || '') + '">' + esc(value) + '</div></div>';
        }

        async function loadLogs() {
            var el = document.getElementById('logs');
            try {
                var res = await fetch('/api/logs');
                var text = await res.text();
                el.textContent = text || 'No logs available.';
                el.scrollTop = el.scrollHeight;
            } catch (e) {
                el.textContent = 'Failed to load logs.';
            }
        }

        async function restartGhost() {
            var yes = await confirmModal('Restart Ghost?', 'The service will restart. This page will reload shortly.', 'Restart');
            if (!yes) return;
            try {
                await fetch('/api/restart', { method: 'POST' });
                toast('Restarting… this page will reload in a few seconds.', true);
                setTimeout(function () { location.reload(); }, 5000);
            } catch (e) {
                toast('Failed to restart.');
            }
        }

        async function resetSetup() {
            var yes = await confirmModal('Reset setup?', 'This erases all configuration and memory. It cannot be undone.', 'Reset');
            if (!yes) return;
            var sure = await confirmModal('Are you absolutely sure?', 'Every setting, channel and memory will be lost. Your Ghost returns to an unconfigured state.', 'Erase everything');
            if (!sure) return;
            try {
                var res = await fetch('/api/reset', { method: 'POST' });
                var data = await res.json();
                toast(data.message || 'Setup reset. Restart to begin setup.', true);
            } catch (e) {
                toast('Failed to reset.');
            }
        }

        async function resetPassword() {
            var pw = document.getElementById('rp-password').value;
            var confirm = document.getElementById('rp-confirm').value;
            if (!pw || !confirm) { toast('Fill in both fields.'); return; }
            if (pw !== confirm) { toast('Passwords do not match.'); return; }
            var yes = await confirmModal('Set new admin password?', 'All current sessions will end. You will log in with the new password.', 'Set password');
            if (!yes) return;
            try {
                var res = await fetch('/api/reset-password', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ password: pw, confirm: confirm })
                });
                var data = await res.json();
                if (data.ok) {
                    toast(data.message || 'Password reset.', true);
                    document.getElementById('rp-password').value = '';
                    document.getElementById('rp-confirm').value = '';
                } else {
                    toast(data.error || 'Failed to reset password.');
                }
            } catch (e) {
                toast('Failed to reset password.');
            }
        }

        document.getElementById('btn-restart').addEventListener('click', restartGhost);
        document.getElementById('btn-refresh').addEventListener('click', loadLogs);
        document.getElementById('btn-reset').addEventListener('click', resetSetup);
        document.getElementById('btn-reset-password').addEventListener('click', resetPassword);

        loadStatus();
        loadLogs();
        setInterval(loadStatus, 10000);
    </script>
</body>
</html>`
