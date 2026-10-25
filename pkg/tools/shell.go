package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"time"
)

type ExecTool struct {
	workingDir          string
	timeout             time.Duration
	denyPatterns        []*regexp.Regexp
	allowPatterns       []*regexp.Regexp
	restrictToWorkspace bool
}

// maxExecTimeout is the longest a single command may be given. A build or test
// run on a small machine takes minutes, not seconds; beyond half an hour a
// command is a job, not a tool call.
const maxExecTimeout = 30 * time.Minute

// execGrace is how long past a command's own deadline the reliability wrapper
// waits, so the tool reports its timeout itself.
const execGrace = 10 * time.Second

// callTimeout is the time a call to exec may run: timeout_seconds when the
// caller gave a sensible one, otherwise the default.
func (t *ExecTool) callTimeout(args map[string]interface{}) time.Duration {
	var secs float64
	switch v := args["timeout_seconds"].(type) {
	case float64:
		secs = v
	case int:
		secs = float64(v)
	case int64:
		secs = float64(v)
	}
	if secs < 1 {
		return t.timeout
	}
	if d := time.Duration(secs * float64(time.Second)); d < maxExecTimeout {
		return d
	}
	return maxExecTimeout
}

// TimeoutFor implements CallTimeoutTool.
func (t *ExecTool) TimeoutFor(args map[string]interface{}) time.Duration {
	return t.callTimeout(args) + execGrace
}

func NewExecTool(workingDir string, restrict bool) *ExecTool {
	denyPatterns := []*regexp.Regexp{}

	return &ExecTool{
		workingDir:          workingDir,
		timeout:             60 * time.Second,
		denyPatterns:        denyPatterns,
		allowPatterns:       nil,
		restrictToWorkspace: restrict,
	}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	return "Run a shell command and return its output. Use when: a skill or task requires an exact command (e.g. a CLI tool), or you need real local/OS data. Do NOT use when: a Ghost skill already does the job, or to double-check a web fact you could look up. Reading a web page is web_fetch's job, not curl's — never reach for this to read a page. " +
		"A command gets 60 seconds unless you pass timeout_seconds (up to 1800); for a build, an install or a test run, pass it up front. Everything the command started stops when it returns, so never background work with & or nohup: run it in the foreground with enough time. " +
		"Commands run under sh, not bash: no [[ ]], arrays or ${PIPESTATUS}; use bash -c '...' if you need them. " +
		"Git works here and your commits are made as Ghost; there are no credentials, so pushing and private repositories need the owner. " +
		"Returns combined stdout/stderr and the exit code."
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The exact shell command to run. Example: \"curl -s https://api.example.com/x\" — run a command a skill gives you verbatim.",
			},
			"timeout_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "How long the command may run, in seconds. Default 60, maximum 1800. Use a larger value for builds, dependency installs and test suites.",
				"minimum":     1,
				"maximum":     int(maxExecTimeout / time.Second),
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory. To build or keep a project (clone a repository, run its tests), use \"projects/<name>\": the folder is created if needed and is the one place a command can write besides tmp/. Elsewhere in the workspace is read-only.",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	command, ok := args["command"].(string)
	if !ok {
		return ErrorResult("command is required")
	}

	cwd := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		cwd = wd
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}
	// One absolute path for every check below and for the process itself;
	// a relative one was judged against the workspace but run elsewhere.
	cwd = resolveWorkingDir(t.workingDir, cwd)

	if guardError := t.guardCommand(command, cwd); guardError != "" {
		return ErrorResult(guardError)
	}
	// A project's folder must exist before the sandbox can bind it writable.
	if err := ensureProjectDir(t.workingDir, cwd); err != nil {
		return ErrorResult(err.Error())
	}

	timeout := t.callTimeout(args)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var argv []string
	if runtime.GOOS == "windows" {
		argv = []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		argv = []string{"sh", "-c", command}
	}
	// OS-level isolation when available (bubblewrap on Linux): read-only
	// root, private /tmp, namespaces, and no access to the config dir. The
	// command legitimately needs network (skills use curl), so network is
	// shared; filesystem and secret boundaries are the isolation here.
	if wrapped, ok, werr := WrapArgv(argv, cwd, t.workingDir, true); werr != nil {
		return ErrorResult(werr.Error())
	} else if ok {
		argv = wrapped
	}
	cmd := exec.CommandContext(cmdCtx, argv[0], argv[1:]...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	// Never let a model-initiated command inherit Ghost's credentials:
	// start from a minimal allowlist and confine HOME to the workspace.
	ensureGitIdentity(t.workingDir)
	HardenCommand(cmd, t.workingDir, gitSandboxEnv())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Command timed out after %v and was stopped. If it was working (a build, an install, a test run), run it again in the foreground with a larger timeout_seconds (up to %d); nothing it started keeps running after exec returns.", timeout, int(maxExecTimeout/time.Second))
			return &ToolResult{
				ForLLM:  msg,
				ForUser: msg,
				IsError: true,
			}
		}
		output += fmt.Sprintf("\nExit code: %v", err)
	}

	if output == "" {
		output = "(no output)"
	}

	maxLen := 10000
	if len(output) > maxLen {
		output = output[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(output)-maxLen)
	}

	if err != nil {
		return &ToolResult{
			ForLLM:  output,
			ForUser: output,
			IsError: true,
		}
	}

	return &ToolResult{
		ForLLM:  output,
		ForUser: output,
		IsError: false,
	}
}

func (t *ExecTool) guardCommand(command, cwd string) string {
	// In "Operator Mode", we trust the agent completely.
	// Memory-estate protection is a second layer behind the file-tool
	// guard: shell must not become a raw bypass of the scoped memory
	// store, even when the model chooses exec instead of read_file.
	if msg, denied := execDeniedByPrivacy(command); denied {
		return msg
	}
	// Primary-file protection is the same second layer for integrity:
	// no command may name the database/config at all or take a
	// destructive shape anywhere near Ghost's primary estate.
	if msg, denied := execDeniedPrimaryFiles(command, cwd, t.workingDir); denied {
		return msg
	}
	// The working directory honors the same roots as the file tools, so
	// exec cannot mount an arbitrary directory writable into the sandbox.
	if t.restrictToWorkspace && t.workingDir != "" {
		if _, err := validatePath(cwd, t.workingDir, true); err != nil {
			return fmt.Sprintf("working directory outside the allowed roots: %s", err.Error())
		}
		if err := confineWorkingDir(t.workingDir, cwd); err != nil {
			return err.Error()
		}
	}
	// Ghost's runtime home beside the workspace (backups, install
	// snapshots) is never a working area: the sandbox does not mount it,
	// and naming it as a working directory is refused outright.
	if ghostRuntimeHomePath(t.workingDir, cwd) {
		return "that working directory belongs to Ghost's own runtime; choose a project directory instead"
	}
	return ""
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}

func (t *ExecTool) SetAllowPatterns(patterns []string) error {
	t.allowPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allow pattern %q: %w", p, err)
		}
		t.allowPatterns = append(t.allowPatterns, re)
	}
	return nil
}
