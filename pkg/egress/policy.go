// Package egress decides which hosts code Ghost runs on the owner's behalf may
// reach, and enforces it.
//
// An allowlist of domains is a capability grant, not just a filter: anything
// reachable through an allowed host is an exfiltration path. So the decision is
// made where it cannot be talked around: the sandbox has no network of its own
// and its only way out is a proxy Ghost runs, which resolves the name itself,
// judges the address it actually dials, and writes down what it let through.
//
// Modes (GHOST_EXEC_EGRESS):
//
//	open       the sandbox shares the Pod's network (the default; nothing changes)
//	guard      the public internet only: loopback, the LAN, link-local and cloud
//	           metadata addresses are refused, so executed code cannot reach the
//	           Pod's own services, the router or other devices. Hosts listed in
//	           the allow file are exempt, which is how one LAN device is opened.
//	allowlist  only hosts named in the allow file
package egress

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Mode string

const (
	Open      Mode = "open"
	Guard     Mode = "guard"
	Allowlist Mode = "allowlist"
)

// Policy is one resolved egress decision procedure.
type Policy struct {
	Mode  Mode
	Allow []string // exact hosts, "*.example.com", or IP literals
}

// FromEnv reads GHOST_EXEC_EGRESS and the allow file (GHOST_EXEC_EGRESS_ALLOW,
// default $GHOST_DIR/egress-allow.txt). The file lives beside the config, never
// in the workspace: code the agent runs can write the workspace, and a policy
// it can edit is not a policy.
func FromEnv() Policy {
	p := Policy{Mode: Open}
	switch Mode(strings.ToLower(strings.TrimSpace(os.Getenv("GHOST_EXEC_EGRESS")))) {
	case Guard:
		p.Mode = Guard
	case Allowlist:
		p.Mode = Allowlist
	}
	path := strings.TrimSpace(os.Getenv("GHOST_EXEC_EGRESS_ALLOW"))
	if path == "" {
		if d := strings.TrimSpace(os.Getenv("GHOST_DIR")); d != "" {
			path = filepath.Join(d, "egress-allow.txt")
		}
	}
	if path != "" {
		p.Allow = readAllow(path)
	}
	return p
}

func readAllow(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			out = append(out, strings.ToLower(line))
		}
	}
	return out
}

// Enforced reports whether the sandbox must be cut off from the Pod's network.
func (p Policy) Enforced() bool { return p.Mode == Guard || p.Mode == Allowlist }

// Listed reports whether host is named in the allow file.
func (p Policy) Listed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, a := range p.Allow {
		if a == host {
			return true
		}
		if strings.HasPrefix(a, "*.") && strings.HasSuffix(host, a[1:]) {
			return true
		}
	}
	return false
}

// Check judges a destination before any connection is made. An empty reason
// means allowed. Names are still judged again by address in AddrOK, because a
// public name can point at a private address.
func (p Policy) Check(host string) string {
	switch p.Mode {
	case Allowlist:
		if !p.Listed(host) {
			return host + " is not on the egress allowlist"
		}
	case Guard:
		if strings.EqualFold(host, "localhost") && !p.Listed(host) {
			return "localhost is not reachable from sandboxed code"
		}
	}
	return ""
}

// AddrOK judges the address actually about to be dialled for host.
func (p Policy) AddrOK(host string, ip net.IP) bool {
	if p.Listed(host) || p.Listed(ip.String()) {
		return true
	}
	return !Private(ip)
}

// Private reports addresses sandboxed code must not reach in guard and
// allowlist modes: this machine, the local network, link-local (including the
// cloud metadata address), carrier-grade NAT and multicast.
func Private(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xC0 == 64 { // 100.64.0.0/10
		return true
	}
	return false
}
