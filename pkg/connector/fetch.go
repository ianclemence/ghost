package connector

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxManifestBytes bounds a fetched connector so a hostile host cannot exhaust
// memory. A connector manifest is small; 1 MiB is generous.
const maxManifestBytes = 1 << 20

// FetchOptions controls remote connector install.
type FetchOptions struct {
	// RequireSignature makes a valid signature from TrustedKeys mandatory. A
	// directory that pins keys installs only what those keys vouch for.
	RequireSignature bool
	// TrustedKeys are the ed25519 public keys a signature must verify against.
	TrustedKeys []ed25519.PublicKey
	// Client overrides the HTTP client (tests, custom timeouts).
	Client *http.Client
}

// Fetch downloads, validates, and (when required) signature-checks a connector
// manifest from an http(s) URL. The body is bounded and the request times out;
// a fetch can never hang or exhaust memory.
func Fetch(ctx context.Context, rawURL string, opts FetchOptions) (*Manifest, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid connector url")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("connector url must be http(s)")
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connector fetch returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read connector: %w", err)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("connector manifest exceeds %d bytes", maxManifestBytes)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse connector: %w", err)
	}
	if verrs := m.Validate(); len(verrs) > 0 {
		return nil, fmt.Errorf("connector is invalid:\n%s", FormatErrors(verrs))
	}
	if opts.RequireSignature {
		if len(opts.TrustedKeys) == 0 {
			return nil, fmt.Errorf("signature required but no trusted keys are configured")
		}
		trusted := false
		var lastErr error
		for _, k := range opts.TrustedKeys {
			if err := VerifySignature(&m, k); err == nil {
				trusted = true
				break
			} else {
				lastErr = err
			}
		}
		if !trusted {
			return nil, fmt.Errorf("connector signature is not trusted: %v", lastErr)
		}
	}
	return &m, nil
}

// FetchAndInstall fetches a connector from a URL and installs it.
func FetchAndInstall(ctx context.Context, rawURL, dir string, force bool, opts FetchOptions) (string, error) {
	m, err := Fetch(ctx, rawURL, opts)
	if err != nil {
		return "", err
	}
	return InstallManifest(m, dir, force)
}
