// Package localtrust is the secret that separates the owner's own programs
// from everything else that can reach the gateway over loopback.
//
// The gateway has to trust loopback for the web console, the terminal and the
// CLI. But commands the model runs in its shell share the Pod's network, so
// they reach 127.0.0.1 too. For anything that grants authority, mints a
// credential or changes policy, loopback alone is therefore not enough: the
// caller must also present this token. It lives in the config directory, which
// the command sandbox never mounts, so a model-run command cannot read it.
package localtrust

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileName is the token file inside the config directory.
const FileName = ".local-token"

// Header carries the token from a local program to the gateway.
const Header = "X-Ghost-Local-Token"

var (
	mu     sync.RWMutex
	served string // the token this process accepts; empty accepts nothing
)

// Ensure returns the token in dir, making it (0600) on first use, and makes
// this process accept it. The gateway calls this once at startup.
func Ensure(dir string) (string, error) {
	tok, err := Read(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if tok, err = create(dir); err != nil {
			return "", err
		}
	}
	Accept(tok)
	return tok, nil
}

// Accept sets the token this process will accept.
func Accept(tok string) {
	mu.Lock()
	served = strings.TrimSpace(tok)
	mu.Unlock()
}

// Read returns the token stored in dir.
func Read(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if len(tok) < 32 {
		return "", errors.New("localtrust: token file is damaged")
	}
	return tok, nil
}

func create(dir string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, FileName+"-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	// Link, not rename: if another process made one first, theirs wins and we
	// use it, so two starters never disagree.
	if err := os.Link(tmp.Name(), filepath.Join(dir, FileName)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Read(dir)
		}
		return "", err
	}
	return tok, nil
}

// Valid reports whether the request carries the token this process accepts.
func Valid(r *http.Request) bool {
	mu.RLock()
	want := served
	mu.RUnlock()
	if want == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(Header))
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Apply adds the token in dir to a request bound for the local gateway. With
// no readable token it leaves the request alone, and the gateway will say so.
func Apply(req *http.Request, dir string) {
	req.Header.Del(Header)
	if tok, err := Read(dir); err == nil {
		req.Header.Set(Header, tok)
	}
}

// Transport adds the token to every request it carries.
type Transport struct {
	Dir  string
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	req = req.Clone(req.Context())
	Apply(req, t.Dir)
	return base.RoundTrip(req)
}
