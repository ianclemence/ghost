// Package httpx is the shared HTTP primitive for provider adapters:
// GET JSON with context, status→failure classification, and a 1MB body
// cap. Individual capabilities own parsing, validation, and strategy.
package httpx

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ianclemence/ghost/pkg/provider"
)

// GetJSON performs a GET and returns the body with per-attempt metadata.
// Non-2xx statuses become classified errors (never successes).
func GetJSON(client *http.Client, ctx context.Context, url string, headers map[string]string) ([]byte, *provider.CallMeta, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Ghost/1.0 (personal AI appliance)")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	meta := &provider.CallMeta{StatusCode: resp.StatusCode}
	if resp.StatusCode == 429 {
		meta.Failure = provider.FailRateLimited
		return nil, meta, fmt.Errorf("http 429")
	}
	if cl := provider.ClassifyHTTP(resp.StatusCode); cl != "" {
		meta.Failure = cl
		return nil, meta, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, meta, err
	}
	return body, meta, nil
}

// NonEmpty rejects blank bodies as provider failures.
func NonEmpty(body []byte, who string) error {
	if len(body) == 0 {
		return provider.Empty(who + ": empty response")
	}
	for _, b := range body {
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			return nil
		}
	}
	return provider.Empty(who + ": empty response")
}
