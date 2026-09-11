package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// execPolicy is the argv-based allowlist for the remote bridge /v1/exec
// endpoint. There is NO shell: commands are parsed into argv and executed
// directly, so metacharacters cannot chain, pipe, redirect, substitute, or
// glob. Every executable must be explicitly allowed, and sensitive
// executables additionally validate their arguments. Fail closed.
type execPolicy struct {
	// allowed maps an executable base name to an argument validator.
	allowed map[string]func(args []string) error
}

// shellMetaChars are refused outright so a caller never gets shell-like
// semantics by accident, even though no shell is involved. This is
// defense-in-depth and keeps argument intent unambiguous.
const shellMetaChars = ";|&<>$`(){}[]*?!~\n\r\\\"'"

func newExecPolicy(extraExecutables []string) *execPolicy {
	p := &execPolicy{allowed: map[string]func(args []string) error{}}

	// Read-only system introspection. No arguments (or only benign ones).
	noArgs := func(args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("takes no arguments")
		}
		return nil
	}
	for _, exe := range []string{"df", "free", "uptime", "hostname", "date"} {
		p.allowed[exe] = noArgs
	}
	p.allowed["systemctl"] = func(args []string) error {
		if len(args) != 2 || args[0] != "status" {
			return fmt.Errorf("only 'status <unit>' is allowed")
		}
		return nil
	}
	p.allowed["journalctl"] = func(args []string) error {
		if len(args) != 2 || args[0] != "-u" || args[1] != "ghost" {
			return fmt.Errorf("only '-u ghost' is allowed")
		}
		return nil
	}
	p.allowed["cat"] = func(args []string) error {
		if len(args) != 1 || !strings.HasPrefix(args[0], "/proc/") {
			return fmt.Errorf("only files under /proc/ may be read")
		}
		return nil
	}
	p.allowed["ls"] = func(args []string) error {
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				continue // flags are harmless for a read-only listing
			}
			if strings.Contains(a, "..") {
				return fmt.Errorf("path traversal not allowed")
			}
		}
		return nil
	}
	p.allowed["ping"] = func(args []string) error {
		// only: ping -c <n> <host>
		if len(args) != 3 || args[0] != "-c" {
			return fmt.Errorf("only 'ping -c <count> <host>' is allowed")
		}
		if _, err := strconv.Atoi(args[1]); err != nil {
			return fmt.Errorf("ping count must be numeric")
		}
		return nil
	}
	p.allowed["xdg-open"] = func(args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("xdg-open takes exactly one target")
		}
		return nil
	}

	// Operator-configured extra executables are added as names only, with
	// a conservative default validator: no metacharacters, no traversal.
	for _, raw := range extraExecutables {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		// Backwards compatibility: legacy ALLOWED_CMDS entries were
		// prefixes like "python3,". Reduce them to an executable name.
		name = strings.Fields(name)[0]
		name = path.Base(name)
		if _, exists := p.allowed[name]; exists {
			continue
		}
		p.allowed[name] = func(args []string) error {
			for _, a := range args {
				if strings.Contains(a, "..") {
					return fmt.Errorf("path traversal not allowed")
				}
			}
			return nil
		}
	}
	return p
}

// parseExecCommand converts a command line into argv with strict rules.
// It refuses shell metacharacters outright, then splits on whitespace.
func parseExecCommand(command string) ([]string, error) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return nil, fmt.Errorf("empty command")
	}
	if strings.ContainsAny(cmd, shellMetaChars) {
		return nil, fmt.Errorf("command contains disallowed shell metacharacters")
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return fields, nil
}

// resolve validates a command against the policy and returns the argv to
// execute (without a shell).
func (p *execPolicy) resolve(command string) ([]string, error) {
	argv, err := parseExecCommand(command)
	if err != nil {
		return nil, err
	}
	exe := path.Base(argv[0])
	validate, ok := p.allowed[exe]
	if !ok {
		return nil, fmt.Errorf("executable %q is not allowed", exe)
	}
	if err := validate(argv[1:]); err != nil {
		return nil, fmt.Errorf("%s: %w", exe, err)
	}
	return argv, nil
}
