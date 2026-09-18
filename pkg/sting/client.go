package sting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultSidecarURL is the loopback-only default. The sidecar never
// listens on LAN: it is a local engine, not a network service.
const DefaultSidecarURL = "http://127.0.0.1:11436"

// Client speaks to the sting-sidecar daemon over loopback HTTP.
type Client struct {
	base   string
	http   *http.Client
	weight string
}

// New returns a sidecar client. Empty url falls back to the loopback
// default; non-positive timeout uses 15s.
func New(sidecarURL string, timeoutSecs int, weights string) *Client {
	if strings.TrimSpace(sidecarURL) == "" {
		sidecarURL = DefaultSidecarURL
	}
	if timeoutSecs <= 0 {
		timeoutSecs = 15
	}
	return &Client{
		base:   strings.TrimRight(strings.TrimSpace(sidecarURL), "/"),
		http:   &http.Client{Timeout: time.Duration(timeoutSecs) * time.Second},
		weight: weights,
	}
}

// Available probes GET /health. It is the only pre-flight check: no
// background prober, matching Ghost's reactive offline posture. Any
// error means "sidecar down" and the caller must escalate, never fail
// the turn.
func (c *Client) Available(ctx context.Context) bool {
	if c == nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Complete sends one routing turn. tools must already be bounded (see
// export.go): the sidecar forwards them verbatim to the engine.
func (c *Client) Complete(ctx context.Context, system, query string, tools []ToolSchema) (*CompleteResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("sting: nil client")
	}
	if len(tools) == 0 {
		return nil, fmt.Errorf("sting: no tools for routing turn")
	}
	body, err := json.Marshal(CompleteRequest{
		System:  system,
		Query:   query,
		Tools:   tools,
		Weights: c.weight,
	})
	if err != nil {
		return nil, fmt.Errorf("sting: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/complete", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("sting: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sting: sidecar unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sting: sidecar status %d", resp.StatusCode)
	}
	var out CompleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("sting: decode response: %w", err)
	}
	return &out, nil
}
