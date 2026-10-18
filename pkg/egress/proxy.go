package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// Proxy is the sandbox's only way out. It speaks plain HTTP proxying and
// CONNECT, which is what curl, pip, npm, go and git over https use.
type Proxy struct {
	policy Policy
}

// Serve accepts connections until ln is closed.
func (p *Proxy) Serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *Proxy) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	host, port := splitHostPort(req)
	if reason := p.policy.Check(host); reason != "" {
		p.deny(c, host, reason)
		return
	}
	upstream, err := p.dial(host, port)
	if err != nil {
		p.deny(c, host, err.Error())
		return
	}
	defer upstream.Close()
	logger.InfoCF("egress", "allowed", map[string]interface{}{"host": host, "port": port, "mode": string(p.policy.Mode)})

	if req.Method == http.MethodConnect {
		fmt.Fprint(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
	} else {
		// Plain HTTP: forward this request, then close; the client reconnects.
		req.RequestURI = ""
		req.Header.Del("Proxy-Connection")
		req.Header.Set("Connection", "close")
		if err := req.Write(upstream); err != nil {
			return
		}
	}
	pipe(c, br, upstream)
}

func (p *Proxy) deny(c net.Conn, host, reason string) {
	logger.WarnCF("egress", "blocked", map[string]interface{}{"host": host, "reason": reason, "mode": string(p.policy.Mode)})
	body := "Ghost blocked this request: " + reason + ".\n"
	fmt.Fprintf(c, "HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
}

// dial resolves host itself and dials the address it judged, so a name cannot
// be resolved to one thing for the check and another for the connection.
func (p *Proxy) dial(host, port string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("could not resolve %s", host)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	var d net.Dialer
	refused := false
	for _, ip := range ips {
		if !p.policy.AddrOK(host, ip) {
			refused = true
			continue
		}
		if c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port)); err == nil {
			return c, nil
		}
	}
	if refused {
		return nil, errors.New(host + " is on a private or local address")
	}
	return nil, fmt.Errorf("could not connect to %s", host)
}

func splitHostPort(req *http.Request) (string, string) {
	target := req.Host
	if req.Method == http.MethodConnect {
		target = req.RequestURI
		if target == "" {
			target = req.URL.Host
		}
	} else if req.URL != nil && req.URL.Host != "" {
		target = req.URL.Host
	}
	h, p, err := net.SplitHostPort(target)
	if err != nil {
		h = strings.Trim(target, "[]")
		p = "80"
	}
	return strings.ToLower(strings.TrimSuffix(h, ".")), p
}

func pipe(client net.Conn, clientBuf *bufio.Reader, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, clientBuf); closeWrite(upstream); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); closeWrite(client); done <- struct{}{} }()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

var (
	startOnce sync.Once
	startPath string
	startErr  error
)

// SocketPath starts the process-wide proxy on first use and returns the unix
// socket the sandbox is given. The directory is private to this user.
func SocketPath(p Policy) (string, error) {
	startOnce.Do(func() {
		dir := filepath.Join(os.TempDir(), fmt.Sprintf("ghost-egress-%d", os.Getuid()))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			startErr = err
			return
		}
		sock := filepath.Join(dir, "proxy.sock")
		_ = os.Remove(sock)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			startErr = err
			return
		}
		_ = os.Chmod(sock, 0o600)
		startPath = sock
		go (&Proxy{policy: p}).Serve(ln)
	})
	return startPath, startErr
}

// NewProxy builds a proxy for a policy (used by tests).
func NewProxy(p Policy) *Proxy { return &Proxy{policy: p} }
