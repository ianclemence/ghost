package egress

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateAddresses(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.5", "172.16.0.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "0.0.0.0", "224.0.0.1"} {
		if !Private(net.ParseIP(s)) {
			t.Errorf("%s should be private", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"} {
		if Private(net.ParseIP(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}

func TestAllowFileOpensOneLANHostInGuardMode(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "allow.txt")
	os.WriteFile(f, []byte("# my devices\n192.168.1.50\n*.home.example # nas\n"), 0o600)
	t.Setenv("GHOST_EXEC_EGRESS", "guard")
	t.Setenv("GHOST_EXEC_EGRESS_ALLOW", f)
	p := FromEnv()
	if !p.Enforced() || p.Mode != Guard {
		t.Fatalf("policy %+v", p)
	}
	if !p.AddrOK("192.168.1.50", net.ParseIP("192.168.1.50")) {
		t.Error("a listed LAN host should be reachable")
	}
	if p.AddrOK("192.168.1.51", net.ParseIP("192.168.1.51")) {
		t.Error("an unlisted LAN host must not be")
	}
	if !p.Listed("nas.home.example") || p.Listed("home.example.evil.com") {
		t.Error("wildcard matching is wrong")
	}
	if p.Check("localhost") == "" {
		t.Error("localhost must be refused in guard mode")
	}
}

func TestAllowlistModeRefusesUnlistedNames(t *testing.T) {
	p := Policy{Mode: Allowlist, Allow: []string{"api.example.com"}}
	if p.Check("api.example.com") != "" {
		t.Error("listed host refused")
	}
	if p.Check("evil.com") == "" {
		t.Error("unlisted host allowed")
	}
}

func TestDefaultIsOpen(t *testing.T) {
	t.Setenv("GHOST_EXEC_EGRESS", "")
	if FromEnv().Enforced() {
		t.Fatal("egress must be off unless the owner turns it on")
	}
}

// connect speaks CONNECT through the proxy over a pipe and returns the status line.
func connect(t *testing.T, p *Proxy, target string) (string, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	go p.handle(b)
	io.WriteString(a, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	line, _ := bufio.NewReader(a).ReadString('\n')
	return strings.TrimSpace(line), a
}

func TestProxyBlocksLoopbackAndAllowsListedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }))
	defer srv.Close()
	addr := srv.Listener.Addr().String() // 127.0.0.1:port

	status, c := connect(t, NewProxy(Policy{Mode: Guard}), addr)
	c.Close()
	if !strings.Contains(status, "403") {
		t.Fatalf("loopback must be blocked in guard mode, got %q", status)
	}

	host, _, _ := net.SplitHostPort(addr)
	status, c = connect(t, NewProxy(Policy{Mode: Guard, Allow: []string{host}}), addr)
	c.Close()
	if !strings.Contains(status, "200") {
		t.Fatalf("a listed host should connect, got %q", status)
	}
}

func TestProxyForwardsPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }))
	defer srv.Close()
	host, _, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p := NewProxy(Policy{Mode: Guard, Allow: []string{host}})
	a, b := net.Pipe()
	go p.handle(b)
	io.WriteString(a, "GET "+srv.URL+"/ HTTP/1.1\r\nHost: "+srv.Listener.Addr().String()+"\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(a), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body %q", body)
	}
}
