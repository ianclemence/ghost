package native

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// chromeBinary finds a Chrome/Chromium to drive. It mirrors the upstream
// search order: an explicit path, then the names common to each platform. The
// caller may always set an explicit binary, which is what a Pod with a pinned
// Chrome-for-Testing install does.
func chromeBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("GHOST_CHROME")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	candidates := []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
		"/snap/bin/chromium",
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	for _, name := range candidates {
		if filepath.IsAbs(name) {
			if _, err := os.Stat(name); err == nil {
				return name, nil
			}
			continue
		}
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chrome/Chromium found; set GHOST_CHROME to its path")
}

// launchOptions configures a Chrome process.
type launchOptions struct {
	Binary    string
	Headless  bool
	Profile   string // persistent user-data-dir; empty gets a temp one
	Viewport  [2]int
	ExtraArgs []string
	// StartURL, when set, opens as the first tab.
	StartURL string
	Env      []string
}

// chromeProcess is a launched browser, its debugging endpoint, and enough state
// to stop it cleanly.
type chromeProcess struct {
	cmd     *exec.Cmd
	port    int
	wsURL   string // browser-level websocket
	profile string
	cleanup func()
}

// devToolsActivePort is the file Chrome writes once its debug port is live. Its
// first line is the port number and its second is the browser websocket path.
func readDevToolsPort(profile string) (int, string, error) {
	// The file appears shortly after launch; poll briefly rather than guess a
	// fixed sleep, and fail honestly if Chrome never came up.
	deadline := time.Now().Add(30 * time.Second)
	path := filepath.Join(profile, "DevToolsActivePort")
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) >= 1 {
				port, perr := strconv.Atoi(strings.TrimSpace(lines[0]))
				if perr == nil && port > 0 {
					wsPath := ""
					if len(lines) >= 2 {
						wsPath = strings.TrimSpace(lines[1])
					}
					return port, wsPath, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return 0, "", fmt.Errorf("Chrome did not open a debug port (no %s after 30s)", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// launchChrome starts a Chrome process with a remote debugging port and returns
// a handle to it. The launcher passes the same conservative, automation-friendly
// flags the upstream project uses: no first-run, mock keychain, and a
// non-automation user agent is a caller concern (Ghost sets that separately).
func launchChrome(ctx context.Context, opts launchOptions) (*chromeProcess, error) {
	bin := opts.Binary
	if bin == "" {
		var err error
		if bin, err = chromeBinary(); err != nil {
			return nil, err
		}
	}
	profile := opts.Profile
	cleanup := func() {}
	if profile == "" {
		dir, err := os.MkdirTemp("", "ghost-chrome-*")
		if err != nil {
			return nil, err
		}
		profile = dir
		cleanup = func() { _ = os.RemoveAll(dir) }
	}

	args := []string{
		"--remote-debugging-port=0",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
		"--user-data-dir=" + profile,
		"--password-store=basic",
		"--use-mock-keychain",
		"--disable-blink-features=AutomationControlled",
		"--disable-dev-shm-usage",
	}
	if opts.Headless {
		args = append(args, "--headless=new", "--hide-scrollbars")
	}
	if opts.Viewport[0] > 0 && opts.Viewport[1] > 0 {
		args = append(args, fmt.Sprintf("--window-size=%d,%d", opts.Viewport[0], opts.Viewport[1]))
	}
	// A sandboxed container (the Pod) needs --no-sandbox; a normal desktop does
	// not. Honor an explicit choice, else infer from running as root.
	if os.Getenv("GHOST_CHROME_NO_SANDBOX") == "1" || os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	if opts.StartURL != "" {
		args = append(args, opts.StartURL)
	}
	args = append(args, opts.ExtraArgs...)

	cmd := exec.CommandContext(ctx, bin, args...)
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	port, wsPath, err := readDevToolsPort(profile)
	if err != nil {
		_ = cmd.Process.Kill()
		cleanup()
		return nil, err
	}
	wsURL := ""
	if wsPath != "" {
		wsURL = fmt.Sprintf("ws://127.0.0.1:%d%s", port, wsPath)
	} else {
		wsURL = fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser", port)
	}
	return &chromeProcess{cmd: cmd, port: port, wsURL: wsURL, profile: profile, cleanup: cleanup}, nil
}

// stop terminates the browser and removes a temp profile.
func (p *chromeProcess) stop() {
	if p == nil {
		return
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	if p.cleanup != nil {
		p.cleanup()
	}
}

// targetInfo is one debuggable target from /json/list.
type targetInfo struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// httpJSON performs a short GET against the local debug endpoint (never the
// network) and decodes JSON into out.
func httpJSON(ctx context.Context, url string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

// firstPageTarget returns the first page target's websocket URL, opening one
// (about:blank) via the browser endpoint if none exists yet.
func firstPageTarget(ctx context.Context, port int) (targetInfo, error) {
	deadline := time.Now().Add(15 * time.Second)
	for {
		var list []targetInfo
		if err := httpJSON(ctx, fmt.Sprintf("http://127.0.0.1:%d/json/list", port), &list); err == nil {
			for _, t := range list {
				if t.Type == "page" && t.WebSocketDebuggerURL != "" {
					return t, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return targetInfo{}, fmt.Errorf("no page target appeared on port %d", port)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// scanLines is a small helper for reading a file line by line where needed.
func scanLines(data string) []string {
	return strings.Split(strings.TrimSpace(data), "\n")
}
